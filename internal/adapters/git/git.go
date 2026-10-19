// Package git is the Repository port over the git CLI.
package git

import (
	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/ports"
)

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
func (Repository) Publish(directory string, remote string, message string) error {
	steps := [][]string{
		{"init", "-q"},
		{"symbolic-ref", "HEAD", "refs/heads/main"},
		{"add", "-A"},
		{"commit", "-q", "-m", message},
		{"remote", "add", "origin", remote},
		{"push", "-q", "-u", "origin", "main"},
	}
	for _, arguments := range steps {
		_, err := execute.Run(directory, "git", arguments...)
		if err != nil {
			return err
		}
	}
	return nil
}

// Pull fast-forwards the clone to its origin; a diverged clone is an error,
// since rein never merges.
func (Repository) Pull(directory string) error {
	_, err := execute.Run(directory, "git", "pull", "-q", "--ff-only")
	return err
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
func (Repository) Push(directory string) error {
	_, err := execute.Run(directory, "git", "push", "-q", "origin", "main")
	return err
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

func (Repository) Clone(remote string, directory string) error {
	_, err := execute.Run("", "git", "clone", "--quiet", remote, directory)
	return err
}
