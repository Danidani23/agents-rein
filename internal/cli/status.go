package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/local"
)

func newStatusCommand(env *environment) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show what rein has set up on this machine",
		Long: "status reads ~/.rein/config.yaml and ~/.rein/state.yaml and shows who you are, which\n" +
			"agent rein writes for, every file it placed into the agent's home and whether its target\n" +
			"still exists, the connected organisations with the active one marked, and the registered\n" +
			"projects. It changes nothing and needs no config: before rein config has run it says so.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runStatus(env)
		},
	}
}

func runStatus(env *environment) error {
	home, err := local.Home()
	if err != nil {
		return crashedError{cause: err}
	}
	config, err := local.Require(home)
	if errors.Is(err, local.ErrNotConfigured) {
		fmt.Fprintf(env.out, "not configured on this machine; run rein config\n")
		return nil
	}
	if err != nil {
		return crashedError{cause: err}
	}
	state, err := local.LoadState(home)
	if err != nil {
		return crashedError{cause: err}
	}
	fmt.Fprintf(env.out, "home        %s\n", home)
	fmt.Fprintf(env.out, "user        %s\n", config.User)
	fmt.Fprintf(env.out, "agent       %s\n", config.Agent)
	if config.UserLevelSource != "" {
		fmt.Fprintf(env.out, "user level  %s\n", config.UserLevelSource)
	}
	fmt.Fprintf(env.out, "\nplacements  %d\n", len(state.Placements))
	missing := 0
	for _, placed := range state.Placements {
		owner := placed.Owner
		if owner == "" {
			owner = local.OwnerConfig
		}
		mark := "ok     "
		_, err := os.Stat(placed.Target)
		if err != nil {
			mark = "missing"
			missing++
		}
		fmt.Fprintf(env.out, "  %s  %-6s %-4s %s -> %s\n", mark, owner, placed.Mode, placed.Path, placed.Target)
	}
	if missing > 0 {
		fmt.Fprintf(env.out, "  %d targets are missing; run rein config again to re-point them\n", missing)
	}
	fmt.Fprintf(env.out, "\norganisations  %d\n", len(config.Organizations))
	for _, ref := range config.Organizations {
		marker := " "
		if ref.Slug == config.Active {
			marker = "*"
		}
		fmt.Fprintf(env.out, "  %s %s  %s\n", marker, ref.Slug, ref.Remote)
	}
	fmt.Fprintf(env.out, "\nprojects  %d\n", len(state.Projects))
	for _, project := range state.Projects {
		fmt.Fprintf(env.out, "  %s  %s\n", project.Address, project.ClonePath)
	}
	if len(state.Imports) > 0 {
		fmt.Fprintf(env.out, "\nimports  %d\n", len(state.Imports))
		for _, imported := range state.Imports {
			fmt.Fprintf(env.out, "  %s  from %s  %s\n", imported.Hash[:12], imported.FromUser, imported.Path)
		}
	}
	return nil
}
