package orchestration

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
)

// RestoreRequest names the project clone to put back as it was before init.
type RestoreRequest struct {
	Directory string
	NoCommit  bool
	// Force overwrites files that are in the way of the put-back.
	Force bool
}

// RestoreResult is what RestoreProject did.
type RestoreResult struct {
	Folder        string
	Address       string
	Restored      []string
	IgnoreRemoved bool
	Unregistered  bool
	Commit        string
	CommitError   string
	// InsertsRemoved are the files sync wrote that went; InsertsKept the
	// ones a hand edit changed, which stay and are reported.
	InsertsRemoved []string
	InsertsKept    []string
}

const restoreCommitMessage = "restore agent settings from rein (rein restore)"

// RestoreProject puts back what init moved out of a clone, from the ledger
// and the snapshots, takes the marked block out of .gitignore, forgets the
// clone on this machine, marks the removals as restored, and commits the
// put-back as init committed the move-out (D12, D14). A failure before the
// commit undoes everything; the commit itself is reported.
func (orchestrator Orchestrator) RestoreProject(request RestoreRequest) (RestoreResult, error) {
	services := orchestrator.Services
	location, err := services.WorkingCopy.Locate(request.Directory)
	if err != nil {
		return RestoreResult{}, refusal.New(fmt.Sprintf("%s is not inside a git repository with a commit: %v", request.Directory, err))
	}
	registered, err := services.Organizations.ProjectClones()
	if err != nil {
		return RestoreResult{}, err
	}
	var registration machine.ProjectClone
	found := false
	for _, project := range registered {
		if project.ClonePath == location.Folder {
			registration = project
			found = true
		}
	}
	if !found {
		return RestoreResult{}, refusal.New(fmt.Sprintf("%s is not initialised on this machine; nothing of rein's to put back", location.Folder))
	}
	result := RestoreResult{Folder: location.Folder, Address: registration.Address}
	pending, err := services.Ledger.Pending(registration.Address)
	if err != nil {
		return RestoreResult{}, err
	}
	files := map[string]string{}
	tracked := map[string]bool{}
	snapshotPaths := map[string]bool{}
	for _, entry := range pending {
		snapshot, err := services.Snapshots.Read(entry.Snapshot)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("the snapshot of %s is gone: %w", entry.Path, err)
		}
		snapshotPaths[entry.Snapshot] = true
		relative, err := filepath.Rel(location.Folder, entry.Path)
		if err != nil {
			return RestoreResult{}, err
		}
		relative = filepath.ToSlash(relative)
		for key, content := range snapshot.Files {
			if key == relative || strings.HasPrefix(key, relative+"/") {
				files[key] = content
				tracked[key] = entry.Tracked
			}
		}
	}
	inserts, err := services.Ledger.InsertsOf(registration.Organization, location.Folder)
	if err != nil {
		return RestoreResult{}, err
	}
	recorded := map[string]string{}
	for _, insert := range inserts {
		recorded[insert.Path] = insert.SHA256
	}
	verification := services.WorkingCopy.Verify(location.Folder, recorded)
	result.InsertsKept = verification.Modified
	var inTheWay []string
	for relative := range files {
		if _, managed := recorded[relative]; managed {
			// sync's own file at that path goes before the put-back
			continue
		}
		if len(services.WorkingCopy.Present(location.Folder, []string{relative})) > 0 {
			inTheWay = append(inTheWay, relative)
		}
	}
	sort.Strings(inTheWay)
	if len(inTheWay) > 0 && !request.Force {
		return RestoreResult{}, refusal.New(fmt.Sprintf("%s already holds %s; move them away, or pass --force to overwrite them with the snapshot", location.Folder, strings.Join(inTheWay, ", ")))
	}
	for relative := range files {
		result.Restored = append(result.Restored, relative)
	}
	sort.Strings(result.Restored)
	hadBlock := services.WorkingCopy.HasIgnoreBlock(location.Folder)
	var snapshots []string
	for snapshotPath := range snapshotPaths {
		snapshots = append(snapshots, snapshotPath)
	}
	sort.Strings(snapshots)
	err = runSteps(services.Progress, []Step{
		{
			Name: "removing what sync wrote",
			Do: func() error {
				if len(verification.Unchanged) == 0 {
					return nil
				}
				err := services.WorkingCopy.Remove(location.Folder, verification.Unchanged)
				if err != nil {
					return err
				}
				services.WorkingCopy.RemoveEmptyFolders(location.Folder, verification.Unchanged)
				result.InsertsRemoved = verification.Unchanged
				return services.Ledger.ForgetInserts(registration.Organization, location.Folder)
			},
			Undo: func() error {
				return services.Ledger.RecordInserts(registration.Organization, location.Folder, inserts)
			},
		},
		{
			Name: "putting the files back",
			Do:   func() error { return services.WorkingCopy.Restore(location.Folder, files) },
			Undo: func() error { return services.WorkingCopy.Remove(location.Folder, result.Restored) },
		},
		{
			Name: "taking the .gitignore block out",
			Do: func() error {
				if !hadBlock {
					return nil
				}
				result.IgnoreRemoved = true
				return services.WorkingCopy.RemoveIgnoreBlock(location.Folder)
			},
			Undo: func() error {
				if !hadBlock {
					return nil
				}
				_, err := services.WorkingCopy.WriteIgnoreBlock(location.Folder, ignoreEntries(services))
				return err
			},
		},
		{
			Name: "marking the removals restored",
			Do:   func() error { return services.Ledger.MarkRestored(snapshots) },
			Undo: func() error { return services.Ledger.UnmarkRestored(snapshots) },
		},
		{
			Name: "forgetting the clone",
			Do: func() error {
				result.Unregistered = true
				return services.Organizations.UnregisterProjectClone(location.Folder)
			},
			Undo: func() error { return services.Organizations.RegisterProjectClone(registration) },
		},
	})
	if err != nil {
		return RestoreResult{}, err
	}
	if request.NoCommit {
		return result, nil
	}
	var toStage []string
	for _, relative := range result.Restored {
		if tracked[relative] {
			toStage = append(toStage, path.Join(location.Path, relative))
		}
	}
	if result.IgnoreRemoved {
		toStage = append(toStage, path.Join(location.Path, ".gitignore"))
	}
	hash, committed, err := services.WorkingCopy.Commit(location.Root, nil, toStage, restoreCommitMessage)
	switch {
	case err != nil:
		result.CommitError = err.Error()
	case committed:
		result.Commit = hash
	}
	return result, nil
}
