package orchestration

import (
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/organizations"
)

// ConnectResult is what ConnectOrganization did.
type ConnectResult struct {
	Slug         string
	Name         string
	Clone        string
	BecameActive bool
}

// ConnectOrganization clones an org repository, verifies that it holds one
// organisation record, registers the organisation and moves the clone into
// its place; the first one connected becomes active. When a step fails the
// registration is undone and the clone removed, and an organisation that is
// already connected is refused before its clone is touched.
func (implementation *local) ConnectOrganization(remote string) (ConnectResult, error) {
	services := implementation.services
	_, err := services.Identity.Who()
	if err != nil {
		return ConnectResult{}, Refusal{Reason: err.Error()}
	}
	err = services.Clones.Preflight()
	if err != nil {
		return ConnectResult{}, Refusal{Reason: err.Error()}
	}
	var temporary string
	var organization declaration.Organization
	var result ConnectResult
	err = run([]Step{
		{
			Name: "clone",
			Do: func() error {
				temporary, err = services.Clones.CloneTemporary(remote)
				return err
			},
			Undo: func() error { return services.Clones.Remove(temporary) },
		},
		{
			Name: "verify",
			Do: func() error {
				organization, err = services.Clones.Verify(temporary)
				if err != nil {
					return Refusal{Reason: err.Error()}
				}
				return nil
			},
		},
		{
			Name: "register",
			Do: func() error {
				ref := machine.OrganizationRef{ID: organization.ID, Slug: organization.Slug, Remote: remote, Host: organization.Host}
				result.BecameActive, err = services.Organizations.Register(ref, services.Clones.Path(organization.ID))
				return refused(err)
			},
			Undo: func() error {
				_, err := services.Organizations.Forget(organization.Slug)
				return err
			},
		},
		{
			Name: "place",
			Do: func() error {
				result.Clone, err = services.Clones.Place(temporary, organization.ID)
				return err
			},
		},
	})
	if err != nil {
		return ConnectResult{}, err
	}
	result.Slug = organization.Slug
	result.Name = organization.Name
	return result, nil
}

// DisconnectResult is what DisconnectOrganization did.
type DisconnectResult struct {
	Clone           string
	RemovedProjects int
	Active          string
}

// DisconnectOrganization forgets an organisation and removes its clone; the
// remote is never touched. If the clone cannot be removed the registration
// is restored.
func (implementation *local) DisconnectOrganization(slug string) (DisconnectResult, error) {
	services := implementation.services
	_, err := services.Identity.Who()
	if err != nil {
		return DisconnectResult{}, Refusal{Reason: err.Error()}
	}
	var forgotten organizations.Forgotten
	err = run([]Step{
		{
			Name: "forget",
			Do: func() error {
				forgotten, err = services.Organizations.Forget(slug)
				return refused(err)
			},
			Undo: func() error { return services.Organizations.Restore(forgotten) },
		},
		{
			Name: "remove clone",
			Do:   func() error { return services.Clones.Remove(forgotten.Clone) },
		},
	})
	if err != nil {
		return DisconnectResult{}, err
	}
	return DisconnectResult{Clone: forgotten.Clone, RemovedProjects: len(forgotten.RemovedProjects), Active: forgotten.Active}, nil
}
