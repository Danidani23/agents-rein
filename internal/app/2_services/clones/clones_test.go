package clones

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/ports"
)

const organizationRecord = "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\nhost: github\n"

func TestClones(t *testing.T) {
	home := t.TempDir()
	repository := &fake.Repository{Record: organizationRecord}
	service := Service{Home: home, Repository: repository}
	if service.OrgClonePath("id-1") != filepath.Join(home, "orgs", "id-1") || service.Preflight() != nil {
		t.Fatalf("paths %s", service.OrgClonePath("id-1"))
	}
	stale := filepath.Join(home, "orgs", ".connect-old")
	_ = os.MkdirAll(stale, 0o755)
	old := time.Now().Add(-2 * staleAfter)
	_ = os.Chtimes(stale, old, old)
	fresh := filepath.Join(home, "orgs", ".create-fresh")
	_ = os.MkdirAll(fresh, 0o755)
	temporary, err := service.TemporaryFolder()
	_, staleErr := os.Stat(stale)
	_, freshErr := os.Stat(fresh)
	if err != nil || !strings.HasPrefix(filepath.Base(temporary), ".create-") || staleErr == nil || freshErr != nil {
		t.Fatalf("temporary %q err %v stale %v fresh %v", temporary, err, staleErr, freshErr)
	}
	cloned, err := service.CloneTemporary(ports.RemoteAccess{}, "/remote/acme.git")
	if err != nil || !strings.HasPrefix(filepath.Base(cloned), ".connect-") {
		t.Fatalf("cloned %q err %v", cloned, err)
	}
	organization, err := service.Verify(cloned)
	if err != nil || organization.Slug != "acme" {
		t.Fatalf("verify %+v err %v", organization, err)
	}
	leftover := service.OrgClonePath(organization.ID)
	_ = os.MkdirAll(leftover, 0o755)
	_ = os.WriteFile(filepath.Join(leftover, "stale.txt"), []byte("x"), 0o644)
	placed, err := service.Place(cloned, organization.ID)
	_, staleFileErr := os.Stat(filepath.Join(placed, "stale.txt"))
	_, recordErr := os.Stat(filepath.Join(placed, "declarations", "organization.yaml"))
	if err != nil || placed != leftover || staleFileErr == nil || recordErr != nil {
		t.Fatalf("placed %q err %v stale %v record %v", placed, err, staleFileErr, recordErr)
	}
	_, err = service.Verify(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not an org repository") {
		t.Fatalf("verify of nothing: %v", err)
	}
	repository.Fail = true
	_, err = service.CloneTemporary(ports.RemoteAccess{}, "/remote/nowhere.git")
	entries, _ := os.ReadDir(filepath.Join(home, "orgs"))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".connect-") {
			t.Fatalf("a failed clone left %s", entry.Name())
		}
	}
	if err == nil || !strings.Contains(err.Error(), "git clone failed") {
		t.Fatalf("failed clone: %v", err)
	}
	if err := service.Remove(""); err != nil {
		t.Fatalf("remove nothing: %v", err)
	}
	if err := service.Remove(placed); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(placed); err == nil {
		t.Fatalf("the clone stayed")
	}
	if err := service.RemoveAllOrgClones(); err != nil {
		t.Fatalf("remove all: %v", err)
	}
	if _, err := os.Stat(service.OrgClonesFolder()); err == nil {
		t.Fatalf("the org clones folder stayed")
	}
}
