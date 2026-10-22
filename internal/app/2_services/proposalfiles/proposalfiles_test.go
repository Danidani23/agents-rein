package proposalfiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/proposal"
)

func TestPendingAndHistory(t *testing.T) {
	home := t.TempDir()
	clone := t.TempDir()
	when := time.Date(2026, 10, 22, 16, 22, 13, 0, time.UTC)
	service := Service{Home: home, Clock: func() time.Time { return when }}
	document := proposal.Proposal{Version: proposal.Version, ID: "abc123abc123", Status: proposal.StatusPending, Organization: "acme", DistilledAt: when}
	folder := PendingFolderOf(clone)
	if folder != filepath.Join(clone, ".rein", "proposals") || service.HomePendingFolder() != filepath.Join(home, "proposals", "pending") {
		t.Fatalf("folders %s %s", folder, service.HomePendingFolder())
	}
	written, err := service.WritePending(folder, document, false)
	if err != nil || written != filepath.Join(folder, "proposal-abc123abc123.yaml") {
		t.Fatalf("written %s err %v", written, err)
	}
	_, err = service.WritePending(folder, document, false)
	if err == nil || !strings.Contains(err.Error(), "is pending at "+written) {
		t.Fatalf("second write: %v", err)
	}
	_, err = service.WritePending(folder, document, true)
	if err != nil {
		t.Fatalf("forced write: %v", err)
	}
	later := document
	later.ID, later.DistilledAt = "def456def456", when.Add(time.Hour)
	_, _ = service.WritePending(folder, later, false)
	pending, err := service.ListPending(folder)
	if err != nil || len(pending) != 2 || pending[0].Proposal.ID != "abc123abc123" || pending[1].Proposal.ID != "def456def456" {
		t.Fatalf("pending %+v err %v", pending, err)
	}
	none, err := service.ListPending(filepath.Join(clone, "nowhere"))
	if err != nil || len(none) != 0 {
		t.Fatalf("none %+v err %v", none, err)
	}
	document.MarkProposed("rein/propose-x", "https://github.com/acme/settings/pull/1", when.Add(2*time.Hour))
	historyPath, err := service.MoveToHistory(written, document)
	if err != nil || historyPath != filepath.Join(home, "proposals", "acme", "20261022-182213-abc123abc123.yaml") {
		t.Fatalf("history %s err %v", historyPath, err)
	}
	_, err = os.Stat(written)
	if err == nil {
		t.Fatalf("the pending file stayed")
	}
	pending, _ = service.ListPending(folder)
	proposed, err := service.History("acme")
	if err != nil || len(pending) != 1 || len(proposed) != 1 || proposed[0].Proposal.Status != proposal.StatusProposed || proposed[0].Proposal.PullRequest != "https://github.com/acme/settings/pull/1" {
		t.Fatalf("pending %d proposed %+v err %v", len(pending), proposed, err)
	}
	err = service.RemoveCloneFolder(clone)
	_, statErr := os.Stat(filepath.Join(clone, ".rein"))
	if err != nil || statErr == nil {
		t.Fatalf("clone folder: err %v stat %v", err, statErr)
	}
	err = service.RemoveAll()
	_, statErr = os.Stat(service.Folder())
	if err != nil || statErr == nil {
		t.Fatalf("remove all: err %v stat %v", err, statErr)
	}
}
