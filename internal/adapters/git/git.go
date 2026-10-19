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

func (Repository) Clone(remote string, directory string) error {
	_, err := execute.Run("", "git", "clone", "--quiet", remote, directory)
	return err
}
