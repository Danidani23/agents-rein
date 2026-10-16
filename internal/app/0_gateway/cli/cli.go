// Package cli is the gateway a person uses: cobra commands that read flags,
// ask through the Prompter port, call the orchestrator and print its result.
// It depends on the orchestration interfaces, never on an adapter other than
// the terminal, which is its own device; main wires the rest. Every byte of
// output goes through the writers Run receives, and a test enforces it.
package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/adapters/terminal"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Exit codes: 0 worked, 1 refused (a precondition or a check failed), 2
// crashed (an error the command did not expect), 3 a program rein ran failed.
const (
	ExitWorked     = 0
	ExitRefused    = 1
	ExitCrashed    = 2
	ExitToolFailed = 3
)

// Orchestrate returns the orchestrator a command talks to; it runs lazily so
// that help and version never touch the machine.
type Orchestrate func() (orchestration.Orchestrator, error)

// environment is what every command receives: the streams, whether a person
// is typing, and the orchestrator.
type environment struct {
	in          io.Reader
	out         io.Writer
	errOut      io.Writer
	terminal    bool
	orchestrate Orchestrate
}

func (env *environment) prompter() ports.Prompter {
	return terminal.New(env.in, env.out, env.terminal)
}

// Run executes rein with arguments and returns the exit code.
func Run(arguments []string, in io.Reader, out io.Writer, errOut io.Writer, orchestrate Orchestrate) int {
	env := &environment{in: in, out: out, errOut: errOut, terminal: terminal.IsTerminal(in), orchestrate: orchestrate}
	return runCommand(env, arguments)
}

func runCommand(env *environment, arguments []string) int {
	root := newRootCommand(env)
	root.SetArgs(arguments)
	err := root.Execute()
	if err == nil {
		return ExitWorked
	}
	fmt.Fprintf(env.errOut, "rein: %v\n", err)
	var failure *execute.Failure
	if errors.As(err, &failure) {
		return ExitToolFailed
	}
	if errors.Is(err, orchestration.ErrRefused) || errors.Is(err, errRefusedByCommand) || isUsageError(err) {
		return ExitRefused
	}
	return ExitCrashed
}

// errRefusedByCommand marks a refusal decided in the gateway itself, such as
// a missing terminal or a declined confirmation.
var errRefusedByCommand = errors.New("refused by command")

type commandRefusal struct {
	reason string
}

func (refusal commandRefusal) Error() string {
	return refusal.reason
}

func (commandRefusal) Is(target error) bool {
	return target == errRefusedByCommand
}

func refuse(reason string) error {
	return commandRefusal{reason: reason}
}

// isUsageError recognises cobra's own errors: an unknown command, a missing
// argument, an unknown flag. They are refusals, not crashes.
func isUsageError(err error) bool {
	message := err.Error()
	for _, prefix := range []string{"unknown command", "accepts ", "requires at least", "unknown flag", "unknown shorthand flag", "flag needs an argument", "invalid argument"} {
		if len(message) >= len(prefix) && message[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func newRootCommand(env *environment) *cobra.Command {
	root := &cobra.Command{
		Use:   "rein",
		Short: "Keep your AI coding agents' settings in one place and bring them to every project",
		Long: "Agents Rein keeps the settings of your AI coding agents, instructions, rules, skills,\n" +
			"tool servers, hooks and permissions, as declarations in one git repository per\n" +
			"organisation, at four levels: organisation, team, project and user. rein checks them,\n" +
			"works out which apply to a project, and writes them into each clone in the form your\n" +
			"agent reads. It is a compiler and a linter, never a service.\n\n" +
			"Your identity is your GitHub account, read from the gh CLI, which must be installed and\n" +
			"logged in. Start with rein org connect. rein status shows what has been set up. Every\n" +
			"command explains itself with --help.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(env.out)
	root.SetErr(env.errOut)
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(newStatusCommand(env), newOrgCommand(env), newLintCommand(env), newResetCommand(env), newVersionCommand(env))
	return root
}
