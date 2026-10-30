package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/adapters/localstate"
	"github.com/Danidani23/agents-rein/internal/adapters/terminal"
	"github.com/Danidani23/agents-rein/internal/app/0_gateway/cli"
	"github.com/Danidani23/agents-rein/internal/app/0_gateway/owner"
	"github.com/Danidani23/agents-rein/internal/ports"
	"github.com/Danidani23/agents-rein/internal/version"
)

// ownerCommand makes rein the background process of its home; the first
// command that needs one starts it.
const ownerCommand = "owner"

// appCommand starts the app of a build that carries it.
const appCommand = "app"

// backgroundFlag starts the app with its window closed, as the CLI and a
// start at login do.
const backgroundFlag = "--background"

// appStartWait is how long a command waits for the app it started to
// answer; a window takes longer to come up than rein owner.
const appStartWait = 30 * time.Second

// ownerIdle is how long the background process waits without a command
// before it ends; the next command starts it again.
const ownerIdle = 15 * time.Minute

// ownerStartWait is how long a command waits for the background process it
// started to answer.
const ownerStartWait = 10 * time.Second

// runRein is rein for this process's caller (D52, D55). A command that
// needs no machine state runs here; owner makes this process the
// background process; every other command is relayed to it. A build with
// the app is the app when started without a command, or with app.
func runRein(arguments []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	switch {
	case len(arguments) > 0 && arguments[0] == ownerCommand:
		return serveAsOwner(errOut)
	case len(arguments) > 0 && arguments[0] == appCommand:
		return runApp(arguments[1:], errOut)
	case appBuild && (len(arguments) == 0 || arguments[0] == backgroundFlag):
		return runApp(arguments, errOut)
	case runsHere(arguments):
		return cli.Run(arguments, in, out, errOut, wire)
	}
	return relay(arguments, in, out, errOut)
}

// runsHere says whether a command needs no machine state, so it runs in
// this process: version, lint and help.
func runsHere(arguments []string) bool {
	if len(arguments) == 0 {
		return true
	}
	for _, argument := range arguments {
		if argument == "-h" || argument == "--help" {
			return true
		}
	}
	switch arguments[0] {
	case "version", "lint", "help":
		return true
	}
	return false
}

// relay sends the command to the background process of rein's home and
// relays this process's streams and Ctrl-C to it, starting it when none
// answers or when the one that answers is of another build, which then
// ends once its running commands have. A third Ctrl-C quits here at once.
func relay(arguments []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	home, err := localstate.Home()
	if err != nil {
		fmt.Fprintf(errOut, "rein: %v\n", err)
		return cli.ExitCrashed
	}
	err = os.MkdirAll(home, 0o755)
	if err != nil {
		fmt.Fprintf(errOut, "rein: %v\n", err)
		return cli.ExitCrashed
	}
	socket := owner.SocketPath(home)
	executable, _ := os.Executable()
	folder, _ := os.Getwd()
	request := owner.Request{Identity: buildIdentity(), Arguments: arguments, Folder: folder, Environment: os.Environ(), Terminal: terminal.IsTerminal(in), Executable: executable}
	interrupts := make(chan struct{}, 1)
	signals := make(chan os.Signal, 3)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)
	go func() {
		count := 0
		for range signals {
			count++
			if count >= 3 {
				os.Exit(cli.ExitInterrupted)
			}
			select {
			case interrupts <- struct{}{}:
			default:
			}
		}
	}()
	for attempt := 0; ; attempt++ {
		code, err := owner.Relay(socket, request, in, out, errOut, interrupts)
		if err == nil {
			return code
		}
		var otherBuild owner.ErrOtherBuild
		if errors.As(err, &otherBuild) && otherBuild.Stays {
			fmt.Fprintf(errOut, "rein: rein's app on this machine is %s and this rein is %s; they work together at one version only: rein update brings this rein up to date, the app updates from its release\n", owner.VersionOf(otherBuild.Identity), version.String())
			return cli.ExitRefused
		}
		if attempt > 0 || !errors.Is(err, owner.ErrNoOwner) && !errors.As(err, &otherBuild) {
			fmt.Fprintf(errOut, "rein: %v; the background process's log is %s\n", err, ownerLog(home))
			return cli.ExitCrashed
		}
		err = startOwner(home, socket, executable)
		if err != nil {
			fmt.Fprintf(errOut, "rein: the background process did not start: %v; its log is %s\n", err, ownerLog(home))
			return cli.ExitCrashed
		}
	}
}

// startOwner starts the background process of home: the app, where it is
// installed, with its window closed, else rein owner; and waits until it
// answers, rein owner as this build.
func startOwner(home string, socket string, executable string) error {
	program, arguments, found := installedApp(execute.Runner{})
	wait := appStartWait
	var err error
	if found {
		err = execute.Runner{}.StartDetached(home, ownerLog(home), program, arguments...)
	} else {
		wait = ownerStartWait
		err = execute.Runner{}.StartDetached(home, ownerLog(home), executable, ownerCommand)
	}
	if err != nil {
		return err
	}
	deadline := time.Now().Add(wait)
	for {
		identity, err := owner.Identify(socket)
		if err == nil && (found || identity == buildIdentity()) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("it did not answer within %s", wait)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// installedApp is how to start the app with its window closed, where it is
// installed: REIN_APP names its program or, on macOS, its bundle, and none
// leaves it out; else the bundle in the Applications folders on macOS.
func installedApp(runner execute.Runner) (program string, arguments []string, found bool) {
	chosen := runner.Getenv("REIN_APP")
	if chosen == "none" {
		return "", nil, false
	}
	candidates := []string{chosen}
	if chosen == "" && runtime.GOOS == "darwin" {
		userHome, _ := os.UserHomeDir()
		candidates = []string{"/Applications/Rein.app", filepath.Join(userHome, "Applications", "Rein.app")}
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.IsDir() && filepath.Ext(candidate) == ".app" {
			return "open", []string{"-g", "-a", candidate, "--args", backgroundFlag}, true
		}
		return candidate, []string{backgroundFlag}, true
	}
	return "", nil, false
}

// serveAsOwner makes this process the background process of rein's home
// until it has had no command for ownerIdle. Another owner that already
// answers there leaves nothing to do.
func serveAsOwner(errOut io.Writer) int {
	home, err := localstate.Home()
	if err != nil {
		fmt.Fprintf(errOut, "rein owner: %v\n", err)
		return cli.ExitCrashed
	}
	err = os.MkdirAll(home, 0o755)
	if err != nil {
		fmt.Fprintf(errOut, "rein owner: %v\n", err)
		return cli.ExitCrashed
	}
	listener, err := owner.Listen(owner.SocketPath(home))
	if errors.Is(err, owner.ErrRunning) {
		return cli.ExitWorked
	}
	if err != nil {
		fmt.Fprintf(errOut, "rein owner: %v\n", err)
		return cli.ExitCrashed
	}
	server := &owner.Server{Identity: buildIdentity(), Run: runSession, Idle: ownerIdle, Log: errOut}
	err = server.Serve(listener)
	if err != nil {
		fmt.Fprintf(errOut, "rein owner: %v\n", err)
		return cli.ExitCrashed
	}
	return cli.ExitWorked
}

// appConfirmer is the app's window, which confirms what removes or
// overwrites (D53); nil unless this process is the app.
var appConfirmer ports.Confirmer

// runSession runs one command the background process was sent, for its
// caller: in the caller's folder, with the caller's environment, its
// programs stopped by the caller's Ctrl-C, its confirmations in the app's
// window where this process is the app.
func runSession(session owner.Session) (int, <-chan struct{}) {
	runner := execute.NewRunner(session.Environment)
	caller := cli.Caller{In: session.In, Out: session.Out, ErrOut: session.ErrOut, Terminal: session.Terminal, Folder: session.Folder, Runner: runner, Confirmer: appConfirmer}
	return cli.RunFor(caller, session.Arguments, session.Interrupts, wireFor(runner, session.Executable))
}

// buildIdentity tells this build from any other: its version and, since
// two builds of one version differ, the size and time of its binary.
func buildIdentity() string {
	identity := version.String()
	executable, err := os.Executable()
	if err != nil {
		return identity
	}
	info, err := os.Stat(executable)
	if err != nil {
		return identity
	}
	return fmt.Sprintf("%s %d %d", identity, info.Size(), info.ModTime().UnixNano())
}

// ownerLog is where the background process writes what it has to say.
func ownerLog(home string) string {
	return filepath.Join(home, "owner.log")
}
