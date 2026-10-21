package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// initAnswers are the answers init needs, from flags or questions.
type initAnswers struct {
	path         string
	organization string
	project      string
	noCommit     bool
	yes          bool
}

const createProjectChoice = "create a new project"

func newInitCommand(environment *commandEnvironment) *cobra.Command {
	var answers initAnswers
	command := &cobra.Command{
		Use:   "init",
		Short: "Bind this clone to its project, move its agent settings out to rein, and commit the move-out",
		Long: "init works inside a project clone. It fingerprints the repository by its root commits and\n" +
			"finds the project record that carries that fingerprint; when none does, it binds the clone\n" +
			"to a record you pick, or creates the project on the spot. Then it moves the agent's files,\n" +
			"CLAUDE.md, .claude/ and .mcp.json, into a snapshot under ~/.rein/snapshots/, records the\n" +
			"removal in ~/.rein/removals.yaml, adds a marked block to .gitignore so rein sync can write\n" +
			"them back as generated files, registers the clone on this machine, and commits exactly the\n" +
			"paths it touched; --no-commit leaves them staged. The first sync is rein sync, once it exists.",
		Example: "  rein init\n  rein init --project shop --yes\n  rein init --path services/shop",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runInit(environment, answers)
		},
	}
	command.Flags().StringVar(&answers.path, "path", ".", "the project's folder; the repository's root, or the project's folder in a monorepo")
	command.Flags().StringVar(&answers.organization, "org", "", "the organisation's slug (default: the active one)")
	command.Flags().StringVar(&answers.project, "project", "", "the project's slug to bind to, when no record carries the fingerprint")
	command.Flags().BoolVar(&answers.noCommit, "no-commit", false, "stage the move-out instead of committing it")
	command.Flags().BoolVar(&answers.yes, "yes", false, "do not ask for confirmation")
	return command
}

const initIntro = "init binds this clone to its project record in the org repository and moves the agent's " +
	"settings out of the repository into rein's keeping, since agent settings are not repository " +
	"settings: a snapshot keeps what was there, .gitignore gets a marked block, and the move-out " +
	"is committed."

func runInit(environment *commandEnvironment, answers initAnswers) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return err
	}
	defer release()
	err = requireLogin(orchestrator)
	if err != nil {
		return err
	}
	directory, err := filepath.Abs(answers.path)
	if err != nil {
		return err
	}
	location, err := orchestrator.LocateProject(directory)
	if err != nil {
		return refusal.New(fmt.Sprintf("%s is not inside a git repository with a commit: %v", directory, err))
	}
	if location.Initialized != "" {
		return refusal.New(fmt.Sprintf("%s is already initialised as %s; run rein sync", location.Folder, location.Initialized))
	}
	if len(location.Matches) > 1 {
		var addresses []string
		for _, match := range location.Matches {
			addresses = append(addresses, match.Address)
		}
		return refusal.New(fmt.Sprintf("this repository matches projects in several organisations, %s; disconnect the ones that are not this clone's", strings.Join(addresses, " and ")))
	}
	request := orchestration.InitializeProjectRequest{Directory: directory, Organization: answers.organization, Project: answers.project, NoCommit: answers.noCommit}
	prompter := environment.prompter()
	var binding string
	switch {
	case len(location.Matches) == 1:
		request.Organization = location.Matches[0].Organization
		binding = "bind it to " + location.Matches[0].Address + ", which carries its fingerprint"
	case answers.project != "":
		binding = "bind it to project " + answers.project
	case !prompter.CanAsk():
		return refusal.New("no project record carries this repository's fingerprint; pass --project <slug> and --yes, or run with a terminal to pick or create one")
	default:
		organization, err := orchestrator.DescribeOrganization(answers.organization)
		if err != nil {
			return err
		}
		prompter.Intro(initIntro)
		request, binding, err = askInitBinding(prompter, organization, location, request)
		if err != nil {
			return refusal.New(err.Error())
		}
	}
	fmt.Fprintf(environment.out, "This will %s, move the agent settings of %s into a snapshot, add the rein block to\n.gitignore, register the clone on this machine, and commit the move-out.\n", binding, location.Folder)
	if !answers.yes {
		if !prompter.CanAsk() {
			return refusal.New("no terminal to confirm on; pass --yes")
		}
		confirmed, err := prompter.Confirm("Continue?", "The files are kept in ~/.rein/snapshots/ and listed in ~/.rein/removals.yaml; rein restore puts them back. The commit holds only the removed paths and .gitignore; pushing it is yours.")
		if err != nil {
			return refusal.New(err.Error())
		}
		if !confirmed {
			return refusal.New("nothing changed")
		}
	}
	result, err := orchestrator.InitializeProject(request)
	if err != nil {
		return err
	}
	printInitialized(environment, result)
	return nil
}

// askInitBinding asks which record the clone is, offering the unbound
// projects of the organisation and the creation of a new one.
func askInitBinding(prompter ports.Prompter, organization orchestration.OrganizationDescription, location orchestration.ProjectLocation, request orchestration.InitializeProjectRequest) (orchestration.InitializeProjectRequest, string, error) {
	choices := []ports.Choice{{Name: createProjectChoice, Description: "ask for its name, teams and labels now"}}
	for _, unbound := range location.Unbound {
		choices = append(choices, ports.Choice{Name: unbound.Project, Description: unbound.Name + ", not bound to a repository yet"})
	}
	picked, err := prompter.Choose(ports.Select{
		Title:       "Project",
		Description: "No project of " + organization.Slug + " carries this repository's fingerprint. Which project is this clone?",
		Explanation: "A project record is bound to its repository by the root commits of the repository, which survive renames and forks. Binding writes the fingerprint into the record and pushes it.",
		Choices:     choices,
		Default:     createProjectChoice,
	})
	if err != nil {
		return request, "", err
	}
	if picked != createProjectChoice {
		request.Project = picked
		return request, "bind it to project " + picked, nil
	}
	if len(organization.Teams) == 0 {
		return request, "", fmt.Errorf("%s has no teams yet; run rein team create first, since a project belongs to at least one team", organization.Slug)
	}
	answers, err := askProjectCreate(prompter, organization, projectCreateAnswers{name: "", path: location.Path}, false)
	if err != nil {
		return request, "", err
	}
	request.Create = &orchestration.CreateProjectRequest{Name: answers.name, Description: answers.description, Teams: answers.teams, Labels: answers.labels}
	return request, fmt.Sprintf("create the project %q in %s and bind it", answers.name, organization.Slug), nil
}

func printInitialized(environment *commandEnvironment, result orchestration.InitializeResult) {
	verb := "bound"
	if result.Created {
		verb = "created and bound"
	}
	fmt.Fprintf(environment.out, "%s %s to %s (fingerprint: %d root commit(s))\n", verb, result.Folder, result.Address, len(result.Fingerprint))
	for _, moveOut := range result.MovedOut {
		fmt.Fprintf(environment.out, "moved out for %s: %s\n  snapshot: %s\n", moveOut.Agent, strings.Join(moveOut.Paths, ", "), moveOut.SnapshotPath)
	}
	if len(result.MovedOut) == 0 {
		fmt.Fprintf(environment.out, "nothing to move out: the clone held no agent settings\n")
	}
	if result.IgnoreChanged {
		fmt.Fprintf(environment.out, ".gitignore: rein block added\n")
	}
	switch {
	case result.Commit != "":
		fmt.Fprintf(environment.out, "committed %s: %s\n", result.Commit[:7], moveOutCommitMessageShown)
	case result.CommitError != "":
		fmt.Fprintf(environment.errOut, "%s\n", environment.style().Yellow(fmt.Sprintf("warning: the move-out is staged but not committed: %s", result.CommitError)))
	default:
		fmt.Fprintf(environment.out, "the move-out is staged, not committed\n")
	}
	fmt.Fprintf(environment.out, "next: rein sync, once it exists, writes the settings this project resolves\n")
}

const moveOutCommitMessageShown = "move agent settings out to rein (rein init)"
