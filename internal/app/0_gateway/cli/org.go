package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newOrgCommand(env *environment) *cobra.Command {
	org := &cobra.Command{
		Use:   "org",
		Short: "Connect to, list, switch and disconnect organisations",
		Long: "An organisation keeps its agent settings in an org repository, a git repository with a\n" +
			"declarations/ folder. org connect clones one into ~/.rein/orgs/ and remembers it; the\n" +
			"first one becomes active. org list shows the connected ones. org use switches the active\n" +
			"organisation, which only decides where commands without a repository context go, such as\n" +
			"init for a new project; a project already initialised is bound to its own organisation.\n" +
			"org disconnect forgets one and removes its clone, and never touches the remote.",
	}
	org.AddCommand(newOrgConnectCommand(env), newOrgListCommand(env), newOrgUseCommand(env), newOrgDisconnectCommand(env))
	return org
}

func newOrgConnectCommand(env *environment) *cobra.Command {
	return &cobra.Command{
		Use:   "connect <remote>",
		Short: "Clone an existing org repository and remember it",
		Long: "connect clones the org repository at <remote>, a git URL or a local path, checks that it\n" +
			"holds exactly one organisation record, keeps the clone under ~/.rein/orgs/<id>/ and adds\n" +
			"the organisation to this machine's state. The first one you connect becomes the active one.",
		Example: "  rein org connect git@github.com:acme/agent-settings.git",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, err := env.orchestrate()
			if err != nil {
				return err
			}
			result, err := orchestrator.Organizations.Connect(arguments[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(env.out, "connected %s (%s) at %s\n", result.Slug, result.Name, result.Clone)
			if result.BecameActive {
				fmt.Fprintf(env.out, "active organisation: %s\n", result.Slug)
			}
			return nil
		},
	}
}

func newOrgListCommand(env *environment) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the connected organisations; the active one is marked",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, err := env.orchestrate()
			if err != nil {
				return err
			}
			organizations, active, err := orchestrator.Organizations.List()
			if err != nil {
				return err
			}
			if len(organizations) == 0 {
				fmt.Fprintf(env.out, "no organisations connected; run rein org connect <remote>\n")
				return nil
			}
			for _, ref := range organizations {
				marker := " "
				if ref.Slug == active {
					marker = "*"
				}
				fmt.Fprintf(env.out, "%s %s  %s\n", marker, ref.Slug, ref.Remote)
			}
			return nil
		},
	}
}

func newOrgUseCommand(env *environment) *cobra.Command {
	return &cobra.Command{
		Use:   "use <slug>",
		Short: "Make an organisation the active one",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, err := env.orchestrate()
			if err != nil {
				return err
			}
			err = orchestrator.Organizations.Use(arguments[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(env.out, "active organisation: %s\n", arguments[0])
			return nil
		},
	}
}

func newOrgDisconnectCommand(env *environment) *cobra.Command {
	return &cobra.Command{
		Use:   "disconnect <slug>",
		Short: "Forget an organisation and remove its clone; the remote is untouched",
		Long: "disconnect forgets the organisation on this machine, deletes its clone under ~/.rein/orgs/\n" +
			"and forgets the projects registered for it on this machine. The remote repository is not\n" +
			"touched; org destroy, once it exists, is the command that deletes one.",
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, err := env.orchestrate()
			if err != nil {
				return err
			}
			result, err := orchestrator.Organizations.Disconnect(arguments[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(env.out, "disconnected %s: removed the clone at %s and %d project registrations\n", arguments[0], result.Clone, result.RemovedProjects)
			if result.Active == "" {
				fmt.Fprintf(env.out, "no active organisation\n")
			} else {
				fmt.Fprintf(env.out, "active organisation: %s\n", result.Active)
			}
			return nil
		},
	}
}
