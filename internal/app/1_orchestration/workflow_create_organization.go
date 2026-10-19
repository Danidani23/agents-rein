package orchestration

import (
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/scaffold"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// CreateOrganizationRequest is what the person decided at org create.
type CreateOrganizationRequest struct {
	Name           string
	Description    string
	Owner          string
	RepositoryName string
	Private        bool
	// Codeowner is the host login that reviews every change.
	Codeowner string
}

// CreateResult is what CreateOrganization did.
type CreateResult struct {
	Owner          string
	RepositoryName string
	Private        bool
	Remote         string
	// ProtectionRefused is the host's reason when it would not protect main,
	// empty when main is protected.
	ProtectionRefused string
	Connected         ConnectResult
}

const firstCommitMessage = "organisation record, label registry and code owners (rein org create)"

// CreateOrganization creates the org repository on the host, scaffolds its
// first content, publishes it, protects main as far as the host allows, and
// connects this machine to it. A failure deletes the repository again and
// removes what was written.
func (orchestrator Orchestrator) CreateOrganization(request CreateOrganizationRequest) (CreateResult, error) {
	services := orchestrator.Services
	err := services.Clones.Preflight()
	if err != nil {
		return CreateResult{}, refusal.New(err.Error())
	}
	slug := scaffold.SlugOf(request.Name)
	if slug == "" {
		return CreateResult{}, refusal.New("the name must contain at least one letter or digit")
	}
	organization := declaration.Organization{ID: scaffold.NewID(), Slug: slug, Name: request.Name, Description: request.Description, Host: "github"}
	result := CreateResult{Owner: request.Owner, RepositoryName: request.RepositoryName, Private: request.Private}
	var temporaryFolder string
	err = runSteps([]Step{
		{
			Name: "create repository",
			Do: func() error {
				result.Remote, err = services.Hosting.CreateRepository(ports.NewRepository{Owner: request.Owner, Name: request.RepositoryName, Description: request.Description, Private: request.Private})
				return err
			},
			Undo: func() error { return services.Hosting.DeleteRepository(request.Owner, request.RepositoryName) },
		},
		{
			Name: "scaffold",
			Do: func() error {
				temporaryFolder, err = services.Clones.TemporaryFolder()
				if err != nil {
					return err
				}
				files, err := scaffold.Files(organization, request.Codeowner)
				if err != nil {
					return err
				}
				return scaffold.Write(temporaryFolder, files)
			},
			Undo: func() error { return services.Clones.Remove(temporaryFolder) },
		},
		{
			Name: "publish",
			Do:   func() error { return services.Hosting.Publish(temporaryFolder, result.Remote, firstCommitMessage) },
		},
		{
			Name: "protect main",
			Do: func() error {
				result.ProtectionRefused, err = services.Hosting.ProtectMainBranch(request.Owner, request.RepositoryName)
				return err
			},
		},
		{
			Name: "connect",
			Do: func() error {
				result.Connected, err = orchestrator.ConnectOrganization(result.Remote)
				return err
			},
		},
	})
	if err != nil {
		return CreateResult{}, err
	}
	err = services.Clones.Remove(temporaryFolder)
	if err != nil {
		return CreateResult{}, err
	}
	return result, nil
}
