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

// orgCreateAnswers are the answers org create needs, from flags or questions.
type orgCreateAnswers struct {
	name           string
	description    string
	owner          string
	repositoryName string
	visibility     string
	yes            bool
}

func newOrgCreateCommand(environment *commandEnvironment) *cobra.Command {
	var answers orgCreateAnswers
	command := &cobra.Command{
		Use:   "create",
		Short: "Create an organisation: its GitHub repository, the first commit, protection, and connect to it",
		Long: "create makes a new org repository on GitHub, the one place an organisation keeps its agent\n" +
			"settings. It asks for the organisation's name, the repository's name, owner and visibility,\n" +
			"then creates the repository, writes the organisation record, the seeded label registry\n" +
			"(language, kind, data) and CODEOWNERS in a first commit, protects main so that changes come\n" +
			"through pull requests reviewed by a code owner, and connects this machine to it, which also\n" +
			"snapshots your agents' homes into ~/.rein/snapshots/ as the record of what was there before\n" +
			"rein. Everything it does is undone when a step fails. Without a terminal, --name and --yes\n" +
			"are required; the other flags have defaults.",
		Example: "  rein org create\n  rein org create --name \"Acme\" --owner acme-inc --visibility private --yes",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runOrgCreate(environment, answers)
		},
	}
	command.Flags().StringVar(&answers.name, "name", "", "the organisation's name; its slug is derived from it")
	command.Flags().StringVar(&answers.description, "description", "", "one line about the organisation")
	command.Flags().StringVar(&answers.owner, "owner", "", "the GitHub account or organisation that owns the repository (default: your account)")
	command.Flags().StringVar(&answers.repositoryName, "repository", "", "the repository's name (default: <slug>-agent-settings)")
	command.Flags().StringVar(&answers.visibility, "visibility", "", "private or public (default: private)")
	command.Flags().BoolVar(&answers.yes, "yes", false, "do not ask for confirmation")
	return command
}

const orgCreateIntro = "org create makes a new org repository on GitHub for an organisation's agent settings: " +
	"the repository, a first commit with the organisation record, the seeded label registry and " +
	"CODEOWNERS, protection of main so changes come through reviewed pull requests, and the " +
	"connection of this machine to it, with a snapshot of your agents' homes as the record of " +
	"what was there before rein."

const orgCreateConfirmExplanation = "The repository is created on GitHub under the owner you chose, with you as its code " +
	"owner, and this machine connects to it. Nothing is placed into any agent yet; that is sync. " +
	"If a step fails, the repository is deleted again and nothing is left behind."

func runOrgCreate(environment *commandEnvironment, answers orgCreateAnswers) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return err
	}
	defer release()
	identity, err := orchestrator.Identity.Who()
	if err != nil {
		return refusal.New(err.Error())
	}
	prompter := environment.prompter()
	if !prompter.CanAsk() {
		if answers.name == "" || !answers.yes {
			return refusal.New("no terminal to ask on; pass --name and --yes, and --owner, --repository, --visibility, --description to change the defaults")
		}
	} else {
		prompter.Intro(orgCreateIntro)
		answers, err = askOrgCreate(prompter, identity, answers)
		if err != nil {
			return refusal.New(err.Error())
		}
	}
	request, reason := orgCreateRequest(identity, answers)
	if reason != "" {
		return refusal.New(reason)
	}
	visibility := "private"
	if !request.Private {
		visibility = "public"
	}
	fmt.Fprintf(environment.out, "This will create %s/%s (%s) on GitHub for the organisation %q, slug %s,\nwith %s as code owner, protect main, and connect this machine to it.\n",
		request.Owner, request.RepositoryName, visibility, request.Name, scaffold.SlugOf(request.Name), "@"+request.Codeowner)
	if !answers.yes {
		confirmed, err := prompter.Confirm("Continue?", orgCreateConfirmExplanation)
		if err != nil {
			return refusal.New(err.Error())
		}
		if !confirmed {
			return refusal.New("nothing changed")
		}
	}
	result, err := orchestrator.CreateOrganization(request)
	if err != nil {
		return err
	}
	fmt.Fprintf(environment.out, "created %s/%s (%s) at %s\n", result.Owner, result.RepositoryName, visibility, result.Remote)
	if result.ProtectionRefused != "" {
		fmt.Fprintf(environment.errOut, "warning: main is not protected; GitHub said: %s\n", result.ProtectionRefused)
	} else {
		fmt.Fprintf(environment.out, "protected main: pull requests with a code-owner review\n")
	}
	printConnected(environment, result.Connected)
	return nil
}

// askOrgCreate asks for what the flags did not give.
func askOrgCreate(prompter ports.Prompter, identity ports.Identity, answers orgCreateAnswers) (orgCreateAnswers, error) {
	var err error
	if answers.name == "" {
		answers.name, err = prompter.Ask(ports.Text{
			Title:       "Organisation name",
			Description: "The name people see; the address slug is derived from it, lowercase with hyphens.",
			Explanation: "Every declaration gets an address such as organizations.acme.teams.web.skills.commit; the first segment is this slug. The name itself can change later, the slug and the id never do.",
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
	slug := scaffold.SlugOf(answers.name)
	if answers.repositoryName == "" {
		answers.repositoryName, err = prompter.Ask(ports.Text{
			Title:       "Repository name",
			Description: "The GitHub repository that holds the settings.",
			Explanation: "The org repository is a plain git repository; teams and projects point at it with rein org connect. Its name does not have to match the slug.",
			Default:     slug + "-agent-settings",
			Validate: func(answer string) string {
				if strings.ContainsAny(answer, " /") {
					return "A repository name has no spaces or slashes."
				}
				return ""
			},
		})
		if err != nil {
			return answers, err
		}
	}
	if answers.owner == "" {
		answers.owner, err = prompter.Ask(ports.Text{
			Title:       "GitHub owner",
			Description: "The account or GitHub organisation the repository is created under; you must be able to create repositories there.",
			Explanation: "Your own account works for a pilot. For a company, a GitHub organisation lets teams own their folders through CODEOWNERS later.",
			Default:     identity.Login,
		})
		if err != nil {
			return answers, err
		}
	}
	if answers.visibility == "" {
		answers.visibility, err = prompter.Choose(ports.Select{
			Title:       "Visibility",
			Description: "Who can see the repository.",
			Explanation: "Agent settings are not secrets, but they describe how the organisation works. GitHub protects branches of private repositories only on paid plans; on a free personal account a private repository is created unprotected and rein says so.",
			Choices: []ports.Choice{
				{Name: "private", Description: "only people given access"},
				{Name: "public", Description: "everyone"},
			},
			Default: "private",
		})
		if err != nil {
			return answers, err
		}
	}
	if answers.description == "" {
		answers.description, err = prompter.Ask(ports.Text{
			Title:       "Description",
			Description: "One line, shown on GitHub and in the organisation record.",
			Default:     "Agent settings of " + answers.name,
		})
		if err != nil {
			return answers, err
		}
	}
	return answers, nil
}

// orgCreateRequest turns the answers into the workflow's request, or says
// what is wrong with them.
func orgCreateRequest(identity ports.Identity, answers orgCreateAnswers) (request orchestration.CreateOrganizationRequest, reason string) {
	slug := scaffold.SlugOf(answers.name)
	if slug == "" {
		return request, "the name must contain at least one letter or digit"
	}
	switch answers.visibility {
	case "", "private":
		request.Private = true
	case "public":
		request.Private = false
	default:
		return request, fmt.Sprintf("visibility %q is not private or public", answers.visibility)
	}
	request.Name = answers.name
	request.Description = answers.description
	if request.Description == "" {
		request.Description = "Agent settings of " + answers.name
	}
	request.Owner = answers.owner
	if request.Owner == "" {
		request.Owner = identity.Login
	}
	request.RepositoryName = answers.repositoryName
	if request.RepositoryName == "" {
		request.RepositoryName = slug + "-agent-settings"
	}
	request.Codeowner = identity.Login
	return request, ""
}

// printConnected prints what a connect did; org connect and org create
// share it.
func printConnected(environment *commandEnvironment, result orchestration.ConnectResult) {
	fmt.Fprintf(environment.out, "connected %s (%s) at %s\n", result.Slug, result.Name, result.OrgClonePath)
	if result.BecameActive {
		fmt.Fprintf(environment.out, "active organisation: %s\n", result.Slug)
	}
	for _, snapshotPath := range result.SnapshotPaths {
		fmt.Fprintf(environment.out, "snapshot of an agent's home: %s\n", snapshotPath)
	}
}
