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
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/adapters/terminal"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Exit codes: 0 worked, 1 refused (a precondition or a check failed), 2
// crashed (an error the command did not expect), 3 a program rein ran
// failed, 130 interrupted by Ctrl-C after undoing what was done.
const (
	ExitWorked      = 0
	ExitRefused     = 1
	ExitCrashed     = 2
	ExitToolFailed  = 3
	ExitInterrupted = 130
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
	// pendingStep is the step whose start line is still open on the
	// terminal, waiting to be rewritten with its outcome.
	pendingStep string
	// spinner redraws the open line with the time elapsed while a step
	// runs; nil off a terminal.
	spinner *spinner
	// releaseLock gives the machine lock back; set while a command holds
	// it, so an interrupt at a prompt leaves no lock behind.
	releaseLock func()
	locking     sync.Mutex
}

// setReleaseLock remembers, or forgets, the release of the machine lock.
func (environment *commandEnvironment) setReleaseLock(release func()) {
	environment.locking.Lock()
	defer environment.locking.Unlock()
	environment.releaseLock = release
}

// releaseLockNow gives the machine lock back if a command holds it.
func (environment *commandEnvironment) releaseLockNow() {
	environment.locking.Lock()
	defer environment.locking.Unlock()
	if environment.releaseLock != nil {
		environment.releaseLock()
		environment.releaseLock = nil
	}
}

// spinner redraws a step's open line a few times a second with a glyph
// and the seconds elapsed, so a long step never looks frozen.
type spinner struct {
	stop chan struct{}
	done chan struct{}
	// note is how far the step has come, drawn after its name.
	note   string
	noting sync.Mutex
}

// spinnerFrames are the glyphs, one per tick.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinnerInterval is the time between two redraws.
const spinnerInterval = 120 * time.Millisecond

// startSpinner begins redrawing the open line with text and the elapsed
// time until stopSpinner is called.
func (environment *commandEnvironment) startSpinner(text string) {
	environment.stopSpinner()
	started := time.Now()
	current := &spinner{stop: make(chan struct{}), done: make(chan struct{})}
	environment.spinner = current
	style := environment.style()
	go func() {
		defer close(current.done)
		ticker := time.NewTicker(spinnerInterval)
		defer ticker.Stop()
		frame := 0
		for {
			select {
			case <-current.stop:
				return
			case <-ticker.C:
				elapsed := time.Since(started).Round(time.Second)
				current.noting.Lock()
				note := current.note
				current.noting.Unlock()
				if note != "" {
					note = style.Dim(note) + " "
				}
				fmt.Fprintf(environment.out, "\r\033[2K  %s %s %s%s", style.Dim(spinnerFrames[frame%len(spinnerFrames)]), style.Dim(text+" …"), note, style.Dim(elapsed.String()))
				frame++
			}
		}
	}()
}

// stopSpinner ends the redrawing and waits for the last frame to be out,
// so the outcome line overwrites it cleanly.
func (environment *commandEnvironment) stopSpinner() {
	if environment.spinner == nil {
		return
	}
	close(environment.spinner.stop)
	<-environment.spinner.done
	environment.spinner = nil
}

func (environment *commandEnvironment) prompter() ports.Prompter {
	return terminal.New(environment.in, environment.out, environment.terminal)
}

// lockWait is how long a writing command waits for another rein to finish.
const lockWait = 5 * time.Second

// orchestrateLocked is orchestrate for a command that changes the machine:
// it holds the machine lock until release is called, so two reins never
// interleave a read and a write of the state (P2.24). A refused lock is a
// refusal, nothing changed.
func (environment *commandEnvironment) orchestrateLocked() (orchestrator orchestration.Orchestrator, release func(), err error) {
	orchestrator, err = environment.orchestrate()
	if err != nil {
		return orchestration.Orchestrator{}, nil, err
	}
	if orchestrator.Lock == nil {
		return orchestrator, func() {}, nil
	}
	acquired, err := orchestrator.Lock.Acquire(lockWait)
	if err != nil {
		return orchestration.Orchestrator{}, nil, refusal.New(err.Error())
	}
	environment.setReleaseLock(acquired)
	release = func() {
		environment.releaseLockNow()
	}
	orchestrator.Progress = environment.printProgress
	return orchestrator, release, nil
}

// printNext ends a command with what comes next, bold after a blank line,
// the way the thing it produced stands out in green above it.
func printNext(environment *commandEnvironment, text string) {
	fmt.Fprintf(environment.out, "\n%s\n", environment.style().Bold("next: "+text))
}

// style paints for a terminal and not for a file.
func (environment *commandEnvironment) style() terminal.Style {
	return terminal.Style{Enabled: environment.terminal}
}

// printProgress shows a workflow's steps. On a terminal a step's line is
// printed when it starts and rewritten with its outcome and duration when
// it ends; elsewhere only the outcome lines are printed, so a file shows
// each step once.
func (environment *commandEnvironment) printProgress(event orchestration.ProgressEvent) {
	style := environment.style()
	elapsed := event.Elapsed.Round(100 * time.Millisecond).String()
	switch event.Kind {
	case orchestration.ProgressStarted:
		environment.openLine(event.Step, style.Dim(event.Step+" …"))
	case orchestration.ProgressDone:
		environment.closeLine(event.Step, style.Green("✓ "+event.Step+" ("+elapsed+")"))
	case orchestration.ProgressFailed:
		environment.closeLine(event.Step, style.Red("✗ "+event.Step+": "+event.Error))
	case orchestration.ProgressUndoing:
		environment.openLine(event.Step, style.Dim("↩ undoing "+event.Step+" …"))
	case orchestration.ProgressUndone:
		environment.closeLine(event.Step, style.Yellow("↩ undone "+event.Step+" ("+elapsed+")"))
	case orchestration.ProgressDetail:
		environment.detailLine(event.Step, style.Dim(event.Note))
	case orchestration.ProgressNote:
		if environment.terminal && environment.pendingStep == event.Step && environment.spinner != nil {
			environment.spinner.noting.Lock()
			environment.spinner.note = event.Note
			environment.spinner.noting.Unlock()
		}
	}
}

// detailLine prints what a running step does on a line of its own, above
// the step's open line on a terminal, which goes on under it.
func (environment *commandEnvironment) detailLine(step string, text string) {
	if !environment.terminal || environment.pendingStep != step {
		fmt.Fprintf(environment.out, "    %s\n", text)
		return
	}
	environment.stopSpinner()
	fmt.Fprintf(environment.out, "\r\033[2K    %s\n  %s", text, environment.style().Dim(step+" …"))
	environment.startSpinner(step)
}

// openLine starts a step's line on a terminal, without ending it, so the
// outcome can replace it. A line still open from an enclosing step is ended
// first and stays as it is; its outcome gets a line of its own. Off a
// terminal nothing is printed until the outcome.
func (environment *commandEnvironment) openLine(step string, text string) {
	if !environment.terminal {
		return
	}
	if environment.pendingStep != "" {
		environment.stopSpinner()
		fmt.Fprint(environment.out, "\n")
	}
	fmt.Fprintf(environment.out, "  %s", text)
	environment.pendingStep = step
	environment.startSpinner(step)
}

// closeLine prints a step's outcome: over its own open line on a terminal,
// on a new line otherwise.
func (environment *commandEnvironment) closeLine(step string, text string) {
	if environment.terminal && environment.pendingStep == step {
		environment.stopSpinner()
		fmt.Fprintf(environment.out, "\r\033[2K  %s\n", text)
		environment.pendingStep = ""
		return
	}
	if environment.terminal && environment.pendingStep != "" {
		environment.stopSpinner()
		fmt.Fprint(environment.out, "\n")
		environment.pendingStep = ""
	}
	fmt.Fprintf(environment.out, "  %s\n", text)
}

// Run executes rein with arguments and returns the exit code. Ctrl-C stops
// the running program so the workflow undoes what it did; a second Ctrl-C
// quits at once.
func Run(arguments []string, in io.Reader, out io.Writer, errOut io.Writer, orchestrate Orchestrate) int {
	environment := &commandEnvironment{in: in, out: out, errOut: errOut, terminal: terminal.IsTerminal(in), orchestrate: orchestrate}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)
	go func() {
		first := true
		for range signals {
			if !first || !execute.Running() {
				environment.releaseLockNow()
				os.Exit(ExitInterrupted)
			}
			first = false
			fmt.Fprintf(errOut, "\ninterrupted; undoing what was done, press Ctrl-C again to quit at once\n")
			execute.Interrupt()
		}
	}()
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
	if errors.Is(err, execute.ErrInterrupted) {
		return ExitInterrupted
	}
	var failure *execute.Failure
	if errors.As(err, &failure) {
		return ExitToolFailed
	}
	if errors.Is(err, refusal.ErrRefused) || isUsageError(err) {
		return ExitRefused
	}
	return ExitCrashed
}

// requireLogin is the gate in front of every command that acts for a user:
// without a login on this machine it refuses and names the way in (D25).
func requireLogin(orchestrator orchestration.Orchestrator) error {
	logins, err := orchestrator.Credentials.Logins()
	if err != nil {
		return err
	}
	if len(logins) == 0 {
		return refusal.New("not logged in; run rein login, which opens the GitHub device flow")
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
			"Your identity is your GitHub account: rein login signs you in through the Agents Rein app and\n" +
			"keeps the token in your keychain. Start with rein org create, or rein org connect for an\n" +
			"organisation that exists. rein status shows what has been set up, rein update keeps rein\n" +
			"current. Every\n" +
			"command explains itself with --help.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(environment.out)
	root.SetErr(environment.errOut)
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(newStatusCommand(environment), newLoginCommand(environment), newLogoutCommand(environment), newGitCredentialCommand(environment), newOrgCommand(environment), newTeamCommand(environment), newProjectCommand(environment), newLabelCommand(environment), newDeclarationCommand(environment), newUpdateCommand(environment), newInitCommand(environment), newSyncCommand(environment), newDistillCommand(environment), newProposeCommand(environment), newResolveCommand(environment), newLintCommand(environment), newMCPCommand(environment), newResetCommand(environment), newVersionCommand(environment))
	return root
}
