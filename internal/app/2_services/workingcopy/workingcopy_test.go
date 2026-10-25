package workingcopy

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestTrackedFiles(t *testing.T) {
	root := t.TempDir()
	_ = fake.NewProjectClone(root, []string{rootCommit}, []string{"main.go", "README.md", "docs/deploy.md"})
	listed, err := Service{Repository: &fake.Repository{}}.TrackedFiles(root)
	if err != nil || strings.Join(listed, ",") != "README.md,docs/deploy.md,main.go" {
		t.Fatalf("listed %v err %v", listed, err)
	}
}

func TestTrackedMarkdown(t *testing.T) {
	root := t.TempDir()
	_ = fake.NewProjectClone(root, []string{rootCommit}, []string{"README.md", "docs/deploy.md", "main.go", "docs/notes.txt"})
	listed, err := Service{Repository: &fake.Repository{}}.TrackedMarkdown(root)
	if err != nil || strings.Join(listed, ",") != "README.md,docs/deploy.md" {
		t.Fatalf("listed %v err %v", listed, err)
	}
	// a repository without documents still says it is one
	empty := t.TempDir()
	_ = fake.NewProjectClone(empty, []string{rootCommit}, []string{"main.go"})
	listed, err = Service{Repository: &fake.Repository{}}.TrackedMarkdown(empty)
	if err != nil || listed == nil || len(listed) != 0 {
		t.Fatalf("empty %v err %v", listed, err)
	}
}

func TestFilesUnder(t *testing.T) {
	folder := t.TempDir()
	service := Service{}
	_, _ = service.WriteFiles(folder, map[string]string{".claude/skills/mine/SKILL.md": "x", ".claude/skills/mine/notes/a.md": "y", ".claude/rules/go.md": "z", "main.go": "package main"})
	listed := service.FilesUnder(folder, []string{".claude/skills", ".claude/hooks"})
	if strings.Join(listed, ",") != ".claude/skills/mine/SKILL.md,.claude/skills/mine/notes/a.md" {
		t.Fatalf("listed %v", listed)
	}
}

func TestVerifyAndWriteFiles(t *testing.T) {
	folder := t.TempDir()
	service := Service{}
	previous, err := service.WriteFiles(folder, map[string]string{"CLAUDE.md": "one", ".claude/hooks/x/run.sh": "#!/bin/sh\n"})
	if err != nil || len(previous) != 0 {
		t.Fatalf("write: %v %v", err, previous)
	}
	info, _ := os.Stat(filepath.Join(folder, ".claude", "hooks", "x", "run.sh"))
	// Windows keeps no Unix permission bits
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("the script is not executable: %v", info.Mode())
	}
	recorded := map[string]string{"CLAUDE.md": hashOf([]byte("one")), ".claude/hooks/x/run.sh": hashOf([]byte("#!/bin/sh\n")), "gone.md": hashOf([]byte("z"))}
	verification := service.Verify(folder, recorded)
	if strings.Join(verification.Unchanged, ",") != ".claude/hooks/x/run.sh,CLAUDE.md" || strings.Join(verification.Missing, ",") != "gone.md" || len(verification.Modified) != 0 {
		t.Fatalf("verification %+v", verification)
	}
	previous, err = service.WriteFiles(folder, map[string]string{"CLAUDE.md": "two"})
	if err != nil || previous["CLAUDE.md"] != "one" {
		t.Fatalf("previous %v err %v", previous, err)
	}
	verification = service.Verify(folder, recorded)
	if strings.Join(verification.Modified, ",") != "CLAUDE.md" {
		t.Fatalf("verification %+v", verification)
	}
	_ = service.Remove(folder, []string{".claude/hooks/x/run.sh"})
	service.RemoveEmptyFolders(folder, []string{".claude/hooks/x/run.sh"})
	_, err = os.Stat(filepath.Join(folder, ".claude"))
	if err == nil {
		t.Fatalf("empty folders left behind")
	}
	t.Setenv("REIN_SET_FOR_TEST", "1")
	unset := service.UnsetVariables([]string{"REIN_SET_FOR_TEST", "REIN_SURELY_UNSET_FOR_TEST"})
	if strings.Join(unset, ",") != "REIN_SURELY_UNSET_FOR_TEST" {
		t.Fatalf("unset %v", unset)
	}
}

func TestRepositoryName(t *testing.T) {
	for remote, expected := range map[string]string{"https://github.com/Danidani23/agents-rein.git": "agents-rein", "git@github.com:acme/shop.git": "shop", "/tmp/remotes/web-shop.git": "web-shop", "https://example.com/group/sub/tool/": "tool", "": "folder-name"} {
		service := Service{Repository: &fake.Repository{}}
		root := filepath.Join(t.TempDir(), "folder-name")
		_ = os.MkdirAll(root, 0o755)
		if remote != "" {
			_ = os.WriteFile(filepath.Join(root, ".fake-origin"), []byte(remote+"\n"), 0o644)
		}
		if name := service.RepositoryName(root); name != expected {
			t.Fatalf("%q: %q", remote, name)
		}
	}
}
