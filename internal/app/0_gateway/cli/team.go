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

func newTeamCommand(environment *commandEnvironment) *cobra.Command {
	team := &cobra.Command{
		Use:   "team",
		Short: "Create, list and edit the teams of an organisation",
		Long: "A team is a record in the org repository, declarations/organizations/<org>/teams/<slug>/team.yaml,\n" +
			"with an id, a slug, a name and a description. Projects name the teams they belong to, and a\n" +
			"team's own declarations later live next to its record. The organisation's owner writes team\n" +
			"records straight to main; everyone else proposes them through a pull request.",
	}
	team.AddCommand(newTeamCreateCommand(environment), newTeamListCommand(environment), newTeamEditCommand(environment))
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
	prompter := environment.prompter()
	if !prompter.CanAsk() {
		if answers.name == "" || !answers.yes {
			return refusal.New("no terminal to ask on; pass --name and --yes, and --description or --org to change the defaults")
		}
	} else {
		prompter.Intro(teamCreateIntro)
	}
	again := false
	for {
		if prompter.CanAsk() {
			answers, err = askTeamCreate(prompter, answers, again)
			if err != nil {
				return refusal.New(err.Error())
			}
		}
		slug := scaffold.SlugOf(answers.name)
		if slug == "" {
			return refusal.New("the name must contain at least one letter or digit")
		}
		fmt.Fprintf(environment.out, "This will add the team %q, slug %s, to %s and push its record to main.\n", answers.name, slug, organization.Slug)
		if answers.yes {
			break
		}
		decision, err := prompter.ConfirmOrEdit("Continue?", "The record is written into the organisation's clone, committed and pushed. If the push is rejected, the clone is put back as the remote has it and nothing changes.")
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
	result, err := orchestrator.CreateTeam(orchestration.CreateTeamRequest{Organization: answers.organization, Name: answers.name, Description: answers.description})
	if err != nil {
		return err
	}
	fmt.Fprintf(environment.out, "created team %s (%s) in %s\nrecord: %s, pushed to main\n", result.Slug, result.Name, result.Organization, result.RelativePath)
	return nil
}

func askTeamCreate(prompter ports.Prompter, answers teamCreateAnswers, again bool) (teamCreateAnswers, error) {
	var err error
	if again || answers.name == "" {
		answers.name, err = prompter.Ask(ports.Text{
			Title:       "Team name",
			Description: "The name people see; the slug is derived from it, lowercase with hyphens.",
			Explanation: "The slug becomes part of every address under the team, such as organizations.acme.teams.web.rules.testing, and never changes afterwards.",
			Default:     answers.name,
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
			Description: "One line about what the team does; Enter for none.",
			Default:     answers.description,
			Optional:    true,
		})
		if err != nil {
			return answers, err
		}
	}
	return answers, nil
}

func newTeamListCommand(environment *commandEnvironment) *cobra.Command {
	var organizationSlug string
	command := &cobra.Command{
		Use:   "list",
		Short: "Show the teams as the org repository's main holds them, with their projects",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, release, err := environment.orchestrateLocked()
			if err != nil {
				return err
			}
			defer release()
			err = requireLogin(orchestrator)
			if err != nil {
				return err
			}
			organization, err := orchestrator.RefreshOrganization(organizationSlug)
			if err != nil {
				return err
			}
			if len(organization.Teams) == 0 {
				fmt.Fprintf(environment.out, "%s has no teams yet; rein team create makes one\n", organization.Slug)
				return nil
			}
			rows := [][]string{{"SLUG", "NAME", "DESCRIPTION", "PROJECTS"}}
			for _, team := range organization.Teams {
				rows = append(rows, []string{team.Slug, team.Name, team.Description, strings.Join(team.Projects, " ")})
			}
			printTable(environment, rows)
			return nil
		},
	}
	command.Flags().StringVar(&organizationSlug, "org", "", "the organisation's slug (default: the active one)")
	return command
}

func newTeamEditCommand(environment *commandEnvironment) *cobra.Command {
	var organizationSlug, description string
	var yes bool
	command := &cobra.Command{
		Use:   "edit [slug]",
		Short: "Change a team's description; asks which team when the slug is left out",
		Long: "edit rewrites a team record in the organisation's clone, commits it and pushes it to main,\n" +
			"as create does. The description is asked with the current one as the default, or given as\n" +
			"--description. The slug and the name never change.",
		Example: "  rein team edit\n  rein team edit web --description \"the web shop\" --yes",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			return runTeamEdit(environment, arguments, organizationSlug, description, command.Flags().Changed("description"), yes)
		},
	}
	command.Flags().StringVar(&organizationSlug, "org", "", "the organisation's slug (default: the active one)")
	command.Flags().StringVar(&description, "description", "", "one line about what the team does")
	command.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return command
}

func runTeamEdit(environment *commandEnvironment, arguments []string, organizationSlug string, description string, descriptionGiven bool, yes bool) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return err
	}
	defer release()
	err = requireLogin(orchestrator)
	if err != nil {
		return err
	}
	organization, err := orchestrator.DescribeOrganization(organizationSlug)
	if err != nil {
		return err
	}
	prompter := environment.prompter()
	slug, err := teamSlug(prompter, organization, arguments)
	if err != nil {
		return err
	}
	var current orchestration.TeamSummary
	found := false
	for _, team := range organization.Teams {
		if team.Slug == slug {
			current, found = team, true
		}
	}
	if !found {
		return refusal.New(fmt.Sprintf("team %q does not exist in %s; rein team list shows them", slug, organization.Slug))
	}
	if !descriptionGiven && !prompter.CanAsk() {
		return refusal.New("nothing to change; pass --description, or run on a terminal to be asked")
	}
	if !descriptionGiven {
		description, err = prompter.Ask(ports.Text{
			Title:       "Description",
			Description: "One line about what the team does; Enter keeps the current one.",
			Default:     current.Description,
			Optional:    true,
		})
		if err != nil {
			return refusal.New(err.Error())
		}
	}
	if description == current.Description {
		return refusal.New("nothing changed on team " + slug)
	}
	fmt.Fprintf(environment.out, "This will change the description of team %s in %s to %q and push its record to main.\n", slug, organization.Slug, description)
	if !yes {
		if !prompter.CanAsk() {
			return refusal.New("no terminal to confirm on; pass --yes")
		}
		confirmed, err := prompter.Confirm("Continue?", "The record is rewritten in the organisation's clone, committed and pushed. If the push is rejected, the clone is put back as the remote has it and nothing changes.")
		if err != nil {
			return refusal.New(err.Error())
		}
		if !confirmed {
			return refusal.New("nothing changed")
		}
	}
	result, err := orchestrator.EditTeam(orchestration.EditTeamRequest{Organization: organization.Slug, Slug: slug, Description: description})
	if err != nil {
		return err
	}
	fmt.Fprintf(environment.out, "changed team %s (%s) in %s: description\nrecord: %s, pushed to main\n", result.Slug, result.Name, result.Organization, result.RelativePath)
	return nil
}

// teamSlug is the slug given as the argument or, when none was, the one
// picked from the organisation's teams on a terminal.
func teamSlug(prompter ports.Prompter, organization orchestration.OrganizationDescription, arguments []string) (string, error) {
	if len(arguments) == 1 {
		return arguments[0], nil
	}
	if len(organization.Teams) == 0 {
		return "", refusal.New(fmt.Sprintf("%s has no teams yet; rein team create makes one", organization.Slug))
	}
	if !prompter.CanAsk() {
		return "", refusal.New("name the team; rein team list shows them")
	}
	var choices []ports.Choice
	for _, team := range organization.Teams {
		choices = append(choices, ports.Choice{Name: team.Slug, Description: team.Name})
	}
	defaultSlug := ""
	if len(choices) == 1 {
		defaultSlug = choices[0].Name
	}
	slug, err := prompter.Choose(ports.Select{Title: "Team", Description: "The team to change.", Choices: choices, Default: defaultSlug})
	if err != nil {
		return "", refusal.New(err.Error())
	}
	return slug, nil
}
