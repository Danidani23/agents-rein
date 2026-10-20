package localstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
)

func TestStore(t *testing.T) {
	t.Run("a missing state is empty with a usable clones map and the current version", func(t *testing.T) {
		state, err := Store{Home: t.TempDir()}.Load()
		if err != nil || state.OrgClonePaths == nil || len(state.Placements) != 0 || state.Version != machine.StateVersion {
			t.Fatalf("state %+v err %v", state, err)
		}
	})
	t.Run("a saved state round-trips, with the header, the version and the previous copy", func(t *testing.T) {
		store := Store{Home: t.TempDir()}
		saved := machine.State{
			Organizations: []machine.OrganizationRef{{ID: "id-1", Slug: "dani", Remote: "git@example:dani.git"}},
			Active:        "dani",
			OrgClonePaths: map[string]string{"id-1": "/clones/id-1"},
			Placements:    []machine.Placement{{Path: "/home/.claude/rules/global.md", Target: "/imports/x/rules/global.md", Mode: "link", Owner: "import", Source: "/toolkit"}},
		}
		err := store.Save(saved)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		content, _ := os.ReadFile(filepath.Join(store.Home, stateFile))
		if !strings.HasPrefix(string(content), "# Written by rein") || !strings.Contains(string(content), "version: 1\n") {
			t.Fatalf("content %q", content)
		}
		loaded, err := store.Load()
		if err != nil || loaded.Active != "dani" || loaded.OrgClonePaths["id-1"] != "/clones/id-1" || len(loaded.Placements) != 1 {
			t.Fatalf("loaded %+v err %v", loaded, err)
		}
		loaded.Active = ""
		err = store.Save(loaded)
		previous, _ := os.ReadFile(filepath.Join(store.Home, previousFile))
		if err != nil || !strings.Contains(string(previous), "active: dani") {
			t.Fatalf("previous %q err %v", previous, err)
		}
		entries, _ := os.ReadDir(store.Home)
		if len(entries) != 2 {
			t.Fatalf("temporary or lock files left: %v", entries)
		}
	})
	t.Run("a file without a version is the first version; a newer one and an unknown field are refused", func(t *testing.T) {
		store := Store{Home: t.TempDir()}
		_ = os.WriteFile(filepath.Join(store.Home, stateFile), []byte("organizations: []\nactive: x\n"), 0o644)
		state, err := store.Load()
		if err != nil || state.Version != 1 || state.Active != "x" {
			t.Fatalf("state %+v err %v", state, err)
		}
		_ = os.WriteFile(filepath.Join(store.Home, stateFile), []byte("version: 9\n"), 0o644)
		_, err = store.Load()
		if err == nil || !strings.Contains(err.Error(), "newer rein") {
			t.Fatalf("err %v", err)
		}
		_ = os.WriteFile(filepath.Join(store.Home, stateFile), []byte("version: 1\nclones: {}\n"), 0o644)
		_, err = store.Load()
		if err == nil || !strings.Contains(err.Error(), "clones") || !strings.Contains(err.Error(), previousFile) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("a live lock refuses the write, a stale one is taken over", func(t *testing.T) {
		store := Store{Home: t.TempDir()}
		_ = os.MkdirAll(store.Home, 0o755)
		lock := filepath.Join(store.Home, lockFile)
		_ = os.WriteFile(lock, []byte("4242"), 0o644)
		err := store.Save(machine.Empty())
		if err == nil || !strings.Contains(err.Error(), "pid 4242") {
			t.Fatalf("err %v", err)
		}
		stale := time.Now().Add(-lockStale - time.Minute)
		_ = os.Chtimes(lock, stale, stale)
		err = store.Save(machine.Empty())
		_, lockErr := os.Stat(lock)
		if err != nil || lockErr == nil {
			t.Fatalf("err %v lock still there %v", err, lockErr == nil)
		}
	})
}

func TestHome(t *testing.T) {
	t.Run("the environment variable overrides the default", func(t *testing.T) {
		t.Setenv(HomeVariable, "/elsewhere/rein")
		home, err := Home()
		if err != nil || home != "/elsewhere/rein" {
			t.Fatalf("home %q err %v", home, err)
		}
	})
}
