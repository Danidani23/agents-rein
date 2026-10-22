package orchestration

import (
	"fmt"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/scaffold"
)

// EditTeamRequest is what changes on a team record: the description. The
// slug, the id and the name never change here.
type EditTeamRequest struct {
	Organization string
	Slug         string
	Description  string
}

// EditTeamResult is what EditTeam did.
type EditTeamResult struct {
	Organization string
	Slug         string
	Name         string
	RelativePath string
}

// EditTeam rewrites a team record in the organisation's clone and pushes it
// to main directly, as create does (D13); the record keeps its id and its
// creation time.
func (orchestrator Orchestrator) EditTeam(request EditTeamRequest) (EditTeamResult, error) {
	services := orchestrator.Services
	organization, orgClonePath, err := resolveOrganization(services, request.Organization)
	if err != nil {
		return EditTeamResult{}, err
	}
	var team declaration.Team
	var relativePath, content string
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
				current, found := tree.Teams[request.Slug]
				if !found {
					return refusal.New(fmt.Sprintf("team %q does not exist in %s; teams are %s", request.Slug, organization.Slug, strings.Join(sortedKeys(tree.Teams), ", ")))
				}
				if current.Description == request.Description {
					return refusal.New("nothing changed on team " + request.Slug)
				}
				team = current
				team.Description = request.Description
				relativePath, content, err = scaffold.TeamRecord(team)
				if err != nil {
					return err
				}
				_, problems := declaration.ParseTeam(team.Slug, []byte(content))
				if len(problems) > 0 {
					var messages []string
					for _, problem := range problems {
						messages = append(messages, problem.Message)
					}
					return refusal.New(strings.Join(messages, "; "))
				}
				return nil
			},
		},
		{
			Name: "rewriting the record",
			Do:   func() error { return services.Records.Rewrite(orgClonePath, relativePath, content) },
			Undo: func() error { return services.Records.Discard(orgClonePath) },
		},
		{
			Name: "committing and pushing the record",
			Do: func() error {
				return services.Records.Publish(accessOf(organization), orgClonePath, []string{relativePath}, fmt.Sprintf("team %s: description (rein team edit)", team.Slug))
			},
		},
	})
	if err != nil {
		return EditTeamResult{}, err
	}
	return EditTeamResult{Organization: organization.Slug, Slug: team.Slug, Name: team.Name, RelativePath: relativePath}, nil
}
