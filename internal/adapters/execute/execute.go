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
	"io"
	"os"
	"os/exec"
	"runtime"
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
	// Lines, when set, receives each line of standard output as it comes,
	// without its newline; Result.Stdout still holds all of it.
	Lines func(line string)
}

// lineWriter hands what a program writes on, one whole line at a time.
type lineWriter struct {
	each    func(line string)
	pending []byte
}

func (writer *lineWriter) Write(written []byte) (int, error) {
	writer.pending = append(writer.pending, written...)
	for {
		end := bytes.IndexByte(writer.pending, '\n')
		if end < 0 {
			return len(written), nil
		}
		writer.each(strings.TrimSuffix(string(writer.pending[:end]), "\r"))
		writer.pending = writer.pending[end+1:]
	}
}

// flush hands on the last line, which no newline ended.
func (writer *lineWriter) flush() {
	if len(writer.pending) > 0 {
		writer.each(string(writer.pending))
		writer.pending = nil
	}
}

// Runner runs programs for one caller: in the caller's environment, found
// on the caller's PATH, and stopped together by the caller's Ctrl-C. The
// zero Runner is this process's own. The package's functions use it.
type Runner struct {
	// Environment is the caller's, as NAME=value; nil means this process's.
	Environment []string
	runs        *group
}

// group is the runs one Interrupt stops: their shared context, which
// Interrupt cancels and replaces, so the programs that follow, the
// compensations, run.
type group struct {
	mutex      sync.Mutex
	generation context.Context
	cancel     context.CancelFunc
	running    int
}

func newGroup() *group {
	runs := &group{}
	runs.generation, runs.cancel = context.WithCancel(context.Background())
	return runs
}

// processRuns is the group of this process's own runner.
var processRuns = newGroup()

// NewRunner is the runner of a caller other than this process, such as a
// command the background process runs for a terminal: its environment and
// a Ctrl-C of its own.
func NewRunner(environment []string) Runner {
	return Runner{Environment: environment, runs: newGroup()}
}

func (runner Runner) group() *group {
	if runner.runs == nil {
		return processRuns
	}
	return runner.runs
}

// Interrupt kills every program of this process's runner and lets later
// ones run.
func Interrupt() {
	Runner{}.Interrupt()
}

// Running reports whether a program of this process's runner is in flight.
func Running() bool {
	return Runner{}.Running()
}

// Preflight checks that a program is on this process's PATH.
func Preflight(name string) error {
	return Runner{}.Preflight(name)
}

// Run runs name with this process's runner.
func Run(directory string, name string, arguments ...string) (Result, error) {
	return Runner{}.Run(directory, name, arguments...)
}

// RunWithInput is Run with text on the program's standard input.
func RunWithInput(directory string, input string, name string, arguments ...string) (Result, error) {
	return Runner{}.RunWithInput(directory, input, name, arguments...)
}

// RunWith runs name with this process's runner under the options.
func RunWith(options Options, name string, arguments ...string) (Result, error) {
	return Runner{}.RunWith(options, name, arguments...)
}

// Interrupt kills every running program of this runner and lets later
// ones run.
func (runner Runner) Interrupt() {
	runs := runner.group()
	runs.mutex.Lock()
	defer runs.mutex.Unlock()
	runs.cancel()
	runs.generation, runs.cancel = context.WithCancel(context.Background())
}

// Running reports whether a program of this runner is in flight right now.
func (runner Runner) Running() bool {
	runs := runner.group()
	runs.mutex.Lock()
	defer runs.mutex.Unlock()
	return runs.running > 0
}

// LookupEnv reads a variable of the caller's environment, as os.LookupEnv
// reads this process's.
func (runner Runner) LookupEnv(name string) (string, bool) {
	if runner.Environment == nil {
		return os.LookupEnv(name)
	}
	value := ""
	found := false
	for _, pair := range runner.Environment {
		key, rest, separated := strings.Cut(pair, "=")
		if separated && (key == name || runtime.GOOS == "windows" && strings.EqualFold(key, name)) {
			value, found = rest, true
		}
	}
	return value, found
}

// Getenv is LookupEnv without saying whether the variable is set.
func (runner Runner) Getenv(name string) string {
	value, _ := runner.LookupEnv(name)
	return value
}

// Preflight checks that a program is on the PATH before anything changes.
func (runner Runner) Preflight(name string) error {
	_, err := runner.lookPath(name)
	if err != nil {
		return fmt.Errorf("%s is not on the PATH; install it first", name)
	}
	return nil
}

// lookPath finds a program on this runner's PATH: the caller's, or this
// process's for the zero Runner.
func (runner Runner) lookPath(name string) (string, error) {
	if runner.Environment == nil {
		return exec.LookPath(name)
	}
	return lookPathIn(name, runner.Environment)
}

// Run runs name with arguments in directory and returns what it printed.
func (runner Runner) Run(directory string, name string, arguments ...string) (Result, error) {
	return runner.RunWith(Options{Directory: directory}, name, arguments...)
}

// RunWithInput is Run with text on the program's standard input.
func (runner Runner) RunWithInput(directory string, input string, name string, arguments ...string) (Result, error) {
	return runner.RunWith(Options{Directory: directory, Input: input}, name, arguments...)
}

// RunWith runs name with arguments under the options. A non-zero exit, the
// time limit and an interruption are each a Failure. Git is told never to
// prompt for credentials.
func (runner Runner) RunWith(options Options, name string, arguments ...string) (Result, error) {
	timeout := options.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	runs := runner.group()
	runs.mutex.Lock()
	parent := runs.generation
	runs.running++
	runs.mutex.Unlock()
	defer func() {
		runs.mutex.Lock()
		runs.running--
		runs.mutex.Unlock()
	}()
	limited, stop := context.WithTimeout(parent, timeout)
	defer stop()
	command := exec.CommandContext(limited, name, arguments...)
	if runner.Environment != nil {
		// exec looked on this process's PATH; the program is the caller's
		path, err := lookPathIn(name, runner.Environment)
		command.Path, command.Err = path, err
	}
	command.Dir = options.Directory
	command.Stdin = strings.NewReader(options.Input)
	command.Env = runner.environment(options)
	command.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	var lines *lineWriter
	if options.Lines != nil {
		lines = &lineWriter{each: options.Lines}
		command.Stdout = io.MultiWriter(&stdout, lines)
	}
	err := command.Run()
	if lines != nil {
		lines.flush()
	}
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

// environment is the program's environment: the runner's, minus the
// variables to unset, plus the ones given, and never a terminal prompt
// from git.
func (runner Runner) environment(options Options) []string {
	unset := map[string]bool{"GIT_TERMINAL_PROMPT": true}
	for _, name := range options.Unset {
		unset[name] = true
	}
	for _, pair := range options.Environment {
		unset[strings.SplitN(pair, "=", 2)[0]] = true
	}
	base := runner.Environment
	if base == nil {
		base = os.Environ()
	}
	var variables []string
	for _, pair := range base {
		if !unset[strings.SplitN(pair, "=", 2)[0]] {
			variables = append(variables, pair)
		}
	}
	variables = append(variables, "GIT_TERMINAL_PROMPT=0")
	return append(variables, options.Environment...)
}
