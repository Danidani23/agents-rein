package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
)

// Caller is who a command runs for: the streams, whether a person types at
// a terminal, the folder relative paths start from, and the runner of its
// programs, which carries its environment and stops on its Ctrl-C. Run
// makes this process the caller; the background process makes one for
// each command it is sent (D55).
type Caller struct {
	In       io.Reader
	Out      io.Writer
	ErrOut   io.Writer
	Terminal bool
	Folder   string
	Runner   execute.Runner
}

func newEnvironment(caller Caller, orchestrate Orchestrate) *commandEnvironment {
	return &commandEnvironment{in: caller.In, out: caller.Out, errOut: caller.ErrOut, terminal: caller.Terminal, folder: caller.Folder, runner: caller.Runner, orchestrate: orchestrate}
}

// RunFor executes rein with arguments for a caller other than this
// process, as the background process does for each command it is sent,
// and returns the exit code for the caller. The caller's Ctrl-C arrives on
// interrupts: the first stops the program the command waits for, so the
// workflow undoes what it did; a second, or one while no program runs,
// lets the caller go at once with ExitInterrupted. The command itself
// goes on here until it ends, and finished closes then, so whoever runs it
// knows when this process is free.
func RunFor(caller Caller, arguments []string, interrupts <-chan struct{}, orchestrate Orchestrate) (code int, finished <-chan struct{}) {
	environment := newEnvironment(caller, orchestrate)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	codes := make(chan int, 1)
	go func() {
		defer close(done)
		defer cancel()
		codes <- runCommandWithin(ctx, environment, arguments)
	}()
	first := true
	for {
		select {
		case code := <-codes:
			return code, done
		case <-interrupts:
			if !first || !caller.Runner.Running() {
				cancel()
				return ExitInterrupted, done
			}
			first = false
			fmt.Fprintf(caller.ErrOut, "\ninterrupted; undoing what was done, press Ctrl-C again to quit at once\n")
			caller.Runner.Interrupt()
		}
	}
}
