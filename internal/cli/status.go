package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/adapters/claudecode"
	"github.com/Danidani23/agents-rein/internal/app"
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
			dependencies, err := env.dependencies()
			if err != nil {
				return err
			}
			report, err := app.Status(dependencies)
			if err != nil {
				return err
			}
			printStatus(env, report)
			return nil
		},
	}
}

func printStatus(env *environment, report app.StatusReport) {
	if report.IdentityError != "" {
		fmt.Fprintf(env.out, "github      none: %s\n", report.IdentityError)
	} else {
		fmt.Fprintf(env.out, "github      %s  (active gh account; slug %s)\n", report.Identity.Login, report.Identity.Slug)
	}
	if len(report.Agents) == 0 {
		fmt.Fprintf(env.out, "agents      none found; this build writes for %s\n", claudecode.Name)
	} else {
		fmt.Fprintf(env.out, "agents      %s\n", strings.Join(report.Agents, ", "))
	}
	fmt.Fprintf(env.out, "home        %s\n", report.Home)
	fmt.Fprintf(env.out, "\nplacements  %d\n", len(report.Placements))
	for _, placed := range report.Placements {
		mark := "ok     "
		if !placed.TargetExists {
			mark = "missing"
		}
		fmt.Fprintf(env.out, "  %s  %-6s %-4s %s -> %s\n", mark, placed.Owner, placed.Mode, placed.Path, placed.Target)
	}
	if report.Missing > 0 {
		fmt.Fprintf(env.out, "  %d targets are missing; run rein reset to remove the dangling links\n", report.Missing)
	}
	fmt.Fprintf(env.out, "\norganisations  %d\n", len(report.Organizations))
	for _, ref := range report.Organizations {
		marker := " "
		if ref.Slug == report.Active {
			marker = "*"
		}
		fmt.Fprintf(env.out, "  %s %s  %s\n", marker, ref.Slug, ref.Remote)
	}
	fmt.Fprintf(env.out, "\nprojects  %d\n", len(report.Projects))
	for _, project := range report.Projects {
		fmt.Fprintf(env.out, "  %s  %s\n", project.Address, project.ClonePath)
	}
	if len(report.Imports) > 0 {
		fmt.Fprintf(env.out, "\nimports  %d\n", len(report.Imports))
		for _, imported := range report.Imports {
			origin := imported.Source
			if imported.FromUser != "" {
				origin = "from " + imported.FromUser
			}
			fmt.Fprintf(env.out, "  %s  %s  %s\n", imported.Hash[:12], origin, imported.Path)
		}
	}
}
