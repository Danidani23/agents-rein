package orchestration

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
)

// ConnectResult is what org connect did.
type ConnectResult struct {
	Slug         string
	Name         string
	Clone        string
	BecameActive bool
}

// Connect clones an org repository, checks that it holds exactly one
// organisation record, keeps the clone under <home>/orgs/<id>/ and remembers
// the organisation; the first one connected becomes active.
func (implementation *local) Connect(remote string) (ConnectResult, error) {
	dependencies := implementation.dependencies
	_, err := dependencies.Host.Identity()
	if err != nil {
		return ConnectResult{}, refuse(err.Error())
	}
	err = dependencies.Repository.Preflight()
	if err != nil {
		return ConnectResult{}, refuse(err.Error())
	}
	state, err := dependencies.State.Load()
	if err != nil {
		return ConnectResult{}, err
	}
	orgsFolder := filepath.Join(dependencies.Home, "orgs")
	err = os.MkdirAll(orgsFolder, 0o755)
	if err != nil {
		return ConnectResult{}, err
	}
	temporary, err := os.MkdirTemp(orgsFolder, ".connect-*")
	if err != nil {
		return ConnectResult{}, err
	}
	// Clone needs a directory that does not exist yet
	os.Remove(temporary)
	err = dependencies.Repository.Clone(remote, temporary)
	if err != nil {
		os.RemoveAll(temporary)
		return ConnectResult{}, err
	}
	organization, err := declaration.FindOrganization(temporary)
	if err != nil {
		os.RemoveAll(temporary)
		return ConnectResult{}, refuse(err.Error())
	}
	_, connected := state.Organization(organization.Slug)
	if connected {
		os.RemoveAll(temporary)
		return ConnectResult{}, refuse(fmt.Sprintf("organisation %q is already connected; run rein org disconnect %s first", organization.Slug, organization.Slug))
	}
	final := filepath.Join(orgsFolder, organization.ID)
	err = os.RemoveAll(final)
	if err != nil {
		return ConnectResult{}, err
	}
	err = os.Rename(temporary, final)
	if err != nil {
		return ConnectResult{}, err
	}
	state.Organizations = append(state.Organizations, machine.OrganizationRef{ID: organization.ID, Slug: organization.Slug, Remote: remote})
	becameActive := state.Active == ""
	if becameActive {
		state.Active = organization.Slug
	}
	state.Clones[organization.ID] = final
	err = dependencies.State.Save(state)
	if err != nil {
		return ConnectResult{}, err
	}
	return ConnectResult{Slug: organization.Slug, Name: organization.Name, Clone: final, BecameActive: becameActive}, nil
}

// List returns the connected organisations and the active slug.
func (implementation *local) List() (organizations []machine.OrganizationRef, active string, err error) {
	dependencies := implementation.dependencies
	_, err = dependencies.Host.Identity()
	if err != nil {
		return nil, "", refuse(err.Error())
	}
	state, err := dependencies.State.Load()
	if err != nil {
		return nil, "", err
	}
	return state.Organizations, state.Active, nil
}

// Use makes a connected organisation the active one.
func (implementation *local) Use(slug string) error {
	dependencies := implementation.dependencies
	_, err := dependencies.Host.Identity()
	if err != nil {
		return refuse(err.Error())
	}
	state, err := dependencies.State.Load()
	if err != nil {
		return err
	}
	_, found := state.Organization(slug)
	if !found {
		return refuse(fmt.Sprintf("organisation %q is not connected; run rein org list", slug))
	}
	state.Active = slug
	return dependencies.State.Save(state)
}

// DisconnectResult is what org disconnect did.
type DisconnectResult struct {
	Clone           string
	RemovedProjects int
	Active          string
}

// Disconnect forgets an organisation on this machine: its clone, its
// registration and its projects. The remote is never touched.
func (implementation *local) Disconnect(slug string) (DisconnectResult, error) {
	dependencies := implementation.dependencies
	_, err := dependencies.Host.Identity()
	if err != nil {
		return DisconnectResult{}, refuse(err.Error())
	}
	state, err := dependencies.State.Load()
	if err != nil {
		return DisconnectResult{}, err
	}
	ref, found := state.Organization(slug)
	if !found {
		return DisconnectResult{}, refuse(fmt.Sprintf("organisation %q is not connected; run rein org list", slug))
	}
	clone, known := state.Clones[ref.ID]
	if !known {
		clone = filepath.Join(dependencies.Home, "orgs", ref.ID)
	}
	err = os.RemoveAll(clone)
	if err != nil {
		return DisconnectResult{}, err
	}
	delete(state.Clones, ref.ID)
	var kept []machine.Project
	removedProjects := 0
	for _, project := range state.Projects {
		if project.Organization == ref.ID {
			removedProjects++
			continue
		}
		kept = append(kept, project)
	}
	state.Projects = kept
	var remaining []machine.OrganizationRef
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
	err = dependencies.State.Save(state)
	if err != nil {
		return DisconnectResult{}, err
	}
	return DisconnectResult{Clone: clone, RemovedProjects: removedProjects, Active: state.Active}, nil
}
