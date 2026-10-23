package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
	"github.com/Danidani23/agents-rein/internal/app/2_services/proposal"
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
			"content and by id; the judge says where each belongs; and everything is written as one\n" +
			"proposal file, under .rein/proposals/ of the project's folder, or under ~/.rein/proposals/\n" +
			"pending/ for your agent's home. Review and edit the file, then rein propose. Nothing is\n" +
			"written into the org repository.",
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
	command.Flags().BoolVar(&request.Force, "force", false, "replace a pending proposal of the same snapshot")
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
	document := result.Proposal
	counts := map[string]int{}
	for _, candidate := range document.Candidates {
		counts[candidate.Compared.Verdict]++
	}
	fmt.Fprintf(environment.out, "distilled %d candidates for %s from %s\n", len(document.Candidates), result.Level, document.Snapshot.Path)
	for _, candidate := range document.Candidates {
		line := fmt.Sprintf("  %-10s %-24s %s", candidate.Compared.Verdict, candidate.Kind+"/"+candidate.ID, strings.Join(candidate.Sources, ", "))
		if len(candidate.Compared.Related) > 0 {
			line += "  = " + strings.Join(candidate.Compared.Related, ", ")
		}
		fmt.Fprintln(environment.out, line)
		for _, note := range candidate.Notes {
			fmt.Fprintf(environment.out, "             note: %s\n", note)
		}
		if candidate.Judge != nil {
			fmt.Fprintf(environment.out, "             judge: %s\n", describeJudgementLine(candidate))
		}
	}
	line := fmt.Sprintf("judge: %s %s", document.Judge.Backend, document.Judge.Status)
	if document.Judge.Session != "" {
		line += ", conversation " + document.Judge.Session
	}
	if len(document.Judge.Problems) > 0 {
		line += ": " + strings.Join(document.Judge.Problems, "; ")
	}
	fmt.Fprintln(environment.out, line)
	for _, skipped := range document.Skipped {
		fmt.Fprintf(environment.out, "  skipped    %s\n", skipped)
	}
	fmt.Fprintf(environment.out, "%d new, %d identical to an existing declaration, %d sharing an id with another level\n", counts[distill.VerdictNew], counts[distill.VerdictIdentical], counts[distill.VerdictSameID])
	style := environment.style()
	fmt.Fprintf(environment.out, "\nwritten to %s\n", style.Bold(style.Green(result.File)))
	printNext(environment, "review and edit it, then rein propose opens the pull request")
	return nil
}

// describeJudgementLine renders a judgement on one line, with the level
// the candidate goes to and its labels.
func describeJudgementLine(candidate proposal.Candidate) string {
	judgement := candidate.Judge
	line := judgement.Verdict
	for _, quote := range judgement.Quotes {
		line += " with " + quote.Address
	}
	switch judgement.Suggestion {
	case "lift":
		line += "; lift to " + candidate.Level
		if len(candidate.ApplyIf) > 0 {
			line += ", applyIf " + strings.Join(candidate.ApplyIf, " ")
		}
	case "drop":
		line += "; drop, left out"
	default:
		line += "; " + judgement.Suggestion + " at " + candidate.Level
	}
	return line + ": " + judgement.Reason
}
