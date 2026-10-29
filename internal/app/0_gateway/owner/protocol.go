// Package owner is the gateway of the background process (D55, D57). The
// owner's side takes each command a client sends over a Unix socket in
// rein's home and runs it, several at once; the client's side relays a
// command and its caller's streams to it, the way ssh shows a program that
// runs elsewhere. The command travels as JSON in an HTTP request that the
// owner answers with 101 Switching Protocols; the connection then carries
// the caller's input and Ctrl-C, and the command's output and exit code,
// as frames. It knows no command: main hands the owner a Run.
package owner

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

// protocol names the stream after the upgrade.
const protocol = "rein-session/1"

// Request is a command as its caller typed it and where: the command line,
// the caller's folder and environment, whether a person types at a
// terminal, the caller's binary, and the build that made it, which must be
// the owner's.
type Request struct {
	Identity    string   `json:"identity"`
	Arguments   []string `json:"arguments"`
	Folder      string   `json:"folder"`
	Environment []string `json:"environment"`
	Terminal    bool     `json:"terminal"`
	Executable  string   `json:"executable"`
}

// Session is one command the owner runs: its request, and its caller's
// streams and Ctrl-C. Interrupts is closed when the caller is gone.
type Session struct {
	Request
	In         io.Reader
	Out        io.Writer
	ErrOut     io.Writer
	Interrupts <-chan struct{}
}

// Run runs a session's command and returns the exit code for its caller;
// finished closes once the command has ended here, which may be after the
// caller was let go.
type Run func(session Session) (code int, finished <-chan struct{})

// The kinds of frame: input, the end of it and Ctrl-C from the caller;
// output, errors and the exit code from the command.
const (
	frameInput     byte = 'i'
	frameInputEnd  byte = 'e'
	frameInterrupt byte = 'c'
	frameOutput    byte = 'o'
	frameError     byte = 'r'
	frameExit      byte = 'x'
)

// maximumFrame is the most a frame carries; a longer write goes as several.
const maximumFrame = 64 << 10

// frames writes frames to one connection, from several goroutines.
type frames struct {
	mutex      sync.Mutex
	connection io.Writer
}

// write sends one frame: its kind, the length of its payload, the payload.
func (frames *frames) write(kind byte, payload []byte) error {
	frame := make([]byte, 5+len(payload))
	frame[0] = kind
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)
	frames.mutex.Lock()
	defer frames.mutex.Unlock()
	_, err := frames.connection.Write(frame)
	return err
}

// stream is a writer whose bytes go out as frames of one kind.
type stream struct {
	frames *frames
	kind   byte
}

func (stream stream) Write(written []byte) (int, error) {
	for start := 0; start < len(written); start += maximumFrame {
		end := min(start+maximumFrame, len(written))
		err := stream.frames.write(stream.kind, written[start:end])
		if err != nil {
			return start, err
		}
	}
	return len(written), nil
}

// readFrame reads the next frame.
func readFrame(reader io.Reader) (kind byte, payload []byte, err error) {
	var header [5]byte
	_, err = io.ReadFull(reader, header[:])
	if err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(header[1:])
	if length > maximumFrame {
		return 0, nil, fmt.Errorf("a frame of %d bytes is longer than %d", length, maximumFrame)
	}
	payload = make([]byte, length)
	_, err = io.ReadFull(reader, payload)
	return header[0], payload, err
}

// exitPayload is an exit code as a frame carries it.
func exitPayload(code int) []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, uint32(int32(code)))
	return payload
}

// exitCodeOf reads the exit code a frame carries.
func exitCodeOf(payload []byte) (int, error) {
	if len(payload) != 4 {
		return 0, fmt.Errorf("an exit frame of %d bytes", len(payload))
	}
	return int(int32(binary.BigEndian.Uint32(payload))), nil
}
