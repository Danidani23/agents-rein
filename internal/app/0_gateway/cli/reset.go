package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
)

const resetExplanation = "reset machine is reset project for everything, then forgetting. Every initialised project is put\n" +
	"back as rein reset project does: the files init moved out return from their snapshots, the rein block\n" +
	"leaves .gitignore, and the put-back is committed. Every link or copy rein made in an agent's\n" +
	"home is removed, dangling ones included; one that points somewhere rein did not record is\n" +
	"left alone and reported. Then the organisations are forgotten and their clones deleted, the\n" +
	"proposals removed, and rein's store, with the ledger and the snapshots, is erased with its key;\n" +
	"your logins stay, their tokens in the keychain. The remotes on\n" +
	"GitHub are never touched. A project whose restore fails stops the reset before anything is\n" +
	"wiped, so no snapshot is ever lost."

// newResetCommand takes rein back, always with a scope: a destructive
// command has no silent default (D36).
func newResetCommand(environment *commandEnvironment) *cobra.Command {
	command := &cobra.Command{
		Use:   "reset",
		Short: "Take rein back from one project clone or from this machine",
		Long: "reset takes rein back, and always says how far: reset project puts one clone back as it\n" +
			"was before rein init, reset machine does that for every clone and then removes everything\n" +
			"else rein placed on this machine. reset alone changes nothing.",
		Example: "  rein reset project\n  rein reset machine",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return refusal.New("reset needs a scope: rein reset project puts this clone back as before rein init, rein reset machine takes everything rein placed off this machine")
		},
	}
	command.AddCommand(newResetProjectCommand(environment), newResetMachineCommand(environment))
	return command
}

func newResetMachineCommand(environment *commandEnvironment) *cobra.Command {
	var yes bool
	command := &cobra.Command{
		Use:   "machine",
		Short: "Put every project back, remove everything rein placed, and forget this machine",
		Long: "reset machine leaves the machine as before rein was first used: it puts back every initialised\n" +
			"project, committing the put-back, removes every link and copy rein made in the agents'\n" +
			"homes, forgets the connected organisations, deletes their clones, removes the proposals,\n" +
			"and erases rein's store, with the ledger and the snapshots, and its key; the logins stay.\n" +
			"The remotes are never touched. It shows the plan and\n" +
			"confirms before changing anything; --yes skips that.",
		Example: "  rein reset machine\n  rein reset machine --yes",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runReset(environment, yes)
		},
	}
	command.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return command
}

func runReset(environment *commandEnvironment, yes bool) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return err
	}
	defer release()
	plan, err := orchestrator.PlanReset()
	if err != nil {
		return err
	}
	fmt.Fprintf(environment.out, "This will:\n")
	fmt.Fprintf(environment.out, "- put back %d initialised projects and commit the put-back in each:\n", len(plan.ProjectClones))
	for _, project := range plan.ProjectClones {
		fmt.Fprintf(environment.out, "    %s  (%s)\n", project.ClonePath, project.Address)
	}
	fmt.Fprintf(environment.out, "- remove %d placements rein made in your agent's home; your own files are untouched\n", len(plan.Placements))
	for _, placed := range plan.Placements {
		fmt.Fprintf(environment.out, "    %s\n", placed.Path)
	}
	if len(plan.Imports) > 0 {
		fmt.Fprintf(environment.out, "- remove %d imports under %s\n", len(plan.Imports), plan.ImportsFolder)
	}
	fmt.Fprintf(environment.out, "- forget %d organisations and delete their clones under %s; the remotes stay\n", len(plan.Organizations), plan.OrgClonesFolder)
	fmt.Fprintf(environment.out, "- remove the proposals under %s, and erase rein's store %s, with the ledger and the snapshots, and its key; your logins stay\n", plan.ProposalsFolder, plan.Store)
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
	for _, project := range result.ProjectClones {
		switch {
		case project.Failed:
			fmt.Fprintf(environment.errOut, "failed   %s: %s\n", project.Folder, project.Skipped)
		case project.Skipped != "":
			fmt.Fprintf(environment.errOut, "skipped  %s: %s\n", project.Folder, project.Skipped)
		default:
			printRestored(environment, project.Restore)
		}
	}
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
	fmt.Fprintf(environment.out, "removed  %s\n", strings.Join(result.Wiped, ", "))
	fmt.Fprintf(environment.out, "reset done; %d placements kept because they were not rein's\n", result.Kept)
	if result.Kept > 0 {
		return refusal.New(fmt.Sprintf("%d placements skipped", result.Kept))
	}
	return nil
}
