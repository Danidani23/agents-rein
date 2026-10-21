package orchestration

import (
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/hosting"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// ConnectResult is what ConnectOrganization did.
type ConnectResult struct {
	Slug         string
	Name         string
	OrgClonePath string
	BecameActive bool
	// SnapshotPaths are the snapshots of the agents' homes taken on the way (D11).
	SnapshotPaths []string
}

// ConnectOrganization clones an org repository, verifies that it holds one
// organisation record, registers the organisation, moves the clone into its
// place and snapshots the agents' homes; the first one connected becomes
// active. When a step fails the registration is undone and the clone
// removed, and an organisation that is already connected is refused before
// its clone is touched.
func (orchestrator Orchestrator) ConnectOrganization(remote string, login string) (ConnectResult, error) {
	owner, repositoryName, _ := hosting.RepositoryOf(remote)
	return connectOrganization(orchestrator, remote, login, owner, repositoryName)
}

// connectOrganization is the connect with the repository on the host known,
// as org create knows it even when the clone address does not say.
func connectOrganization(orchestrator Orchestrator, remote string, login string, owner string, repositoryName string) (ConnectResult, error) {
	services := orchestrator.Services
	err := services.Clones.Preflight()
	if err != nil {
		return ConnectResult{}, refusal.New(err.Error())
	}
	identity, err := services.Credentials.Resolve(login)
	if err != nil {
		return ConnectResult{}, err
	}
	access := ports.RemoteAccess{Login: identity.Login}
	var temporaryOrgClonePath string
	var organization declaration.Organization
	var result ConnectResult
	err = runSteps(services.Progress, []Step{
		{
			Name: "cloning the org repository",
			Do: func() error {
				temporaryOrgClonePath, err = services.Clones.CloneTemporary(access, remote)
				return err
			},
			Undo: func() error { return services.Clones.Remove(temporaryOrgClonePath) },
		},
		{
			Name: "verifying the organisation record",
			Do: func() error {
				organization, err = services.Clones.Verify(temporaryOrgClonePath)
				if err != nil {
					return refusal.New(err.Error())
				}
				return nil
			},
		},
		{
			Name: "registering the organisation",
			Do: func() error {
				organizationRef := machine.OrganizationRef{ID: organization.ID, Slug: organization.Slug, Remote: remote, Host: organization.Host, Login: identity.Login, Owner: owner, Repository: repositoryName}
				result.BecameActive, err = services.Organizations.Register(organizationRef, services.Clones.OrgClonePath(organization.ID))
				return err
			},
			Undo: func() error {
				_, err := services.Organizations.Forget(organization.Slug)
				return err
			},
		},
		{
			Name: "placing the clone",
			Do: func() error {
				result.OrgClonePath, err = services.Clones.Place(temporaryOrgClonePath, organization.ID)
				return err
			},
			Undo: func() error { return services.Clones.Remove(result.OrgClonePath) },
		},
		{
			Name: "snapshotting the agents' homes",
			Do: func() error {
				result.SnapshotPaths, err = services.Snapshots.TakeAgentHomes()
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
