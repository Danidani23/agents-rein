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
func (Repository) Clone(remote string, directory string) error {
	_, err := execute.Run("", "git", "clone", "--quiet", remote, directory)
	return err
}
