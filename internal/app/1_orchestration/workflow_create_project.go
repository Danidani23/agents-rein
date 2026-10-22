package orchestration

import (
	"fmt"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/scaffold"
)

// CreateProjectRequest is what the person decided at project create. An
// empty Organization means the active one; Labels are category:value.
type CreateProjectRequest struct {
	Organization string
	Name         string
	Description  string
	Teams        []string
	Labels       []string
	Path         string
	// Fingerprint binds a clone at once, when init creates the project.
	Fingerprint []string
}

// CreateProjectResult is what CreateProject did.
type CreateProjectResult struct {
	Organization string
	Slug         string
	Name         string
	Teams        []string
	RelativePath string
}

// CreateProject writes a project record into the organisation's clone and
// pushes it to main directly (D13). The teams must exist and the labels be
// registered; the fingerprint stays empty until init binds a clone.
func (orchestrator Orchestrator) CreateProject(request CreateProjectRequest) (CreateProjectResult, error) {
	services := orchestrator.Services
	organization, orgClonePath, err := resolveOrganization(services, request.Organization)
	if err != nil {
		return CreateProjectResult{}, err
	}
	slug := scaffold.SlugOf(request.Name)
	if slug == "" {
		return CreateProjectResult{}, refusal.New("the name must contain at least one letter or digit")
	}
	project := declaration.Project{ID: scaffold.NewID(), Slug: slug, Name: request.Name, Description: request.Description, Teams: request.Teams, Labels: request.Labels, Path: request.Path, Fingerprint: request.Fingerprint, CreatedAt: services.Clock()}
	relativePath, content, err := scaffold.ProjectRecord(project)
	if err != nil {
		return CreateProjectResult{}, err
	}
	_, problems := declaration.ParseProject(slug, []byte(content))
	if len(problems) > 0 {
		var messages []string
		for _, problem := range problems {
			messages = append(messages, problem.Message)
		}
		return CreateProjectResult{}, refusal.New(strings.Join(messages, "; "))
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
				return checkProjectAgainstTree(tree, organization.Slug, project)
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
				return services.Records.Publish(accessOf(organization), orgClonePath, []string{relativePath}, fmt.Sprintf("project %s (rein project create)", slug))
			},
		},
	})
	if err != nil {
		return CreateProjectResult{}, err
	}
	return CreateProjectResult{Organization: organization.Slug, Slug: slug, Name: request.Name, Teams: request.Teams, RelativePath: relativePath}, nil
}

// checkProjectAgainstTree refuses a project whose slug is taken, whose teams
// do not exist or whose labels are not in the registry.
func checkProjectAgainstTree(tree declaration.Tree, organizationSlug string, project declaration.Project) error {
	_, exists := tree.Projects[project.Slug]
	if exists {
		return refusal.New(fmt.Sprintf("project %q already exists in %s", project.Slug, organizationSlug))
	}
	return checkProjectReferences(tree, organizationSlug, project)
}

// checkProjectReferences refuses a project whose teams do not exist or
// whose labels are not in the registry.
func checkProjectReferences(tree declaration.Tree, organizationSlug string, project declaration.Project) error {
	for _, team := range project.Teams {
		_, found := tree.Teams[team]
		if !found {
			return refusal.New(fmt.Sprintf("team %q does not exist in %s; teams are %s", team, organizationSlug, strings.Join(sortedKeys(tree.Teams), ", ")))
		}
	}
	for _, label := range project.Labels {
		category, value, _ := declaration.SplitLabel(label)
		registered, found := tree.Labels[category]
		if !found {
			return refusal.New(fmt.Sprintf("label %q names no category of the registry; categories are %s", label, strings.Join(sortedKeys(tree.Labels), ", ")))
		}
		_, found = registered.Labels[value]
		if !found {
			return refusal.New(fmt.Sprintf("label %q is not in the registry; %s has %s", label, category, strings.Join(registered.LabelValues(), ", ")))
		}
	}
	conflicts := declaration.SingleValueConflicts(tree.Labels, project.Labels)
	if len(conflicts) > 0 {
		return refusal.New(strings.Join(conflicts, "; "))
	}
	return nil
}

func sortedKeys[Value any](items map[string]Value) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}
