// Package cli is the gateway a person uses: cobra commands that read flags,
// ask through the Prompter port, and print. A single operation goes straight
// to a service; several in order go to a workflow of the orchestrator (D17).
// It holds the one concrete Orchestrator (D18) and knows no adapter other than
// the terminal, which is its own device; main wires the rest. A command that
// acts for a user passes the identity gate first. Every byte of output goes
// through the writers Run receives, and a test enforces it.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
// is typing, the caller's folder and runner, and the orchestrator.
type commandEnvironment struct {
	in       io.Reader
	out      io.Writer
	errOut   io.Writer
	terminal bool
	// folder is where the caller's relative paths start; "" is this
	// process's working folder.
	folder string
	// runner runs programs for the caller and reads its environment.
	runner      execute.Runner
	orchestrate Orchestrate
	// arguments and ctx are the command as typed and the context that ends
	// when its caller is let go, set when it starts.
	arguments []string
	ctx       context.Context
	// pendingStep is the step whose start line is still open on the
	// terminal, waiting to be rewritten with its outcome.
	pendingStep string
	// spinner redraws the open line with the time elapsed while a step
	// runs; nil off a terminal.
	spinner *spinner
	// releaseLock gives the machine lock back with the command's outcome;
	// set while a command holds it, so an interrupt at a prompt leaves no
	// lock behind.
	releaseLock func(outcome error)
	locking     sync.Mutex
}

// setReleaseLock remembers the release of the machine lock.
func (environment *commandEnvironment) setReleaseLock(release func(outcome error)) {
	environment.locking.Lock()
	defer environment.locking.Unlock()
	environment.releaseLock = release
}

// holdsLock says whether the command holds the machine lock.
func (environment *commandEnvironment) holdsLock() bool {
	environment.locking.Lock()
	defer environment.locking.Unlock()
	return environment.releaseLock != nil
}

// releaseLockWith gives the machine lock back, if the command holds it,
// with how the command ended, which an identical command waiting for it
// ends with too (R3).
func (environment *commandEnvironment) releaseLockWith(outcome error) {
	environment.locking.Lock()
	defer environment.locking.Unlock()
	if environment.releaseLock != nil {
		environment.releaseLock(outcome)
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

// absolute is path from the caller's folder.
func (environment *commandEnvironment) absolute(path string) (string, error) {
	if filepath.IsAbs(path) || environment.folder == "" {
		return filepath.Abs(path)
	}
	return filepath.Join(environment.folder, path), nil
}

func (environment *commandEnvironment) prompter() ports.Prompter {
	return terminal.New(environment.in, environment.out, environment.terminal)
}

// orchestrateLocked is orchestrate for a command that changes the machine:
// it waits for its turn at the machine lock, saying what it waits for, and
// holds the lock until the command ends, so two commands never interleave
// a read and a write of the state (P2.24, D56). The lock goes back with the
// command's outcome when the command ends; release is for the command's
// defer and changes nothing. Ctrl-C while waiting leaves the line, nothing
// changed. A command identical to one in line ends as that one did (R3).
func (environment *commandEnvironment) orchestrateLocked() (orchestrator orchestration.Orchestrator, release func(), err error) {
	orchestrator, err = environment.orchestrate()
	if err != nil {
		return orchestration.Orchestrator{}, nil, err
	}
	release = func() {}
	if orchestrator.Lock == nil || environment.holdsLock() {
		orchestrator.Progress = environment.printProgress
		return orchestrator, release, nil
	}
	ctx := environment.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	acquired, err := orchestrator.Lock.Acquire(ctx, environment.lockHolder(), environment.printWaiting)
	if err != nil {
		return orchestration.Orchestrator{}, nil, err
	}
	environment.setReleaseLock(acquired)
	orchestrator.Progress = environment.printProgress
	return orchestrator, release, nil
}

// lockHolder is the command as the machine lock tells others of it: as
// typed, from which caller, in which folder.
func (environment *commandEnvironment) lockHolder() ports.LockHolder {
	caller := "a program without a terminal"
	for _, argument := range environment.arguments {
		if argument == "--hook" {
			caller = "the session-start hook"
		}
	}
	if environment.terminal {
		caller = "a terminal"
	}
	return ports.LockHolder{Command: strings.TrimSpace("rein " + strings.Join(environment.arguments, " ")), Caller: caller, Folder: environment.folder}
}

// printWaiting says what the command waits for, each time that changes.
func (environment *commandEnvironment) printWaiting(wait ports.LockWait) {
	style := environment.style()
	if wait.Joining {
		fmt.Fprintf(environment.errOut, "%s\n", style.Yellow(fmt.Sprintf("the same command is running: %s; waiting for it instead of running it twice, Ctrl-C stops waiting", wait.Holder)))
		return
	}
	more := ""
	if wait.Ahead > 1 {
		more = fmt.Sprintf(", %d commands ahead", wait.Ahead)
	}
	fmt.Fprintf(environment.errOut, "%s\n", style.Yellow(fmt.Sprintf("waiting for %s%s; Ctrl-C leaves the line", wait.Holder, more)))
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

// Run executes rein with arguments for this process and returns the exit
// code. Ctrl-C stops the running program so the workflow undoes what it
// did; a second Ctrl-C quits at once.
func Run(arguments []string, in io.Reader, out io.Writer, errOut io.Writer, orchestrate Orchestrate) int {
	folder, _ := os.Getwd()
	environment := newEnvironment(Caller{In: in, Out: out, ErrOut: errOut, Terminal: terminal.IsTerminal(in), Folder: folder}, orchestrate)
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)
	go func() {
		first := true
		for range signals {
			if !first || !execute.Running() {
				environment.releaseLockWith(execute.ErrInterrupted)
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
	return runCommandWithin(context.Background(), environment, arguments)
}

// runCommandWithin runs the command; a command that serves until it is
// stopped, ui or mcp, stops when ctx ends.
func runCommandWithin(ctx context.Context, environment *commandEnvironment, arguments []string) int {
	environment.arguments, environment.ctx = arguments, ctx
	root := newRootCommand(environment)
	root.SetArgs(arguments)
	err := root.ExecuteContext(ctx)
	environment.releaseLockWith(err)
	if err == nil {
		return ExitWorked
	}
	var joined ports.Joined
	if errors.As(err, &joined) && joined.Outcome == nil {
		fmt.Fprintf(environment.out, "%s\n", joined.Error())
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
			"agent reads. It is a compiler and a linter that runs on this machine, never a hosted\n" +
			"service.\n\n" +
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
	root.AddCommand(newStatusCommand(environment), newLoginCommand(environment), newLogoutCommand(environment), newGitCredentialCommand(environment), newOrgCommand(environment), newTeamCommand(environment), newProjectCommand(environment), newLabelCommand(environment), newDeclarationCommand(environment), newUpdateCommand(environment), newInitCommand(environment), newSyncCommand(environment), newDistillCommand(environment), newProposeCommand(environment), newResolveCommand(environment), newKeyCommand(environment), newLintCommand(environment), newMCPCommand(environment), newUICommand(environment), newResetCommand(environment), newVersionCommand(environment))
	return root
}
