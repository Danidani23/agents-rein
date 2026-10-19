// Package cli is the gateway a person uses: cobra commands that read flags,
// ask through the Prompter port, and print. A single operation goes straight
// to a service; several in order go to a workflow of the orchestrator (D17).
// It holds the one concrete Orchestrator (D18) and knows no adapter other than
// the terminal, which is its own device; main wires the rest. A command that
// acts for a user passes the identity gate first. Every byte of output goes
// through the writers Run receives, and a test enforces it.
package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/adapters/terminal"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
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

// commandEnvironment is what every command receives: the streams, whether a person
// is typing, and the orchestrator.
type commandEnvironment struct {
	in          io.Reader
	out         io.Writer
	errOut      io.Writer
	terminal    bool
	orchestrate Orchestrate
}

func (environment *commandEnvironment) prompter() ports.Prompter {
	return terminal.New(environment.in, environment.out, environment.terminal)
}

// Run executes rein with arguments and returns the exit code.
func Run(arguments []string, in io.Reader, out io.Writer, errOut io.Writer, orchestrate Orchestrate) int {
	environment := &commandEnvironment{in: in, out: out, errOut: errOut, terminal: terminal.IsTerminal(in), orchestrate: orchestrate}
	return runCommand(environment, arguments)
}

func runCommand(environment *commandEnvironment, arguments []string) int {
	root := newRootCommand(environment)
	root.SetArgs(arguments)
	err := root.Execute()
	if err == nil {
		return ExitWorked
	}
	fmt.Fprintf(environment.errOut, "rein: %v\n", err)
	var failure *execute.Failure
	if errors.As(err, &failure) {
		return ExitToolFailed
	}
	if errors.Is(err, refusal.ErrRefused) || isUsageError(err) {
		return ExitRefused
	}
	return ExitCrashed
}

// requireIdentity is the gate in front of every command that acts for a
// user: without a logged-in host it refuses and names the way in (D8, D18).
func requireIdentity(orchestrator orchestration.Orchestrator) error {
	_, err := orchestrator.Identity.Who()
	if err != nil {
		return refusal.New(err.Error())
	}
	return nil
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

func newRootCommand(environment *commandEnvironment) *cobra.Command {
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
	root.SetOut(environment.out)
	root.SetErr(environment.errOut)
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(newStatusCommand(environment), newOrgCommand(environment), newTeamCommand(environment), newProjectCommand(environment), newInitCommand(environment), newDistillCommand(environment), newLintCommand(environment), newResetCommand(environment), newVersionCommand(environment))
	return root
}
