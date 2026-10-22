// Package proposalfiles is the service that owns proposal files on disk:
// the pending ones, under .rein/proposals/ in a project clone or under
// <home>/proposals/pending/ for a home distillation, and the history under
// <home>/proposals/<org>/ once they were proposed.
package proposalfiles

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/proposal"
)

// CloneFolder is the folder a project clone keeps rein's own files in; the
// rein block of .gitignore covers it.
const CloneFolder = ".rein"

// Service writes, lists and moves proposal files. Clock names the history
// entries (D20); nil means the wall clock.
type Service struct {
	Home  string
	Clock func() time.Time
}

// Pending is a pending proposal with the file it is in.
type Pending struct {
	Path     string
	Proposal proposal.Proposal
}

// Folder is where the history and the home's pending proposals live.
func (service Service) Folder() string {
	return filepath.Join(service.Home, "proposals")
}

// HomePendingFolder holds the pending proposals distilled from the agents'
// homes, which belong to no clone.
func (service Service) HomePendingFolder() string {
	return filepath.Join(service.Folder(), "pending")
}

// PendingFolderOf is where a clone keeps its pending proposals.
func PendingFolderOf(cloneRoot string) string {
	return filepath.Join(cloneRoot, CloneFolder, "proposals")
}

func (service Service) now() time.Time {
	if service.Clock == nil {
		return time.Now().UTC()
	}
	return service.Clock()
}

// WritePending writes a proposal into folder under its file name. A
// pending proposal of the same id already there is refused unless force,
// since it may hold edits.
func (service Service) WritePending(folder string, document proposal.Proposal, force bool) (string, error) {
	filePath := filepath.Join(folder, proposal.FileName(document.ID))
	_, err := os.Stat(filePath)
	if err == nil && !force {
		return "", fmt.Errorf("a proposal of this snapshot is pending at %s; edit and propose it, or distill with --force to replace it", filePath)
	}
	return filePath, document.Write(filePath)
}

// ListPending reads the pending proposals in folder, oldest first by their
// distillation time; a folder that does not exist holds none.
func (Service) ListPending(folder string) ([]Pending, error) {
	entries, err := os.ReadDir(folder)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pending []Pending
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "proposal-") || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		filePath := filepath.Join(folder, entry.Name())
		document, err := proposal.Read(filePath)
		if err != nil {
			return nil, err
		}
		if document.Status == proposal.StatusPending {
			pending = append(pending, Pending{Path: filePath, Proposal: document})
		}
	}
	sort.Slice(pending, func(left, right int) bool {
		return pending[left].Proposal.DistilledAt.Before(pending[right].Proposal.DistilledAt)
	})
	return pending, nil
}

// Read reads one proposal file.
func (Service) Read(filePath string) (proposal.Proposal, error) {
	return proposal.Read(filePath)
}

// MoveToHistory writes the proposed document under the organisation's
// history folder and removes the pending file.
func (service Service) MoveToHistory(pendingPath string, document proposal.Proposal) (string, error) {
	historyPath := filepath.Join(service.Folder(), document.Organization, proposal.HistoryName(document.ID, document.ProposedAt))
	err := document.Write(historyPath)
	if err != nil {
		return "", err
	}
	err = os.Remove(pendingPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return historyPath, nil
}

// History lists the proposed proposals of an organisation, newest first.
func (service Service) History(organizationSlug string) ([]Pending, error) {
	folder := filepath.Join(service.Folder(), organizationSlug)
	entries, err := os.ReadDir(folder)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var proposed []Pending
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		filePath := filepath.Join(folder, entry.Name())
		document, err := proposal.Read(filePath)
		if err != nil {
			return nil, err
		}
		proposed = append(proposed, Pending{Path: filePath, Proposal: document})
	}
	return proposed, nil
}

// Remove deletes one proposal file: the compensation of a failed workflow.
func (Service) Remove(filePath string) error {
	if filePath == "" {
		return nil
	}
	err := os.Remove(filePath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// RemoveCloneFolder deletes a clone's .rein/ folder: the end of a restore.
func (Service) RemoveCloneFolder(cloneRoot string) error {
	return os.RemoveAll(filepath.Join(cloneRoot, CloneFolder))
}

// RemoveAll deletes the history and the home's pending proposals: the end
// of a reset.
func (service Service) RemoveAll() error {
	return os.RemoveAll(service.Folder())
}
