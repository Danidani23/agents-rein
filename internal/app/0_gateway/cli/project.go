package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/scaffold"
	"github.com/Danidani23/agents-rein/internal/ports"
)

func newProjectCommand(environment *commandEnvironment) *cobra.Command {
	project := &cobra.Command{
		Use:   "project",
		Short: "Create projects in an organisation",
		Long: "A project is a record in the org repository, declarations/organizations/<org>/projects/<slug>/project.yaml,\n" +
			"with an id, a slug, a name, a description, the teams it belongs to, its labels from the\n" +
			"registry (language, kind, data) and, in a monorepo, its path. rein init later binds a clone\n" +
			"to it by fingerprint. The organisation's owner writes project records straight to main;\n" +
			"everyone else proposes them through a pull request.",
	}
	project.AddCommand(newProjectCreateCommand(environment), newProjectEditCommand(environment))
	return project
}

// projectCreateAnswers are the answers project create needs, from flags or
// questions.
type projectCreateAnswers struct {
	organization string
	name         string
	// nameDefault is offered when no name was given: the repository's name
	// when init creates the project on the spot.
	nameDefault string
	description string
	teams       []string
	labels      []string
	path        string
	yes         bool
	// editing means the name is settled: project edit asks the rest.
	editing bool
}

func newProjectCreateCommand(environment *commandEnvironment) *cobra.Command {
	var answers projectCreateAnswers
	command := &cobra.Command{
		Use:   "create",
		Short: "Create a project in the active organisation and push its record to main",
		Long: "create asks for the project's name, description, teams, labels and path, writes the project\n" +
			"record into the organisation's clone under ~/.rein/orgs/, commits it and pushes it to main.\n" +
			"The teams must exist and the labels be in the registry. Without a terminal, --name, --team\n" +
			"and --yes are required.",
		Example: "  rein project create\n  rein project create --name \"Shop\" --team web --label language:go --label kind:web-app --yes",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runProjectCreate(environment, answers)
		},
	}
	command.Flags().StringVar(&answers.organization, "org", "", "the organisation's slug (default: the active one)")
	command.Flags().StringVar(&answers.name, "name", "", "the project's name; its slug is derived from it")
	command.Flags().StringVar(&answers.description, "description", "", "one line about the project")
	command.Flags().StringArrayVar(&answers.teams, "team", nil, "a team the project belongs to, by slug; repeat for several")
	command.Flags().StringArrayVar(&answers.labels, "label", nil, "a label as category:value, from the registry; repeat for several")
	command.Flags().StringVar(&answers.path, "path", "", "the project's folder inside its repository (default: the root)")
	command.Flags().BoolVar(&answers.yes, "yes", false, "do not ask for confirmation")
	return command
}

const projectCreateIntro = "project create adds a project to an organisation: a record in the org repository that says " +
	"which teams it belongs to and what it is, through labels from the registry. The record is " +
	"committed and pushed to main directly, as the organisation's owner may do. Binding a clone " +
	"to the project is rein init, later."

func runProjectCreate(environment *commandEnvironment, answers projectCreateAnswers) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return err
	}
	defer release()
	err = requireLogin(orchestrator)
	if err != nil {
		return err
	}
	organization, err := orchestrator.DescribeOrganization(answers.organization)
	if err != nil {
		return err
	}
	if len(organization.Teams) == 0 {
		return refusal.New(fmt.Sprintf("%s has no teams yet; run rein team create first, since a project belongs to at least one team", organization.Slug))
	}
	prompter := environment.prompter()
	if !prompter.CanAsk() {
		if answers.name == "" || len(answers.teams) == 0 || !answers.yes {
			return refusal.New("no terminal to ask on; pass --name, --team and --yes, and --description, --label, --path or --org to change the defaults")
		}
	} else {
		prompter.Intro(projectCreateIntro)
	}
	again := false
	for {
		if prompter.CanAsk() {
			answers, err = askProjectCreate(prompter, organization, answers, again)
			if err != nil {
				return refusal.New(err.Error())
			}
		}
		slug := scaffold.SlugOf(answers.name)
		if slug == "" {
			return refusal.New("the name must contain at least one letter or digit")
		}
		if reason := missingRequiredLabels(organization, answers.labels); reason != "" {
			return refusal.New(reason)
		}
		fmt.Fprintf(environment.out, "This will add the project %q, slug %s, to %s for the team(s) %s, with %s, and push its record to main.\n", answers.name, slug, organization.Slug, strings.Join(answers.teams, ", "), labelsPhrase(answers.labels))
		if answers.yes {
			break
		}
		decision, err := prompter.ConfirmOrEdit("Continue?", "The record is written into the organisation's clone, committed and pushed. If the push is rejected, the clone is put back as the remote has it and nothing changes. The fingerprint that binds a clone to the project is added by rein init.")
		if err != nil {
			return refusal.New(err.Error())
		}
		if decision == ports.DecisionEdit {
			again = true
			continue
		}
		if decision == ports.DecisionNo {
			return refusal.New("nothing changed")
		}
		break
	}
	result, err := orchestrator.CreateProject(orchestration.CreateProjectRequest{Organization: answers.organization, Name: answers.name, Description: answers.description, Teams: answers.teams, Labels: answers.labels, Path: answers.path})
	if err != nil {
		return err
	}
	fmt.Fprintf(environment.out, "created project %s (%s) in %s for the team(s) %s\nrecord: %s, pushed to main\n", result.Slug, result.Name, result.Organization, strings.Join(result.Teams, ", "), result.RelativePath)
	return nil
}

func askProjectCreate(prompter ports.Prompter, organization orchestration.OrganizationDescription, answers projectCreateAnswers, again bool) (projectCreateAnswers, error) {
	var err error
	if !answers.editing && (again || answers.name == "") {
		answers.name, err = prompter.Ask(ports.Text{
			Title:       "Project name",
			Description: "The name people see; the slug is derived from it, lowercase with hyphens.",
			Explanation: "The slug becomes part of every address under the project, such as organizations.acme.projects.shop.rules.checkout, and never changes afterwards.",
			Default:     firstOf(answers.name, answers.nameDefault),
			Validate: func(answer string) string {
				if scaffold.SlugOf(answer) == "" {
					return "The name must contain at least one letter or digit."
				}
				return ""
			},
		})
		if err != nil {
			return answers, err
		}
	}
	if again || answers.description == "" {
		answers.description, err = prompter.Ask(ports.Text{
			Title:       "Description",
			Description: "One line about what the project is; Enter for none.",
			Default:     answers.description,
			Optional:    true,
		})
		if err != nil {
			return answers, err
		}
	}
	if again || len(answers.teams) == 0 {
		var choices []ports.Choice
		for _, team := range organization.Teams {
			choices = append(choices, ports.Choice{Name: team.Slug, Description: team.Name})
		}
		defaultTeam := strings.Join(answers.teams, ",")
		if defaultTeam == "" && len(choices) == 1 {
			defaultTeam = choices[0].Name
		}
		answers.teams, err = prompter.ChooseMany(ports.Select{
			Title:       "Teams",
			Description: "The teams the project belongs to; their declarations apply to it.",
			Explanation: "A project resolves its settings from the organisation, then from each of its teams, then from its own level and the user's. Several teams are fine; conflicts between them are what rein review flags.",
			Choices:     choices,
			Default:     defaultTeam,
		})
		if err != nil {
			return answers, err
		}
	}
	if again || len(answers.labels) == 0 {
		answers.labels, err = askProjectLabels(prompter, organization, answers.labels)
		if err != nil {
			return answers, err
		}
	}
	if again || answers.path == "" {
		answers.path, err = prompter.Ask(ports.Text{
			Title:       "Path",
			Description: "The project's folder inside its repository; the root unless it is a monorepo.",
			Explanation: "In a monorepo several projects share one repository, each under its own folder; rein writes each project's settings under its path.",
			Default:     firstOf(answers.path, "."),
		})
		if err != nil {
			return answers, err
		}
	}
	return answers, nil
}

// askProjectLabels asks the required categories one by one, at least one
// value each, then every optional value in one list; previous answers are
// the defaults.
func askProjectLabels(prompter ports.Prompter, organization orchestration.OrganizationDescription, previous []string) ([]string, error) {
	previousByCategory := map[string][]string{}
	previousOptional := map[string]bool{}
	for _, label := range previous {
		category, value, ok := declaration.SplitLabel(label)
		if ok {
			previousByCategory[category] = append(previousByCategory[category], value)
			previousOptional[label] = true
		}
	}
	const explanation = "Declarations carry applyIf and doNotApplyIf selectors over these labels; a Go service gets the Go rules without anyone listing it. rein label changes the registry."
	introduction := "Labels say what the project is, so declarations can apply to projects like it. "
	var labels []string
	var optional []ports.Choice
	var optionalDefaults []string
	for _, category := range organization.LabelCategories {
		if !category.Required {
			for _, value := range category.Values {
				name := category.Category + ":" + value.Value
				optional = append(optional, ports.Choice{Name: name, Description: value.Description})
				if previousOptional[name] {
					optionalDefaults = append(optionalDefaults, name)
				}
			}
			continue
		}
		var choices []ports.Choice
		for _, value := range category.Values {
			choices = append(choices, ports.Choice{Name: value.Value, Description: value.Description})
		}
		picked, err := prompter.ChooseMany(ports.Select{
			Title:       "Label " + category.Category,
			Description: introduction + "Label " + category.Category + ", required: at least one; several separated by commas.",
			Explanation: explanation,
			Choices:     choices,
			Default:     strings.Join(previousByCategory[category.Category], ","),
		})
		if err != nil {
			return nil, err
		}
		introduction = ""
		for _, value := range picked {
			labels = append(labels, category.Category+":"+value)
		}
	}
	if len(optional) == 0 {
		return labels, nil
	}
	picked, err := prompter.ChooseMany(ports.Select{
		Title:       "Other labels",
		Description: introduction + "Other labels, any that apply, several separated by commas; Enter for none.",
		Explanation: explanation,
		Choices:     optional,
		Default:     strings.Join(optionalDefaults, ","),
		Optional:    true,
	})
	if err != nil {
		return nil, err
	}
	return append(labels, picked...), nil
}

// missingRequiredLabels says which required categories the labels miss,
// as a refusal that names the values to pass; "" when none is missing.
func missingRequiredLabels(organization orchestration.OrganizationDescription, labels []string) string {
	present := map[string]bool{}
	for _, label := range labels {
		category, _, ok := declaration.SplitLabel(label)
		if ok {
			present[category] = true
		}
	}
	var missing []string
	for _, category := range organization.LabelCategories {
		if category.Required && !present[category.Category] {
			var values []string
			for _, value := range category.Values {
				values = append(values, value.Value)
			}
			missing = append(missing, fmt.Sprintf("the label category %s is required; pass --label %s:<value>, one of %s", category.Category, category.Category, strings.Join(values, ", ")))
		}
	}
	return strings.Join(missing, "; ")
}

// labelsPhrase names labels for a summary line.
func labelsPhrase(labels []string) string {
	if len(labels) == 0 {
		return "no labels"
	}
	return "labels " + strings.Join(labels, ", ")
}

func newProjectEditCommand(environment *commandEnvironment) *cobra.Command {
	var answers projectCreateAnswers
	command := &cobra.Command{
		Use:   "edit [slug]",
		Short: "Change a project's description, teams, labels or path; asks which project when the slug is left out",
		Long: "edit rewrites a project record in the organisation's clone under ~/.rein/orgs/, commits it and\n" +
			"pushes it to main, as create does. On a terminal it asks the description, the teams, the labels\n" +
			"and the path again with the current values as the defaults; with flags only the given ones\n" +
			"change. The slug, the name and the fingerprint never change here. Without a slug it asks which\n" +
			"project.",
		Example: "  rein project edit\n  rein project edit shop --label kind:service --label language:go --yes",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			given := map[string]bool{}
			for _, name := range []string{"description", "team", "label", "path"} {
				given[name] = command.Flags().Changed(name)
			}
			return runProjectEdit(environment, arguments, answers, given)
		},
	}
	command.Flags().StringVar(&answers.organization, "org", "", "the organisation's slug (default: the active one)")
	command.Flags().StringVar(&answers.description, "description", "", "one line about the project")
	command.Flags().StringArrayVar(&answers.teams, "team", nil, "a team the project belongs to, by slug; repeat for several, replacing the current ones")
	command.Flags().StringArrayVar(&answers.labels, "label", nil, "a label as category:value; repeat for several, replacing the current ones")
	command.Flags().StringVar(&answers.path, "path", "", "the project's folder inside its repository")
	command.Flags().BoolVar(&answers.yes, "yes", false, "do not ask for confirmation")
	return command
}

func runProjectEdit(environment *commandEnvironment, arguments []string, given projectCreateAnswers, flagsGiven map[string]bool) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return err
	}
	defer release()
	err = requireLogin(orchestrator)
	if err != nil {
		return err
	}
	organization, err := orchestrator.DescribeOrganization(given.organization)
	if err != nil {
		return err
	}
	prompter := environment.prompter()
	slug, err := projectSlug(prompter, organization, arguments)
	if err != nil {
		return err
	}
	var current orchestration.ProjectSummary
	found := false
	for _, project := range organization.Projects {
		if project.Slug == slug {
			current, found = project, true
		}
	}
	if !found {
		return refusal.New(fmt.Sprintf("project %q does not exist in %s; rein project edit without a slug lists them", slug, organization.Slug))
	}
	answers := projectCreateAnswers{organization: organization.Slug, name: current.Name, description: current.Description, teams: current.Teams, labels: current.Labels, path: firstOf(current.Path, "."), yes: given.yes, editing: true}
	anyGiven := false
	for _, name := range []string{"description", "team", "label", "path"} {
		anyGiven = anyGiven || flagsGiven[name]
	}
	if flagsGiven["description"] {
		answers.description = given.description
	}
	if flagsGiven["team"] {
		answers.teams = given.teams
	}
	if flagsGiven["label"] {
		answers.labels = given.labels
	}
	if flagsGiven["path"] {
		answers.path = given.path
	}
	if !anyGiven && !prompter.CanAsk() {
		return refusal.New("nothing to change; pass --description, --team, --label or --path, or run on a terminal to be asked")
	}
	again := !anyGiven
	for {
		if again && prompter.CanAsk() {
			answers, err = askProjectCreate(prompter, organization, answers, true)
			if err != nil {
				return refusal.New(err.Error())
			}
		}
		if reason := missingRequiredLabels(organization, answers.labels); reason != "" {
			return refusal.New(reason)
		}
		if answers.description == current.Description && strings.Join(answers.teams, ",") == strings.Join(current.Teams, ",") && strings.Join(answers.labels, ",") == strings.Join(current.Labels, ",") && answers.path == firstOf(current.Path, ".") {
			return refusal.New("nothing changed on project " + slug)
		}
		fmt.Fprintf(environment.out, "This will change the project %s in %s: description %q, teams %s, %s, path %s, and push its record to main.\n", slug, organization.Slug, answers.description, strings.Join(answers.teams, ", "), labelsPhrase(answers.labels), answers.path)
		if answers.yes {
			break
		}
		decision, err := prompter.ConfirmOrEdit("Continue?", "The record is rewritten in the organisation's clone, committed and pushed. If the push is rejected, the clone is put back as the remote has it and nothing changes. Declarations apply by the new labels at the next sync of its clones.")
		if err != nil {
			return refusal.New(err.Error())
		}
		if decision == ports.DecisionEdit {
			again = true
			continue
		}
		if decision == ports.DecisionNo {
			return refusal.New("nothing changed")
		}
		break
	}
	result, err := orchestrator.EditProject(orchestration.EditProjectRequest{Organization: organization.Slug, Slug: slug, Description: answers.description, Teams: answers.teams, Labels: answers.labels, Path: answers.path})
	if err != nil {
		return err
	}
	fmt.Fprintf(environment.out, "changed project %s (%s) in %s: %s\nrecord: %s, pushed to main\n", result.Slug, result.Name, result.Organization, strings.Join(result.Changed, ", "), result.RelativePath)
	return nil
}

// projectSlug is the slug given as the argument or, when none was, the one
// picked from the organisation's projects on a terminal.
func projectSlug(prompter ports.Prompter, organization orchestration.OrganizationDescription, arguments []string) (string, error) {
	if len(arguments) == 1 {
		return arguments[0], nil
	}
	if len(organization.Projects) == 0 {
		return "", refusal.New(fmt.Sprintf("%s has no projects yet; rein project create or rein init makes one", organization.Slug))
	}
	if !prompter.CanAsk() {
		return "", refusal.New("name the project; rein project edit without a slug lists them on a terminal")
	}
	var choices []ports.Choice
	for _, project := range organization.Projects {
		choices = append(choices, ports.Choice{Name: project.Slug, Description: project.Name + ", teams " + strings.Join(project.Teams, ", ")})
	}
	defaultSlug := ""
	if len(choices) == 1 {
		defaultSlug = choices[0].Name
	}
	slug, err := prompter.Choose(ports.Select{
		Title:       "Project",
		Description: "The project to change.",
		Choices:     choices,
		Default:     defaultSlug,
	})
	if err != nil {
		return "", refusal.New(err.Error())
	}
	return slug, nil
}
