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
func (implementation *local) Status() (StatusReport, error) {
	services := implementation.services
	report := StatusReport{Home: services.Home}
	for _, agent := range services.Agents {
		if agent.Installed() {
			report.Agents = append(report.Agents, agent.Name())
		}
	}
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
	All           bool
	Placements    []machine.Placement
	Imports       []machine.Import
	Organizations []machine.OrganizationRef
	Projects      []machine.Project
	ImportsFolder string
	OrgsFolder    string
}

// PlanReset reads the state and says what a reset would remove.
func (implementation *local) PlanReset(all bool) (ResetPlan, error) {
	services := implementation.services
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
		All:           all,
		Placements:    placed,
		Imports:       imports,
		Organizations: connected,
		Projects:      projects,
		ImportsFolder: services.Placements.ImportsFolder(),
		OrgsFolder:    services.Clones.Folder(),
	}, nil
}

// Unplaced is the fate of one placement.
type Unplaced struct {
	machine.Placement
	// Removed, or gone (nothing was there), or skipped with a Reason.
	Removed bool
	Reason  string
}

// ResetResult is what a reset did.
type ResetResult struct {
	Placements    []Unplaced
	RemovedClones []string
	Kept          int
}

// ResetMachine removes every placement rein made, dangling ones included,
// the imports, and with the plan's All the clones, the organisations and the
// projects. A placement that is not what rein recorded is skipped and kept.
// Deletions cannot be compensated, so the steps run in the order that leaves
// the least damage if one fails: files first, then the state that describes
// them.
func (implementation *local) ResetMachine(plan ResetPlan) (ResetResult, error) {
	services := implementation.services
	var result ResetResult
	var kept []machine.Placement
	placed, _, err := services.Placements.List()
	if err != nil {
		return ResetResult{}, err
	}
	for _, placement := range placed {
		removed, reason, err := services.Placements.Unplace(placement)
		if err != nil {
			return ResetResult{}, err
		}
		result.Placements = append(result.Placements, Unplaced{Placement: placement, Removed: removed, Reason: reason})
		if !removed && reason != "" {
			kept = append(kept, placement)
		}
	}
	result.Kept = len(kept)
	err = services.Placements.Keep(kept)
	if err != nil {
		return ResetResult{}, err
	}
	if plan.All {
		_, clones, err := services.Organizations.ForgetAll()
		if err != nil {
			return ResetResult{}, err
		}
		for id, clone := range clones {
			err = services.Clones.Remove(clone)
			if err != nil {
				return ResetResult{}, err
			}
			result.RemovedClones = append(result.RemovedClones, id)
		}
		err = services.Clones.RemoveAll()
		if err != nil {
			return ResetResult{}, err
		}
	}
	return result, nil
}
