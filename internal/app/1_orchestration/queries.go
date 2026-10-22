package orchestration

import (
	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
	"sort"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/proposalfiles"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/resolve"
)

// PlacementStatus is one placement and whether its target still exists.
type PlacementStatus struct {
	machine.Placement
	TargetExists bool
}

// CloneStatus is one initialised project clone with the drift of what sync
// wrote: how many files are as written, edited by hand, or gone.
type CloneStatus struct {
	machine.ProjectClone
	Inserts  int
	Modified int
	Missing  int
}

// StatusReport is everything status shows.
type StatusReport struct {
	// Logins are the host accounts logged in on this machine (D25).
	Logins        []string
	Agents        []string
	Home          string
	Placements    []PlacementStatus
	Missing       int
	Organizations []machine.OrganizationRef
	Active        string
	ProjectClones []machine.ProjectClone
	Clones        []CloneStatus
	Imports       []machine.Import
	// Pending are the proposals waiting in the clones and the home.
	Pending []PendingProposal
}

// Status reads across the services and changes nothing. It runs without a
// login: a machine without one is reported, not refused.
func (orchestrator Orchestrator) Status() (StatusReport, error) {
	services := orchestrator.Services
	report := StatusReport{Home: services.Home, Agents: services.Placements.InstalledAgents()}
	logins, err := services.Credentials.Logins()
	if err != nil {
		return StatusReport{}, err
	}
	report.Logins = logins
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
	report.ProjectClones, err = services.Organizations.ProjectClones()
	if err != nil {
		return StatusReport{}, err
	}
	homePending, err := services.ProposalFiles.ListPending(services.ProposalFiles.HomePendingFolder())
	if err != nil {
		return StatusReport{}, err
	}
	for _, entry := range homePending {
		report.Pending = append(report.Pending, PendingProposal{Path: entry.Path, ID: entry.Proposal.ID, Level: entry.Proposal.DistilledFrom, Candidates: len(entry.Proposal.Candidates), DistilledAt: entry.Proposal.DistilledAt})
	}
	for _, clone := range report.ProjectClones {
		pending, err := services.ProposalFiles.ListPending(proposalfiles.PendingFolderOf(clone.ClonePath))
		if err != nil {
			return StatusReport{}, err
		}
		for _, entry := range pending {
			report.Pending = append(report.Pending, PendingProposal{Path: entry.Path, ID: entry.Proposal.ID, Level: entry.Proposal.DistilledFrom, Candidates: len(entry.Proposal.Candidates), DistilledAt: entry.Proposal.DistilledAt})
		}
		status := CloneStatus{ProjectClone: clone}
		inserts, err := services.Ledger.InsertsOf(clone.Organization, clone.ClonePath)
		if err != nil {
			return StatusReport{}, err
		}
		recorded := map[string]string{}
		for _, insert := range inserts {
			recorded[insert.Path] = insert.SHA256
		}
		verification := services.WorkingCopy.Verify(clone.ClonePath, recorded)
		status.Inserts = len(inserts)
		status.Modified = len(verification.Modified)
		status.Missing = len(verification.Missing)
		report.Clones = append(report.Clones, status)
	}
	return report, nil
}

// ResetPlan is what a reset would put back and remove; the gateway shows
// it and asks.
type ResetPlan struct {
	Placements      []machine.Placement
	Imports         []machine.Import
	Organizations   []machine.OrganizationRef
	ProjectClones   []machine.ProjectClone
	ImportsFolder   string
	OrgClonesFolder string
	RemovalsFile    string
	SnapshotsFolder string
	ProposalsFolder string
	InsertsFolder   string
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
	projects, err := services.Organizations.ProjectClones()
	if err != nil {
		return ResetPlan{}, err
	}
	return ResetPlan{
		Placements:      placed,
		Imports:         imports,
		Organizations:   connected,
		ProjectClones:   projects,
		ImportsFolder:   services.Placements.ImportsFolder(),
		OrgClonesFolder: services.Clones.OrgClonesFolder(),
		RemovalsFile:    services.Ledger.Path(),
		SnapshotsFolder: services.Snapshots.Folder(),
		ProposalsFolder: services.ProposalFiles.Folder(),
		InsertsFolder:   services.Ledger.InsertsFolder(),
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
	Required bool
	Multiple bool
	Values   []LabelValueSummary
}

// ProjectSummary is a project record as the gateway shows and edits it.
type ProjectSummary struct {
	Slug        string
	Name        string
	Description string
	Teams       []string
	Labels      []string
	Path        string
}

// OrganizationDescription is an organisation as the gateway needs it to ask
// its questions: its teams and its label registry, sorted.
type OrganizationDescription struct {
	Slug            string
	Name            string
	OrgClonePath    string
	Teams           []TeamSummary
	Projects        []ProjectSummary
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
	for _, projectSlug := range sortedKeys(tree.Projects) {
		project := tree.Projects[projectSlug]
		description.Projects = append(description.Projects, ProjectSummary{Slug: projectSlug, Name: project.Name, Description: project.Description, Teams: project.Teams, Labels: project.Labels, Path: project.Path})
	}
	for _, category := range sortedKeys(tree.Labels) {
		summary := LabelCategorySummary{Category: category, Required: tree.Labels[category].Required, Multiple: tree.Labels[category].Multiple}
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
	Root string
	// RepositoryName is the repository's name from its origin, or the
	// folder's; the default name of a project created on the spot.
	RepositoryName string
	Path           string
	Folder         string
	Fingerprint    []string
	Matches        []ProjectMatch
	Unbound        []ProjectMatch
	Initialized    string
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
	result := ProjectLocation{Root: location.Root, RepositoryName: services.WorkingCopy.RepositoryName(location.Root), Path: location.Path, Folder: location.Folder, Fingerprint: location.Fingerprint}
	registered, err := services.Organizations.ProjectClones()
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

// Resolution is what a project resolves: the declarations that apply, in
// level order, what was skipped and what is wrong.
type Resolution struct {
	Organization string
	Project      string
	Address      string
	Labels       []string
	Teams        []string
	User         string
	Result       resolve.Result
}

// ResolveProject runs the resolution for a project of the named or active
// organisation, with the user's level, and changes nothing.
func (orchestrator Orchestrator) ResolveProject(organizationSlug string, projectSlug string) (Resolution, error) {
	services := orchestrator.Services
	organization, orgClonePath, err := resolveOrganization(services, organizationSlug)
	if err != nil {
		return Resolution{}, err
	}
	tree, err := services.Records.Tree(orgClonePath)
	if err != nil {
		return Resolution{}, err
	}
	userSlug := userSlugOf(organization)
	context, found := resolve.ContextOf(tree, projectSlug, userSlug, services.Clock())
	if !found {
		return Resolution{}, refusal.New("project " + projectSlug + " does not exist in " + organization.Slug + "; projects are " + strings.Join(sortedKeys(tree.Projects), ", "))
	}
	declarations, err := resolve.LoadDeclarations(orgClonePath)
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{Organization: organization.Slug, Project: projectSlug, Address: projectAddress(organization.Slug, projectSlug), Labels: context.Labels, Teams: context.Teams, User: userSlug, Result: resolve.Resolve(declarations, context)}, nil
}

// PlanProposal says what ProposeDeclarations would do with the request:
// each included candidate at its level, the identical ones left out, the
// excluded ones named. It pulls the org clone and changes nothing else.
func (orchestrator Orchestrator) PlanProposal(request ProposeRequest) (ProposalPlan, error) {
	prepared, err := prepareProposal(orchestrator.Services, request)
	if err != nil {
		return ProposalPlan{}, err
	}
	return prepared.ProposalPlan, nil
}

// PendingProposal is a proposal waiting in a folder.
type PendingProposal struct {
	Path        string
	ID          string
	Level       string
	Candidates  int
	DistilledAt time.Time
}

// PendingProposals lists the proposals waiting under the project folder
// around directory, or under the home's pending folder when directory is
// not inside an initialised project.
func (orchestrator Orchestrator) PendingProposals(directory string) ([]PendingProposal, error) {
	services := orchestrator.Services
	folder := services.ProposalFiles.HomePendingFolder()
	location, err := services.WorkingCopy.Locate(directory)
	if err == nil {
		folder = proposalfiles.PendingFolderOf(location.Folder)
	}
	pending, err := services.ProposalFiles.ListPending(folder)
	if err != nil {
		return nil, err
	}
	var listed []PendingProposal
	for _, entry := range pending {
		listed = append(listed, PendingProposal{Path: entry.Path, ID: entry.Proposal.ID, Level: entry.Proposal.DistilledFrom, Candidates: len(entry.Proposal.Candidates), DistilledAt: entry.Proposal.DistilledAt})
	}
	return listed, nil
}

// DeclarationDescription is a declaration as the gateway shows and edits
// it: its place and its selectors.
type DeclarationDescription struct {
	Organization string
	Address      string
	Level        string
	Kind         string
	ID           string
	Selectors    declaration.Selectors
	Files        []string
}

// DescribeDeclaration reads one declaration from the org clone and changes
// nothing.
func (orchestrator Orchestrator) DescribeDeclaration(organizationSlug string, declarationAddress string) (DeclarationDescription, error) {
	located, err := locateDeclaration(orchestrator.Services, organizationSlug, declarationAddress)
	if err != nil {
		return DeclarationDescription{}, err
	}
	files, selectors, err := readDeclaration(located)
	if err != nil {
		return DeclarationDescription{}, err
	}
	description := DeclarationDescription{Organization: located.organization.Slug, Address: declarationAddress, Level: located.level, Kind: located.kind, ID: located.id, Selectors: selectors, Files: sortedKeys(files)}
	return description, nil
}

// ListDeclarations reads every declaration of an organisation from its
// clone, the ones under level when given, sorted by address.
func (orchestrator Orchestrator) ListDeclarations(organizationSlug string, level string) ([]DeclarationDescription, error) {
	services := orchestrator.Services
	organization, orgClonePath, err := resolveOrganization(services, organizationSlug)
	if err != nil {
		return nil, err
	}
	catalogue, err := distill.LoadCatalogue(orgClonePath)
	if err != nil {
		return nil, err
	}
	var listed []DeclarationDescription
	for _, entry := range catalogue.Entries {
		if level != "" && entry.Level != level && !strings.HasPrefix(entry.Address, level+".") {
			continue
		}
		located := declarationLocation{organization: organization, orgClonePath: orgClonePath, level: entry.Level, levelPath: strings.ReplaceAll(entry.Level, ".", "/"), kind: entry.Kind, id: entry.ID}
		files, selectors, err := readDeclaration(located)
		if err != nil {
			return nil, err
		}
		listed = append(listed, DeclarationDescription{Organization: organization.Slug, Address: entry.Address, Level: entry.Level, Kind: entry.Kind, ID: entry.ID, Selectors: selectors, Files: sortedKeys(files)})
	}
	sort.Slice(listed, func(left, right int) bool { return listed[left].Address < listed[right].Address })
	return listed, nil
}
