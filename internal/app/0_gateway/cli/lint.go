package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/2_services/lint"
)

func newLintCommand(env *environment) *cobra.Command {
	return &cobra.Command{
		Use:   "lint <path>",
		Short: "Check the skills, rules and settings under a path",
		Long: "lint walks the path and checks every SKILL.md against the Agent Skills specification,\n" +
			"every rule file's frontmatter, every settings file's permission rules, and the skills\n" +
			"that share a name. Inside an org repository each finding also carries the address of the\n" +
			"declaration. Exit 1 when it finds an error, 0 when only warnings or nothing. It needs no\n" +
			"config, so CI runs it on an org repository and you can run it on any folder.",
		Example: "  rein lint .\n  rein lint ~/.rein/orgs/<id>/declarations",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			report, err := lint.Run(arguments[0])
			if err != nil {
				return err
			}
			for _, finding := range report.Findings {
				location := finding.Path
				if finding.Address != "" {
					location += " (" + finding.Address + ")"
				}
				fmt.Fprintf(env.out, "%s: %s: %s\n", location, finding.Severity, finding.Message)
			}
			fmt.Fprintf(env.out, "%d errors, %d warnings\n", report.Errors(), report.Warnings())
			if report.Errors() > 0 {
				return refuse(fmt.Sprintf("lint found %d errors", report.Errors()))
			}
			return nil
		},
	}
}
