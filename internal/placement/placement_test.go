package placement

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
}

func source(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "global", "AGENTS.md"), "# Global\n")
	writeFile(t, filepath.Join(root, "skills", "commit", "SKILL.md"), "---\nname: commit\ndescription: d\n---\n")
	writeFile(t, filepath.Join(root, "skills", "adr", "SKILL.md"), "---\nname: adr\ndescription: d\n---\n")
	return root
}

func statuses(outcomes []Outcome) map[string]Status {
	result := map[string]Status{}
	for _, outcome := range outcomes {
		result[filepath.Base(outcome.Path)] = outcome.Status
	}
	return result
}

func TestPlan(t *testing.T) {
	t.Run("the instructions and every skill become pairs, skills sorted", func(t *testing.T) {
		pairs, err := Plan(source(t), "/claude")
		if err != nil || len(pairs) != 3 {
			t.Fatalf("pairs %v err %v", pairs, err)
		}
		if pairs[0].Path != filepath.Join("/claude", "CLAUDE.md") || filepath.Base(pairs[1].Path) != "adr" || filepath.Base(pairs[2].Path) != "commit" {
			t.Fatalf("pairs %v", pairs)
		}
	})
	t.Run("a folder without global/AGENTS.md is refused", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "skills", "commit", "SKILL.md"), "x")
		_, err := Plan(root, "/claude")
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
	t.Run("a folder without skills is refused", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "global", "AGENTS.md"), "x")
		_, err := Plan(root, "/claude")
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
}

func TestApply(t *testing.T) {
	t.Run("links are made, then reported ok on the second run", func(t *testing.T) {
		claudeHome := t.TempDir()
		pairs, _ := Plan(source(t), claudeHome)
		first := statuses(Apply(pairs, ModeLink, nil))
		second := statuses(Apply(pairs, ModeLink, nil))
		if first["CLAUDE.md"] != StatusLinked || first["commit"] != StatusLinked || second["CLAUDE.md"] != StatusOk || second["commit"] != StatusOk {
			t.Fatalf("first %v second %v", first, second)
		}
		resolved, err := os.Readlink(filepath.Join(claudeHome, "skills", "commit"))
		if err != nil || resolved != pairs[2].Target {
			t.Fatalf("link %q err %v", resolved, err)
		}
	})
	t.Run("a file that is not reins's own is refused and left alone", func(t *testing.T) {
		claudeHome := t.TempDir()
		writeFile(t, filepath.Join(claudeHome, "CLAUDE.md"), "mine\n")
		pairs, _ := Plan(source(t), claudeHome)
		outcomes := Apply(pairs, ModeLink, nil)
		if outcomes[0].Status != StatusRefused {
			t.Fatalf("outcome %+v", outcomes[0])
		}
		content, _ := os.ReadFile(filepath.Join(claudeHome, "CLAUDE.md"))
		if string(content) != "mine\n" {
			t.Fatalf("file changed: %q", content)
		}
	})
	t.Run("a link to somewhere else is refused", func(t *testing.T) {
		claudeHome := t.TempDir()
		elsewhere := filepath.Join(t.TempDir(), "other.md")
		writeFile(t, elsewhere, "x")
		err := os.MkdirAll(claudeHome, 0o755)
		if err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		err = os.Symlink(elsewhere, filepath.Join(claudeHome, "CLAUDE.md"))
		if err != nil {
			t.Fatalf("symlink: %v", err)
		}
		pairs, _ := Plan(source(t), claudeHome)
		outcomes := Apply(pairs, ModeLink, nil)
		if outcomes[0].Status != StatusRefused {
			t.Fatalf("outcome %+v", outcomes[0])
		}
	})
	t.Run("copies are made, refreshed when known, and refused when not", func(t *testing.T) {
		claudeHome := t.TempDir()
		pairs, _ := Plan(source(t), claudeHome)
		first := statuses(Apply(pairs, ModeCopy, nil))
		if first["CLAUDE.md"] != StatusCopied || first["commit"] != StatusCopied {
			t.Fatalf("first %v", first)
		}
		content, _ := os.ReadFile(filepath.Join(claudeHome, "skills", "commit", "SKILL.md"))
		if len(content) == 0 {
			t.Fatalf("skill folder not copied")
		}
		refusedAgain := statuses(Apply(pairs, ModeCopy, nil))
		if refusedAgain["CLAUDE.md"] != StatusRefused {
			t.Fatalf("an unknown copy must be refused: %v", refusedAgain)
		}
		known := map[string]string{}
		for _, pair := range pairs {
			known[pair.Path] = pair.Target
		}
		refreshed := statuses(Apply(pairs, ModeCopy, known))
		if refreshed["CLAUDE.md"] != StatusCopied || refreshed["commit"] != StatusCopied {
			t.Fatalf("refreshed %v", refreshed)
		}
	})
}
