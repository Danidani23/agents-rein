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
			"record is added when the org repository has none.\n\n" +
			"A distillation carries the judge's view of each candidate, and propose applies it: a\n" +
			"candidate the judge lifted goes to that level, the organisation, the project's team or\n" +
			"your user level, with the labels it should apply to in its frontmatter; one the judge\n" +
			"dropped stays out. The placement is printed before the confirmation and in the pull\n" +
			"request. --as-distilled keeps everything at the distilled level and leaves the judge's\n" +
			"view as advice; --team names the team when the project belongs to several.",
		Example: "  rein propose ~/.rein/distill/shop-20261019-130000\n  rein propose ~/.rein/distill/shop-20261019-130000 --as-distilled\n  rein propose ./my-rules --level organizations.acme.teams.web\n  rein propose export.zip",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			request.Source = arguments[0]
			return runPropose(environment, request, yes)
		},
	}
	command.Flags().StringVar(&request.Level, "level", "", "the level's address (default: the distillation's, else users.<you>)")
	command.Flags().StringVar(&request.Organization, "org", "", "the organisation's slug for a user level (default: the active one)")
	command.Flags().StringVar(&request.Title, "title", "", "the pull request's title")
	command.Flags().BoolVar(&request.AsDistilled, "as-distilled", false, "keep every candidate at the distilled level; the judge's placement stays advice")
	command.Flags().StringVar(&request.Team, "team", "", "the team a lift to the team level goes to, when the project belongs to several")
	command.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return command
}

func runPropose(environment *commandEnvironment, request orchestration.ProposeRequest, yes bool) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return err
	}
	defer release()
	err = requireLogin(orchestrator)
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
		plan, err := orchestrator.PlanProposal(request)
		if err != nil {
			return err
		}
		fmt.Fprintf(environment.out, "This will write %d declarations from %s on a branch of the org clone of %s, push the branch\nand open a pull request on GitHub:\n", len(plan.Included), request.Source, plan.Organization)
		printPlacement(environment, plan)
		confirmed, err := prompter.Confirm("Continue?", "Nothing reaches main: the pull request is reviewed by the code owners, and rein review checks it. If a step fails, the branch is removed again. A lift or a drop follows the judge; --as-distilled keeps every candidate at the distilled level.")
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
	printPlacement(environment, result.ProposalPlan)
	fmt.Fprintf(environment.out, "pull request: %s\n", result.PullRequestURL)
	return nil
}

// printPlacement lists each candidate with its verdict and level, the ones
// lifted with the judge's reason, the dropped and the left out.
func printPlacement(environment *commandEnvironment, plan orchestration.ProposalPlan) {
	lifted := map[string]orchestration.Placement{}
	for _, placement := range plan.Lifted {
		lifted[placement.Kind+"/"+placement.ID] = placement
	}
	for _, summary := range plan.Included {
		line := fmt.Sprintf("  %-10s %s/%s  at %s", summary.Verdict, summary.Kind, summary.ID, orchestration.LevelOf(summary))
		if placement, moved := lifted[summary.Kind+"/"+summary.ID]; moved {
			line += "  lifted"
			if len(placement.ApplyIf) > 0 {
				line += ", applyIf " + strings.Join(placement.ApplyIf, " ")
			}
			line += ": " + placement.Reason
		}
		if len(summary.Related) > 0 {
			line += "  same id at " + strings.Join(summary.Related, ", ")
		}
		fmt.Fprintln(environment.out, line)
	}
	for _, placement := range plan.Dropped {
		fmt.Fprintf(environment.out, "  dropped    %s/%s  %s\n", placement.Kind, placement.ID, placement.Reason)
	}
	for _, summary := range plan.LeftOut {
		fmt.Fprintf(environment.out, "  left out   %s/%s  identical to %s\n", summary.Kind, summary.ID, strings.Join(summary.Related, ", "))
	}
}
