package orchestration

import (
	"os"

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

// Status reads everything and changes nothing. It runs without an identity:
// a logged-out host is reported, not refused.
func (implementation *local) Status() (StatusReport, error) {
	dependencies := implementation.dependencies
	report := StatusReport{Home: dependencies.Home, Agents: implementation.installedAgents()}
	who, err := dependencies.Host.Identity()
	if err != nil {
		report.IdentityError = err.Error()
	} else {
		report.Identity = who
	}
	state, err := dependencies.State.Load()
	if err != nil {
		return StatusReport{}, err
	}
	for _, placed := range state.Placements {
		_, err := os.Stat(placed.Target)
		exists := err == nil
		if !exists {
			report.Missing++
		}
		report.Placements = append(report.Placements, PlacementStatus{Placement: placed, TargetExists: exists})
	}
	report.Organizations = state.Organizations
	report.Active = state.Active
	report.Projects = state.Projects
	report.Imports = state.Imports
	return report, nil
}
