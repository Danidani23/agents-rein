package orchestration

import (
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
