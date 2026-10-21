package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
)

func newSyncCommand(environment *commandEnvironment) *cobra.Command {
	var request orchestration.SyncRequest
	var hook bool
	command := &cobra.Command{
		Use:   "sync",
		Short: "Write the harness this clone's project resolves, and keep it written",
		Long: "sync is what delivers settings to an agent. In an initialised clone it pulls the org\n" +
			"repository, resolves the project's declarations, compiles them for the agent and writes\n" +
			"the files into the clone, gitignored: CLAUDE.md with one imported file per level under\n" +
			".claude/rein/, rules with paths under .claude/rules/, skills and hook scripts under .claude/,\n" +
			"permissions and hooks into .claude/settings.local.json, merged with what you allowed yourself,\n" +
			"and tool servers into .mcp.json. Every file is recorded with its hash; a file you edited by\n" +
			"hand stops the next sync unless --force, which snapshots it first. The session-start hook\n" +
			"sync installs runs sync --hook, which never fails a session. --all syncs every initialised\n" +
			"clone. sync ends by naming the REIN_ variables your tool servers need and your shell lacks.",
		Example: "  rein sync\n  rein sync --all\n  rein sync --force",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runSync(environment, request, hook)
		},
	}
	command.Flags().StringVar(&request.Directory, "path", ".", "the project's folder")
	command.Flags().BoolVar(&request.All, "all", false, "sync every initialised clone on this machine")
	command.Flags().BoolVar(&request.Force, "force", false, "overwrite files edited by hand, after snapshotting them")
	command.Flags().BoolVar(&hook, "hook", false, "run as the session-start hook: one line, never a failure")
	return command
}

func runSync(environment *commandEnvironment, request orchestration.SyncRequest, hook bool) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return hookOrError(environment, hook, err)
	}
	defer release()
	if hook {
		orchestrator.Progress = nil
	}
	err = requireLogin(orchestrator)
	if err != nil {
		return hookOrError(environment, hook, err)
	}
	request.Directory, err = filepath.Abs(request.Directory)
	if err != nil {
		return err
	}
	request.ReinExecutable, err = os.Executable()
	if err != nil {
		return err
	}
	result, err := orchestrator.SyncClone(request)
	for _, synced := range result.Clones {
		printSynced(environment, synced, hook)
	}
	return hookOrError(environment, hook, err)
}

// hookOrError keeps a session alive: as the hook, an error is a line on
// stdout and exit 0; otherwise it is the error it is.
func hookOrError(environment *commandEnvironment, hook bool, err error) error {
	if err == nil || !hook {
		return err
	}
	fmt.Fprintf(environment.out, "rein: %v\n", err)
	return nil
}

func printSynced(environment *commandEnvironment, synced orchestration.CloneSync, hook bool) {
	if synced.Skipped != "" {
		fmt.Fprintf(environment.errOut, "skipped  %s: %s\n", synced.Folder, synced.Skipped)
		return
	}
	if hook {
		fmt.Fprintf(environment.out, "rein: %s synced for %s, %d files written, %d unchanged\n", synced.Address, synced.Agent, len(synced.Written), len(synced.Unchanged))
		if synced.Created && synced.AlwaysOn != "" {
			fmt.Fprintf(environment.out, "The settings below were just written and apply from now on:\n\n%s", synced.AlwaysOn)
		}
	} else {
		fmt.Fprintf(environment.out, "synced %s for %s\n", synced.Address, synced.Agent)
		for _, written := range synced.Written {
			fmt.Fprintf(environment.out, "  written    %s\n", written)
		}
		for _, unchanged := range synced.Unchanged {
			fmt.Fprintf(environment.out, "  unchanged  %s\n", unchanged)
		}
		for _, removed := range synced.Removed {
			fmt.Fprintf(environment.out, "  removed    %s\n", removed)
		}
		if synced.SnapshotPath != "" {
			fmt.Fprintf(environment.out, "  your edits to %s were snapshotted to %s before being overwritten\n", strings.Join(synced.Modified, ", "), synced.SnapshotPath)
		}
	}
	for _, note := range synced.Notes {
		fmt.Fprintf(environment.out, "  note: %s\n", note)
	}
	if len(synced.UnsetVariables) > 0 {
		fmt.Fprintf(environment.errOut, "%s\n", environment.style().Yellow(fmt.Sprintf("warning: the tool servers of %s need %s in your shell, not set", synced.Address, strings.Join(synced.UnsetVariables, ", "))))
	}
}
