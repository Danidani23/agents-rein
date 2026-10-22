package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/ports"
)

// isolate keeps git from reading the machine's configuration and gives the
// commits an identity, as the end-to-end test does.
func isolate(t *testing.T) {
	t.Helper()
	emptyConfig := filepath.Join(t.TempDir(), "gitconfig")
	_ = os.WriteFile(emptyConfig, nil, 0o644)
	t.Setenv("GIT_CONFIG_GLOBAL", emptyConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "test")
	t.Setenv("GIT_COMMITTER_NAME", "test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

// bareRemote makes an empty bare repository to push into.
func bareRemote(t *testing.T) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	output, err := exec.Command("git", "init", "-q", "--bare", "--initial-branch=main", remote).CombinedOutput()
	if err != nil {
		t.Fatalf("bare: %v %s", err, output)
	}
	return remote
}

// git runs a git command for an assertion and returns its trimmed output.
func git(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestRepositoryOverABareRemote(t *testing.T) {
	isolate(t)
	repository := Repository{}
	access := ports.RemoteAccess{}
	if err := repository.Preflight(); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	remote := bareRemote(t)
	// publish a folder as the first commit of main
	source := t.TempDir()
	_ = os.WriteFile(filepath.Join(source, "README.md"), []byte("hello\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(source, "declarations", "rules"), 0o755)
	_ = os.WriteFile(filepath.Join(source, "declarations", "rules", "brief.md"), []byte("Be brief.\n"), 0o644)
	err := repository.Publish(access, source, remote, "first")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if git(t, remote, "rev-list", "--count", "main") != "1" || git(t, remote, "log", "-1", "--format=%s", "main") != "first" {
		t.Fatalf("the remote did not get the first commit")
	}
	// clone, locate, fingerprint
	clone := filepath.Join(t.TempDir(), "clone")
	err = repository.Clone(access, remote, clone)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	root, err := repository.Root(filepath.Join(clone, "declarations"))
	if err != nil || filepath.Base(root) != "clone" {
		t.Fatalf("root %q err %v", root, err)
	}
	remoteURL, err := repository.RemoteURL(clone)
	if err != nil || remoteURL != remote {
		t.Fatalf("remote %q err %v", remoteURL, err)
	}
	none, err := repository.RemoteURL(source)
	if err != nil || none != remote {
		// publish added origin to the source too
		t.Fatalf("source remote %q err %v", none, err)
	}
	plain := t.TempDir()
	git(t, plain, "init", "-q")
	noRemote, err := repository.RemoteURL(plain)
	if err != nil || noRemote != "" {
		t.Fatalf("a repository without origin: %q err %v", noRemote, err)
	}
	roots, err := repository.RootCommits(clone)
	if err != nil || len(roots) != 1 || len(roots[0]) != 40 {
		t.Fatalf("roots %v err %v", roots, err)
	}
	tracked, err := repository.TrackedPaths(clone, []string{"README.md", "declarations/rules/brief.md", "missing.md"})
	if err != nil || strings.Join(tracked, ",") != "README.md,declarations/rules/brief.md" {
		t.Fatalf("tracked %v err %v", tracked, err)
	}
	// stage, commit, push
	staged, err := repository.HasStagedChanges(clone)
	if err != nil || staged {
		t.Fatalf("staged before any change: %v err %v", staged, err)
	}
	_ = os.WriteFile(filepath.Join(clone, "CLAUDE.md"), []byte("instructions\n"), 0o644)
	err = repository.StagePaths(clone, []string{"CLAUDE.md"})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	staged, err = repository.HasStagedChanges(clone)
	if err != nil || !staged {
		t.Fatalf("staged after a change: %v err %v", staged, err)
	}
	hash, err := repository.CommitStaged(clone, "add instructions")
	if err != nil || len(hash) != 40 {
		t.Fatalf("commit %q err %v", hash, err)
	}
	err = repository.Push(access, clone)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if git(t, remote, "rev-list", "--count", "main") != "2" {
		t.Fatalf("the push did not reach the remote")
	}
	// a commit of paths carries a deletion, which a move between levels needs
	_ = os.Remove(filepath.Join(clone, "declarations", "rules", "brief.md"))
	_ = os.MkdirAll(filepath.Join(clone, "declarations", "teams", "web", "rules"), 0o755)
	_ = os.WriteFile(filepath.Join(clone, "declarations", "teams", "web", "rules", "brief.md"), []byte("Be brief.\n"), 0o644)
	err = repository.CommitPaths(clone, []string{"declarations/rules/brief.md", "declarations/teams/web/rules/brief.md"}, "move brief")
	if err != nil {
		t.Fatalf("commit paths: %v", err)
	}
	if status := git(t, clone, "show", "--name-status", "--no-renames", "--format=", "HEAD"); !strings.Contains(status, "D\tdeclarations/rules/brief.md") || !strings.Contains(status, "A\tdeclarations/teams/web/rules/brief.md") {
		t.Fatalf("the commit did not carry the move: %q", status)
	}
	// a branch pushed to the remote, then dropped on both sides
	err = repository.CreateBranch(clone, "rein/propose-x")
	if err != nil {
		t.Fatalf("branch: %v", err)
	}
	err = repository.PushBranch(access, clone, "rein/propose-x")
	if err != nil {
		t.Fatalf("push branch: %v", err)
	}
	if !strings.Contains(git(t, remote, "branch", "--list", "rein/propose-x"), "rein/propose-x") {
		t.Fatalf("the branch is not on the remote")
	}
	err = repository.Checkout(clone, "main")
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	err = repository.DeleteBranch(clone, "rein/propose-x")
	if err != nil {
		t.Fatalf("delete branch: %v", err)
	}
	err = repository.DeleteRemoteBranch(access, clone, "rein/propose-x")
	if err != nil {
		t.Fatalf("delete remote branch: %v", err)
	}
	if git(t, remote, "branch", "--list", "rein/propose-x") != "" || git(t, clone, "branch", "--list", "rein/propose-x") != "" {
		t.Fatalf("the branch survived")
	}
	// a change and an untracked file vanish with a reset to the remote; the moved commit on main stays local, so it survives
	_ = os.WriteFile(filepath.Join(clone, "README.md"), []byte("changed\n"), 0o644)
	_ = os.WriteFile(filepath.Join(clone, "untracked.md"), []byte("x\n"), 0o644)
	err = repository.ResetToRemote(clone)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(clone, "README.md"))
	_, untrackedErr := os.Stat(filepath.Join(clone, "untracked.md"))
	if string(content) != "hello\n" || untrackedErr == nil {
		t.Fatalf("reset left README %q untracked err %v", content, untrackedErr)
	}
	// a second clone pulls what the first pushed
	other := filepath.Join(t.TempDir(), "other")
	err = repository.Clone(access, remote, other)
	if err != nil {
		t.Fatalf("second clone: %v", err)
	}
	_ = os.WriteFile(filepath.Join(clone, "declarations", "rules", "late.md"), []byte("Late.\n"), 0o644)
	err = repository.CommitPaths(clone, []string{"declarations/rules/late.md"}, "late")
	if err != nil {
		t.Fatalf("late commit: %v", err)
	}
	err = repository.Push(access, clone)
	if err != nil {
		t.Fatalf("late push: %v", err)
	}
	err = repository.Pull(access, other)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	_, err = os.Stat(filepath.Join(other, "declarations", "rules", "late.md"))
	if err != nil {
		t.Fatalf("the pull did not bring the late file: %v", err)
	}
}

func TestRepositoryFailuresNameWhatRan(t *testing.T) {
	isolate(t)
	repository := Repository{}
	err := repository.Clone(ports.RemoteAccess{}, filepath.Join(t.TempDir(), "nowhere.git"), filepath.Join(t.TempDir(), "clone"))
	if err == nil || !strings.Contains(err.Error(), "git clone") {
		t.Fatalf("clone of nothing: %v", err)
	}
	_, err = repository.Root(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "git rev-parse") {
		t.Fatalf("root outside a repository: %v", err)
	}
}
