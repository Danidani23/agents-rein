package orchestration

import (
	"sort"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// PlacementStatus is one placement and whether its target still exists.
type PlacementStatus struct {
	machine.Placement
	TargetExists bool
}

// StatusReport is everything status shows.
type StatusReport struct {
	Identity      ports.Identity
	IdentityError string
	Agents        []string
	Home          string
	Placements    []PlacementStatus
	Missing       int
	Organizations []machine.OrganizationRef
	Active        string
	Projects      []machine.Project
	Imports       []machine.Import
}

// Status reads across the services and changes nothing. It runs without an
// identity: a logged-out host is reported, not refused.
func (orchestrator Orchestrator) Status() (StatusReport, error) {
	services := orchestrator.Services
	report := StatusReport{Home: services.Home, Agents: services.Placements.InstalledAgents()}
	who, err := services.Identity.Who()
	if err != nil {
		report.IdentityError = err.Error()
	} else {
		report.Identity = who
	}
	placed, imports, err := services.Placements.List()
	if err != nil {
		return StatusReport{}, err
	}
	for _, placement := range placed {
		exists := services.Placements.TargetExists(placement)
		if !exists {
			report.Missing++
		}
		report.Placements = append(report.Placements, PlacementStatus{Placement: placement, TargetExists: exists})
	}
	report.Imports = imports
	report.Organizations, report.Active, err = services.Organizations.List()
	if err != nil {
		return StatusReport{}, err
	}
	report.Projects, err = services.Organizations.Projects()
	if err != nil {
		return StatusReport{}, err
	}
	return report, nil
}

// ResetPlan is what a reset would remove; the gateway shows it and asks.
type ResetPlan struct {
	All             bool
	Placements      []machine.Placement
	Imports         []machine.Import
	Organizations   []machine.OrganizationRef
	Projects        []machine.Project
	ImportsFolder   string
	OrgClonesFolder string
}

// PlanReset reads the state and says what a reset would remove.
func (orchestrator Orchestrator) PlanReset(all bool) (ResetPlan, error) {
	services := orchestrator.Services
	placed, imports, err := services.Placements.List()
	if err != nil {
		return ResetPlan{}, err
	}
	connected, _, err := services.Organizations.List()
	if err != nil {
		return ResetPlan{}, err
	}
	projects, err := services.Organizations.Projects()
	if err != nil {
		return ResetPlan{}, err
	}
	return ResetPlan{
		All:             all,
		Placements:      placed,
		Imports:         imports,
		Organizations:   connected,
		Projects:        projects,
		ImportsFolder:   services.Placements.ImportsFolder(),
		OrgClonesFolder: services.Clones.OrgClonesFolder(),
	}, nil
}

// TeamSummary is a team as a question lists it.
type TeamSummary struct {
	Slug string
	Name string
}

// LabelValueSummary is one registered value of a category.
type LabelValueSummary struct {
	Value       string
	Description string
}

// LabelCategorySummary is one category of the registry with its values.
type LabelCategorySummary struct {
	Category string
	Values   []LabelValueSummary
}

// OrganizationDescription is an organisation as the gateway needs it to ask
// its questions: its teams and its label registry, sorted.
type OrganizationDescription struct {
	Slug            string
	Name            string
	OrgClonePath    string
	Teams           []TeamSummary
	LabelCategories []LabelCategorySummary
}

// DescribeOrganization reads the organisation's clone, the named one or the
// active one, and changes nothing.
func (orchestrator Orchestrator) DescribeOrganization(slug string) (OrganizationDescription, error) {
	services := orchestrator.Services
	organization, orgClonePath, err := resolveOrganization(services, slug)
	if err != nil {
		return OrganizationDescription{}, err
	}
	tree, err := services.Records.Tree(orgClonePath)
	if err != nil {
		return OrganizationDescription{}, err
	}
	description := OrganizationDescription{Slug: organization.Slug, Name: tree.Organization.Name, OrgClonePath: orgClonePath}
	for _, teamSlug := range sortedKeys(tree.Teams) {
		description.Teams = append(description.Teams, TeamSummary{Slug: teamSlug, Name: tree.Teams[teamSlug].Name})
	}
	for _, category := range sortedKeys(tree.Labels) {
		summary := LabelCategorySummary{Category: category}
		registered := tree.Labels[category]
		for _, value := range registered.LabelValues() {
			summary.Values = append(summary.Values, LabelValueSummary{Value: value, Description: registered.Labels[value]})
		}
		description.LabelCategories = append(description.LabelCategories, summary)
	}
	return description, nil
}

func sortStrings(items []string) {
	sort.Strings(items)
}
