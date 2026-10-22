package orchestration

import (
	"fmt"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/scaffold"
)

// LabelOperation is what a registry change does.
type LabelOperation string

// The registry operations.
const (
	// LabelAdd adds a value to a category, creating the category when it
	// does not exist yet.
	LabelAdd LabelOperation = "add"
	// LabelRemove takes a value out of a category; a value a project
	// carries, or the last value of a category, stays.
	LabelRemove LabelOperation = "remove"
	// LabelRequire marks a category required: every project carries one.
	LabelRequire LabelOperation = "require"
	// LabelOptional marks a category optional again.
	LabelOptional LabelOperation = "optional"
)

// LabelRegistryRequest is one change to the label registry. Required
// applies when LabelAdd creates the category.
type LabelRegistryRequest struct {
	Organization string
	Operation    LabelOperation
	Category     string
	Value        string
	Description  string
	Required     bool
}

// LabelRegistryResult is what EditLabelRegistry did.
type LabelRegistryResult struct {
	Organization    string
	Category        string
	Value           string
	CreatedCategory bool
	Required        bool
	RelativePath    string
}

// EditLabelRegistry changes one category of the organisation's label
// registry in its clone and pushes it to main directly, as the record
// creators do (D13).
func (orchestrator Orchestrator) EditLabelRegistry(request LabelRegistryRequest) (LabelRegistryResult, error) {
	services := orchestrator.Services
	if !address.IsSlug(request.Category) {
		return LabelRegistryResult{}, refusal.New(fmt.Sprintf("category %q is not a slug: lowercase letters, digits and single hyphens", request.Category))
	}
	if (request.Operation == LabelAdd || request.Operation == LabelRemove) && !address.IsSlug(request.Value) {
		return LabelRegistryResult{}, refusal.New(fmt.Sprintf("label value %q is not a slug: lowercase letters, digits and single hyphens", request.Value))
	}
	organization, orgClonePath, err := resolveOrganization(services, request.Organization)
	if err != nil {
		return LabelRegistryResult{}, err
	}
	result := LabelRegistryResult{Organization: organization.Slug, Category: request.Category, Value: request.Value}
	var relativePath, content, message string
	err = runSteps(services.Progress, []Step{
		{
			Name: "pulling the org repository",
			Do:   func() error { return services.Records.Pull(accessOf(organization), orgClonePath) },
		},
		{
			Name: "checking the registry",
			Do: func() error {
				tree, err := services.Records.Tree(orgClonePath)
				if err != nil {
					return err
				}
				category, exists := tree.Labels[request.Category]
				switch request.Operation {
				case LabelAdd:
					if !exists {
						category = declaration.LabelCategory{Category: request.Category, Required: request.Required, Labels: map[string]string{}}
						result.CreatedCategory = true
					}
					if _, taken := category.Labels[request.Value]; taken {
						return refusal.New(fmt.Sprintf("%s:%s is already in the registry", request.Category, request.Value))
					}
					category.Labels[request.Value] = request.Description
					message = fmt.Sprintf("label %s:%s added", request.Category, request.Value)
				case LabelRemove:
					if !exists {
						return refusal.New(fmt.Sprintf("category %q is not in the registry; categories are %s", request.Category, strings.Join(sortedKeys(tree.Labels), ", ")))
					}
					if _, found := category.Labels[request.Value]; !found {
						return refusal.New(fmt.Sprintf("%s:%s is not in the registry; %s has %s", request.Category, request.Value, request.Category, strings.Join(category.LabelValues(), ", ")))
					}
					if len(category.Labels) == 1 {
						return refusal.New(fmt.Sprintf("%s:%s is the last value of its category, which keeps at least one; a project may carry none of an optional category", request.Category, request.Value))
					}
					var carriers []string
					for _, slug := range sortedKeys(tree.Projects) {
						for _, label := range tree.Projects[slug].Labels {
							if label == request.Category+":"+request.Value {
								carriers = append(carriers, slug)
							}
						}
					}
					if len(carriers) > 0 {
						return refusal.New(fmt.Sprintf("%s:%s is carried by the projects %s; change them first with rein project edit", request.Category, request.Value, strings.Join(carriers, ", ")))
					}
					delete(category.Labels, request.Value)
					message = fmt.Sprintf("label %s:%s removed", request.Category, request.Value)
				case LabelRequire, LabelOptional:
					if !exists {
						return refusal.New(fmt.Sprintf("category %q is not in the registry; categories are %s", request.Category, strings.Join(sortedKeys(tree.Labels), ", ")))
					}
					category.Required = request.Operation == LabelRequire
					message = fmt.Sprintf("label category %s %s", request.Category, request.Operation)
				default:
					return refusal.New(fmt.Sprintf("%q is not a registry operation", request.Operation))
				}
				result.Required = category.Required
				relativePath, content, err = scaffold.LabelCategoryRecord(organization.Slug, category)
				if err != nil {
					return err
				}
				_, problems := declaration.ParseLabelCategory(request.Category, []byte(content))
				if len(problems) > 0 {
					var messages []string
					for _, problem := range problems {
						messages = append(messages, problem.Message)
					}
					return refusal.New(strings.Join(messages, "; "))
				}
				result.RelativePath = relativePath
				return nil
			},
		},
		{
			Name: "writing the registry",
			Do: func() error {
				if result.CreatedCategory {
					return services.Records.Write(orgClonePath, relativePath, content)
				}
				return services.Records.Rewrite(orgClonePath, relativePath, content)
			},
			Undo: func() error { return services.Records.Discard(orgClonePath) },
		},
		{
			Name: "committing and pushing the registry",
			Do: func() error {
				return services.Records.Publish(accessOf(organization), orgClonePath, []string{relativePath}, message+" (rein label)")
			},
		},
	})
	if err != nil {
		return LabelRegistryResult{}, err
	}
	return result, nil
}
