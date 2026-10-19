package localstate

import (
	"testing"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
)

func TestStore(t *testing.T) {
	t.Run("a missing state is empty with a usable clones map", func(t *testing.T) {
		state, err := Store{Home: t.TempDir()}.Load()
		if err != nil || state.OrgClonePaths == nil || len(state.Placements) != 0 {
			t.Fatalf("state %+v err %v", state, err)
		}
	})
	t.Run("a saved state round-trips", func(t *testing.T) {
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
		loaded, err := store.Load()
		if err != nil || loaded.Active != "dani" || loaded.OrgClonePaths["id-1"] != "/clones/id-1" || len(loaded.Placements) != 1 {
			t.Fatalf("loaded %+v err %v", loaded, err)
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
