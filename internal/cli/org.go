package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/declaration"
	"github.com/Danidani23/agents-rein/internal/execute"
	"github.com/Danidani23/agents-rein/internal/local"
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
			return runOrgConnect(env, arguments[0])
		},
	}
}

func runOrgConnect(env *environment, remote string) error {
	home, _, err := requireIdentity()
	if err != nil {
		return err
	}
	err = execute.Preflight("git")
	if err != nil {
		return err
	}
	state, err := local.LoadState(home)
	if err != nil {
		return crashedError{cause: err}
	}
	orgsFolder := filepath.Join(home, "orgs")
	err = os.MkdirAll(orgsFolder, 0o755)
	if err != nil {
		return crashedError{cause: err}
	}
	temporary, err := os.MkdirTemp(orgsFolder, ".connect-*")
	if err != nil {
		return crashedError{cause: err}
	}
	_, err = execute.Run(home, "git", "clone", "--quiet", remote, temporary)
	if err != nil {
		os.RemoveAll(temporary)
		return err
	}
	organization, err := declaration.FindOrganization(temporary)
	if err != nil {
		os.RemoveAll(temporary)
		return err
	}
	_, connected := state.Organization(organization.Slug)
	if connected {
		os.RemoveAll(temporary)
		return fmt.Errorf("organisation %q is already connected; run rein org disconnect %s first", organization.Slug, organization.Slug)
	}
	final := filepath.Join(orgsFolder, organization.ID)
	err = os.RemoveAll(final)
	if err != nil {
		return crashedError{cause: err}
	}
	err = os.Rename(temporary, final)
	if err != nil {
		return crashedError{cause: err}
	}
	state.Organizations = append(state.Organizations, local.OrganizationRef{ID: organization.ID, Slug: organization.Slug, Remote: remote})
	becameActive := state.Active == ""
	if becameActive {
		state.Active = organization.Slug
	}
	state.Clones[organization.ID] = final
	err = local.SaveState(home, state)
	if err != nil {
		return crashedError{cause: err}
	}
	fmt.Fprintf(env.out, "connected %s (%s) at %s\n", organization.Slug, organization.Name, final)
	if becameActive {
		fmt.Fprintf(env.out, "active organisation: %s\n", organization.Slug)
	}
	return nil
}

func newOrgListCommand(env *environment) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the connected organisations; the active one is marked",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			home, _, err := requireIdentity()
			if err != nil {
				return err
			}
			state, err := local.LoadState(home)
			if err != nil {
				return crashedError{cause: err}
			}
			if len(state.Organizations) == 0 {
				fmt.Fprintf(env.out, "no organisations connected; run rein org connect <remote>\n")
				return nil
			}
			for _, ref := range state.Organizations {
				marker := " "
				if ref.Slug == state.Active {
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
			home, _, err := requireIdentity()
			if err != nil {
				return err
			}
			state, err := local.LoadState(home)
			if err != nil {
				return crashedError{cause: err}
			}
			_, found := state.Organization(arguments[0])
			if !found {
				return fmt.Errorf("organisation %q is not connected; run rein org list", arguments[0])
			}
			state.Active = arguments[0]
			err = local.SaveState(home, state)
			if err != nil {
				return crashedError{cause: err}
			}
			fmt.Fprintf(env.out, "active organisation: %s\n", state.Active)
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
			return runOrgDisconnect(env, arguments[0])
		},
	}
}

func runOrgDisconnect(env *environment, slug string) error {
	home, _, err := requireIdentity()
	if err != nil {
		return err
	}
	state, err := local.LoadState(home)
	if err != nil {
		return crashedError{cause: err}
	}
	ref, found := state.Organization(slug)
	if !found {
		return fmt.Errorf("organisation %q is not connected; run rein org list", slug)
	}
	clone, known := state.Clones[ref.ID]
	if !known {
		clone = filepath.Join(home, "orgs", ref.ID)
	}
	err = os.RemoveAll(clone)
	if err != nil {
		return crashedError{cause: err}
	}
	delete(state.Clones, ref.ID)
	var kept []local.Project
	removedProjects := 0
	for _, project := range state.Projects {
		if project.Organization == ref.ID {
			removedProjects++
			continue
		}
		kept = append(kept, project)
	}
	state.Projects = kept
	var remaining []local.OrganizationRef
	for _, candidate := range state.Organizations {
		if candidate.Slug != slug {
			remaining = append(remaining, candidate)
		}
	}
	state.Organizations = remaining
	if state.Active == slug {
		state.Active = ""
		if len(remaining) > 0 {
			state.Active = remaining[0].Slug
		}
	}
	err = local.SaveState(home, state)
	if err != nil {
		return crashedError{cause: err}
	}
	fmt.Fprintf(env.out, "disconnected %s: removed the clone at %s and %d project registrations\n", slug, clone, removedProjects)
	if state.Active == "" {
		fmt.Fprintf(env.out, "no active organisation\n")
	} else {
		fmt.Fprintf(env.out, "active organisation: %s\n", state.Active)
	}
	return nil
}
