package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
)

func newDistillCommand(environment *commandEnvironment) *cobra.Command {
	var request orchestration.DistillRequest
	command := &cobra.Command{
		Use:   "distill",
		Short: "Turn a snapshotted harness into candidate declarations and compare them with the organisation",
		Long: "distill reads the snapshot rein took of a harness, the agent's files of an initialised project\n" +
			"or of your agent's home, and turns it into candidate declarations at that level: CLAUDE.md and\n" +
			"the rules become rules, skills stay skills, the settings' permissions and hooks become a\n" +
			"permission set and hooks, .mcp.json becomes tool servers, with any literal secret replaced by a\n" +
			"REIN_ reference. Each candidate is compared with what the organisation already declares, by\n" +
			"content and by id, and everything is written under ~/.rein/distill/ with a report, for rein\n" +
			"propose. Nothing is written into the org repository.",
		Example: "  rein distill\n  rein distill --project shop\n  rein distill --home\n  rein distill --snapshot ~/.rein/snapshots/claude-code-20261019-093000.json",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runDistill(environment, request)
		},
	}
	command.Flags().StringVar(&request.Directory, "path", ".", "the initialised project's folder")
	command.Flags().StringVar(&request.Organization, "org", "", "the organisation's slug (default: the active one)")
	command.Flags().StringVar(&request.Project, "project", "", "the project's slug, instead of the folder")
	command.Flags().BoolVar(&request.Home, "home", false, "distill your agent's home into your user level instead of a project")
	command.Flags().StringVar(&request.SnapshotPath, "snapshot", "", "a snapshot file to distill, instead of the latest")
	command.Flags().StringVar(&request.Judge, "judge", "", "agent-cli to ask your agent's CLI, none to skip the model pass (default: the organisation's provider, else agent-cli)")
	command.Flags().BoolVar(&request.DryRun, "dry-run", false, "print the brief that would go to the judge and write nothing")
	return command
}

func runDistill(environment *commandEnvironment, request orchestration.DistillRequest) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return err
	}
	defer release()
	err = requireLogin(orchestrator)
	if err != nil {
		return err
	}
	request.Directory, err = filepath.Abs(request.Directory)
	if err != nil {
		return err
	}
	switch request.Judge {
	case "", "agent-cli", "none":
	default:
		return refusal.New(fmt.Sprintf("--judge %q is not agent-cli or none", request.Judge))
	}
	result, err := orchestrator.DistillHarness(request)
	if err != nil {
		return err
	}
	if request.DryRun {
		fmt.Fprintf(environment.out, "%s", result.Brief)
		fmt.Fprintf(environment.errOut, "dry run: the brief above would go to the judge; nothing was written\n")
		return nil
	}
	counts := map[string]int{}
	for _, candidate := range result.Candidates {
		counts[candidate.Verdict]++
	}
	fmt.Fprintf(environment.out, "distilled %d candidates for %s from %s\n", len(result.Candidates), result.Level, strings.Join(result.Report.Snapshots, ", "))
	for _, summary := range result.Report.Candidates {
		line := fmt.Sprintf("  %-10s %-24s %s", summary.Verdict, summary.Kind+"/"+summary.ID, strings.Join(summary.Sources, ", "))
		if len(summary.Related) > 0 {
			line += "  = " + strings.Join(summary.Related, ", ")
		}
		fmt.Fprintln(environment.out, line)
		for _, note := range summary.Notes {
			fmt.Fprintf(environment.out, "             note: %s\n", note)
		}
		if summary.Judgement != nil {
			fmt.Fprintf(environment.out, "             judge: %s\n", describeJudgementLine(*summary.Judgement))
		}
	}
	if result.Report.Judge != nil {
		line := fmt.Sprintf("judge: %s %s", result.Report.Judge.Backend, result.Report.Judge.Status)
		if len(result.Report.Judge.Problems) > 0 {
			line += ": " + strings.Join(result.Report.Judge.Problems, "; ")
		}
		fmt.Fprintln(environment.out, line)
	}
	for _, skipped := range result.Report.Skipped {
		fmt.Fprintf(environment.out, "  skipped    %s\n", skipped)
	}
	fmt.Fprintf(environment.out, "%d new, %d identical to an existing declaration, %d sharing an id with another level\n", counts[distill.VerdictNew], counts[distill.VerdictIdentical], counts[distill.VerdictSameID])
	fmt.Fprintf(environment.out, "written to %s\nnext: rein propose %s, once it exists, opens the pull request\n", result.Folder, result.Folder)
	return nil
}

// describeJudgementLine renders a judgement on one line.
func describeJudgementLine(judgement distill.Judgement) string {
	line := judgement.Verdict
	if len(judgement.Related) > 0 {
		line += " with " + strings.Join(judgement.Related, ", ")
	}
	line += "; " + judgement.Suggestion
	if judgement.Suggestion == "lift" && judgement.Level != "" {
		line += " to the " + judgement.Level + " level"
	}
	return line + ": " + judgement.Reason
}
