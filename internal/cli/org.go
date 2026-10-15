package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Danidani23/ai-assisted-dev-tool/internal/declaration"
	"github.com/Danidani23/ai-assisted-dev-tool/internal/execute"
	"github.com/Danidani23/ai-assisted-dev-tool/internal/local"
)

func newOrgCommand(env *environment) *cobra.Command {
	org := &cobra.Command{
		Use:   "org",
		Short: "Connect to, list, switch and disconnect organisations",
		Long: "An organisation is an org repository: a git repository holding declarations/. connect\n" +
			"clones one into the reins home and remembers it; use makes one the active organisation,\n" +
			"which only decides where commands without a repository context go; disconnect removes\n" +
			"the clone and the registration and never touches the remote.",
	}
	org.AddCommand(newOrgConnectCommand(env), newOrgListCommand(env), newOrgUseCommand(env), newOrgDisconnectCommand(env))
	return org
}

func newOrgConnectCommand(env *environment) *cobra.Command {
	return &cobra.Command{
		Use:   "connect <remote>",
		Short: "Clone an existing org repository and remember it",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			return runOrgConnect(env, arguments[0])
		},
	}
}

func runOrgConnect(env *environment, remote string) error {
	home, config, err := requireConfig()
	if err != nil {
		return err
	}
	err = execute.Preflight("git")
	if err != nil {
		return err
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
	_, connected := config.Organization(organization.Slug)
	if connected {
		os.RemoveAll(temporary)
		return fmt.Errorf("organisation %q is already connected; run reins org disconnect %s first", organization.Slug, organization.Slug)
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
	config.Organizations = append(config.Organizations, local.OrganizationRef{ID: organization.ID, Slug: organization.Slug, Remote: remote})
	becameActive := config.Active == ""
	if becameActive {
		config.Active = organization.Slug
	}
	err = local.SaveConfig(home, config)
	if err != nil {
		return crashedError{cause: err}
	}
	state, err := local.LoadState(home)
	if err != nil {
		return crashedError{cause: err}
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
			_, config, err := requireConfig()
			if err != nil {
				return err
			}
			if len(config.Organizations) == 0 {
				fmt.Fprintf(env.out, "no organisations connected; run reins org connect <remote>\n")
				return nil
			}
			for _, ref := range config.Organizations {
				marker := " "
				if ref.Slug == config.Active {
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
			home, config, err := requireConfig()
			if err != nil {
				return err
			}
			_, found := config.Organization(arguments[0])
			if !found {
				return fmt.Errorf("organisation %q is not connected; run reins org list", arguments[0])
			}
			config.Active = arguments[0]
			err = local.SaveConfig(home, config)
			if err != nil {
				return crashedError{cause: err}
			}
			fmt.Fprintf(env.out, "active organisation: %s\n", config.Active)
			return nil
		},
	}
}

func newOrgDisconnectCommand(env *environment) *cobra.Command {
	return &cobra.Command{
		Use:   "disconnect <slug>",
		Short: "Forget an organisation and remove its clone; the remote is untouched",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			return runOrgDisconnect(env, arguments[0])
		},
	}
}

func runOrgDisconnect(env *environment, slug string) error {
	home, config, err := requireConfig()
	if err != nil {
		return err
	}
	ref, found := config.Organization(slug)
	if !found {
		return fmt.Errorf("organisation %q is not connected; run reins org list", slug)
	}
	state, err := local.LoadState(home)
	if err != nil {
		return crashedError{cause: err}
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
	for _, candidate := range config.Organizations {
		if candidate.Slug != slug {
			remaining = append(remaining, candidate)
		}
	}
	config.Organizations = remaining
	if config.Active == slug {
		config.Active = ""
		if len(remaining) > 0 {
			config.Active = remaining[0].Slug
		}
	}
	err = local.SaveConfig(home, config)
	if err != nil {
		return crashedError{cause: err}
	}
	err = local.SaveState(home, state)
	if err != nil {
		return crashedError{cause: err}
	}
	fmt.Fprintf(env.out, "disconnected %s: removed the clone at %s and %d project registrations\n", slug, clone, removedProjects)
	if config.Active == "" {
		fmt.Fprintf(env.out, "no active organisation\n")
	} else {
		fmt.Fprintf(env.out, "active organisation: %s\n", config.Active)
	}
	return nil
}
