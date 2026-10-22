package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
)

func newUpdateCommand(environment *commandEnvironment) *cobra.Command {
	var check, yes bool
	command := &cobra.Command{
		Use:   "update",
		Short: "Update rein to the latest release, or say how, depending on how it was installed",
		Long: "update asks GitHub for the latest release of rein. A rein installed by Homebrew, Scoop or go\n" +
			"install is updated by that tool, and update prints its command. A rein installed by the\n" +
			"install script, under ~/.local/bin, is updated by rein itself: the release's archive for this\n" +
			"machine is downloaded, checked against the release's checksum file, and put in place of the\n" +
			"running binary. --check only reports whether a newer release exists.",
		Example: "  rein update --check\n  rein update",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runUpdate(environment, check, yes)
		},
	}
	command.Flags().BoolVar(&check, "check", false, "report whether a newer release exists and stop")
	command.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return command
}

func runUpdate(environment *commandEnvironment, check bool, yes bool) error {
	orchestrator, err := environment.orchestrate()
	if err != nil {
		return err
	}
	service := orchestrator.Updates
	checked, err := service.CheckNow()
	if err != nil {
		return refusal.New("the latest release could not be read: " + err.Error())
	}
	if !checked.Newer {
		if checked.Latest != checked.Current {
			fmt.Fprintf(environment.out, "rein %s is the latest release; this build, %s, is newer than it\n", checked.Latest, checked.Current)
			return nil
		}
		fmt.Fprintf(environment.out, "rein %s is the latest release, and this is it\n", checked.Latest)
		return nil
	}
	fmt.Fprintf(environment.out, "rein %s is available; this is %s\n", checked.Latest, checked.Current)
	if check {
		return nil
	}
	install := service.HowInstalled()
	if install.Command != "" {
		fmt.Fprintf(environment.out, "this rein was installed by %s, which updates it:\n\n  %s\n", install.Kind, install.Command)
		return nil
	}
	if !install.Self {
		return refusal.New(fmt.Sprintf("this rein at %s was not installed by a package manager or the install script, so rein does not replace it; install the new release the way this one was installed", service.Executable))
	}
	if !service.Writable() {
		return refusal.New(fmt.Sprintf("%s cannot be replaced: its folder is not writable", service.Executable))
	}
	fmt.Fprintf(environment.out, "This will download rein %s for this machine, verify it against the release's checksum file, and put it in place of %s.\n", checked.Latest, service.Executable)
	if !yes {
		prompter := environment.prompter()
		if !prompter.CanAsk() {
			return refusal.New("no terminal to confirm on; pass --yes")
		}
		confirmed, err := prompter.Confirm("Continue?", "The archive comes from the GitHub release of Danidani23/agents-rein. A build that fails the checksum is not installed. The running command finishes; the next rein is the new one.")
		if err != nil {
			return refusal.New(err.Error())
		}
		if !confirmed {
			return refusal.New("nothing changed")
		}
	}
	latest, err := service.Releases.Latest()
	if err != nil {
		return refusal.New(err.Error())
	}
	replaced, err := service.SelfUpdate(latest)
	if err != nil {
		return refusal.New(err.Error())
	}
	fmt.Fprintf(environment.out, "updated %s to rein %s\n", replaced, latest.Version)
	return nil
}
