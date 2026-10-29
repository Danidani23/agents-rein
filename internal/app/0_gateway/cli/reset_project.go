package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
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
	fmt.Fprintf(environment.out, "This will put back the agent's files rein init moved out of %s, take the rein block out of\n.gitignore, forget the clone on this machine, and commit the put-back.\n", request.Directory)
	if !yes {
		prompter := environment.prompter()
		if !prompter.CanAsk() {
			return refusal.New("no terminal to confirm on; pass --yes")
		}
		confirmed, err := prompter.Confirm("Continue?", "The files come back from ~/.rein/snapshots/ exactly as they were. The commit holds only the files that were tracked and .gitignore; pushing it is yours. The snapshots stay until rein reset machine.")
		if err != nil {
			return refusal.New(err.Error())
		}
		if !confirmed {
			return refusal.New("nothing changed")
		}
	}
	result, err := orchestrator.RestoreProject(request)
	if err != nil {
		return err
	}
	printRestored(environment, result)
	return nil
}

func printRestored(environment *commandEnvironment, result orchestration.RestoreResult) {
	fmt.Fprintf(environment.out, "restored %s, formerly %s\n", result.Folder, result.Address)
	if len(result.Restored) > 0 {
		fmt.Fprintf(environment.out, "put back: %s\n", strings.Join(result.Restored, ", "))
	} else {
		fmt.Fprintf(environment.out, "nothing to put back: init had moved nothing out\n")
	}
	if len(result.InsertsRemoved) > 0 {
		fmt.Fprintf(environment.out, "removed sync's files: %s\n", strings.Join(result.InsertsRemoved, ", "))
	}
	if len(result.InsertsKept) > 0 {
		fmt.Fprintf(environment.errOut, "kept, edited by hand since sync wrote them: %s\n", strings.Join(result.InsertsKept, ", "))
	}
	if result.IgnoreRemoved {
		fmt.Fprintf(environment.out, ".gitignore: rein block removed\n")
	}
	switch {
	case result.Commit != "":
		fmt.Fprintf(environment.out, "committed %s: %s\n", result.Commit[:7], "restore agent settings from rein (rein reset project)")
	case result.CommitError != "":
		fmt.Fprintf(environment.errOut, "%s\n", environment.style().Yellow(fmt.Sprintf("warning: the put-back is staged but not committed: %s", result.CommitError)))
	default:
		fmt.Fprintf(environment.out, "the put-back is staged, not committed\n")
	}
	printNext(environment, "the clone is as before rein init; rein init puts it under rein again, rein org destroy and rein reset machine take the rest off this machine")
}
