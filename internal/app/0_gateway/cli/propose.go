package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
)

func newProposeCommand(environment *commandEnvironment) *cobra.Command {
	var request orchestration.ProposeRequest
	var yes bool
	command := &cobra.Command{
		Use:   "propose <folder or zip>",
		Short: "Open a pull request that adds declarations to a level of the org repository",
		Long: "propose is the one way declarations enter an org repository. It takes a distillation folder\n" +
			"from rein distill, a plain level folder with rules/, skills/, mcpservers/, permissions/ and\n" +
			"hooks/, or an export zip; lints it; compares every candidate with what the organisation has\n" +
			"and leaves out the identical ones; writes the rest on a branch of the organisation's clone,\n" +
			"pushes it and opens the pull request, with the comparison as its body. A distillation knows\n" +
			"its level; anything else goes to your user level unless --level says otherwise. Your user\n" +
			"record is added when the org repository has none.",
		Example: "  rein propose ~/.rein/distill/shop-20261019-130000\n  rein propose ./my-rules --level organizations.acme.teams.web\n  rein propose export.zip",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			request.Source = arguments[0]
			return runPropose(environment, request, yes)
		},
	}
	command.Flags().StringVar(&request.Level, "level", "", "the level's address (default: the distillation's, else users.<you>)")
	command.Flags().StringVar(&request.Organization, "org", "", "the organisation's slug for a user level (default: the active one)")
	command.Flags().StringVar(&request.Title, "title", "", "the pull request's title")
	command.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return command
}

func runPropose(environment *commandEnvironment, request orchestration.ProposeRequest, yes bool) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return err
	}
	defer release()
	err = requireIdentity(orchestrator)
	if err != nil {
		return err
	}
	request.Source, err = filepath.Abs(request.Source)
	if err != nil {
		return err
	}
	if !yes {
		prompter := environment.prompter()
		if !prompter.CanAsk() {
			return refusal.New("no terminal to confirm on; pass --yes")
		}
		fmt.Fprintf(environment.out, "This will lint %s, write what the organisation does not have yet on a branch of its clone,\npush the branch and open a pull request on GitHub.\n", request.Source)
		confirmed, err := prompter.Confirm("Continue?", "Nothing reaches main: the pull request is reviewed by the code owners, and rein review checks it. If a step fails, the branch is removed again.")
		if err != nil {
			return refusal.New(err.Error())
		}
		if !confirmed {
			return refusal.New("nothing changed")
		}
	}
	result, err := orchestrator.ProposeDeclarations(request)
	if err != nil {
		return err
	}
	fmt.Fprintf(environment.out, "proposed %d declarations to %s on branch %s\n", len(result.Included), result.Level, result.Branch)
	for _, summary := range result.Included {
		line := fmt.Sprintf("  %-10s %s/%s", summary.Verdict, summary.Kind, summary.ID)
		if len(summary.Related) > 0 {
			line += "  same id at " + strings.Join(summary.Related, ", ")
		}
		fmt.Fprintln(environment.out, line)
	}
	for _, summary := range result.LeftOut {
		fmt.Fprintf(environment.out, "  left out   %s/%s  identical to %s\n", summary.Kind, summary.ID, strings.Join(summary.Related, ", "))
	}
	fmt.Fprintf(environment.out, "pull request: %s\n", result.PullRequestURL)
	return nil
}
