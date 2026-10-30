package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/0_gateway/wording"
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
	plan, err := orchestrator.PlanUpdate()
	if err != nil {
		return err
	}
	lines, summary, explanation, refused := wording.Update(plan)
	if !plan.Newer || check {
		fmt.Fprintf(environment.out, "%s\n", lines[0])
		return nil
	}
	for _, line := range lines {
		fmt.Fprintf(environment.out, "%s\n", line)
	}
	if refused != "" {
		return refusal.New(refused)
	}
	if summary == "" {
		return nil
	}
	fmt.Fprintf(environment.out, "%s\n", summary)
	if !yes {
		prompter := environment.prompter()
		if !prompter.CanAsk() {
			return refusal.New("no terminal to confirm on; pass --yes")
		}
		confirmed, err := prompter.Confirm("Continue?", explanation)
		if err != nil {
			return refusal.New(err.Error())
		}
		if !confirmed {
			return refusal.New("nothing changed")
		}
	}
	replaced, version, err := orchestrator.Update()
	if err != nil {
		return err
	}
	fmt.Fprintf(environment.out, "%s\n", wording.Updated(replaced, version))
	return nil
}
