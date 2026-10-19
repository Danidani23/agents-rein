package orchestration

import "github.com/Danidani23/agents-rein/internal/app/2_services/machine"

// Unplaced is the fate of one placement.
type Unplaced struct {
	machine.Placement
	// Removed, or gone (nothing was there), or skipped with a Reason.
	Removed bool
	Reason  string
}

// ResetResult is what a reset did.
type ResetResult struct {
	Placements []Unplaced
	// RemovedOrgClones holds the ids of the organisations whose clones went.
	RemovedOrgClones []string
	Kept             int
}

// ResetMachine removes every placement rein made, dangling ones included,
// the imports, and with the plan's All the clones, the organisations and the
// projects. A placement that is not what rein recorded is skipped and kept.
// Deletions cannot be compensated, so the steps run in the order that leaves
// the least damage if one fails: files first, then the state that describes
// them.
func (orchestrator Orchestrator) ResetMachine(plan ResetPlan) (ResetResult, error) {
	services := orchestrator.Services
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
		_, orgClonePaths, err := services.Organizations.ForgetAll()
		if err != nil {
			return ResetResult{}, err
		}
		for organizationID, orgClonePath := range orgClonePaths {
			err = services.Clones.Remove(orgClonePath)
			if err != nil {
				return ResetResult{}, err
			}
			result.RemovedOrgClones = append(result.RemovedOrgClones, organizationID)
		}
		err = services.Clones.RemoveAllOrgClones()
		if err != nil {
			return ResetResult{}, err
		}
	}
	return result, nil
}
