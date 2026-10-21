package orchestration

import (
	"fmt"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/scaffold"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// CreateOrganizationRequest is what the person decided at org create.
type CreateOrganizationRequest struct {
	Host           string
	Login          string
	Name           string
	Description    string
	Owner          string
	RepositoryName string
	Private        bool
}

// CreateResult is what CreateOrganization did.
type CreateResult struct {
	Login          string
	Owner          string
	RepositoryName string
	Private        bool
	Remote         string
	// Adopted says the repository existed, empty, and was used as it was.
	Adopted bool
	// ProtectionRefused is the host's reason when it would not protect main,
	// empty when main is protected.
	ProtectionRefused string
	Connected         ConnectResult
}

const firstCommitMessage = "organisation record, label registry and code owners (rein org create)"

// CreateOrganization creates the org repository on the host with the
// login's token, or adopts an empty one of that name, scaffolds its first
// content, publishes it, protects main as far as the host allows, and
// connects this machine to it. Rein never deletes a repository (D26): a
// failure after the creation leaves the empty repository, which a rerun
// adopts.
func (orchestrator Orchestrator) CreateOrganization(request CreateOrganizationRequest) (CreateResult, error) {
	services := orchestrator.Services
	if request.Host != "" && request.Host != "github" {
		return CreateResult{}, refusal.New(fmt.Sprintf("host %q is not supported yet; this build knows github", request.Host))
	}
	err := services.Clones.Preflight()
	if err != nil {
		return CreateResult{}, refusal.New(err.Error())
	}
	identity, err := services.Credentials.Resolve(request.Login)
	if err != nil {
		return CreateResult{}, err
	}
	accessToken, err := services.Credentials.AccessToken(identity.Login)
	if err != nil {
		return CreateResult{}, err
	}
	slug := scaffold.SlugOf(request.Name)
	if slug == "" {
		return CreateResult{}, refusal.New("the name must contain at least one letter or digit")
	}
	owner := request.Owner
	if owner == "" {
		owner = identity.Login
	}
	organization := declaration.Organization{ID: scaffold.NewID(), Slug: slug, Name: request.Name, Description: request.Description, Host: "github", CreatedAt: services.Clock()}
	result := CreateResult{Login: identity.Login, Owner: owner, RepositoryName: request.RepositoryName, Private: request.Private}
	access := ports.RemoteAccess{Login: identity.Login}
	var temporaryFolder string
	err = runSteps(services.Progress, []Step{
		{
			Name: "looking for the repository on the host",
			Do: func() error {
				info, err := services.Hosting.DescribeRepository(accessToken, owner, request.RepositoryName)
				if err != nil {
					return err
				}
				switch {
				case info.Exists && info.Empty:
					result.Adopted = true
					result.Remote = info.CloneURL
				case info.Exists:
					return refusal.New(fmt.Sprintf("%s/%s exists and holds commits; choose another repository name, or connect it with rein org connect if it is an org repository", owner, request.RepositoryName))
				}
				return nil
			},
		},
		{
			Name: "creating the repository on the host",
			Do: func() error {
				if result.Adopted {
					return nil
				}
				var err error
				result.Remote, err = services.Hosting.CreateRepository(accessToken, ports.NewRepository{Owner: owner, Name: request.RepositoryName, Description: request.Description, Private: request.Private})
				return err
			},
		},
		{
			Name: "writing the first content",
			Do: func() error {
				var err error
				temporaryFolder, err = services.Clones.TemporaryFolder()
				if err != nil {
					return err
				}
				files, err := scaffold.Files(organization, identity.Login)
				if err != nil {
					return err
				}
				return scaffold.Write(temporaryFolder, files)
			},
			Undo: func() error { return services.Clones.Remove(temporaryFolder) },
		},
		{
			Name: "pushing the first commit",
			Do: func() error {
				return services.Hosting.Publish(access, temporaryFolder, result.Remote, firstCommitMessage)
			},
		},
		{
			Name: "protecting main",
			Do: func() error {
				var err error
				result.ProtectionRefused, err = services.Hosting.ProtectMainBranch(accessToken, owner, request.RepositoryName)
				return err
			},
		},
		{
			Name: "connecting this machine",
			Do: func() error {
				var err error
				result.Connected, err = connectOrganization(orchestrator, result.Remote, identity.Login, owner, request.RepositoryName)
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
