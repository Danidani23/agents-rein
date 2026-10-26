package cli

import (
	"fmt"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/ports"
	"github.com/spf13/cobra"
)

func newOrgCommand(environment *commandEnvironment) *cobra.Command {
	org := &cobra.Command{
		Use:   "org",
		Short: "Create, connect to, list, switch and disconnect organisations",
		Long: "An organisation keeps its agent settings in an org repository, a git repository with a\n" +
			"declarations/ folder. org connect clones one into ~/.rein/orgs/ and remembers it; the\n" +
			"first one becomes active. org list shows the connected ones. org use switches the active\n" +
			"organisation, which only decides where commands without a repository context go, such as\n" +
			"init for a new project; a project already initialised is bound to its own organisation.\n" +
			"org disconnect forgets one and removes its clone, and never touches the remote. org destroy\n" +
			"does the same and then prints the steps to delete the repository by hand: rein never\n" +
			"deletes a repository.",
	}
	org.AddCommand(newOrgCreateCommand(environment), newOrgConnectCommand(environment), newOrgListCommand(environment), newOrgUseCommand(environment), newOrgDisconnectCommand(environment), newOrgDestroyCommand(environment))
	return org
}

func newOrgConnectCommand(environment *commandEnvironment) *cobra.Command {
	var login string
	command := &cobra.Command{
		Use:   "connect <remote>",
		Short: "Clone an existing org repository and remember it",
		Long: "connect clones the org repository at <remote>, a git URL or a local path, checks that it\n" +
			"holds exactly one organisation record, keeps the clone under ~/.rein/orgs/<id>/ and adds\n" +
			"the organisation to this machine's state. The first one you connect becomes the active one.",
		Example: "  rein org connect git@github.com:acme/agent-settings.git",
		Args:    cobra.ExactArgs(1),
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
			result, err := orchestrator.ConnectOrganization(arguments[0], login)
			if err != nil {
				return err
			}
			printConnected(environment, result)
			return nil
		},
	}
	command.Flags().StringVar(&login, "login", "", "the GitHub account to act with (default: the only one logged in)")
	return command
}

func newOrgListCommand(environment *commandEnvironment) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the connected organisations; the active one is marked",
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
			organizations, active, err := orchestrator.Organizations.List()
			if err != nil {
				return err
			}
			if len(organizations) == 0 {
				fmt.Fprintf(environment.out, "no organisations connected; run rein org connect <remote>\n")
				return nil
			}
			for _, ref := range organizations {
				marker := " "
				if ref.Slug == active {
					marker = "*"
				}
				fmt.Fprintf(environment.out, "%s %s  %s\n", marker, ref.Slug, ref.Remote)
			}
			return nil
		},
	}
}

func newOrgUseCommand(environment *commandEnvironment) *cobra.Command {
	return &cobra.Command{
		Use:   "use [slug]",
		Short: "Make an organisation the active one; asks which when the slug is left out",
		Args:  cobra.MaximumNArgs(1),
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
			slug, err := organizationSlug(environment.prompter(), orchestrator, arguments, "make active")
			if err != nil {
				return err
			}
			err = orchestrator.Organizations.Use(slug)
			if err != nil {
				return err
			}
			fmt.Fprintf(environment.out, "active organisation: %s\n", slug)
			return nil
		},
	}
}

func newOrgDisconnectCommand(environment *commandEnvironment) *cobra.Command {
	return &cobra.Command{
		Use:     "disconnect [slug]",
		Short:   "Forget an organisation and remove its clone; the remote is untouched",
		Example: "  rein org disconnect\n  rein org disconnect acme",
		Long: "disconnect forgets the organisation on this machine, deletes its clone under ~/.rein/orgs/\n" +
			"and forgets the projects registered for it on this machine. The remote repository is not\n" +
			"touched; org destroy is disconnect plus the steps to delete it by hand. Without a slug it\n" +
			"asks which of the connected organisations.",
		Args: cobra.MaximumNArgs(1),
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
			slug, err := organizationSlug(environment.prompter(), orchestrator, arguments, "disconnect")
			if err != nil {
				return err
			}
			result, err := orchestrator.DisconnectOrganization(slug)
			if err != nil {
				return err
			}
			fmt.Fprintf(environment.out, "disconnected %s: removed the clone at %s and %d project registrations\n", slug, result.OrgClonePath, result.RemovedProjectClones)
			if result.ActiveAfter == "" {
				fmt.Fprintf(environment.out, "no active organisation\n")
			} else {
				fmt.Fprintf(environment.out, "active organisation: %s\n", result.ActiveAfter)
			}
			return nil
		},
	}
}

func newOrgDestroyCommand(environment *commandEnvironment) *cobra.Command {
	var yes bool
	command := &cobra.Command{
		Use:   "destroy [slug]",
		Short: "Disconnect an organisation here and print how to delete its repository by hand",
		Long: "destroy is the way back to nothing for an organisation: it forgets it on this machine,\n" +
			"removes its clone under ~/.rein/orgs/ and forgets the projects registered for it, as\n" +
			"disconnect does, and then prints the steps to delete the repository on the host by hand.\n" +
			"Rein never deletes a repository: deletion is the one act that cannot be undone, so it stays\n" +
			"yours, with the host's own confirmation. Project clones initialised for the organisation\n" +
			"keep their generated files until rein reset project runs in each; run that first. Without a\n" +
			"slug it asks which of the connected organisations.",
		Example: "  rein org destroy\n  rein org destroy acme --yes",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			return runOrgDestroy(environment, arguments, yes)
		},
	}
	command.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return command
}

func runOrgDestroy(environment *commandEnvironment, arguments []string, yes bool) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return err
	}
	defer release()
	err = requireLogin(orchestrator)
	if err != nil {
		return err
	}
	// one prompter for the whole command: each one reads ahead, so a second
	// would miss what the first already took from the input
	prompter := environment.prompter()
	slug, err := organizationSlug(prompter, orchestrator, arguments, "destroy")
	if err != nil {
		return err
	}
	organization, orgClonePath, err := orchestrator.Organizations.Lookup(slug)
	if err != nil {
		return err
	}
	projectClones, err := orchestrator.Organizations.ProjectClones()
	if err != nil {
		return err
	}
	var registered []string
	for _, projectClone := range projectClones {
		if projectClone.Organization == organization.ID {
			registered = append(registered, projectClone.ClonePath)
		}
	}
	fmt.Fprintf(environment.out, "This will disconnect %s on this machine: forget it, remove its clone at %s and forget %d project registrations.\n", slug, orgClonePath, len(registered))
	if len(registered) > 0 {
		fmt.Fprintf(environment.out, "These project clones keep their generated files until rein reset project runs in them:\n")
		for _, clonePath := range registered {
			fmt.Fprintf(environment.out, "  %s\n", clonePath)
		}
	}
	fmt.Fprintf(environment.out, "%s; rein never deletes a repository. The steps to delete it by hand follow.\n", repositoryPhrase(organization.Host, organization.Owner, organization.Repository, organization.Remote)+" stays")
	if !yes {
		if !prompter.CanAsk() {
			return refusal.New("no terminal to confirm on; pass --yes")
		}
		confirmed, err := prompter.Confirm("Continue?", "The clone under ~/.rein/orgs/ is deleted and the organisation and its projects leave this machine's state; the snapshots stay until rein reset machine. Nothing on the host changes.")
		if err != nil {
			return refusal.New(err.Error())
		}
		if !confirmed {
			return refusal.New("nothing changed")
		}
	}
	result, err := orchestrator.DisconnectOrganization(slug)
	if err != nil {
		return err
	}
	fmt.Fprintf(environment.out, "disconnected %s: removed the clone at %s and %d project registrations\n", slug, result.OrgClonePath, result.RemovedProjectClones)
	if result.ActiveAfter == "" {
		fmt.Fprintf(environment.out, "no active organisation\n")
	} else {
		fmt.Fprintf(environment.out, "active organisation: %s\n", result.ActiveAfter)
	}
	printDeletionSteps(environment, orchestrator, organization)
	return nil
}

// organizationSlug is the slug given as the argument or, when none was, the
// one picked from the connected organisations on a terminal; the only one
// connected is the default, several have none. Off a terminal the slug is
// required.
func organizationSlug(prompter ports.Prompter, orchestrator orchestration.Orchestrator, arguments []string, purpose string) (string, error) {
	if len(arguments) == 1 {
		return arguments[0], nil
	}
	organizations, active, err := orchestrator.Organizations.List()
	if err != nil {
		return "", err
	}
	if len(organizations) == 0 {
		return "", refusal.New("no organisations connected; run rein org connect <remote>")
	}
	if !prompter.CanAsk() {
		return "", refusal.New("name the organisation to " + purpose + "; rein org list shows the connected ones")
	}
	var choices []ports.Choice
	for _, organization := range organizations {
		description := organization.Remote
		if organization.Slug == active {
			description += ", active"
		}
		choices = append(choices, ports.Choice{Name: organization.Slug, Description: description})
	}
	defaultSlug := ""
	if len(choices) == 1 {
		defaultSlug = choices[0].Name
	}
	slug, err := prompter.Choose(ports.Select{
		Title:       "Organisation",
		Description: "The connected organisation to " + purpose + ".",
		Explanation: "The same list as rein org list. The active one is where commands without a repository context go, such as init for a new project.",
		Choices:     choices,
		Default:     defaultSlug,
	})
	if err != nil {
		return "", refusal.New(err.Error())
	}
	return slug, nil
}

// repositoryPhrase names the org repository for people: owner/name on the
// host when the record knows them, the remote otherwise.
func repositoryPhrase(host string, owner string, name string, remote string) string {
	if owner == "" || name == "" {
		return "The remote " + remote
	}
	label, known := hostLabels[host]
	if !known {
		label = "the host"
	}
	return "The repository " + owner + "/" + name + " on " + label
}

// printDeletionSteps prints what a person does to delete the org repository
// by hand, since rein never does (D26).
func printDeletionSteps(environment *commandEnvironment, orchestrator orchestration.Orchestrator, organization machine.OrganizationRef) {
	if organization.Owner == "" || organization.Repository == "" {
		fmt.Fprintf(environment.out, "The remote %s is untouched; delete it where it lives if you want it gone.\n", organization.Remote)
		fmt.Fprintf(environment.out, "Afterwards rein reset machine removes what is left of rein on this machine.\n")
		return
	}
	fmt.Fprintf(environment.out, "To delete %s/%s on %s by hand:\n", organization.Owner, organization.Repository, hostLabels[organization.Host])
	fmt.Fprintf(environment.out, "  1. Open %s\n", orchestrator.Hosting.RepositorySettingsURL(organization.Owner, organization.Repository))
	fmt.Fprintf(environment.out, "  2. At the bottom of the page, under Danger Zone, choose Delete this repository.\n")
	fmt.Fprintf(environment.out, "  3. Type %s/%s to confirm; %s asks for your password or passkey.\n", organization.Owner, organization.Repository, hostLabels[organization.Host])
	fmt.Fprintf(environment.out, "Afterwards rein reset machine removes what is left of rein on this machine.\n")
}
