// Package execute is the one place reins runs another program. A failure
// names what ran and carries the program's own stderr, and no program it runs
// may ever prompt.
package execute

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Result is what a program printed.
type Result struct {
	Stdout string
	Stderr string
}

// Failure is a program that ran and failed.
type Failure struct {
	Command string
	Stderr  string
	Cause   error
}

func (failure *Failure) Error() string {
	stderr := strings.TrimSpace(failure.Stderr)
	if stderr == "" {
		return fmt.Sprintf("%s failed: %v", failure.Command, failure.Cause)
	}
	return fmt.Sprintf("%s failed: %s", failure.Command, stderr)
}

// Preflight checks that a program is on the PATH before anything changes.
func Preflight(name string) error {
	_, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s is not on the PATH; install it first", name)
	}
	return nil
}

// Run runs name with arguments in directory and returns what it printed. A
// non-zero exit is a Failure. Git is told never to prompt for credentials.
func Run(directory string, name string, arguments ...string) (Result, error) {
	command := exec.Command(name, arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		return result, &Failure{Command: name + " " + strings.Join(arguments, " "), Stderr: stderr.String(), Cause: err}
	}
	return result, nil
}
