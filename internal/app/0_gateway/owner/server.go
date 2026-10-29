package owner

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// Server is the owner's side. It runs each command a client sends, several
// at once, until it is stopped, until a client of another build arrives,
// or until it has had no command for Idle; then it takes no more and ends
// once the running ones have.
type Server struct {
	// Identity is the build that made this owner.
	Identity string
	Run      Run
	// Idle is how long the owner waits without a command before it ends.
	Idle time.Duration
	// Log receives what the owner says about itself, such as a request it
	// could not read.
	Log io.Writer

	mutex    sync.Mutex
	listener net.Listener
	running  int
	stopping bool
	// idleRound tells the latest idle timer from the ones a command made
	// stale.
	idleRound int
	ended     chan struct{}
}

// identityAnswer is the owner's answer to who it is.
type identityAnswer struct {
	Identity string `json:"identity"`
}

// Serve takes commands on listener until the owner ends.
func (server *Server) Serve(listener net.Listener) error {
	server.mutex.Lock()
	server.prepare()
	server.listener = listener
	stopping := server.stopping
	if !stopping {
		server.waitIdle()
	}
	server.mutex.Unlock()
	if stopping {
		_ = listener.Close()
	}
	routes := http.NewServeMux()
	routes.HandleFunc("GET /v1/identity", server.identify)
	routes.HandleFunc("POST /v1/sessions", server.session)
	web := &http.Server{Handler: routes, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		err := web.Serve(listener)
		if err != nil && err != http.ErrServerClosed && !server.isStopping() {
			fmt.Fprintf(server.Log, "rein owner: the socket stopped taking commands: %v\n", err)
		}
		server.Stop()
	}()
	<-server.ended
	return web.Close()
}

// Stop takes no more commands; the owner ends once the running ones have.
func (server *Server) Stop() {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.prepare()
	if !server.stopping {
		server.stopping = true
		if server.listener != nil {
			_ = server.listener.Close()
		}
	}
	server.endIfDone()
}

// prepare makes what Serve and Stop share, whichever comes first; the
// mutex is held.
func (server *Server) prepare() {
	if server.ended == nil {
		server.ended = make(chan struct{})
	}
	if server.Log == nil {
		server.Log = io.Discard
	}
}

func (server *Server) isStopping() bool {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.stopping
}

// endIfDone ends the owner once it is stopping and runs nothing; the mutex
// is held.
func (server *Server) endIfDone() {
	if !server.stopping || server.running > 0 {
		return
	}
	select {
	case <-server.ended:
	default:
		close(server.ended)
	}
}

// waitIdle starts the wait after which an owner without commands stops;
// a command that starts makes it stale. The mutex is held.
func (server *Server) waitIdle() {
	if server.Idle <= 0 {
		return
	}
	server.idleRound++
	round := server.idleRound
	time.AfterFunc(server.Idle, func() {
		server.mutex.Lock()
		idle := round == server.idleRound && server.running == 0
		server.mutex.Unlock()
		if idle {
			server.Stop()
		}
	})
}

// begin counts a command in, unless the owner is stopping.
func (server *Server) begin() bool {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	if server.stopping {
		return false
	}
	server.running++
	server.idleRound++
	return true
}

// finish counts a command out.
func (server *Server) finish() {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.running--
	if server.running > 0 {
		return
	}
	if server.stopping {
		server.endIfDone()
		return
	}
	server.waitIdle()
}

func (server *Server) identify(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(identityAnswer{Identity: server.Identity})
}

// session runs one command. A client of another build is answered 409:
// this owner makes way for it, taking no more commands and ending once its
// running ones have, so the client starts an owner of its own build.
func (server *Server) session(writer http.ResponseWriter, request *http.Request) {
	var sent Request
	err := json.NewDecoder(io.LimitReader(request.Body, 16<<20)).Decode(&sent)
	if err != nil {
		http.Error(writer, "the command could not be read: "+err.Error(), http.StatusBadRequest)
		return
	}
	if sent.Identity != server.Identity {
		server.Stop()
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(writer).Encode(identityAnswer{Identity: server.Identity})
		return
	}
	if !server.begin() {
		http.Error(writer, "the background process is ending", http.StatusServiceUnavailable)
		return
	}
	connection, buffered, err := http.NewResponseController(writer).Hijack()
	if err != nil {
		server.finish()
		fmt.Fprintf(server.Log, "rein owner: the connection could not be taken over: %v\n", err)
		return
	}
	_, err = buffered.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: " + protocol + "\r\n\r\n")
	if err == nil {
		err = buffered.Flush()
	}
	if err != nil {
		_ = connection.Close()
		server.finish()
		return
	}
	server.runSession(sent, connection, buffered.Reader)
}

// runSession runs the command with the caller's streams on connection and
// counts it out once it has finished here.
func (server *Server) runSession(sent Request, connection net.Conn, reader *bufio.Reader) {
	defer server.finish()
	output := &frames{connection: connection}
	input, typing := io.Pipe()
	chunks := make(chan []byte, 256)
	interrupts := make(chan struct{}, 8)
	gone := make(chan struct{})
	go feed(typing, chunks)
	go func() {
		defer close(gone)
		inputOpen := true
		endInput := func() {
			if inputOpen {
				inputOpen = false
				close(chunks)
			}
		}
		for {
			kind, payload, err := readFrame(reader)
			if err != nil {
				// the caller is gone: its input ends, and it counts as Ctrl-C
				endInput()
				close(interrupts)
				return
			}
			switch kind {
			case frameInput:
				if inputOpen {
					chunks <- payload
				}
			case frameInputEnd:
				endInput()
			case frameInterrupt:
				select {
				case interrupts <- struct{}{}:
				default:
				}
			}
		}
	}()
	code, finished := server.Run(Session{Request: sent, In: input, Out: stream{frames: output, kind: frameOutput}, ErrOut: stream{frames: output, kind: frameError}, Interrupts: interrupts})
	_ = output.write(frameExit, exitPayload(code))
	// the caller closes once it has the exit code; wait for that, so no
	// unread input of its makes this side's close lose the code on the way
	if unix, ok := connection.(interface{ CloseWrite() error }); ok {
		_ = unix.CloseWrite()
	}
	select {
	case <-gone:
	case <-time.After(5 * time.Second):
	}
	_ = connection.Close()
	<-finished
	_ = input.Close()
}

// feed hands the caller's input to the command in the order it came, so
// a command that reads nothing never holds up the caller's Ctrl-C.
func feed(typing *io.PipeWriter, chunks <-chan []byte) {
	for chunk := range chunks {
		_, err := typing.Write(chunk)
		if err != nil {
			for range chunks {
			}
			return
		}
	}
	_ = typing.Close()
}
