package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
)

func newStatusCommand(environment *commandEnvironment) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show who you are to rein and what it has set up on this machine",
		Long: "status shows the GitHub accounts logged in on this machine, which are your identities\n" +
			"(each organisation remembers the one that connected it), the supported agents found here,\n" +
			"every file rein placed into the agent's home and whether its target still exists, the\n" +
			"connected organisations with the active one marked, the initialised project clones with\n" +
			"the drift of what sync wrote, and the\n" +
			"imports. It changes nothing and runs without a login: a machine without one is reported,\n" +
			"not refused, and it\n" +
			"says so and still shows the rest.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, err := environment.orchestrate()
			if err != nil {
				return err
			}
			report, err := orchestrator.Status()
			if err != nil {
				return err
			}
			printStatus(environment, report)
			return nil
		},
	}
}

func printStatus(environment *commandEnvironment, report orchestration.StatusReport) {
	if len(report.Logins) == 0 {
		fmt.Fprintf(environment.out, "logins      none; run rein login\n")
	} else {
		fmt.Fprintf(environment.out, "logins      %s\n", strings.Join(report.Logins, ", "))
	}
	if len(report.Agents) == 0 {
		fmt.Fprintf(environment.out, "agents      none found; this build writes for claude-code\n")
	} else {
		fmt.Fprintf(environment.out, "agents      %s\n", strings.Join(report.Agents, ", "))
	}
	fmt.Fprintf(environment.out, "home        %s\n", report.Home)
	fmt.Fprintf(environment.out, "\nplacements  %d\n", len(report.Placements))
	for _, placed := range report.Placements {
		mark := "ok     "
		if !placed.TargetExists {
			mark = "missing"
		}
		fmt.Fprintf(environment.out, "  %s  %-6s %-4s %s -> %s\n", mark, placed.Owner, placed.Mode, placed.Path, placed.Target)
	}
	if report.Missing > 0 {
		fmt.Fprintf(environment.out, "  %d targets are missing; run rein reset to remove the dangling links\n", report.Missing)
	}
	fmt.Fprintf(environment.out, "\norganisations  %d\n", len(report.Organizations))
	for _, ref := range report.Organizations {
		marker := " "
		if ref.Slug == report.Active {
			marker = "*"
		}
		fmt.Fprintf(environment.out, "  %s %s  %s\n", marker, ref.Slug, ref.Remote)
	}
	fmt.Fprintf(environment.out, "\nproject clones  %d\n", len(report.Clones))
	for _, clone := range report.Clones {
		drift := "never synced"
		if clone.Inserts > 0 {
			drift = fmt.Sprintf("%d files from sync", clone.Inserts)
			if clone.Modified > 0 {
				drift += fmt.Sprintf(", %d edited by hand", clone.Modified)
			}
			if clone.Missing > 0 {
				drift += fmt.Sprintf(", %d missing", clone.Missing)
			}
		}
		fmt.Fprintf(environment.out, "  %s  %s  %s\n", clone.Address, clone.ClonePath, drift)
	}
	if len(report.Imports) > 0 {
		fmt.Fprintf(environment.out, "\nimports  %d\n", len(report.Imports))
		for _, imported := range report.Imports {
			origin := imported.Source
			if imported.FromUser != "" {
				origin = "from " + imported.FromUser
			}
			fmt.Fprintf(environment.out, "  %s  %s  %s\n", imported.Hash[:12], origin, imported.Path)
		}
	}
}
