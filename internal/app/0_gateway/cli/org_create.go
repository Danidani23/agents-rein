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
	host           string
	login          string
	name           string
	description    string
	owner          string
	repositoryName string
	visibility     string
	yes            bool
}

// hostLabels name the hosts for people.
var hostLabels = map[string]string{"github": "GitHub", "gitlab": "GitLab"}

// hostPhrase names the hosts in general, for anyone who is not sure what a
// version control host is.
const hostPhrase = "the version control host (GitHub, GitLab and others)"

func newOrgCreateCommand(environment *commandEnvironment) *cobra.Command {
	var answers orgCreateAnswers
	command := &cobra.Command{
		Use:   "create",
		Short: "Create an organisation: its repository on " + hostPhrase + ", the first commit, protection, and connect to it",
		Long: "create makes a new org repository on " + hostPhrase + ", the one place an organisation keeps\n" +
			"its agent settings. It asks for the host, the account to act with, the organisation's name,\n" +
			"the repository's name, owner and visibility, then creates the repository, or adopts an empty\n" +
			"one of that name, writes the organisation record, the seeded label registry (language, kind,\n" +
			"data) and CODEOWNERS in a first commit, protects main so that changes come through pull\n" +
			"requests reviewed by a code owner, and connects this machine to it. Connecting also takes a\n" +
			"snapshot of your agents' local settings into ~/.rein/snapshots/ on this machine, as the record\n" +
			"of what was there before rein; the snapshot never leaves this machine. Rein never deletes a\n" +
			"repository: a step that fails leaves the empty repository, and a rerun adopts it. Without a\n" +
			"terminal, --name and --yes are required; the other flags have defaults.",
		Example: "  rein org create\n  rein org create --name \"Acme\" --owner acme-inc --visibility private --yes",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runOrgCreate(environment, answers)
		},
	}
	command.Flags().StringVar(&answers.host, "host", "", "the version control host: github (default)")
	command.Flags().StringVar(&answers.login, "login", "", "the account to act with (default: the only one logged in)")
	command.Flags().StringVar(&answers.name, "name", "", "the organisation's name; its slug is derived from it")
	command.Flags().StringVar(&answers.description, "description", "", "one line about the organisation")
	command.Flags().StringVar(&answers.owner, "owner", "", "the account or organisation on the host that owns the repository (default: the login)")
	command.Flags().StringVar(&answers.repositoryName, "repository", "", "the repository's name (default: <slug>-agent-settings)")
	command.Flags().StringVar(&answers.visibility, "visibility", "", "private or public (default: private)")
	command.Flags().BoolVar(&answers.yes, "yes", false, "do not ask for confirmation")
	return command
}

func orgCreateIntro(host string) string {
	return "org create makes a new org repository on " + hostLabels[host] + " for an organisation's agent settings: " +
		"the repository, a first commit with the organisation record, the seeded label registry and " +
		"CODEOWNERS, protection of main so changes come through reviewed pull requests, and the " +
		"connection of this machine to it. Connecting takes a snapshot of your agents' local settings " +
		"into ~/.rein/snapshots/ on this machine, as the record of what was there before rein; nothing " +
		"of them goes to " + hostLabels[host] + "."
}

const orgCreateConfirmExplanation = "The repository is created under the owner you chose, with you as its code owner, and this " +
	"machine connects to it. Nothing is placed into any agent yet; that is sync. Rein never deletes " +
	"a repository: if a step fails, the empty repository stays and running org create again adopts it."

func runOrgCreate(environment *commandEnvironment, answers orgCreateAnswers) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return err
	}
	defer release()
	prompter := environment.prompter()
	if !prompter.CanAsk() {
		if answers.name == "" || !answers.yes {
			return refusal.New("no terminal to ask on; pass --name and --yes, and --host, --login, --owner, --repository, --visibility, --description to change the defaults")
		}
	} else {
		answers, err = askOrgCreateHost(prompter, answers)
		if err != nil {
			return refusal.New(err.Error())
		}
		prompter.Intro(orgCreateIntro(answers.host))
	}
	if answers.host == "" {
		answers.host = "github"
	}
	if hostLabels[answers.host] == "" || answers.host != "github" {
		return refusal.New(fmt.Sprintf("host %q is not supported yet; this build knows github", answers.host))
	}
	logins, err := orchestrator.Credentials.Logins()
	if err != nil {
		return err
	}
	if len(logins) == 0 {
		if !prompter.CanAsk() {
			return refusal.New("not logged in; run rein login first")
		}
		identity, err := loginInline(environment, orchestrator, prompter)
		if err != nil {
			return err
		}
		logins = []string{identity.Login}
	}
	if answers.login == "" && len(logins) > 1 && prompter.CanAsk() {
		answers.login, err = prompter.Choose(ports.Select{
			Title:       "Account",
			Description: "The " + hostLabels[answers.host] + " account rein acts with for this organisation.",
			Explanation: "Several accounts are logged in on this machine. The organisation remembers the one you choose; every later command on it acts with that account's token.",
			Choices:     loginChoices(logins),
			Default:     logins[0],
		})
		if err != nil {
			return refusal.New(err.Error())
		}
	}
	identity, err := orchestrator.Credentials.Resolve(answers.login)
	if err != nil {
		return err
	}
	if prompter.CanAsk() {
		answers, err = askOrgCreate(prompter, orchestrator, identity, answers)
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
	fmt.Fprintf(environment.out, "This will create %s/%s (%s) on %s as %s for the organisation %q, slug %s,\nwith @%s as code owner, protect main, and connect this machine to it.\n",
		request.Owner, request.RepositoryName, visibility, hostLabels[answers.host], identity.Login, request.Name, scaffold.SlugOf(request.Name), identity.Login)
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
	if result.Adopted {
		fmt.Fprintf(environment.out, "adopted the empty repository %s/%s at %s\n", result.Owner, result.RepositoryName, result.Remote)
	} else {
		fmt.Fprintf(environment.out, "created %s/%s (%s) at %s\n", result.Owner, result.RepositoryName, visibility, result.Remote)
	}
	if result.ProtectionRefused != "" {
		fmt.Fprintf(environment.errOut, "warning: main is not protected; %s said: %s\n", hostLabels[answers.host], result.ProtectionRefused)
	} else {
		fmt.Fprintf(environment.out, "protected main: pull requests with a code-owner review\n")
	}
	printConnected(environment, result.Connected)
	return nil
}

// loginInline runs the device flow inside another command.
func loginInline(environment *commandEnvironment, orchestrator orchestration.Orchestrator, prompter ports.Prompter) (ports.Identity, error) {
	code, err := orchestrator.Credentials.BeginLogin()
	if err != nil {
		return ports.Identity{}, err
	}
	fmt.Fprintf(environment.out, "No account is logged in yet. Open %s and enter the code %s\nWaiting for the approval; the code is valid until %s.\n", code.VerificationURI, code.UserCode, code.ExpiresAt.Local().Format("15:04"))
	identity, err := orchestrator.Credentials.CompleteLogin(code)
	if err != nil {
		return ports.Identity{}, err
	}
	fmt.Fprintf(environment.out, "logged in as %s; the token is in your keychain\n\n", identity.Login)
	return identity, nil
}

func loginChoices(logins []string) []ports.Choice {
	var choices []ports.Choice
	for _, login := range logins {
		choices = append(choices, ports.Choice{Name: login})
	}
	return choices
}

// askOrgCreateHost asks for the host first, so every later question can
// name it; the planned hosts are listed and refused.
func askOrgCreateHost(prompter ports.Prompter, answers orgCreateAnswers) (orgCreateAnswers, error) {
	if answers.host != "" {
		return answers, nil
	}
	var err error
	answers.host, err = prompter.Choose(ports.Select{
		Title:       "Version control host",
		Description: "Where the organisation keeps its repositories: " + hostPhrase + ".",
		Explanation: "The org repository lives on the host, next to the organisation's code; rein creates it there, opens pull requests on it, and reads it on every machine. Your identity is your account on that host.",
		Choices: []ports.Choice{
			{Name: "github", Description: "GitHub, github.com"},
			{Name: "gitlab", Description: "GitLab", Unavailable: "planned, not yet supported"},
		},
		Default: "github",
	})
	return answers, err
}

// askOrgCreate asks for what the flags did not give.
func askOrgCreate(prompter ports.Prompter, orchestrator orchestration.Orchestrator, identity ports.Identity, answers orgCreateAnswers) (orgCreateAnswers, error) {
	var err error
	hostLabel := hostLabels[answers.host]
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
			Description: "The repository on " + hostLabel + " that holds the settings.",
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
		accessToken, err := orchestrator.Credentials.AccessToken(identity.Login)
		if err != nil {
			return answers, err
		}
		owners, err := orchestrator.Hosting.Owners(accessToken)
		if err != nil {
			return answers, err
		}
		const other = "another owner"
		var choices []ports.Choice
		for _, owner := range owners {
			description := "an organisation you belong to"
			if owner == identity.Login {
				description = "your own account"
			}
			choices = append(choices, ports.Choice{Name: owner, Description: description})
		}
		choices = append(choices, ports.Choice{Name: other, Description: "type an account or organisation name"})
		answers.owner, err = prompter.Choose(ports.Select{
			Title:       hostLabel + " owner",
			Description: "The account or " + hostLabel + " organisation the repository is created under.",
			Explanation: "Your own account works for a pilot. For a company, a " + hostLabel + " organisation lets teams own their folders through CODEOWNERS later. The organisations listed are the ones the Agents Rein app is installed on; another needs the app installed first, at " + orchestrator.Hosting.InstallURL() + ".",
			Choices:     choices,
			Default:     identity.Login,
		})
		if err != nil {
			return answers, err
		}
		if answers.owner == other {
			answers.owner, err = prompter.Ask(ports.Text{Title: "Owner name", Description: "The exact name of the account or organisation on " + hostLabel + "."})
			if err != nil {
				return answers, err
			}
		}
	}
	if answers.visibility == "" {
		answers.visibility, err = prompter.Choose(ports.Select{
			Title:       "Visibility",
			Description: "Who can see the repository on " + hostLabel + ".",
			Explanation: "Agent settings are not secrets, but they describe how the organisation works. " + hostLabel + " protects branches of private repositories only on paid plans; on a free personal account a private repository is created unprotected and rein says so.",
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
			Description: "One line, shown on " + hostLabel + " and in the organisation record.",
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
	request.Host = answers.host
	request.Login = identity.Login
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
		fmt.Fprintf(environment.out, "snapshot of an agent's local settings, kept on this machine: %s\n", snapshotPath)
	}
}
