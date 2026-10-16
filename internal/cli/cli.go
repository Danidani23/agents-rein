// Package cli is the driving adapter: cobra commands that read flags, ask
// through the Prompter port, call a use case in internal/app and print its
// result. Every byte of output goes through the writers Run receives; nothing
// in this module writes to the process streams directly except main, and a
// test enforces it.
package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/adapters/claudecode"
	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/adapters/git"
	"github.com/Danidani23/agents-rein/internal/adapters/github"
	"github.com/Danidani23/agents-rein/internal/adapters/localstate"
	"github.com/Danidani23/agents-rein/internal/adapters/terminal"
	"github.com/Danidani23/agents-rein/internal/app"
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

// environment is what every command receives: the streams, whether a person
// is typing, and the wired dependencies.
type environment struct {
	in           io.Reader
	out          io.Writer
	errOut       io.Writer
	terminal     bool
	dependencies func() (app.Dependencies, error)
}

func (env *environment) prompter() ports.Prompter {
	return terminal.New(env.in, env.out, env.terminal)
}

// Run executes rein with arguments and returns the exit code.
func Run(arguments []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	env := &environment{in: in, out: out, errOut: errOut, terminal: terminal.IsTerminal(in), dependencies: realDependencies}
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
	if errors.Is(err, app.ErrRefused) || errors.Is(err, errRefusedByCommand) {
		return ExitRefused
	}
	if isUsageError(err) {
		return ExitRefused
	}
	return ExitCrashed
}

// errRefusedByCommand marks a refusal decided in the CLI itself, such as a
// missing terminal or a declined confirmation.
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

// realDependencies wires the adapters of this machine.
func realDependencies() (app.Dependencies, error) {
	home, err := localstate.Home()
	if err != nil {
		return app.Dependencies{}, err
	}
	return app.Dependencies{
		Home:       home,
		Host:       github.Host{},
		Repository: git.Repository{},
		Agents:     []ports.Agent{claudecode.Agent{}},
		State:      localstate.Store{Home: home},
	}, nil
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
