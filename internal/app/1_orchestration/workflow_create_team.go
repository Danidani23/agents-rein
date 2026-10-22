package orchestration

import (
	"fmt"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/scaffold"
)

// CreateTeamRequest is what the person decided at team create. An empty
// Organization means the active one.
type CreateTeamRequest struct {
	Organization string
	Name         string
	Description  string
}

// CreateTeamResult is what CreateTeam did.
type CreateTeamResult struct {
	Organization string
	Slug         string
	Name         string
	RelativePath string
}

// CreateTeam writes a team record into the organisation's clone and pushes
// it to main directly, the owner's way (D13). The clone is brought up to
// date first; on a failed publish it is put back to what the remote holds.
func (orchestrator Orchestrator) CreateTeam(request CreateTeamRequest) (CreateTeamResult, error) {
	services := orchestrator.Services
	organization, orgClonePath, err := resolveOrganization(services, request.Organization)
	if err != nil {
		return CreateTeamResult{}, err
	}
	slug := scaffold.SlugOf(request.Name)
	if slug == "" {
		return CreateTeamResult{}, refusal.New("the name must contain at least one letter or digit")
	}
	team := declaration.Team{ID: scaffold.NewID(), Slug: slug, Name: request.Name, Description: request.Description, CreatedAt: services.Clock()}
	relativePath, content, err := scaffold.TeamRecord(team)
	if err != nil {
		return CreateTeamResult{}, err
	}
	err = runSteps(services.Progress, []Step{
		{
			Name: "pulling the org repository",
			Do:   func() error { return services.Records.Pull(accessOf(organization), orgClonePath) },
		},
		{
			Name: "checking the records",
			Do: func() error {
				tree, err := services.Records.Tree(orgClonePath)
				if err != nil {
					return err
				}
				_, exists := tree.Teams[slug]
				if exists {
					return refusal.New(fmt.Sprintf("team %q already exists in %s", slug, organization.Slug))
				}
				return nil
			},
		},
		{
			Name: "writing the record",
			Do:   func() error { return services.Records.Write(orgClonePath, relativePath, content) },
			Undo: func() error { return services.Records.Discard(orgClonePath) },
		},
		{
			Name: "committing and pushing the record",
			Do: func() error {
				return services.Records.Publish(accessOf(organization), orgClonePath, []string{relativePath}, fmt.Sprintf("team %s (rein team create)", slug))
			},
		},
	})
	if err != nil {
		return CreateTeamResult{}, err
	}
	return CreateTeamResult{Organization: organization.Slug, Slug: slug, Name: request.Name, RelativePath: relativePath}, nil
}
