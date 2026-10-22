// reinctl is the operator CLI of this repository: what a maintainer runs
// to release rein. It is never shipped to users; rein is. The human runs it,
// and the verbs wrap the git commands, which is how the git rule holds: the
// agent never runs a mutating git command.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
)

// The exit codes, as rein's: worked, refused, crashed, a tool failed.
const (
	exitWorked     = 0
	exitRefused    = 1
	exitCrashed    = 2
	exitToolFailed = 3
)

// refused is a precondition that did not hold; nothing changed.
type refused struct{ reason string }

func (refusal refused) Error() string { return refusal.reason }

func refuse(format string, arguments ...any) error {
	return refused{reason: fmt.Sprintf(format, arguments...)}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes reinctl with arguments and returns the exit code.
func run(arguments []string, out io.Writer, errOut io.Writer) int {
	root := &cobra.Command{
		Use:           "reinctl",
		Short:         "The operator CLI of the agents-rein repository: release",
		Long:          "reinctl is what a maintainer runs on this repository. rein is what users run.\nrelease cuts the next version on main from the conventional commits since the last tag and\npublishes it from this machine; publish repeats the last step for a release that failed there.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs(arguments)
	root.AddCommand(newReleaseCommand(out), newPublishCommand(out))
	err := root.Execute()
	if err == nil {
		return exitWorked
	}
	fmt.Fprintf(errOut, "reinctl: %v\n", err)
	var refusal refused
	if errors.As(err, &refusal) {
		return exitRefused
	}
	var failure *execute.Failure
	if errors.As(err, &failure) {
		return exitToolFailed
	}
	return exitCrashed
}
