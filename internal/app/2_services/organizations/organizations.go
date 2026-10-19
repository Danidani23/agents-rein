// Package organizations is the service that owns the organisations section
// of the machine state: which org repositories are connected, since when,
// where their clones are, and which one is active. It touches no file but
// the state.
package organizations

import (
	"fmt"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Service owns the connected organisations. Clock stamps every record it
// writes (D20); nil means the wall clock.
type Service struct {
	State ports.State
	Clock func() time.Time
}

// ErrNotConnected is returned for a slug that is not connected.
type ErrNotConnected struct {
	Slug string
}

func (err ErrNotConnected) Error() string {
	return fmt.Sprintf("organisation %q is not connected; run rein org list", err.Slug)
}

// Is makes it a refusal.
func (ErrNotConnected) Is(target error) bool {
	return target == refusal.ErrRefused
}

// ErrAlreadyConnected is returned when registering a slug twice.
type ErrAlreadyConnected struct {
	Slug string
}

func (err ErrAlreadyConnected) Error() string {
	return fmt.Sprintf("organisation %q is already connected; run rein org disconnect %s first", err.Slug, err.Slug)
}

// Is makes it a refusal.
func (ErrAlreadyConnected) Is(target error) bool {
	return target == refusal.ErrRefused
}

// ErrNoActiveOrganization is returned when a command needs the active
// organisation and none is connected.
type ErrNoActiveOrganization struct{}

func (ErrNoActiveOrganization) Error() string {
	return "no active organisation; run rein org create, rein org connect <remote> or rein org use <slug> first"
}

// Is makes it a refusal.
func (ErrNoActiveOrganization) Is(target error) bool {
	return target == refusal.ErrRefused
}

// Active returns the active organisation and its clone.
func (service Service) Active() (organization machine.OrganizationRef, orgClonePath string, err error) {
	state, err := service.State.Load()
	if err != nil {
		return machine.OrganizationRef{}, "", err
	}
	if state.Active == "" {
		return machine.OrganizationRef{}, "", ErrNoActiveOrganization{}
	}
	return service.Lookup(state.Active)
}

func (service Service) now() time.Time {
	if service.Clock == nil {
		return time.Now().UTC()
	}
	return service.Clock()
}

// List returns the connected organisations and the active slug.
func (service Service) List() (organizations []machine.OrganizationRef, active string, err error) {
	state, err := service.State.Load()
	if err != nil {
		return nil, "", err
	}
	return state.Organizations, state.Active, nil
}

// Projects returns the registered projects of every connected organisation.
func (service Service) Projects() ([]machine.Project, error) {
	state, err := service.State.Load()
	if err != nil {
		return nil, err
	}
	return state.Projects, nil
}

// Lookup finds one connected organisation and its clone.
func (service Service) Lookup(slug string) (organization machine.OrganizationRef, orgClonePath string, err error) {
	state, err := service.State.Load()
	if err != nil {
		return machine.OrganizationRef{}, "", err
	}
	organization, found := state.Organization(slug)
	if !found {
		return machine.OrganizationRef{}, "", ErrNotConnected{Slug: slug}
	}
	return organization, state.OrgClonePaths[organization.ID], nil
}

// Register remembers an organisation, stamped with the time, and its clone;
// the first one connected becomes active.
func (service Service) Register(organization machine.OrganizationRef, orgClonePath string) (becameActive bool, err error) {
	state, err := service.State.Load()
	if err != nil {
		return false, err
	}
	_, connected := state.Organization(organization.Slug)
	if connected {
		return false, ErrAlreadyConnected{Slug: organization.Slug}
	}
	organization.ConnectedAt = service.now()
	state.Organizations = append(state.Organizations, organization)
	state.OrgClonePaths[organization.ID] = orgClonePath
	becameActive = state.Active == ""
	if becameActive {
		state.Active = organization.Slug
	}
	return becameActive, service.State.Save(state)
}

// Use makes a connected organisation the active one.
func (service Service) Use(slug string) error {
	state, err := service.State.Load()
	if err != nil {
		return err
	}
	_, found := state.Organization(slug)
	if !found {
		return ErrNotConnected{Slug: slug}
	}
	state.Active = slug
	return service.State.Save(state)
}

// Forgotten is what Forget removed from the state, and which organisation
// is active afterwards.
type Forgotten struct {
	Organization    machine.OrganizationRef
	OrgClonePath    string
	RemovedProjects []machine.Project
	ActiveAfter     string
}

// Forget removes an organisation, its clone path and its projects from the
// state; the next one becomes active. The clone folder itself is the clones
// service's to remove.
func (service Service) Forget(slug string) (Forgotten, error) {
	state, err := service.State.Load()
	if err != nil {
		return Forgotten{}, err
	}
	organization, found := state.Organization(slug)
	if !found {
		return Forgotten{}, ErrNotConnected{Slug: slug}
	}
	forgotten := Forgotten{Organization: organization, OrgClonePath: state.OrgClonePaths[organization.ID]}
	delete(state.OrgClonePaths, organization.ID)
	var keptProjects []machine.Project
	for _, project := range state.Projects {
		if project.Organization == organization.ID {
			forgotten.RemovedProjects = append(forgotten.RemovedProjects, project)
			continue
		}
		keptProjects = append(keptProjects, project)
	}
	state.Projects = keptProjects
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
	forgotten.ActiveAfter = state.Active
	return forgotten, service.State.Save(state)
}

// Restore puts back what Forget removed, the compensation of a workflow
// that failed after forgetting.
func (service Service) Restore(forgotten Forgotten) error {
	state, err := service.State.Load()
	if err != nil {
		return err
	}
	state.Organizations = append(state.Organizations, forgotten.Organization)
	state.OrgClonePaths[forgotten.Organization.ID] = forgotten.OrgClonePath
	state.Projects = append(state.Projects, forgotten.RemovedProjects...)
	if state.Active == "" {
		state.Active = forgotten.Organization.Slug
	}
	return service.State.Save(state)
}

// ForgetAll empties the section: every organisation, clone path and project.
func (service Service) ForgetAll() (forgotten []machine.OrganizationRef, orgClonePaths map[string]string, err error) {
	state, err := service.State.Load()
	if err != nil {
		return nil, nil, err
	}
	forgotten = state.Organizations
	orgClonePaths = state.OrgClonePaths
	state.Organizations = nil
	state.OrgClonePaths = map[string]string{}
	state.Active = ""
	state.Projects = nil
	return forgotten, orgClonePaths, service.State.Save(state)
}
