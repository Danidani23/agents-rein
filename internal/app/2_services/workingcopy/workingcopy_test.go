package workingcopy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
)

const rootCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// realTempDir is a temp folder with its links resolved, as git reports paths.
func realTempDir(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	return resolved
}

func TestLocate(t *testing.T) {
	root := realTempDir(t)
	err := fake.NewProjectClone(root, []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", rootCommit}, nil)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	_ = os.MkdirAll(filepath.Join(root, "services", "shop"), 0o755)
	service := Service{Repository: &fake.Repository{}}
	location, err := service.Locate(filepath.Join(root, "services", "shop"))
	if err != nil || location.Root != root || location.Path != "services/shop" || location.Folder != filepath.Join(root, "services", "shop") || strings.Join(location.Fingerprint, ",") != rootCommit+",bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("location %+v err %v", location, err)
	}
	location, err = service.Locate(root)
	if err != nil || location.Path != "." {
		t.Fatalf("location %+v err %v", location, err)
	}
	_, err = service.Locate(t.TempDir())
	if err == nil {
		t.Fatalf("a folder outside a repository located")
	}
}

func TestIgnoreBlock(t *testing.T) {
	folder := t.TempDir()
	service := Service{}
	changed, err := service.WriteIgnoreBlock(folder, []string{"CLAUDE.md", ".claude"})
	content, _ := os.ReadFile(filepath.Join(folder, ".gitignore"))
	if err != nil || !changed || string(content) != ignoreBlockStart+"\nCLAUDE.md\n.claude\n"+ignoreBlockEnd+"\n" {
		t.Fatalf("changed %v err %v content %q", changed, err, content)
	}
	changed, err = service.WriteIgnoreBlock(folder, []string{"CLAUDE.md", ".claude"})
	if err != nil || changed {
		t.Fatalf("a second write changed the file: %v %v", changed, err)
	}
	_ = os.WriteFile(filepath.Join(folder, ".gitignore"), []byte("node_modules/\n"+ignoreBlockStart+"\nold\n"+ignoreBlockEnd+"\ndist/\n"), 0o644)
	changed, err = service.WriteIgnoreBlock(folder, []string{"CLAUDE.md"})
	content, _ = os.ReadFile(filepath.Join(folder, ".gitignore"))
	if err != nil || !changed || string(content) != "node_modules/\n\ndist/\n\n"+ignoreBlockStart+"\nCLAUDE.md\n"+ignoreBlockEnd+"\n" {
		t.Fatalf("changed %v err %v content %q", changed, err, content)
	}
	err = service.RemoveIgnoreBlock(folder)
	content, _ = os.ReadFile(filepath.Join(folder, ".gitignore"))
	if err != nil || string(content) != "node_modules/\n\ndist/\n" {
		t.Fatalf("err %v content %q", err, content)
	}
	_ = os.WriteFile(filepath.Join(folder, ".gitignore"), []byte(ignoreBlockStart+"\nCLAUDE.md\n"+ignoreBlockEnd+"\n"), 0o644)
	err = service.RemoveIgnoreBlock(folder)
	_, statErr := os.Stat(filepath.Join(folder, ".gitignore"))
	if err != nil || statErr == nil {
		t.Fatalf("err %v; a .gitignore with only the block should be gone", err)
	}
}

func TestCommit(t *testing.T) {
	root := t.TempDir()
	_ = fake.NewProjectClone(root, []string{rootCommit}, []string{"CLAUDE.md", ".claude/rules/go.md", "main.go"})
	repository := &fake.Repository{}
	service := Service{Repository: repository}
	hash, committed, err := service.Commit(root, []string{"CLAUDE.md", ".claude", "CLAUDE.local.md"}, []string{".gitignore"}, "move out")
	if err != nil || !committed || hash == "" || strings.Join(repository.Commits, ",") != "move out" {
		t.Fatalf("hash %q committed %v err %v commits %v", hash, committed, err, repository.Commits)
	}
	_, committed, err = service.Commit(root, []string{"CLAUDE.local.md"}, nil, "nothing")
	if err != nil || committed {
		t.Fatalf("an untracked removal committed: %v %v", committed, err)
	}
}
