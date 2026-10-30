package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/0_gateway/wording"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
)

func newResetProjectCommand(environment *commandEnvironment) *cobra.Command {
	var request orchestration.RestoreRequest
	var yes bool
	command := &cobra.Command{
		Use:   "project",
		Short: "Put a project clone back as it was before rein init, and commit the put-back",
		Long: "reset project reverses init for one clone: the agent's files init moved out come back from their\n" +
			"snapshots, the rein block leaves .gitignore, the clone is forgotten on this machine, the\n" +
			"removals are marked restored in the ledger, and the put-back is committed as the move-out\n" +
			"was; --no-commit leaves it staged. A file now in the way of the put-back stops it unless\n" +
			"--force overwrites it. The snapshots stay until rein reset machine.",
		Example: "  rein reset project\n  rein reset project --path services/shop --yes",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runResetProject(environment, request, yes)
		},
	}
	command.Flags().StringVar(&request.Directory, "path", ".", "the project's folder")
	command.Flags().BoolVar(&request.NoCommit, "no-commit", false, "stage the put-back instead of committing it")
	command.Flags().BoolVar(&request.Force, "force", false, "overwrite files that are in the way of the put-back")
	command.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return command
}

func runResetProject(environment *commandEnvironment, request orchestration.RestoreRequest, yes bool) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return err
	}
	defer release()
	request.Directory, err = environment.absolute(request.Directory)
	if err != nil {
		return err
	}
	summary, explanation := wording.ResetProject(request.Directory)
	fmt.Fprintf(environment.out, "%s\n", summary)
	err = environment.confirmChange([]string{summary, explanation}, "Continue?", explanation, yes)
	if err != nil {
		return err
	}
	result, err := orchestrator.RestoreProject(request)
	if err != nil {
		return err
	}
	printRestored(environment, result)
	return nil
}

func printRestored(environment *commandEnvironment, result orchestration.RestoreResult) {
	printOutcome(environment, wording.Restored(result))
}

// printOutcome prints what a command did, as its page shows it too: the
// headline, each line, a warning on the error stream, and the next step.
func printOutcome(environment *commandEnvironment, done wording.Outcome) {
	fmt.Fprintf(environment.out, "%s\n", done.Headline)
	for _, line := range done.Lines {
		printLine(environment, line, "")
	}
	if done.Next != "" {
		printNext(environment, done.Next)
	}
}

// printLine prints one sentence of what a command did, a warning in yellow
// on the error stream.
func printLine(environment *commandEnvironment, line wording.Line, indent string) {
	if line.Warning {
		fmt.Fprintf(environment.errOut, "%s%s\n", indent, environment.style().Yellow("warning: "+line.Text))
		return
	}
	fmt.Fprintf(environment.out, "%s%s\n", indent, line.Text)
}
