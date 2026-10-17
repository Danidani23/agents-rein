// Package organizations is the service that owns the organisations section
// of the machine state: which org repositories are connected, where their
// clones are, and which one is active. It touches no file but the state.
package organizations

import (
	"fmt"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Service owns the connected organisations.
type Service struct {
	State ports.State
}

// ErrNotConnected is returned for a slug that is not connected.
type ErrNotConnected struct {
	Slug string
}

func (err ErrNotConnected) Error() string {
	return fmt.Sprintf("organisation %q is not connected; run rein org list", err.Slug)
}

// ErrAlreadyConnected is returned when registering a slug twice.
type ErrAlreadyConnected struct {
	Slug string
}

func (err ErrAlreadyConnected) Error() string {
	return fmt.Sprintf("organisation %q is already connected; run rein org disconnect %s first", err.Slug, err.Slug)
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
func (service Service) Lookup(slug string) (ref machine.OrganizationRef, clone string, err error) {
	state, err := service.State.Load()
	if err != nil {
		return machine.OrganizationRef{}, "", err
	}
	ref, found := state.Organization(slug)
	if !found {
		return machine.OrganizationRef{}, "", ErrNotConnected{Slug: slug}
	}
	return ref, state.Clones[ref.ID], nil
}

// Register remembers an organisation and its clone; the first one connected
// becomes active.
func (service Service) Register(ref machine.OrganizationRef, clone string) (becameActive bool, err error) {
	state, err := service.State.Load()
	if err != nil {
		return false, err
	}
	_, connected := state.Organization(ref.Slug)
	if connected {
		return false, ErrAlreadyConnected{Slug: ref.Slug}
	}
	state.Organizations = append(state.Organizations, ref)
	state.Clones[ref.ID] = clone
	becameActive = state.Active == ""
	if becameActive {
		state.Active = ref.Slug
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

// Forgotten is what Forget removed from the state.
type Forgotten struct {
	Ref             machine.OrganizationRef
	Clone           string
	RemovedProjects []machine.Project
	Active          string
}

// Forget removes an organisation, its clone path and its projects from the
// state; the next one becomes active. The clone folder itself is the clones
// service's to remove.
func (service Service) Forget(slug string) (Forgotten, error) {
	state, err := service.State.Load()
	if err != nil {
		return Forgotten{}, err
	}
	ref, found := state.Organization(slug)
	if !found {
		return Forgotten{}, ErrNotConnected{Slug: slug}
	}
	forgotten := Forgotten{Ref: ref, Clone: state.Clones[ref.ID]}
	delete(state.Clones, ref.ID)
	var kept []machine.Project
	for _, project := range state.Projects {
		if project.Organization == ref.ID {
			forgotten.RemovedProjects = append(forgotten.RemovedProjects, project)
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
	forgotten.Active = state.Active
	return forgotten, service.State.Save(state)
}

// Restore puts back what Forget removed, the compensation of a workflow
// that failed after forgetting.
func (service Service) Restore(forgotten Forgotten) error {
	state, err := service.State.Load()
	if err != nil {
		return err
	}
	state.Organizations = append(state.Organizations, forgotten.Ref)
	state.Clones[forgotten.Ref.ID] = forgotten.Clone
	state.Projects = append(state.Projects, forgotten.RemovedProjects...)
	if state.Active == "" {
		state.Active = forgotten.Ref.Slug
	}
	return service.State.Save(state)
}

// ForgetAll empties the section: every organisation, clone path and project.
func (service Service) ForgetAll() (removed []machine.OrganizationRef, clones map[string]string, err error) {
	state, err := service.State.Load()
	if err != nil {
		return nil, nil, err
	}
	removed = state.Organizations
	clones = state.Clones
	state.Organizations = nil
	state.Clones = map[string]string{}
	state.Active = ""
	state.Projects = nil
	return removed, clones, service.State.Save(state)
}
