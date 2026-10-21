// Package git is the Repository port over the git CLI.
package git

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// askpassVariables are the ways an editor or a desktop injects a dialog
// into git; rein's git never shows one, it fails instead.
var askpassVariables = []string{"GIT_ASKPASS", "SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", "VSCODE_GIT_ASKPASS_NODE", "VSCODE_GIT_ASKPASS_MAIN", "VSCODE_GIT_ASKPASS_EXTRA_ARGS", "VSCODE_GIT_IPC_HANDLE"}

// network runs a git command that talks to a remote: a longer time limit,
// no dialog, and rein itself as the only credential helper, answering with
// the login's token (D25). A remote without a login, a local path, gets no
// helper.
func network(access ports.RemoteAccess, directory string, arguments ...string) error {
	options := execute.Options{Directory: directory, Timeout: execute.NetworkTimeout, Unset: append([]string{"GIT_CONFIG_COUNT"}, askpassVariables...)}
	if access.Login != "" {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		options.Environment = []string{
			"REIN_LOGIN=" + access.Login,
			"GIT_CONFIG_COUNT=2",
			"GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=",
			"GIT_CONFIG_KEY_1=credential.helper", "GIT_CONFIG_VALUE_1=!" + strconv.Quote(executable) + " git-credential",
		}
	}
	_, err := execute.RunWith(options, "git", arguments...)
	return err
}

// Repository runs git on this machine.
type Repository struct{}

var _ ports.Repository = Repository{}

// Preflight checks that git is installed before anything changes.
func (Repository) Preflight() error {
	return execute.Preflight("git")
}

// Clone clones remote into directory, quietly and without prompting.
// Publish initialises directory on main, commits everything in it with the
// user's own git identity, and pushes to remote as origin.
func (Repository) Publish(access ports.RemoteAccess, directory string, remote string, message string) error {
	steps := [][]string{
		{"init", "-q"},
		{"symbolic-ref", "HEAD", "refs/heads/main"},
		{"add", "-A"},
		{"commit", "-q", "-m", message},
		{"remote", "add", "origin", remote},
	}
	for _, arguments := range steps {
		_, err := execute.Run(directory, "git", arguments...)
		if err != nil {
			return err
		}
	}
	return network(access, directory, "push", "-q", "-u", "origin", "main")
}

// Pull fast-forwards the clone to its origin; a diverged clone is an error,
// since rein never merges.
func (Repository) Pull(access ports.RemoteAccess, directory string) error {
	return network(access, directory, "pull", "-q", "--ff-only")
}

// CommitPaths stages the paths and commits them with the user's identity.
func (Repository) CommitPaths(directory string, relativePaths []string, message string) error {
	arguments := append([]string{"add", "--"}, relativePaths...)
	_, err := execute.Run(directory, "git", arguments...)
	if err != nil {
		return err
	}
	_, err = execute.Run(directory, "git", "commit", "-q", "-m", message)
	return err
}

// Push pushes main to origin.
func (Repository) Push(access ports.RemoteAccess, directory string) error {
	return network(access, directory, "push", "-q", "origin", "main")
}

// ResetToRemote throws away local commits and changes: the clone is rein's
// cache of the remote and nothing in it is the user's work.
func (Repository) ResetToRemote(directory string) error {
	_, err := execute.Run(directory, "git", "reset", "-q", "--hard", "origin/main")
	if err != nil {
		return err
	}
	_, err = execute.Run(directory, "git", "clean", "-qfd")
	return err
}

// Root is the repository's top folder.
func (Repository) Root(directory string) (string, error) {
	result, err := execute.Run(directory, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

// RootCommits lists the parentless commits reachable from HEAD.
func (Repository) RootCommits(directory string) ([]string, error) {
	result, err := execute.Run(directory, "git", "rev-list", "--max-parents=0", "HEAD")
	if err != nil {
		return nil, err
	}
	return strings.Fields(result.Stdout), nil
}

// TrackedPaths asks the index which of the paths it holds.
func (Repository) TrackedPaths(directory string, relativePaths []string) ([]string, error) {
	arguments := append([]string{"ls-files", "--"}, relativePaths...)
	result, err := execute.Run(directory, "git", arguments...)
	if err != nil {
		return nil, err
	}
	return strings.Fields(result.Stdout), nil
}

// StagePaths stages additions, changes and deletions under the paths.
func (Repository) StagePaths(directory string, relativePaths []string) error {
	arguments := append([]string{"add", "-A", "--"}, relativePaths...)
	_, err := execute.Run(directory, "git", arguments...)
	return err
}

// HasStagedChanges is git diff --cached --quiet, which exits 1 when there is
// something to commit.
func (Repository) HasStagedChanges(directory string) (bool, error) {
	_, err := execute.Run(directory, "git", "diff", "--cached", "--quiet")
	if err == nil {
		return false, nil
	}
	var failure *execute.Failure
	if errors.As(err, &failure) && failure.Stderr == "" {
		return true, nil
	}
	return false, err
}

// CommitStaged commits with the user's identity and returns the hash.
func (Repository) CommitStaged(directory string, message string) (string, error) {
	_, err := execute.Run(directory, "git", "commit", "-q", "-m", message)
	if err != nil {
		return "", err
	}
	result, err := execute.Run(directory, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

// CreateBranch is git checkout -b.
func (Repository) CreateBranch(directory string, branch string) error {
	_, err := execute.Run(directory, "git", "checkout", "-q", "-b", branch)
	return err
}

// Checkout switches branches.
func (Repository) Checkout(directory string, branch string) error {
	_, err := execute.Run(directory, "git", "checkout", "-q", branch)
	return err
}

// DeleteBranch is git branch -D.
func (Repository) DeleteBranch(directory string, branch string) error {
	_, err := execute.Run(directory, "git", "branch", "-q", "-D", branch)
	return err
}

// PushBranch pushes the branch to origin.
func (Repository) PushBranch(access ports.RemoteAccess, directory string, branch string) error {
	return network(access, directory, "push", "-q", "-u", "origin", branch)
}

// DeleteRemoteBranch removes the branch from origin.
func (Repository) DeleteRemoteBranch(access ports.RemoteAccess, directory string, branch string) error {
	return network(access, directory, "push", "-q", "origin", "--delete", branch)
}

func (Repository) Clone(access ports.RemoteAccess, remote string, directory string) error {
	return network(access, "", "clone", "--quiet", remote, directory)
}
