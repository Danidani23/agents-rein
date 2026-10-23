package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/ports"
)

func newProposeCommand(environment *commandEnvironment) *cobra.Command {
	var request orchestration.ProposeRequest
	var yes bool
	command := &cobra.Command{
		Use:   "propose [proposal file, folder or zip]",
		Short: "Open a pull request that adds declarations to the org repository",
		Long: "propose is the one way declarations enter an org repository. It takes the proposal file rein\n" +
			"distill wrote, which says for every candidate whether it goes, to which level and with which\n" +
			"labels, and does exactly that: lints the candidates, leaves out the ones identical to what the\n" +
			"organisation has, writes the rest on a branch of the organisation's clone at their levels,\n" +
			"pushes it and opens the pull request with the comparison as its body. The proposal is then\n" +
			"marked proposed and moved to ~/.rein/proposals/ as the history. Inside a project folder with\n" +
			"one pending proposal, no argument is needed; with several, it asks which.\n\n" +
			"A plain level folder with rules/, skills/, mcpservers/, permissions/ and hooks/, or an export\n" +
			"zip, goes whole to your user level unless --level says otherwise. Your user record is added\n" +
			"when the org repository has none.",
		Example: "  rein propose\n  rein propose .rein/proposals/proposal-3f9c2e1a7b4d.yaml\n  rein propose ./my-rules --level organizations.acme.teams.web\n  rein propose export.zip",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			if len(arguments) == 1 {
				request.Source = arguments[0]
			}
			return runPropose(environment, request, yes)
		},
	}
	command.Flags().StringVar(&request.Level, "level", "", "for a folder or a zip: the level's address (default: users.<you>)")
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
	err = requireLogin(orchestrator)
	if err != nil {
		return err
	}
	prompter := environment.prompter()
	if request.Source == "" {
		request.Source, err = pendingProposalSource(prompter, orchestrator)
		if err != nil {
			return err
		}
	}
	request.Source, err = filepath.Abs(request.Source)
	if err != nil {
		return err
	}
	if !yes {
		if !prompter.CanAsk() {
			return refusal.New("no terminal to confirm on; pass --yes")
		}
		plan, err := orchestrator.PlanProposal(request)
		if err != nil {
			return err
		}
		fmt.Fprintf(environment.out, "This will write %d declarations from %s on a branch of the org clone of %s, push the branch\nand open a pull request on GitHub:\n", len(plan.Included), request.Source, plan.Organization)
		printPlacement(environment, plan)
		confirmed, err := prompter.Confirm("Continue?", "Nothing reaches main: the pull request is reviewed by the code owners, and rein review checks it. If a step fails, the branch is removed again. To change a level, a label or a file, answer n and edit the proposal file.")
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
	fmt.Fprintf(environment.out, "proposed %d declarations on branch %s\n", len(result.Included), result.Branch)
	printPlacement(environment, result.ProposalPlan)
	style := environment.style()
	fmt.Fprintf(environment.out, "\npull request: %s\n", style.Bold(style.Green(result.PullRequestURL)))
	if result.HistoryFile != "" {
		fmt.Fprintf(environment.out, "the proposal moved to %s\n", result.HistoryFile)
	}
	printNext(environment, "review and merge it, then rein sync in each clone of its projects")
	return nil
}

// pendingProposalSource picks the pending proposal to propose: the one in
// the current project folder, or on a terminal one of several.
func pendingProposalSource(prompter ports.Prompter, orchestrator orchestration.Orchestrator) (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	pending, err := orchestrator.PendingProposals(directory)
	if err != nil {
		return "", err
	}
	switch {
	case len(pending) == 0:
		return "", refusal.New("no pending proposal here; rein distill writes one, or name a proposal file, a folder or a zip")
	case len(pending) == 1:
		return pending[0].Path, nil
	case !prompter.CanAsk():
		var paths []string
		for _, entry := range pending {
			paths = append(paths, entry.Path)
		}
		return "", refusal.New("several proposals are pending; name one: " + strings.Join(paths, ", "))
	}
	var choices []ports.Choice
	for _, entry := range pending {
		choices = append(choices, ports.Choice{Name: filepath.Base(entry.Path), Description: fmt.Sprintf("%d candidates for %s, distilled %s", entry.Candidates, entry.Level, entry.DistilledAt.Local().Format("2006-01-02 15:04"))})
	}
	picked, err := prompter.Choose(ports.Select{Title: "Proposal", Description: "The pending proposal to open the pull request for.", Choices: choices})
	if err != nil {
		return "", refusal.New(err.Error())
	}
	for _, entry := range pending {
		if filepath.Base(entry.Path) == picked {
			return entry.Path, nil
		}
	}
	return "", refusal.New("no such proposal")
}

// printPlacement lists each candidate with its verdict, level and labels,
// the ones the proposal excludes, and the ones left out as identical.
func printPlacement(environment *commandEnvironment, plan orchestration.ProposalPlan) {
	for _, planned := range plan.Included {
		line := fmt.Sprintf("  %-10s %s/%s  at %s", planned.Verdict, planned.Kind, planned.ID, planned.Level)
		if len(planned.ApplyIf) > 0 {
			line += ", applyIf " + strings.Join(planned.ApplyIf, " ")
		}
		if len(planned.Related) > 0 {
			line += "  same id at " + strings.Join(planned.Related, ", ")
		}
		fmt.Fprintln(environment.out, line)
	}
	for _, planned := range plan.Excluded {
		fmt.Fprintf(environment.out, "  excluded   %s/%s  %s\n", planned.Kind, planned.ID, orNotJudged(planned.Judge))
	}
	for _, planned := range plan.LeftOut {
		fmt.Fprintf(environment.out, "  left out   %s/%s  identical to %s\n", planned.Kind, planned.ID, strings.Join(planned.Related, ", "))
	}
}

func orNotJudged(text string) string {
	if text == "" {
		return "by the proposal"
	}
	return text
}
