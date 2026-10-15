package local

import (
	"errors"
	"testing"
)

func TestConfig(t *testing.T) {
	t.Run("a fresh home has no config and Require refuses", func(t *testing.T) {
		home := t.TempDir()
		_, found, err := LoadConfig(home)
		if err != nil || found {
			t.Fatalf("err %v found %v", err, found)
		}
		_, err = Require(home)
		if !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("expected ErrNotConfigured, got %v", err)
		}
	})
	t.Run("a saved config round-trips and Require returns it", func(t *testing.T) {
		home := t.TempDir()
		saved := Config{User: "danielmolnar", Organizations: []OrganizationRef{{ID: "id-1", Slug: "dani", Remote: "git@example:dani.git"}}, Active: "dani"}
		err := SaveConfig(home, saved)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		loaded, err := Require(home)
		if err != nil {
			t.Fatalf("require: %v", err)
		}
		ref, found := loaded.Organization("dani")
		if loaded.User != "danielmolnar" || !found || ref.Remote != "git@example:dani.git" || loaded.Active != "dani" {
			t.Fatalf("loaded %+v", loaded)
		}
	})
}

func TestState(t *testing.T) {
	t.Run("a missing state is empty with a usable clones map", func(t *testing.T) {
		state, err := LoadState(t.TempDir())
		if err != nil || state.Clones == nil || len(state.Placements) != 0 {
			t.Fatalf("state %+v err %v", state, err)
		}
	})
	t.Run("a saved state round-trips", func(t *testing.T) {
		home := t.TempDir()
		saved := State{Clones: map[string]string{"id-1": "/clones/id-1"}, Placements: []Placement{{Path: "/home/.claude/CLAUDE.md", Target: "/toolkit/global/AGENTS.md", Mode: "link"}}}
		err := SaveState(home, saved)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		loaded, err := LoadState(home)
		if err != nil || loaded.Clones["id-1"] != "/clones/id-1" || len(loaded.Placements) != 1 {
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
