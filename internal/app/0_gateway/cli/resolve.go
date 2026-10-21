package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
)

func newResolveCommand(environment *commandEnvironment) *cobra.Command {
	var organization, project string
	command := &cobra.Command{
		Use:   "resolve",
		Short: "Show which declarations apply to a project, in level order, and what stands in the way",
		Long: "resolve runs the resolution for a project without writing anything: it collects the\n" +
			"declarations of the organisation, the project's teams, the project and you, drops the ones\n" +
			"whose selectors do not match the project's labels, applies the skip lists with their\n" +
			"exceptions, settles the same kind and id across levels, a lower default replacing, a\n" +
			"required one never replaced, and prints the outcome. A problem means sync would refuse.",
		Example: "  rein resolve --project shop",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runResolve(environment, organization, project)
		},
	}
	command.Flags().StringVar(&organization, "org", "", "the organisation's slug (default: the active one)")
	command.Flags().StringVar(&project, "project", "", "the project's slug")
	return command
}

func runResolve(environment *commandEnvironment, organization string, project string) error {
	if project == "" {
		return refusal.New("pass --project <slug>")
	}
	orchestrator, err := environment.orchestrate()
	if err != nil {
		return err
	}
	err = requireLogin(orchestrator)
	if err != nil {
		return err
	}
	resolution, err := orchestrator.ResolveProject(organization, project)
	if err != nil {
		return err
	}
	labels := strings.Join(resolution.Labels, ", ")
	if labels == "" {
		labels = "none"
	}
	fmt.Fprintf(environment.out, "%s: teams %s, labels %s, user %s\n", resolution.Address, strings.Join(resolution.Teams, ", "), labels, resolution.User)
	if len(resolution.Result.Chosen) == 0 {
		fmt.Fprintf(environment.out, "no declarations apply yet\n")
	}
	currentLevel := ""
	for _, chosen := range resolution.Result.Chosen {
		if chosen.Level != currentLevel {
			currentLevel = chosen.Level
			fmt.Fprintf(environment.out, "%s\n", currentLevel)
		}
		enforcement := chosen.Selectors.Enforcement
		if enforcement == "" {
			enforcement = "default"
		}
		line := fmt.Sprintf("  %-12s %-9s %s", chosen.Kind, enforcement, chosen.ID)
		if len(chosen.Selectors.ApplyIf) > 0 {
			line += "  applyIf " + strings.Join(chosen.Selectors.ApplyIf, " ")
		}
		if len(chosen.Replaced) > 0 {
			line += "  replaces " + strings.Join(chosen.Replaced, ", ")
		}
		fmt.Fprintln(environment.out, line)
	}
	for _, skipped := range resolution.Result.Skipped {
		line := fmt.Sprintf("skipped  %s by %s", skipped.Address, skipped.By)
		if skipped.Exception != "" {
			line += " through " + skipped.Exception
		}
		fmt.Fprintln(environment.out, line)
	}
	for _, problem := range resolution.Result.Problems {
		fmt.Fprintf(environment.errOut, "problem  %s: %s\n", problem.Address, problem.Message)
	}
	if len(resolution.Result.Problems) > 0 {
		return refusal.New(fmt.Sprintf("%d problems; sync would refuse until they are gone", len(resolution.Result.Problems)))
	}
	return nil
}
