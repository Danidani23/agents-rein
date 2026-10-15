// Package cli wires the reins commands. Every byte of output goes through the
// writers Run receives; nothing in this module writes to the process streams
// directly except main, and a test enforces it, so a whole run can be captured.
package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Danidani23/reins/internal/execute"
	"github.com/Danidani23/reins/internal/local"
)

// Exit codes: 0 worked, 1 refused (a precondition or a check failed), 2
// crashed (an error the command did not expect), 3 a program reins ran failed.
const (
	ExitWorked     = 0
	ExitRefused    = 1
	ExitCrashed    = 2
	ExitToolFailed = 3
)

// crashedError marks an error the command did not expect, such as an
// unreadable file; every other error is a refusal.
type crashedError struct {
	cause error
}

func (crashed crashedError) Error() string {
	return crashed.cause.Error()
}

// environment is what every command receives: the three streams.
type environment struct {
	in     io.Reader
	out    io.Writer
	errOut io.Writer
}

// Run executes reins with arguments and returns the exit code.
func Run(arguments []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	env := &environment{in: in, out: out, errOut: errOut}
	root := newRootCommand(env)
	root.SetArgs(arguments)
	err := root.Execute()
	if err == nil {
		return ExitWorked
	}
	fmt.Fprintf(errOut, "reins: %v\n", err)
	var crashed crashedError
	if errors.As(err, &crashed) {
		return ExitCrashed
	}
	var failure *execute.Failure
	if errors.As(err, &failure) {
		return ExitToolFailed
	}
	return ExitRefused
}

// requireConfig loads the machine's config or refuses. Every command that
// acts for a user starts here; lint and version do not.
func requireConfig() (home string, config local.Config, err error) {
	home, err = local.Home()
	if err != nil {
		return "", local.Config{}, crashedError{cause: err}
	}
	config, err = local.Require(home)
	if errors.Is(err, local.ErrNotConfigured) {
		return "", local.Config{}, err
	}
	if err != nil {
		return "", local.Config{}, crashedError{cause: err}
	}
	return home, config, nil
}

func newRootCommand(env *environment) *cobra.Command {
	root := &cobra.Command{
		Use:   "reins",
		Short: "Compile and lint the agent harness of an organisation",
		Long: "reins turns the declarations of an org repository into the files each agent tool\n" +
			"reads, and checks them against the specifications. It is a compiler and a linter,\n" +
			"never a service.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(env.out)
	root.SetErr(env.errOut)
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(newConfigCommand(env), newOrgCommand(env), newLintCommand(env), newExportCommand(env), newImportCommand(env), newVersionCommand(env))
	return root
}
