package owner

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// serve starts an owner that runs commands with run, on a socket of its
// own, and returns the socket, the server and a channel closed when it has
// ended; the test's end stops it.
func serve(t *testing.T, identity string, idle time.Duration, run Run) (socket string, server *Server, ended <-chan struct{}) {
	t.Helper()
	socket = SocketPath(t.TempDir())
	listener, err := Listen(socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server = &Server{Identity: identity, Run: run, Idle: idle, Log: io.Discard}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		<-done
	})
	return socket, server, done
}

// runs is a Run whose command is finished once it returns.
func runs(command func(session Session) int) Run {
	return func(session Session) (int, <-chan struct{}) {
		code := command(session)
		finished := make(chan struct{})
		close(finished)
		return code, finished
	}
}

// relay sends arguments as the caller with identity and returns what came
// back.
func relay(t *testing.T, socket string, identity string, in io.Reader, interrupts <-chan struct{}, arguments ...string) (code int, out string, errOut string, err error) {
	t.Helper()
	var outBuffer, errBuffer strings.Builder
	code, err = Relay(socket, Request{Identity: identity, Arguments: arguments}, in, &outBuffer, &errBuffer, interrupts)
	return code, outBuffer.String(), errBuffer.String(), err
}

// waitFor waits until condition holds, or fails after a few seconds.
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func isClosed(channel <-chan struct{}) bool {
	select {
	case <-channel:
		return true
	default:
		return false
	}
}

func TestRelay(t *testing.T) {
	t.Run("the command's output, its errors and its exit code come back to the caller", func(t *testing.T) {
		socket, _, _ := serve(t, "build", time.Minute, runs(func(session Session) int {
			_, _ = io.WriteString(session.Out, "synced shop\n")
			_, _ = io.WriteString(session.ErrOut, "warning: one file was edited by hand\n")
			return 3
		}))
		code, out, errOut, err := relay(t, socket, "build", strings.NewReader(""), nil, "sync")
		if err != nil || code != 3 || out != "synced shop\n" || errOut != "warning: one file was edited by hand\n" {
			t.Fatalf("code %d out %q err %q relay %v", code, out, errOut, err)
		}
	})
	t.Run("what the caller types reaches the command, and the end of it too", func(t *testing.T) {
		socket, _, _ := serve(t, "build", time.Minute, runs(func(session Session) int {
			typed, _ := io.ReadAll(session.In)
			_, _ = session.Out.Write(typed)
			return 0
		}))
		_, out, _, err := relay(t, socket, "build", strings.NewReader("y\nand more"), nil, "reset", "project")
		if err != nil || out != "y\nand more" {
			t.Fatalf("out %q relay %v", out, err)
		}
	})
	t.Run("the owner receives the command line, the folder, the environment and whether a terminal is attached", func(t *testing.T) {
		received := make(chan Request, 1)
		socket, _, _ := serve(t, "build", time.Minute, runs(func(session Session) int {
			received <- session.Request
			return 0
		}))
		sent := Request{Identity: "build", Arguments: []string{"sync", "--path", "shop"}, Folder: "/work", Environment: []string{"REIN_LOGIN=Danidani23", "PATH=/bin"}, Terminal: true, Executable: "/usr/local/bin/rein"}
		_, err := Relay(socket, sent, strings.NewReader(""), io.Discard, io.Discard, nil)
		if err != nil {
			t.Fatalf("relay: %v", err)
		}
		if got := <-received; !reflect.DeepEqual(got, sent) {
			t.Fatalf("received %+v, sent %+v", got, sent)
		}
	})
	t.Run("the caller's Ctrl-C reaches the command", func(t *testing.T) {
		socket, _, _ := serve(t, "build", time.Minute, runs(func(session Session) int {
			select {
			case <-session.Interrupts:
				return 130
			case <-time.After(5 * time.Second):
				return 0
			}
		}))
		interrupts := make(chan struct{}, 1)
		interrupts <- struct{}{}
		code, _, _, err := relay(t, socket, "build", strings.NewReader(""), interrupts, "distill")
		if err != nil || code != 130 {
			t.Fatalf("code %d relay %v", code, err)
		}
	})
	t.Run("without an owner the caller is told there is none", func(t *testing.T) {
		_, _, _, err := relay(t, SocketPath(t.TempDir()), "build", strings.NewReader(""), nil, "status")
		if !errors.Is(err, ErrNoOwner) {
			t.Fatalf("relay %v", err)
		}
	})
}

func TestServer(t *testing.T) {
	t.Run("two commands run at the same time", func(t *testing.T) {
		var started sync.WaitGroup
		started.Add(2)
		socket, _, _ := serve(t, "build", time.Minute, runs(func(session Session) int {
			started.Done()
			started.Wait()
			return 0
		}))
		results := make(chan error, 2)
		for range 2 {
			go func() {
				_, _, _, err := relay(t, socket, "build", strings.NewReader(""), nil, "status")
				results <- err
			}()
		}
		for range 2 {
			select {
			case err := <-results:
				if err != nil {
					t.Fatalf("relay: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("the commands waited for each other")
			}
		}
	})
	t.Run("a caller built differently is told so, and the owner ends once its running commands have", func(t *testing.T) {
		release := make(chan struct{})
		started := make(chan struct{})
		socket, _, ended := serve(t, "build-a", time.Minute, runs(func(session Session) int {
			close(started)
			<-release
			return 0
		}))
		identity, err := Identify(socket)
		if err != nil || identity != "build-a" {
			t.Fatalf("identity %q err %v", identity, err)
		}
		running := make(chan error, 1)
		go func() {
			_, _, _, err := relay(t, socket, "build-a", strings.NewReader(""), nil, "distill")
			running <- err
		}()
		<-started
		_, _, _, err = relay(t, socket, "build-b", strings.NewReader(""), nil, "status")
		var otherBuild ErrOtherBuild
		if !errors.As(err, &otherBuild) || otherBuild.Identity != "build-a" {
			t.Fatalf("relay %v", err)
		}
		waitFor(t, "the socket's closing", func() bool {
			_, err := Identify(socket)
			return errors.Is(err, ErrNoOwner)
		})
		if isClosed(ended) {
			t.Fatalf("the owner ended while a command ran")
		}
		close(release)
		if err := <-running; err != nil {
			t.Fatalf("the running command: %v", err)
		}
		waitFor(t, "the owner's end", func() bool { return isClosed(ended) })
	})
	t.Run("an owner ends after a while without commands, and never during one", func(t *testing.T) {
		_, _, ended := serve(t, "build", 50*time.Millisecond, runs(func(Session) int { return 0 }))
		waitFor(t, "the idle owner's end", func() bool { return isClosed(ended) })

		release := make(chan struct{})
		socket, _, busyEnded := serve(t, "build", 50*time.Millisecond, func(session Session) (int, <-chan struct{}) {
			finished := make(chan struct{})
			go func() {
				<-release
				close(finished)
			}()
			return 0, finished
		})
		_, _, _, err := relay(t, socket, "build", strings.NewReader(""), nil, "sync")
		if err != nil {
			t.Fatalf("relay: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
		if isClosed(busyEnded) {
			t.Fatalf("the owner ended while a command it had let its caller go from still ran")
		}
		close(release)
		waitFor(t, "the owner's end once the command finished", func() bool { return isClosed(busyEnded) })
	})
}

func TestListen(t *testing.T) {
	t.Run("a socket left behind is taken over, one that answers is refused", func(t *testing.T) {
		socket := SocketPath(t.TempDir())
		err := os.WriteFile(socket, nil, 0o600)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		listener, err := Listen(socket)
		if err != nil {
			t.Fatalf("the socket left behind was not taken over: %v", err)
		}
		server := &Server{Identity: "build", Run: runs(func(Session) int { return 0 }), Idle: time.Minute, Log: io.Discard}
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = server.Serve(listener)
		}()
		defer func() {
			server.Stop()
			<-done
		}()
		_, err = Listen(socket)
		if !errors.Is(err, ErrRunning) {
			t.Fatalf("a second listen: %v", err)
		}
	})
	t.Run("the socket is in rein's home, or in the temporary folder when that path is too long", func(t *testing.T) {
		if got := SocketPath("/home/dani/.rein"); got != filepath.Join("/home/dani/.rein", "owner.sock") {
			t.Fatalf("short home: %s", got)
		}
		long := filepath.Join(os.TempDir(), strings.Repeat("a-long-folder-name/", 8))
		got := SocketPath(long)
		if filepath.Dir(got) != filepath.Clean(os.TempDir()) || !strings.HasPrefix(filepath.Base(got), "rein-") || SocketPath(long) != got {
			t.Fatalf("long home: %s", got)
		}
	})
}
