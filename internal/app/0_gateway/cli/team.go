package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/scaffold"
	"github.com/Danidani23/agents-rein/internal/ports"
)

func newTeamCommand(environment *commandEnvironment) *cobra.Command {
	team := &cobra.Command{
		Use:   "team",
		Short: "Create teams in an organisation",
		Long: "A team is a record in the org repository, declarations/organizations/<org>/teams/<slug>/team.yaml,\n" +
			"with an id, a slug, a name and a description. Projects name the teams they belong to, and a\n" +
			"team's own declarations later live next to its record. The organisation's owner writes team\n" +
			"records straight to main; everyone else proposes them through a pull request.",
	}
	team.AddCommand(newTeamCreateCommand(environment))
	return team
}

// teamCreateAnswers are the answers team create needs, from flags or questions.
type teamCreateAnswers struct {
	organization string
	name         string
	description  string
	yes          bool
}

func newTeamCreateCommand(environment *commandEnvironment) *cobra.Command {
	var answers teamCreateAnswers
	command := &cobra.Command{
		Use:   "create",
		Short: "Create a team in the active organisation and push its record to main",
		Long: "create asks for the team's name and description, writes the team record into the\n" +
			"organisation's clone under ~/.rein/orgs/, commits it and pushes it to main. The clone is\n" +
			"brought up to date first, and a failed push leaves it as the remote has it. Without a\n" +
			"terminal, --name and --yes are required.",
		Example: "  rein team create\n  rein team create --name \"Web\" --description \"the web shop\" --yes",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runTeamCreate(environment, answers)
		},
	}
	command.Flags().StringVar(&answers.organization, "org", "", "the organisation's slug (default: the active one)")
	command.Flags().StringVar(&answers.name, "name", "", "the team's name; its slug is derived from it")
	command.Flags().StringVar(&answers.description, "description", "", "one line about the team")
	command.Flags().BoolVar(&answers.yes, "yes", false, "do not ask for confirmation")
	return command
}

const teamCreateIntro = "team create adds a team to an organisation: a record in the org repository that projects " +
	"name as theirs. The record is committed and pushed to main directly, as the organisation's " +
	"owner may do."

func runTeamCreate(environment *commandEnvironment, answers teamCreateAnswers) error {
	orchestrator, err := environment.orchestrate()
	if err != nil {
		return err
	}
	err = requireIdentity(orchestrator)
	if err != nil {
		return err
	}
	organization, err := orchestrator.DescribeOrganization(answers.organization)
	if err != nil {
		return err
	}
	prompter := environment.prompter()
	if !prompter.CanAsk() {
		if answers.name == "" || !answers.yes {
			return refusal.New("no terminal to ask on; pass --name and --yes, and --description or --org to change the defaults")
		}
	} else {
		prompter.Intro(teamCreateIntro)
		answers, err = askTeamCreate(prompter, answers)
		if err != nil {
			return refusal.New(err.Error())
		}
	}
	slug := scaffold.SlugOf(answers.name)
	if slug == "" {
		return refusal.New("the name must contain at least one letter or digit")
	}
	fmt.Fprintf(environment.out, "This will add the team %q, slug %s, to %s and push its record to main.\n", answers.name, slug, organization.Slug)
	if !answers.yes {
		confirmed, err := prompter.Confirm("Continue?", "The record is written into the organisation's clone, committed and pushed. If the push is rejected, the clone is put back as the remote has it and nothing changes.")
		if err != nil {
			return refusal.New(err.Error())
		}
		if !confirmed {
			return refusal.New("nothing changed")
		}
	}
	result, err := orchestrator.CreateTeam(orchestration.CreateTeamRequest{Organization: answers.organization, Name: answers.name, Description: answers.description})
	if err != nil {
		return err
	}
	fmt.Fprintf(environment.out, "created team %s (%s) in %s\nrecord: %s, pushed to main\n", result.Slug, result.Name, result.Organization, result.RelativePath)
	return nil
}

func askTeamCreate(prompter ports.Prompter, answers teamCreateAnswers) (teamCreateAnswers, error) {
	var err error
	if answers.name == "" {
		answers.name, err = prompter.Ask(ports.Text{
			Title:       "Team name",
			Description: "The name people see; the slug is derived from it, lowercase with hyphens.",
			Explanation: "The slug becomes part of every address under the team, such as organizations.acme.teams.web.rules.testing, and never changes afterwards.",
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
			Description: "One line about what the team does.",
			Default:     "The " + answers.name + " team",
		})
		if err != nil {
			return answers, err
		}
	}
	return answers, nil
}
