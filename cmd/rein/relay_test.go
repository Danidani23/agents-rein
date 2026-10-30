package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/0_gateway/cli"
	"github.com/Danidani23/agents-rein/internal/app/0_gateway/owner"
)

// runRelayed runs rein as a terminal does: through runRein, which relays
// every command that needs the machine to the background process.
func runRelayed(input string, arguments ...string) (code int, out string, errOut string) {
	var outBuffer, errBuffer bytes.Buffer
	code = runRein(arguments, strings.NewReader(input), &outBuffer, &errBuffer)
	return code, outBuffer.String(), errBuffer.String()
}

// stopOwner makes the background process on socket end, as a client of
// another build does, and waits until it has.
func stopOwner(t *testing.T, socket string) {
	t.Helper()
	_, _ = owner.Relay(socket, owner.Request{Identity: "a build that stops it"}, nil, io.Discard, io.Discard, nil)
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err := owner.Identify(socket)
		if errors.Is(err, owner.ErrNoOwner) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the background process did not end: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestTheBackgroundProcess runs rein the way a terminal does: the first
// command that needs the machine starts the background process of rein's
// home, a copy of this test binary as rein owner, and each command runs
// there for its caller (D55).
func TestTheBackgroundProcess(t *testing.T) {
	reinHome := isolate(t, "Danidani23")
	socket := owner.SocketPath(reinHome)
	t.Cleanup(func() { stopOwner(t, socket) })
	t.Run("version runs in the caller's own process", func(t *testing.T) {
		code, out, errOut := runRelayed("", "version")
		if code != cli.ExitWorked || !strings.Contains(out, "rein") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		_, err := owner.Identify(socket)
		if !errors.Is(err, owner.ErrNoOwner) {
			t.Fatalf("version started the background process: %v", err)
		}
	})
	t.Run("the first command starts the background process, which runs it for the caller", func(t *testing.T) {
		code, out, errOut := runRelayed("", "status")
		if code != cli.ExitWorked || !strings.Contains(out, "  login        Danidani23 · agents claude-code") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		identity, err := owner.Identify(socket)
		if err != nil || identity != buildIdentity() {
			t.Fatalf("identity %q, this build %q, err %v", identity, buildIdentity(), err)
		}
	})
	t.Run("a later command runs with its own caller's environment", func(t *testing.T) {
		// the background process started without REIN_LOGIN; this caller has it, as git's has
		t.Setenv("REIN_LOGIN", "Danidani23")
		code, out, errOut := runRelayed("protocol=https\nhost=github.com\n\n", "git-credential", "get")
		if code != cli.ExitWorked || !strings.Contains(out, "username=x-access-token\npassword=") || strings.HasSuffix(out, "password=\n") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
	})
}

// TestTheApp runs rein as a terminal does where the app is installed: the
// app starts when no background process answers, and an app of another
// version is reported, never replaced (D50).
func TestTheApp(t *testing.T) {
	t.Run("where the app is installed, the first command starts it", func(t *testing.T) {
		reinHome := isolate(t, "Danidani23")
		socket := owner.SocketPath(reinHome)
		t.Cleanup(func() { stopOwner(t, socket) })
		executable, err := os.Executable()
		if err != nil {
			t.Fatalf("executable: %v", err)
		}
		// this test binary, started with --background, plays the app
		t.Setenv("REIN_APP", executable)
		code, out, errOut := runRelayed("", "status")
		if code != cli.ExitWorked || !strings.Contains(out, "  login        Danidani23") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
	})
	t.Run("an app of another version that stays is reported, and keeps running", func(t *testing.T) {
		reinHome := isolate(t, "Danidani23")
		socket := owner.SocketPath(reinHome)
		listener, err := owner.Listen(socket)
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		app := &owner.Server{Identity: "v9.9.9 1 1", Stays: true, Run: func(owner.Session) (int, <-chan struct{}) { return 0, nil }}
		served := make(chan struct{})
		go func() {
			defer close(served)
			_ = app.Serve(listener)
		}()
		t.Cleanup(func() {
			app.Stop()
			<-served
		})
		code, _, errOut := runRelayed("", "status")
		if code != cli.ExitRefused || !strings.Contains(errOut, "rein's app on this machine is v9.9.9 and this rein is ") {
			t.Fatalf("code %d err %q", code, errOut)
		}
		identity, err := owner.Identify(socket)
		if err != nil || identity != "v9.9.9 1 1" {
			t.Fatalf("the app did not stay: %q %v", identity, err)
		}
	})
}
