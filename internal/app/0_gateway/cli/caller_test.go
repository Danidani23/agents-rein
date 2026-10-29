package cli

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// lockedBuffer is a buffer a command may still write while the test reads.
type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (locked *lockedBuffer) Write(written []byte) (int, error) {
	locked.mutex.Lock()
	defer locked.mutex.Unlock()
	return locked.buffer.Write(written)
}

func (locked *lockedBuffer) String() string {
	locked.mutex.Lock()
	defer locked.mutex.Unlock()
	return locked.buffer.String()
}

// runFor executes rein the way the background process does for a caller
// other than this process, and waits until the command has finished.
func (fixture *harness) runFor(caller Caller, arguments ...string) (code int, out string, errOut string) {
	var outBuffer, errBuffer lockedBuffer
	caller.Out, caller.ErrOut = &outBuffer, &errBuffer
	if caller.In == nil {
		caller.In = strings.NewReader("")
	}
	code, finished := RunFor(caller, arguments, nil, fixture.orchestrate)
	<-finished
	return code, outBuffer.String(), errBuffer.String()
}

func TestRunFor(t *testing.T) {
	t.Run("the caller's environment names the login git asks a token for, not this process's", func(t *testing.T) {
		fixture := newHarness(t)
		t.Setenv("REIN_LOGIN", "")
		caller := Caller{In: strings.NewReader("protocol=https\nhost=github.com\n\n"), Runner: execute.NewRunner([]string{"REIN_LOGIN=Danidani23"})}
		code, out, errOut := fixture.runFor(caller, "git-credential", "get")
		if code != ExitWorked || !strings.Contains(out, "password=token-of-Danidani23") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
	})
	t.Run("a relative path starts from the caller's folder, not this process's", func(t *testing.T) {
		fixture := newHarness(t)
		callerFolder := t.TempDir()
		_, out, errOut := fixture.runFor(Caller{Folder: callerFolder, Runner: execute.NewRunner(nil)}, "reset", "project", "--path", "sub", "--yes")
		if !strings.Contains(out, "moved out of "+filepath.Join(callerFolder, "sub")+",") {
			t.Fatalf("out %q err %q", out, errOut)
		}
	})
	t.Run("a Ctrl-C while no program runs ends the command for the caller at once, and the command finishes here", func(t *testing.T) {
		fixture := newHarness(t)
		input, typing := io.Pipe()
		var outBuffer, errBuffer lockedBuffer
		caller := Caller{In: input, Out: &outBuffer, ErrOut: &errBuffer, Terminal: true, Folder: t.TempDir(), Runner: execute.NewRunner(nil)}
		interrupts := make(chan struct{}, 1)
		codes := make(chan int, 1)
		var finished <-chan struct{}
		started := make(chan struct{})
		go func() {
			code, done := RunFor(caller, []string{"reset", "project"}, interrupts, fixture.orchestrate)
			finished = done
			close(started)
			codes <- code
		}()
		deadline := time.Now().Add(5 * time.Second)
		for !strings.Contains(outBuffer.String(), "Continue?") {
			if time.Now().After(deadline) {
				t.Fatalf("the question never came: %q", outBuffer.String())
			}
			time.Sleep(10 * time.Millisecond)
		}
		interrupts <- struct{}{}
		select {
		case code := <-codes:
			if code != ExitInterrupted {
				t.Fatalf("code %d", code)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("the caller was not let go")
		}
		<-started
		select {
		case <-finished:
			t.Fatalf("the command finished while its question still waited")
		default:
		}
		_ = typing.Close()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Fatalf("the command did not finish once its input ended")
		}
	})
	t.Run("a Ctrl-C while the command waits for its turn takes it out of the line", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.lock.Ahead = &ports.LockHolder{Command: "rein sync --all", Caller: "a terminal", Folder: "/work", Since: time.Now()}
		fixture.lock.Free = make(chan struct{})
		var outBuffer, errBuffer lockedBuffer
		caller := Caller{In: strings.NewReader(""), Out: &outBuffer, ErrOut: &errBuffer, Folder: t.TempDir(), Runner: execute.NewRunner(nil)}
		interrupts := make(chan struct{}, 1)
		codes := make(chan int, 1)
		finishedOf := make(chan (<-chan struct{}), 1)
		go func() {
			code, finished := RunFor(caller, []string{"org", "use", "acme"}, interrupts, fixture.orchestrate)
			finishedOf <- finished
			codes <- code
		}()
		deadline := time.Now().Add(5 * time.Second)
		for !strings.Contains(errBuffer.String(), "waiting for rein sync --all") {
			if time.Now().After(deadline) {
				t.Fatalf("the wait was never said: %q", errBuffer.String())
			}
			time.Sleep(10 * time.Millisecond)
		}
		interrupts <- struct{}{}
		finished := <-finishedOf
		code := <-codes
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Fatalf("the command still waits in line")
		}
		if code != ExitInterrupted || fixture.lock.Acquired != 0 {
			t.Fatalf("code %d acquired %d", code, fixture.lock.Acquired)
		}
	})
}
