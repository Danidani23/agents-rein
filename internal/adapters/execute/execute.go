// Package execute is the one place rein runs another program. Every run has
// a time limit and can be interrupted: a program that waits for input is
// killed when its limit passes, and Ctrl-C kills the running program so the
// workflow around it can undo what it did. A failure names what ran and
// carries the program's own stderr, and no program it runs may ever prompt.
package execute

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// DefaultTimeout is the time limit of a run that names none.
const DefaultTimeout = 60 * time.Second

// NetworkTimeout is the time limit for a clone, a pull or a push.
const NetworkTimeout = 5 * time.Minute

// ErrInterrupted marks a run that Ctrl-C stopped.
var ErrInterrupted = errors.New("interrupted")

// ErrTimedOut marks a run that passed its time limit.
var ErrTimedOut = errors.New("timed out")

// Result is what a program printed.
type Result struct {
	Stdout string
	Stderr string
}

// Failure is a program that ran and failed, or was stopped.
type Failure struct {
	Command string
	Stderr  string
	Cause   error
}

func (failure *Failure) Error() string {
	stderr := strings.TrimSpace(failure.Stderr)
	switch {
	case errors.Is(failure.Cause, ErrTimedOut):
		return fmt.Sprintf("%s %v; it was probably waiting for input", failure.Command, failure.Cause)
	case errors.Is(failure.Cause, ErrInterrupted):
		return fmt.Sprintf("%s %v", failure.Command, failure.Cause)
	case stderr == "":
		return fmt.Sprintf("%s failed: %v", failure.Command, failure.Cause)
	}
	return fmt.Sprintf("%s failed: %s", failure.Command, stderr)
}

// Unwrap lets errors.Is see the cause.
func (failure *Failure) Unwrap() error {
	return failure.Cause
}

// Options shape one run.
type Options struct {
	Directory string
	Input     string
	// Timeout is the time limit; zero means DefaultTimeout.
	Timeout time.Duration
	// Environment adds or overrides variables, as NAME=value.
	Environment []string
	// Unset removes variables from the program's environment.
	Unset []string
}

// generation is the context every run shares; Interrupt cancels it and
// starts a new one, so the programs that follow, the compensations, run.
var (
	mutex      sync.Mutex
	generation context.Context
	cancel     context.CancelFunc
	running    int
)

func init() {
	generation, cancel = context.WithCancel(context.Background())
}

// Interrupt kills every running program and lets later ones run.
func Interrupt() {
	mutex.Lock()
	defer mutex.Unlock()
	cancel()
	generation, cancel = context.WithCancel(context.Background())
}

// Running reports whether a program is in flight right now.
func Running() bool {
	mutex.Lock()
	defer mutex.Unlock()
	return running > 0
}

// Preflight checks that a program is on the PATH before anything changes.
func Preflight(name string) error {
	_, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s is not on the PATH; install it first", name)
	}
	return nil
}

// Run runs name with arguments in directory and returns what it printed.
func Run(directory string, name string, arguments ...string) (Result, error) {
	return RunWith(Options{Directory: directory}, name, arguments...)
}

// RunWithInput is Run with text on the program's standard input.
func RunWithInput(directory string, input string, name string, arguments ...string) (Result, error) {
	return RunWith(Options{Directory: directory, Input: input}, name, arguments...)
}

// RunWith runs name with arguments under the options. A non-zero exit, the
// time limit and an interruption are each a Failure. Git is told never to
// prompt for credentials.
func RunWith(options Options, name string, arguments ...string) (Result, error) {
	timeout := options.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	mutex.Lock()
	parent := generation
	running++
	mutex.Unlock()
	defer func() {
		mutex.Lock()
		running--
		mutex.Unlock()
	}()
	limited, stop := context.WithTimeout(parent, timeout)
	defer stop()
	command := exec.CommandContext(limited, name, arguments...)
	command.Dir = options.Directory
	command.Stdin = strings.NewReader(options.Input)
	command.Env = environment(options)
	command.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return result, nil
	}
	described := name + " " + strings.Join(arguments, " ")
	switch {
	case errors.Is(parent.Err(), context.Canceled):
		return result, &Failure{Command: described, Cause: ErrInterrupted}
	case errors.Is(limited.Err(), context.DeadlineExceeded):
		return result, &Failure{Command: described, Cause: fmt.Errorf("%w after %s", ErrTimedOut, timeout)}
	}
	return result, &Failure{Command: described, Stderr: stderr.String(), Cause: err}
}

// environment is the program's environment: this process's, minus the
// variables to unset, plus the ones given, and never a terminal prompt
// from git.
func environment(options Options) []string {
	unset := map[string]bool{"GIT_TERMINAL_PROMPT": true}
	for _, name := range options.Unset {
		unset[name] = true
	}
	for _, pair := range options.Environment {
		unset[strings.SplitN(pair, "=", 2)[0]] = true
	}
	var variables []string
	for _, pair := range os.Environ() {
		if !unset[strings.SplitN(pair, "=", 2)[0]] {
			variables = append(variables, pair)
		}
	}
	variables = append(variables, "GIT_TERMINAL_PROMPT=0")
	return append(variables, options.Environment...)
}
