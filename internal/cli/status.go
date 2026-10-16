package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/local"
)

func newStatusCommand(env *environment) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show who you are to rein and what it has set up on this machine",
		Long: "status shows the GitHub account gh is logged in as, which is your identity (gh can hold\n" +
			"several accounts; the active one counts), the supported agents found on this machine,\n" +
			"every file rein placed into the agent's home and whether its target still exists, the\n" +
			"connected organisations with the active one marked, the registered projects and the\n" +
			"imports. It changes nothing and runs without an identity: when gh is not logged in it\n" +
			"says so and still shows the rest.",
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
	state, err := local.LoadState(home)
	if err != nil {
		return crashedError{cause: err}
	}
	who, err := githubIdentity()
	if err != nil {
		fmt.Fprintf(env.out, "github      none: %v\n", err)
	} else {
		fmt.Fprintf(env.out, "github      %s  (active gh account; slug %s)\n", who.Login, who.Slug)
	}
	agents := installedAgents()
	if len(agents) == 0 {
		fmt.Fprintf(env.out, "agents      none found; this build writes for %s\n", local.AgentClaudeCode)
	} else {
		fmt.Fprintf(env.out, "agents      %s\n", strings.Join(agents, ", "))
	}
	fmt.Fprintf(env.out, "home        %s\n", home)
	fmt.Fprintf(env.out, "\nplacements  %d\n", len(state.Placements))
	missing := 0
	for _, placed := range state.Placements {
		mark := "ok     "
		_, err := os.Stat(placed.Target)
		if err != nil {
			mark = "missing"
			missing++
		}
		fmt.Fprintf(env.out, "  %s  %-6s %-4s %s -> %s\n", mark, placed.Owner, placed.Mode, placed.Path, placed.Target)
	}
	if missing > 0 {
		fmt.Fprintf(env.out, "  %d targets are missing; run rein reset to remove the dangling links\n", missing)
	}
	fmt.Fprintf(env.out, "\norganisations  %d\n", len(state.Organizations))
	for _, ref := range state.Organizations {
		marker := " "
		if ref.Slug == state.Active {
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
			origin := imported.Source
			if imported.FromUser != "" {
				origin = "from " + imported.FromUser
			}
			fmt.Fprintf(env.out, "  %s  %s  %s\n", imported.Hash[:12], origin, imported.Path)
		}
	}
	return nil
}
