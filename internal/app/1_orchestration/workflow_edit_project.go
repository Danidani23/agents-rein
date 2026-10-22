package orchestration

import (
	"fmt"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/scaffold"
)

// EditProjectRequest is what changes on a project record: the description,
// the teams, the labels and the path. The slug, the id, the name and the
// fingerprint never change here.
type EditProjectRequest struct {
	Organization string
	Slug         string
	Description  string
	Teams        []string
	Labels       []string
	Path         string
}

// EditProjectResult is what EditProject did.
type EditProjectResult struct {
	Organization string
	Slug         string
	Name         string
	RelativePath string
	// Changed names the fields that differ from before; empty means the
	// record was written as it was.
	Changed []string
}

// EditProject rewrites a project record in the organisation's clone and
// pushes it to main directly, as create does (D13). The teams must exist
// and the labels be registered; the record keeps its id, its creation time
// and its fingerprint and gains the time of the change (D20).
func (orchestrator Orchestrator) EditProject(request EditProjectRequest) (EditProjectResult, error) {
	services := orchestrator.Services
	organization, orgClonePath, err := resolveOrganization(services, request.Organization)
	if err != nil {
		return EditProjectResult{}, err
	}
	var project declaration.Project
	var relativePath, content string
	var changed []string
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
				current, found := tree.Projects[request.Slug]
				if !found {
					return refusal.New(fmt.Sprintf("project %q does not exist in %s; projects are %s", request.Slug, organization.Slug, strings.Join(sortedKeys(tree.Projects), ", ")))
				}
				project = current
				if project.Description != request.Description {
					changed = append(changed, "description")
				}
				if strings.Join(project.Teams, ",") != strings.Join(request.Teams, ",") {
					changed = append(changed, "teams")
				}
				if strings.Join(project.Labels, ",") != strings.Join(request.Labels, ",") {
					changed = append(changed, "labels")
				}
				if project.Path != request.Path {
					changed = append(changed, "path")
				}
				project.Description, project.Teams, project.Labels, project.Path = request.Description, request.Teams, request.Labels, request.Path
				project.UpdatedAt = services.Clock()
				relativePath, content, err = scaffold.ProjectRecord(project)
				if err != nil {
					return err
				}
				_, problems := declaration.ParseProject(project.Slug, []byte(content))
				if len(problems) > 0 {
					var messages []string
					for _, problem := range problems {
						messages = append(messages, problem.Message)
					}
					return refusal.New(strings.Join(messages, "; "))
				}
				return checkProjectReferences(tree, organization.Slug, project)
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
				return services.Records.Publish(accessOf(organization), orgClonePath, []string{relativePath}, fmt.Sprintf("project %s: %s (rein project edit)", project.Slug, strings.Join(changed, ", ")))
			},
		},
	})
	if err != nil {
		return EditProjectResult{}, err
	}
	return EditProjectResult{Organization: organization.Slug, Slug: project.Slug, Name: project.Name, RelativePath: relativePath, Changed: changed}, nil
}
