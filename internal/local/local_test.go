package local

import "testing"

func TestState(t *testing.T) {
	t.Run("a missing state is empty with a usable clones map", func(t *testing.T) {
		state, err := LoadState(t.TempDir())
		if err != nil || state.Clones == nil || len(state.Placements) != 0 || state.Active != "" {
			t.Fatalf("state %+v err %v", state, err)
		}
	})
	t.Run("a saved state round-trips", func(t *testing.T) {
		home := t.TempDir()
		saved := State{
			Organizations: []OrganizationRef{{ID: "id-1", Slug: "dani", Remote: "git@example:dani.git"}},
			Active:        "dani",
			Clones:        map[string]string{"id-1": "/clones/id-1"},
			Placements:    []Placement{{Path: "/home/.claude/rules/global.md", Target: "/imports/x/rules/global.md", Mode: "link", Owner: OwnerImport, Source: "/toolkit"}},
		}
		err := SaveState(home, saved)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		loaded, err := LoadState(home)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		ref, found := loaded.Organization("dani")
		if !found || ref.Remote != "git@example:dani.git" || loaded.Active != "dani" || loaded.Clones["id-1"] != "/clones/id-1" {
			t.Fatalf("loaded %+v", loaded)
		}
		if len(loaded.OwnedBy(OwnerImport, "/toolkit")) != 1 || len(loaded.OwnedBy(OwnerImport, "/other")) != 0 {
			t.Fatalf("owned %v", loaded.OwnedBy(OwnerImport, "/toolkit"))
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
