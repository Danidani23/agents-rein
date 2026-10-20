package orchestration

import (
	"sort"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
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

// ResetPlan is what a reset would put back and remove; the gateway shows
// it and asks.
type ResetPlan struct {
	Placements      []machine.Placement
	Imports         []machine.Import
	Organizations   []machine.OrganizationRef
	Projects        []machine.Project
	ImportsFolder   string
	OrgClonesFolder string
	RemovalsFile    string
	SnapshotsFolder string
	DistillFolder   string
}

// PlanReset reads the state and says what a reset would do.
func (orchestrator Orchestrator) PlanReset() (ResetPlan, error) {
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
		Placements:      placed,
		Imports:         imports,
		Organizations:   connected,
		Projects:        projects,
		ImportsFolder:   services.Placements.ImportsFolder(),
		OrgClonesFolder: services.Clones.OrgClonesFolder(),
		RemovalsFile:    services.Ledger.Path(),
		SnapshotsFolder: services.Snapshots.Folder(),
		DistillFolder:   services.Distillations.Folder(),
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

// ProjectMatch is a project record as seen from a clone.
type ProjectMatch struct {
	Organization string
	Project      string
	Name         string
	Address      string
}

// ProjectLocation is what init learns about a folder before it changes
// anything: where the repository is, its fingerprint, the records across
// the connected organisations that carry that fingerprint and path, the
// records of the active organisation that are not bound yet, and whether
// this machine already initialised the folder.
type ProjectLocation struct {
	Root        string
	Path        string
	Folder      string
	Fingerprint []string
	Matches     []ProjectMatch
	Unbound     []ProjectMatch
	Initialized string
}

// LocateProject reads the repository around directory and the connected
// organisations, and changes nothing. Outside a repository, or in one
// without commits, it is a refusal.
func (orchestrator Orchestrator) LocateProject(directory string) (ProjectLocation, error) {
	services := orchestrator.Services
	location, err := services.WorkingCopy.Locate(directory)
	if err != nil {
		return ProjectLocation{}, err
	}
	result := ProjectLocation{Root: location.Root, Path: location.Path, Folder: location.Folder, Fingerprint: location.Fingerprint}
	registered, err := services.Organizations.Projects()
	if err != nil {
		return ProjectLocation{}, err
	}
	for _, project := range registered {
		if project.ClonePath == location.Folder {
			result.Initialized = project.Address
		}
	}
	connected, active, err := services.Organizations.List()
	if err != nil {
		return ProjectLocation{}, err
	}
	for _, organization := range connected {
		_, orgClonePath, err := services.Organizations.Lookup(organization.Slug)
		if err != nil {
			return ProjectLocation{}, err
		}
		tree, err := services.Records.Tree(orgClonePath)
		if err != nil {
			return ProjectLocation{}, err
		}
		for _, slug := range sortedKeys(tree.Projects) {
			project := tree.Projects[slug]
			match := ProjectMatch{Organization: organization.Slug, Project: slug, Name: project.Name, Address: projectAddress(organization.Slug, slug)}
			switch {
			case sameFingerprint(project.Fingerprint, location.Fingerprint) && project.Path == location.Path:
				result.Matches = append(result.Matches, match)
			case len(project.Fingerprint) == 0 && organization.Slug == active:
				result.Unbound = append(result.Unbound, match)
			}
		}
	}
	return result, nil
}

func projectAddress(organizationSlug string, projectSlug string) string {
	return "organizations." + organizationSlug + ".projects." + projectSlug
}

func sameFingerprint(recorded []string, found []string) bool {
	if len(recorded) == 0 || len(recorded) != len(found) {
		return false
	}
	sortedRecorded := append([]string(nil), recorded...)
	sort.Strings(sortedRecorded)
	return strings.Join(sortedRecorded, ",") == strings.Join(found, ",")
}

// projectRecordOf finds one project record in a tree.
func projectRecordOf(tree declaration.Tree, slug string) (declaration.Project, bool) {
	project, found := tree.Projects[slug]
	return project, found
}
