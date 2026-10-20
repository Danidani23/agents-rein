package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
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
	project.AddCommand(newProjectCreateCommand(environment))
	return project
}

// projectCreateAnswers are the answers project create needs, from flags or
// questions.
type projectCreateAnswers struct {
	organization string
	name         string
	description  string
	teams        []string
	labels       []string
	path         string
	yes          bool
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
	err = requireIdentity(orchestrator)
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
		answers, err = askProjectCreate(prompter, organization, answers)
		if err != nil {
			return refusal.New(err.Error())
		}
	}
	slug := scaffold.SlugOf(answers.name)
	if slug == "" {
		return refusal.New("the name must contain at least one letter or digit")
	}
	labels := "no labels"
	if len(answers.labels) > 0 {
		labels = "labels " + strings.Join(answers.labels, ", ")
	}
	fmt.Fprintf(environment.out, "This will add the project %q, slug %s, to %s for the team(s) %s, with %s, and push its record to main.\n", answers.name, slug, organization.Slug, strings.Join(answers.teams, ", "), labels)
	if !answers.yes {
		confirmed, err := prompter.Confirm("Continue?", "The record is written into the organisation's clone, committed and pushed. If the push is rejected, the clone is put back as the remote has it and nothing changes. The fingerprint that binds a clone to the project is added by rein init.")
		if err != nil {
			return refusal.New(err.Error())
		}
		if !confirmed {
			return refusal.New("nothing changed")
		}
	}
	result, err := orchestrator.CreateProject(orchestration.CreateProjectRequest{Organization: answers.organization, Name: answers.name, Description: answers.description, Teams: answers.teams, Labels: answers.labels, Path: answers.path})
	if err != nil {
		return err
	}
	fmt.Fprintf(environment.out, "created project %s (%s) in %s for the team(s) %s\nrecord: %s, pushed to main\n", result.Slug, result.Name, result.Organization, strings.Join(result.Teams, ", "), result.RelativePath)
	return nil
}

func askProjectCreate(prompter ports.Prompter, organization orchestration.OrganizationDescription, answers projectCreateAnswers) (projectCreateAnswers, error) {
	var err error
	if answers.name == "" {
		answers.name, err = prompter.Ask(ports.Text{
			Title:       "Project name",
			Description: "The name people see; the slug is derived from it, lowercase with hyphens.",
			Explanation: "The slug becomes part of every address under the project, such as organizations.acme.projects.shop.rules.checkout, and never changes afterwards.",
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
	if answers.description == "" {
		answers.description, err = prompter.Ask(ports.Text{
			Title:       "Description",
			Description: "One line about what the project is.",
			Default:     "The " + answers.name + " project",
		})
		if err != nil {
			return answers, err
		}
	}
	if len(answers.teams) == 0 {
		var choices []ports.Choice
		for _, team := range organization.Teams {
			choices = append(choices, ports.Choice{Name: team.Slug, Description: team.Name})
		}
		defaultTeam := ""
		if len(choices) == 1 {
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
	if len(answers.labels) == 0 {
		for _, category := range organization.LabelCategories {
			var choices []ports.Choice
			for _, value := range category.Values {
				choices = append(choices, ports.Choice{Name: value.Value, Description: value.Description})
			}
			picked, err := prompter.ChooseMany(ports.Select{
				Title:       "Label: " + category.Category,
				Description: "Label " + category.Category + ": what the project is, so declarations can apply to projects like it. Enter for none.",
				Explanation: "Declarations carry applyIf and doNotApplyIf selectors over these labels; a Go service gets the Go rules without anyone listing it. The registry is edited in the org repository.",
				Choices:     choices,
				Optional:    true,
			})
			if err != nil {
				return answers, err
			}
			for _, value := range picked {
				answers.labels = append(answers.labels, category.Category+":"+value)
			}
		}
	}
	if answers.path == "" {
		answers.path, err = prompter.Ask(ports.Text{
			Title:       "Path",
			Description: "The project's folder inside its repository; the root unless it is a monorepo.",
			Explanation: "In a monorepo several projects share one repository, each under its own folder; rein writes each project's settings under its path.",
			Default:     ".",
		})
		if err != nil {
			return answers, err
		}
	}
	return answers, nil
}
