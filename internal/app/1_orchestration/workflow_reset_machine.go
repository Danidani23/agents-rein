package orchestration

import (
	"errors"
	"fmt"
	"os"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
)

// Unplaced is the fate of one placement.
type Unplaced struct {
	machine.Placement
	// Removed, or gone (nothing was there), or skipped with a Reason.
	Removed bool
	Reason  string
}

// ProjectReset is what happened to one initialised project.
type ProjectReset struct {
	Address string
	Folder  string
	Restore RestoreResult
	// Skipped says why the project was not restored: its folder is gone, or
	// the restore failed, in which case the reset stops before wiping.
	Skipped string
	Failed  bool
}

// ResetResult is what a reset did.
type ResetResult struct {
	ProjectClones []ProjectReset
	Placements    []Unplaced
	// RemovedOrgClones holds the ids of the organisations whose clones went.
	RemovedOrgClones []string
	Kept             int
	// Wiped names the folders and files removed at the end.
	Wiped []string
	// Stopped says the reset ended after the restores, because one failed,
	// with the snapshots and the ledger kept.
	Stopped bool
}

// ResetMachine puts every initialised project back as restore does, removes
// every placement rein made in the agents' homes, dangling ones included,
// forgets the organisations and deletes their clones, and deletes the
// ledger, the snapshots, the distillations and the imports: the machine as
// before rein (D12). A project whose folder is gone is skipped; a project
// whose restore fails stops the reset before anything is wiped, so no
// snapshot is lost. Deletions cannot be compensated, so the order leaves the
// least damage if one fails: files first, then the state that describes them.
func (orchestrator Orchestrator) ResetMachine(plan ResetPlan) (ResetResult, error) {
	services := orchestrator.Services
	var result ResetResult
	registered, err := services.Organizations.ProjectClones()
	if err != nil {
		return ResetResult{}, err
	}
	for _, project := range registered {
		reset := ProjectReset{Address: project.Address, Folder: project.ClonePath}
		_, statErr := os.Stat(project.ClonePath)
		if statErr != nil {
			reset.Skipped = "the folder is gone; its registration is dropped and its snapshots stay in the ledger"
			result.ProjectClones = append(result.ProjectClones, reset)
			continue
		}
		restored, err := orchestrator.RestoreProject(RestoreRequest{Directory: project.ClonePath})
		if err != nil {
			reset.Failed = true
			reset.Skipped = err.Error()
			result.ProjectClones = append(result.ProjectClones, reset)
			continue
		}
		reset.Restore = restored
		result.ProjectClones = append(result.ProjectClones, reset)
	}
	for _, reset := range result.ProjectClones {
		if reset.Failed {
			result.Stopped = true
			return result, refusal.New(fmt.Sprintf("the reset stopped before wiping anything: %s could not be put back (%s); fix that and run reset again", reset.Folder, reset.Skipped))
		}
	}
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
	wipes := []struct {
		name string
		wipe func() error
	}{
		{plan.OrgClonesFolder, services.Clones.RemoveAllOrgClones},
		{plan.RemovalsFile, services.Ledger.Delete},
		{plan.SnapshotsFolder, services.Snapshots.RemoveAll},
		{plan.DistillFolder, services.Distillations.RemoveAll},
		{plan.InsertsFolder, services.Ledger.DeleteInserts},
	}
	for _, wipe := range wipes {
		err = wipe.wipe()
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return ResetResult{}, err
		}
		result.Wiped = append(result.Wiped, wipe.name)
	}
	return result, nil
}
