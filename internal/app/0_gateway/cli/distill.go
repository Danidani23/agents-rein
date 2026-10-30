package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/0_gateway/wording"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
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
	done := wording.Distilled(result)
	fmt.Fprintf(environment.out, "%s\n", done.Headline)
	for _, line := range done.Lines {
		printDistilledLine(environment, line)
	}
	printNext(environment, done.Next)
	for _, line := range done.Closing {
		printDistilledLine(environment, line)
	}
	return nil
}

// printDistilledLine prints one line of what distill did, a warning in
// yellow, all on the standard output, where the paths are opened.
func printDistilledLine(environment *commandEnvironment, line wording.Line) {
	if line.Warning {
		fmt.Fprintf(environment.out, "%s\n", environment.style().Yellow(line.Text))
		return
	}
	fmt.Fprintf(environment.out, "%s\n", line.Text)
}
