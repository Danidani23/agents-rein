package cli

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutputGoesThroughTheWriters(t *testing.T) {
	forbidden := []string{"fmt.Print", "os.Stdout", "os.Stderr"}
	for _, directory := range []string{filepath.Join("..", "..", "cmd"), filepath.Join("..")} {
		err := filepath.WalkDir(directory, func(filePath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(filePath, ".go") || strings.HasSuffix(filePath, "_test.go") {
				return nil
			}
			if strings.HasSuffix(filepath.ToSlash(filePath), "cmd/reins/main.go") {
				return nil
			}
			content, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			for _, pattern := range forbidden {
				if strings.Contains(string(content), pattern) {
					t.Errorf("%s uses %s; output goes through the writers Run receives", filePath, pattern)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
	}
}

func writeFile(t *testing.T, root string, relative string, content string) {
	t.Helper()
	fullPath := filepath.Join(root, relative)
	err := os.MkdirAll(filepath.Dir(fullPath), 0o755)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err = os.WriteFile(fullPath, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestRun(t *testing.T) {
	t.Run("a clean tree exits 0 and reports the counts", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "skills/commit/SKILL.md", "---\nname: commit\ndescription: Draft commits.\n---\n")
		var out, errOut bytes.Buffer
		code := Run([]string{"lint", root}, &out, &errOut)
		if code != ExitWorked || !strings.Contains(out.String(), "0 errors, 0 warnings") || errOut.Len() != 0 {
			t.Fatalf("code %d out %q err %q", code, out.String(), errOut.String())
		}
	})
	t.Run("a tree with an error exits 1 and names the file", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "skills/commit/SKILL.md", "---\nname: commits\ndescription: Draft commits.\n---\n")
		var out, errOut bytes.Buffer
		code := Run([]string{"lint", root}, &out, &errOut)
		if code != ExitRefused || !strings.Contains(out.String(), "SKILL.md: error:") || !strings.Contains(errOut.String(), "lint found 1 errors") {
			t.Fatalf("code %d out %q err %q", code, out.String(), errOut.String())
		}
	})
	t.Run("a missing path exits 2", func(t *testing.T) {
		var out, errOut bytes.Buffer
		code := Run([]string{"lint", filepath.Join(t.TempDir(), "missing")}, &out, &errOut)
		if code != ExitCrashed || errOut.Len() == 0 {
			t.Fatalf("code %d err %q", code, errOut.String())
		}
	})
	t.Run("lint without a path is refused", func(t *testing.T) {
		var out, errOut bytes.Buffer
		code := Run([]string{"lint"}, &out, &errOut)
		if code != ExitRefused {
			t.Fatalf("code %d err %q", code, errOut.String())
		}
	})
	t.Run("an unknown command is refused", func(t *testing.T) {
		var out, errOut bytes.Buffer
		code := Run([]string{"frobnicate"}, &out, &errOut)
		if code != ExitRefused {
			t.Fatalf("code %d err %q", code, errOut.String())
		}
	})
}
