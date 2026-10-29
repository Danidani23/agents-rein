package localstate

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
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
		// the keys of the builds before 2026-10-19 are read and rewritten
		_ = os.WriteFile(filepath.Join(store.Home, stateFile), []byte("clones:\n  id-1: /clones/1\nprojects:\n  - organization: id-1\n    address: organizations.acme.projects.shop\n    clone_path: /p\nplacements:\n  - path: /a\n    target: /t\n    mode: link\n"), 0o644)
		state, err = store.Load()
		if err != nil || state.OrgClonePaths["id-1"] != "/clones/1" || len(state.ProjectClones) != 1 || len(state.Placements) != 1 {
			t.Fatalf("migrated state %+v err %v", state, err)
		}
		err = store.Save(state)
		content, _ := os.ReadFile(filepath.Join(store.Home, stateFile))
		if err != nil || strings.Contains(string(content), "\nclones:") || !strings.Contains(string(content), "org_clone_paths:") || !strings.Contains(string(content), "project_clones:") {
			t.Fatalf("rewritten %q err %v", content, err)
		}
		_ = os.WriteFile(filepath.Join(store.Home, stateFile), []byte("version: 9\n"), 0o644)
		_, err = store.Load()
		if err == nil || !strings.Contains(err.Error(), "newer rein") {
			t.Fatalf("err %v", err)
		}
		_ = os.WriteFile(filepath.Join(store.Home, stateFile), []byte("version: 1\nmystery: {}\n"), 0o644)
		_, err = store.Load()
		if err == nil || !strings.Contains(err.Error(), "mystery") || !strings.Contains(err.Error(), previousFile) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("the lock file is held until released, waits for a live process and takes over one left behind", func(t *testing.T) {
		store := Store{Home: t.TempDir()}
		release, err := store.acquireFile(context.Background(), nil)
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		lock := filepath.Join(store.Home, lockFile)
		holder, _ := os.ReadFile(lock)
		if string(holder) != strconv.Itoa(os.Getpid()) {
			t.Fatalf("the lock names %q, not this process", holder)
		}
		release()
		_, lockErr := os.Stat(lock)
		if lockErr == nil {
			t.Fatalf("the lock stayed after release")
		}
		// a live process holds it: the wait names it and ends with the context
		live := strconv.Itoa(os.Getppid())
		_ = os.WriteFile(lock, []byte(live), 0o644)
		var told []ports.LockWait
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		_, err = store.acquireFile(ctx, func(wait ports.LockWait) { told = append(told, wait) })
		if err == nil || !strings.Contains(err.Error(), "another rein is running (pid "+live) {
			t.Fatalf("err %v", err)
		}
		if len(told) != 1 || told[0].Holder.Process != os.Getppid() || !strings.HasPrefix(told[0].Holder.String(), "another rein, process "+live+", since ") {
			t.Fatalf("told %+v", told)
		}
		// a holder that releases within the wait is waited for
		go func() {
			time.Sleep(300 * time.Millisecond)
			os.Remove(lock)
		}()
		release, err = store.acquireFile(context.Background(), func(ports.LockWait) {})
		if err != nil {
			t.Fatalf("waiting for a holder: %v", err)
		}
		release()
		// a stale lock, a holder that is gone such as a rein killed at a
		// prompt, and this process, whose line has no holder, are taken over
		for _, left := range []string{"4242", "999999", strconv.Itoa(os.Getpid())} {
			_ = os.WriteFile(lock, []byte(left), 0o644)
			if left == "4242" {
				stale := time.Now().Add(-lockStale - time.Minute)
				_ = os.Chtimes(lock, stale, stale)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			release, err = store.acquireFile(ctx, func(ports.LockWait) {})
			cancel()
			if err != nil {
				t.Fatalf("%s was not taken over: %v", left, err)
			}
			release()
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
