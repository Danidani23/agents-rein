package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/local"
	"github.com/Danidani23/agents-rein/internal/prompt"
)

const resetExplanation = "A placement is one link (a copy on Windows) that rein made in your agent's home folder,\n" +
	"for Claude Code ~/.claude: rules/<id>.md for a rule and skills/<id> for a skill. Through those\n" +
	"links the agent reads your settings. reset removes exactly the ones rein recorded, dangling\n" +
	"ones included; a link that points somewhere rein did not record is left alone and reported.\n" +
	"An import is a level you brought in with rein import, copied under ~/.rein/imports/; the\n" +
	"placements point into those copies, so reset deletes them too. Nothing outside ~/.rein and\n" +
	"the placed links is touched, and rein import brings everything back."

func newResetCommand(env *environment) *cobra.Command {
	var all, yes bool
	command := &cobra.Command{
		Use:   "reset",
		Short: "Remove everything rein placed into your agent's home; --all forgets this machine entirely",
		Long: "reset removes every link and copy rein made in the agent's home, dangling ones included,\n" +
			"deletes the imports under ~/.rein/imports/, and clears them from state. It touches nothing\n" +
			"it did not place: a link that points somewhere else than rein recorded is left alone and\n" +
			"reported. With --all it also forgets the connected organisations, deletes their clones and\n" +
			"the project registrations, which leaves the machine as before rein was first used; the\n" +
			"remotes are never touched. It confirms before deleting; --yes skips that.",
		Example: "  rein reset\n  rein reset --all --yes",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runReset(env, all, yes)
		},
	}
	command.Flags().BoolVar(&all, "all", false, "also forget organisations, delete their clones and the project registrations")
	command.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return command
}

func runReset(env *environment, all bool, yes bool) error {
	home, err := local.Home()
	if err != nil {
		return crashedError{cause: err}
	}
	state, err := local.LoadState(home)
	if err != nil {
		return crashedError{cause: err}
	}
	fmt.Fprintf(env.out, "This will remove:\n")
	fmt.Fprintf(env.out, "- %d placements: the links rein made in your agent's home so the agent reads your rules\n  and skills. Removing them takes those away from the agent; your own files are untouched.\n", len(state.Placements))
	for _, placed := range state.Placements {
		fmt.Fprintf(env.out, "    %s\n", placed.Path)
	}
	fmt.Fprintf(env.out, "- %d imports: the copies of the levels you imported, under %s, that the\n  placements point at.\n", len(state.Imports), filepath.Join(home, "imports"))
	if all {
		fmt.Fprintf(env.out, "- %d organisations with their clones under %s, and %d project registrations:\n  what this machine knows about them. The remotes on GitHub are not touched.\n", len(state.Organizations), filepath.Join(home, "orgs"), len(state.Projects))
	}
	if !yes {
		session := prompt.New(env.in, env.out, env.terminal)
		if !session.CanAsk() {
			return fmt.Errorf("no terminal to confirm on; pass --yes")
		}
		confirmed, err := session.Confirm("Continue?", resetExplanation)
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("nothing changed")
		}
	}
	var kept []local.Placement
	for _, placed := range state.Placements {
		removed, reason := unplace(placed)
		if removed {
			fmt.Fprintf(env.out, "removed  %s\n", placed.Path)
			continue
		}
		if reason != "" {
			fmt.Fprintf(env.errOut, "skipped  %s: %s\n", placed.Path, reason)
			kept = append(kept, placed)
			continue
		}
		fmt.Fprintf(env.out, "gone     %s\n", placed.Path)
	}
	state.Placements = kept
	err = os.RemoveAll(filepath.Join(home, "imports"))
	if err != nil {
		return crashedError{cause: err}
	}
	state.Imports = nil
	if all {
		for id, clone := range state.Clones {
			err = os.RemoveAll(clone)
			if err != nil {
				return crashedError{cause: err}
			}
			fmt.Fprintf(env.out, "removed  clone %s\n", id)
		}
		err = os.RemoveAll(filepath.Join(home, "orgs"))
		if err != nil {
			return crashedError{cause: err}
		}
		state.Clones = map[string]string{}
		state.Organizations = nil
		state.Active = ""
		state.Projects = nil
	}
	// a config.yaml from before D9 is a leftover
	os.Remove(filepath.Join(home, "config.yaml"))
	err = local.SaveState(home, state)
	if err != nil {
		return crashedError{cause: err}
	}
	fmt.Fprintf(env.out, "reset done; %d placements kept because they were not rein's\n", len(kept))
	if len(kept) > 0 {
		return fmt.Errorf("%d placements skipped", len(kept))
	}
	return nil
}

// unplace removes one placement when it is still what rein made: a link
// whose target is the recorded one, dangling or not, or a recorded copy.
// removed is false with an empty reason when nothing was there.
func unplace(placed local.Placement) (removed bool, reason string) {
	info, err := os.Lstat(placed.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, ""
	}
	if err != nil {
		return false, err.Error()
	}
	if info.Mode()&os.ModeSymlink != 0 {
		existing, err := os.Readlink(placed.Path)
		if err != nil {
			return false, err.Error()
		}
		if existing != placed.Target {
			return false, "links to " + existing + ", not what rein recorded"
		}
		err = os.Remove(placed.Path)
		if err != nil {
			return false, err.Error()
		}
		return true, ""
	}
	if placed.Mode != "copy" {
		return false, "is a file rein did not place"
	}
	err = os.RemoveAll(placed.Path)
	if err != nil {
		return false, err.Error()
	}
	return true, ""
}
