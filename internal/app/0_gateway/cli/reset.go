package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
)

const resetExplanation = "A placement is one link (a copy on Windows) that rein made in your agent's home folder,\n" +
	"for Claude Code ~/.claude: CLAUDE.md, rules/<id>.md for a rule and skills/<id> for a skill.\n" +
	"Through those links the agent read your settings. reset removes exactly the ones rein\n" +
	"recorded, dangling ones included; a link that points somewhere rein did not record is left\n" +
	"alone and reported. Nothing outside ~/.rein and the placed links is touched. In the current\n" +
	"design rein writes nothing into the agent's home: every level reaches a project clone through\n" +
	"rein sync, so after reset nothing needs bringing back."

func newResetCommand(environment *commandEnvironment) *cobra.Command {
	var all, yes bool
	command := &cobra.Command{
		Use:   "reset",
		Short: "Remove everything rein placed into your agent's home; --all forgets this machine entirely",
		Long: "reset removes every link and copy rein made in the agent's home, dangling ones included,\n" +
			"and clears them from state, together with any imports left under ~/.rein/imports/ from\n" +
			"before import went through the org repository. It touches nothing it did not place: a link\n" +
			"that points somewhere else than rein recorded is left alone and reported. With --all it\n" +
			"also forgets the connected organisations, deletes their clones and the project\n" +
			"registrations, which leaves the machine as before rein was first used; the remotes are\n" +
			"never touched. It confirms before deleting; --yes skips that.",
		Example: "  rein reset\n  rein reset --all --yes",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runReset(environment, all, yes)
		},
	}
	command.Flags().BoolVar(&all, "all", false, "also forget organisations, delete their clones and the project registrations")
	command.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return command
}

func runReset(environment *commandEnvironment, all bool, yes bool) error {
	orchestrator, err := environment.orchestrate()
	if err != nil {
		return err
	}
	plan, err := orchestrator.PlanReset(all)
	if err != nil {
		return err
	}
	fmt.Fprintf(environment.out, "This will remove:\n")
	fmt.Fprintf(environment.out, "- %d placements: the links rein made in your agent's home so the agent reads your rules\n  and skills. Removing them takes those away from the agent; your own files are untouched.\n", len(plan.Placements))
	for _, placed := range plan.Placements {
		fmt.Fprintf(environment.out, "    %s\n", placed.Path)
	}
	if len(plan.Imports) > 0 {
		fmt.Fprintf(environment.out, "- %d imports: copies under %s that the placements point at,\n  from before import went through the org repository.\n", len(plan.Imports), plan.ImportsFolder)
	}
	if plan.All {
		fmt.Fprintf(environment.out, "- %d organisations with their clones under %s, and %d project registrations:\n  what this machine knows about them. The remotes on GitHub are not touched.\n", len(plan.Organizations), plan.OrgClonesFolder, len(plan.Projects))
	}
	if !yes {
		prompter := environment.prompter()
		if !prompter.CanAsk() {
			return refusal.New("no terminal to confirm on; pass --yes")
		}
		confirmed, err := prompter.Confirm("Continue?", resetExplanation)
		if err != nil {
			return refusal.New(err.Error())
		}
		if !confirmed {
			return refusal.New("nothing changed")
		}
	}
	result, err := orchestrator.ResetMachine(plan)
	if err != nil {
		return err
	}
	for _, unplaced := range result.Placements {
		switch {
		case unplaced.Removed:
			fmt.Fprintf(environment.out, "removed  %s\n", unplaced.Path)
		case unplaced.Reason != "":
			fmt.Fprintf(environment.errOut, "skipped  %s: %s\n", unplaced.Path, unplaced.Reason)
		default:
			fmt.Fprintf(environment.out, "gone     %s\n", unplaced.Path)
		}
	}
	for _, organizationID := range result.RemovedOrgClones {
		fmt.Fprintf(environment.out, "removed  clone %s\n", organizationID)
	}
	fmt.Fprintf(environment.out, "reset done; %d placements kept because they were not rein's\n", result.Kept)
	if result.Kept > 0 {
		return refusal.New(fmt.Sprintf("%d placements skipped", result.Kept))
	}
	return nil
}
