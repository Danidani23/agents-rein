package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/0_gateway/wording"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
)

// resetExplanation stands behind reset machine's question, as on its page.
const resetExplanation = wording.ResetMachineExplanation

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
	summary := wording.ResetMachine(plan)
	for _, line := range summary {
		fmt.Fprintf(environment.out, "%s\n", line)
	}
	err = environment.confirmChange(summary, "Continue?", resetExplanation, yes)
	if err != nil {
		return err
	}
	result, err := orchestrator.ResetMachine(plan)
	for _, project := range result.ProjectClones {
		if project.Skipped != "" {
			fmt.Fprintf(environment.errOut, "%s\n", wording.ProjectReset(project).Text)
			continue
		}
		printRestored(environment, project.Restore)
	}
	if err != nil {
		return err
	}
	done := wording.MachineReset(result)
	for _, line := range done.Lines {
		if line.Warning {
			fmt.Fprintf(environment.errOut, "%s\n", line.Text)
			continue
		}
		fmt.Fprintf(environment.out, "%s\n", line.Text)
	}
	fmt.Fprintf(environment.out, "%s\n", done.Headline)
	if result.Kept > 0 {
		return refusal.New(fmt.Sprintf("%d placements skipped", result.Kept))
	}
	return nil
}
