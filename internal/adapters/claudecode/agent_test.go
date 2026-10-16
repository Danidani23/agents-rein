package claudecode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Danidani23/agents-rein/internal/domain/machine"
)

func TestUnplace(t *testing.T) {
	agent := Agent{}
	t.Run("a link to the recorded target is removed, dangling or not", func(t *testing.T) {
		home := t.TempDir()
		link := filepath.Join(home, "rules", "global.md")
		_ = os.MkdirAll(filepath.Dir(link), 0o755)
		target := filepath.Join(t.TempDir(), "gone.md")
		_ = os.Symlink(target, link)
		removed, reason, err := agent.Unplace(machine.Placement{Path: link, Target: target, Mode: "link"})
		if err != nil || !removed || reason != "" {
			t.Fatalf("removed %v reason %q err %v", removed, reason, err)
		}
		_, err = os.Lstat(link)
		if err == nil {
			t.Fatalf("link still there")
		}
	})
	t.Run("a link elsewhere is left with a reason", func(t *testing.T) {
		home := t.TempDir()
		link := filepath.Join(home, "x")
		_ = os.Symlink("/elsewhere", link)
		removed, reason, err := agent.Unplace(machine.Placement{Path: link, Target: "/recorded", Mode: "link"})
		if err != nil || removed || reason == "" {
			t.Fatalf("removed %v reason %q err %v", removed, reason, err)
		}
	})
	t.Run("a plain file that rein did not place is left", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "mine")
		_ = os.WriteFile(file, []byte("x"), 0o644)
		removed, reason, _ := agent.Unplace(machine.Placement{Path: file, Target: "/t", Mode: "link"})
		if removed || reason == "" {
			t.Fatalf("removed %v reason %q", removed, reason)
		}
	})
	t.Run("a recorded copy is removed", func(t *testing.T) {
		folder := filepath.Join(t.TempDir(), "skill")
		_ = os.MkdirAll(folder, 0o755)
		removed, reason, err := agent.Unplace(machine.Placement{Path: folder, Target: "/t", Mode: "copy"})
		if err != nil || !removed || reason != "" {
			t.Fatalf("removed %v reason %q err %v", removed, reason, err)
		}
	})
	t.Run("nothing there is not removed and has no reason", func(t *testing.T) {
		removed, reason, err := agent.Unplace(machine.Placement{Path: filepath.Join(t.TempDir(), "none"), Target: "/t", Mode: "link"})
		if err != nil || removed || reason != "" {
			t.Fatalf("removed %v reason %q err %v", removed, reason, err)
		}
	})
}
