package app

import (
	"os"
	"path/filepath"

	"github.com/Danidani23/agents-rein/internal/domain/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// ResetPlan is what a reset would remove; the CLI shows it and asks.
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
func PlanReset(dependencies Dependencies, all bool) (ResetPlan, error) {
	state, err := dependencies.State.Load()
	if err != nil {
		return ResetPlan{}, err
	}
	return ResetPlan{
		All:           all,
		Placements:    state.Placements,
		Imports:       state.Imports,
		Organizations: state.Organizations,
		Projects:      state.Projects,
		ImportsFolder: filepath.Join(dependencies.Home, "imports"),
		OrgsFolder:    filepath.Join(dependencies.Home, "orgs"),
	}, nil
}

// Unplaced is the fate of one placement.
type Unplaced struct {
	machine.Placement
	// Removed, Gone (nothing was there) or Skipped with a Reason.
	Removed bool
	Reason  string
}

// ResetResult is what a reset did.
type ResetResult struct {
	Placements    []Unplaced
	RemovedClones []string
	Kept          int
}

// ApplyReset removes every placement rein made, dangling ones included, the
// imports, and with all also the clones, the organisations and the projects.
// A placement that is not what rein recorded is skipped and kept in state.
func ApplyReset(dependencies Dependencies, plan ResetPlan) (ResetResult, error) {
	state, err := dependencies.State.Load()
	if err != nil {
		return ResetResult{}, err
	}
	var result ResetResult
	var kept []machine.Placement
	for _, placed := range state.Placements {
		removed, reason, err := unplace(dependencies.Agents, placed)
		if err != nil {
			return ResetResult{}, err
		}
		result.Placements = append(result.Placements, Unplaced{Placement: placed, Removed: removed, Reason: reason})
		if !removed && reason != "" {
			kept = append(kept, placed)
		}
	}
	state.Placements = kept
	result.Kept = len(kept)
	err = os.RemoveAll(plan.ImportsFolder)
	if err != nil {
		return ResetResult{}, err
	}
	state.Imports = nil
	if plan.All {
		for id, clone := range state.Clones {
			err = os.RemoveAll(clone)
			if err != nil {
				return ResetResult{}, err
			}
			result.RemovedClones = append(result.RemovedClones, id)
		}
		err = os.RemoveAll(plan.OrgsFolder)
		if err != nil {
			return ResetResult{}, err
		}
		state.Clones = map[string]string{}
		state.Organizations = nil
		state.Active = ""
		state.Projects = nil
	}
	// a config.yaml from before D9 is a leftover
	os.Remove(filepath.Join(dependencies.Home, "config.yaml"))
	err = dependencies.State.Save(state)
	if err != nil {
		return ResetResult{}, err
	}
	return result, nil
}

// unplace asks the first agent; every agent unplaces the same way today, and
// a placement does not yet record which agent's home it is in.
func unplace(agents []ports.Agent, placed machine.Placement) (removed bool, reason string, err error) {
	if len(agents) == 0 {
		return false, "no agent adapter to remove it", nil
	}
	return agents[0].Unplace(placed)
}
