package snapshots

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/ports"
)

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err = os.WriteFile(path, content, 0o644)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestTakeAgentHomes(t *testing.T) {
	t.Run("the harness text files are recorded, binaries and empty homes are not", func(t *testing.T) {
		agentHome := t.TempDir()
		writeFile(t, filepath.Join(agentHome, "INSTRUCTIONS.md"), []byte("be brief"))
		writeFile(t, filepath.Join(agentHome, "skills", "commit", "SKILL.md"), []byte("---\nname: commit\n---\n"))
		writeFile(t, filepath.Join(agentHome, "skills", "commit", "logo.png"), []byte{0xff, 0xfe, 0x00, 0x01})
		writeFile(t, filepath.Join(agentHome, "transcript.jsonl"), []byte("not a harness file"))
		withSettings := &fake.Agent{Present: true, HomePath: agentHome}
		emptyHome := &fake.Agent{Present: true, HomePath: t.TempDir()}
		takenAt := time.Date(2026, 10, 19, 9, 30, 0, 0, time.UTC)
		service := Service{Home: t.TempDir(), Agents: []ports.Agent{withSettings, emptyHome}, Clock: func() time.Time { return takenAt }}
		snapshotPaths, err := service.TakeAgentHomes()
		if err != nil || len(snapshotPaths) != 1 || filepath.Base(snapshotPaths[0]) != "fake-agent-20261019-093000.json" {
			t.Fatalf("paths %v err %v", snapshotPaths, err)
		}
		info, _ := os.Stat(snapshotPaths[0])
		content, _ := os.ReadFile(snapshotPaths[0])
		var snapshot Snapshot
		err = json.Unmarshal(content, &snapshot)
		if err != nil || snapshot.Agent != "fake-agent" || snapshot.Folder != agentHome || !snapshot.TakenAt.Equal(takenAt) {
			t.Fatalf("snapshot %+v err %v", snapshot, err)
		}
		if len(snapshot.Files) != 2 || snapshot.Files["INSTRUCTIONS.md"] != "be brief" || snapshot.Files["skills/commit/SKILL.md"] == "" || info.Mode().Perm() != 0o600 {
			t.Fatalf("files %v mode %v", snapshot.Files, info.Mode())
		}
		// a second snapshot in the same second gets its own file
		snapshotPaths, err = service.TakeAgentHomes()
		if err != nil || len(snapshotPaths) != 1 || filepath.Base(snapshotPaths[0]) != "fake-agent-20261019-093000-1.json" {
			t.Fatalf("paths %v err %v", snapshotPaths, err)
		}
	})
	t.Run("a missing home is skipped, a dangling link is skipped", func(t *testing.T) {
		agentHome := t.TempDir()
		_ = os.MkdirAll(filepath.Join(agentHome, "skills"), 0o755)
		_ = os.Symlink(filepath.Join(agentHome, "nowhere"), filepath.Join(agentHome, "skills", "gone"))
		absent := &fake.Agent{Present: true, HomePath: filepath.Join(t.TempDir(), "absent")}
		dangling := &fake.Agent{Present: true, HomePath: agentHome}
		service := Service{Home: t.TempDir(), Agents: []ports.Agent{absent, dangling}}
		snapshotPaths, err := service.TakeAgentHomes()
		if err != nil || len(snapshotPaths) != 0 {
			t.Fatalf("paths %v err %v", snapshotPaths, err)
		}
	})
}
