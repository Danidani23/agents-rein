package cli

import (
	"fmt"
	"path/filepath"
	"slices"
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
			"the rules become rules, one per ## section, skills stay skills, the settings' permissions\n" +
			"and hooks become a permission set and hooks, .mcp.json becomes tool servers, with any literal\n" +
			"secret replaced by a REIN_ reference. Each candidate is compared with what the organisation\n" +
			"already declares, by content and by id; the distiller says where each belongs; and everything is\n" +
			"written as one proposal folder, a YAML file per candidate with where it goes and its text,\n" +
			"beside proposal.yaml, under .rein/proposals/ of the project's folder, or under\n" +
			"~/.rein/proposals/pending/ for your agent's home. Review and edit it, then rein propose. A\n" +
			"distiller that fails or is not there writes nothing, so the next distill starts fresh;\n" +
			"--distiller none takes rein's own cut alone. Nothing is written into the org repository.",
		Example: "  rein distill\n  rein distill --project shop\n  rein distill --home\n  rein distill --snapshot claude-code-20261019-093000.json",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runDistill(environment, request)
		},
	}
	command.Flags().StringVar(&request.Directory, "path", ".", "the initialised project's folder")
	command.Flags().StringVar(&request.Organization, "org", "", "the organisation's slug (default: the active one)")
	command.Flags().StringVar(&request.Project, "project", "", "the project's slug, instead of the folder")
	command.Flags().BoolVar(&request.Home, "home", false, "distill your agent's home into your user level instead of a project")
	command.Flags().StringVar(&request.SnapshotPath, "snapshot", "", "a snapshot to distill by its name, as rein store lists it, instead of the latest")
	command.Flags().StringVar(&request.Judge, "distiller", "", "agent-cli to ask your agent's CLI, none to skip the model pass (default: the organisation's provider, else agent-cli)")
	command.Flags().StringVar(&request.Model, "model", "", "the model the distiller asks, such as sonnet for a large harness (default: your agent's default)")
	command.Flags().BoolVar(&request.DryRun, "dry-run", false, "print the brief that would go to the distiller and write nothing")
	// --judge is what --distiller was called before the distiller had its name
	command.Flags().StringVar(&request.Judge, "judge", "", "the old name of --distiller")
	_ = command.Flags().MarkDeprecated("judge", "use --distiller")
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
	request.Directory, err = environment.absolute(request.Directory)
	if err != nil {
		return err
	}
	switch request.Judge {
	case "", "agent-cli", "none":
	default:
		return refusal.New(fmt.Sprintf("--distiller %q is not agent-cli or none", request.Judge))
	}
	result, err := orchestrator.DistillHarness(request)
	if err != nil {
		return err
	}
	if request.DryRun {
		fmt.Fprintf(environment.out, "%s", result.Brief)
		fmt.Fprintf(environment.errOut, "dry run: the brief above would go to the distiller; nothing was written\n")
		return nil
	}
	document := result.Proposal
	counts := map[string]int{}
	for _, candidate := range document.Candidates {
		counts[candidate.Compared.Verdict]++
	}
	fmt.Fprintf(environment.out, "distilled %d candidates for %s from %s\n", len(document.Candidates), result.Level, "the snapshot "+filepath.Base(document.Snapshot.Path))
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
			fmt.Fprintf(environment.out, "             distiller: %s\n", describeJudgementLine(candidate))
		}
		// whom a declaration for the organisation or a team reaches (D40)
		if reached, found := result.Reaches[candidate.Kind+"/"+candidate.ID]; found {
			projects := "no project yet"
			if len(reached) > 0 {
				projects = strings.Join(reached, ", ")
			}
			fmt.Fprintf(environment.out, "             reaches: %s\n", projects)
		}
	}
	line := fmt.Sprintf("distiller: %s %s", document.Judge.Backend, document.Judge.Status)
	// an answer sent back is said after the fact, not only by the spinner (P2.58)
	if len(document.Judge.Rejected) > 0 {
		line += ", after its first answer was sent back for: " + strings.Join(document.Judge.Rejected, "; ")
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
	var conflicts []proposal.Candidate
	var failing []proposal.Candidate
	for _, candidate := range document.Candidates {
		if candidate.Conflict != nil {
			conflicts = append(conflicts, candidate)
		}
		if len(result.LintErrors[candidate.Kind+"/"+candidate.ID]) > 0 {
			failing = append(failing, candidate)
		}
	}
	fmt.Fprintf(environment.out, "\nwritten to %s\n", style.Bold(style.Green(result.File)))
	// the files in conflict or failing the lint close the output, each a full path the terminal opens
	if len(conflicts) > 0 {
		fmt.Fprintf(environment.out, "\n%s\n", style.Yellow(fmt.Sprintf("%d in conflict, which rein propose waits for: drop, override or amend each", len(conflicts))))
		for _, candidate := range conflicts {
			fmt.Fprintf(environment.out, "  %s  with %s\n", filepath.Join(result.File, proposal.CandidateFileName(candidate)), strings.Join(candidate.Conflict.With, ", "))
		}
	}
	if len(failing) > 0 {
		fmt.Fprintf(environment.out, "\n%s\n", style.Yellow(fmt.Sprintf("%d failing the lint, which rein propose refuses: mend the text under files in each", len(failing))))
		for _, candidate := range failing {
			for _, message := range result.LintErrors[candidate.Kind+"/"+candidate.ID] {
				fmt.Fprintf(environment.out, "  %s  %s\n", filepath.Join(result.File, proposal.CandidateFileName(candidate)), message)
			}
		}
	}
	switch {
	case len(conflicts) > 0 && len(failing) > 0:
		printNext(environment, "set conflict.resolution in each file in conflict, mend the files that fail the lint, review the rest, then rein propose opens the pull request")
	case len(conflicts) > 0:
		printNext(environment, "set conflict.resolution in each file in conflict, review the rest, then rein propose opens the pull request")
	case len(failing) > 0:
		printNext(environment, "mend the files that fail the lint, review the rest, then rein propose opens the pull request")
	default:
		printNext(environment, "review and edit it, then rein propose opens the pull request")
	}
	// the last line opens the distiller's conversation, which claude keeps out of its picker (Q49)
	if result.Conversation != "" {
		fmt.Fprintf(environment.out, "\nthe distiller's conversation, to read or go on with it: %s\n", result.Conversation)
	}
	return nil
}

// describeJudgementLine renders a judgement on one line, with the level
// the candidate goes to and its labels.
func describeJudgementLine(candidate proposal.Candidate) string {
	judgement := candidate.Judge
	line := judgement.Verdict
	// the distiller may quote one declaration twice; it is named once
	var named []string
	for _, quote := range judgement.Quotes {
		if slices.Contains(named, quote.Address) {
			continue
		}
		named = append(named, quote.Address)
		line += " with " + quote.Address
	}
	switch judgement.Suggestion {
	case "lift":
		line += "; lift to " + candidate.Level
		if len(candidate.ApplyIf) > 0 {
			line += ", applyIf " + strings.Join(candidate.ApplyIf, " ")
		} else if judgement.Everywhere != "" {
			line += ", every project: " + judgement.Everywhere
		}
	case "drop":
		line += "; drop, left out"
	default:
		line += "; " + judgement.Suggestion + " at " + candidate.Level
	}
	return line + ": " + judgement.Reason
}
