// Package records is the service that writes records into an org repository
// clone and publishes them straight to main, the owner's way (D13): a team,
// a project, later a user. It reads the tree to check them, and it never
// merges: a clone that cannot fast-forward is an error.
package records

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Service writes and publishes records.
type Service struct {
	Repository ports.Repository
}

// Tree reads the org repository whole; a record with problems of its own is
// left out, as the lint reports those.
func (Service) Tree(orgClonePath string) (declaration.Tree, error) {
	tree, _, err := declaration.LoadTree(orgClonePath)
	return tree, err
}

// Pull brings the clone up to date before anything is written.
func (service Service) Pull(orgClonePath string) error {
	return service.Repository.Pull(orgClonePath)
}

// Write puts a new record file into the clone; an existing file is an error,
// since records are never overwritten.
func (Service) Write(orgClonePath string, relativePath string, content string) error {
	target := filepath.Join(orgClonePath, filepath.FromSlash(relativePath))
	_, err := os.Stat(target)
	if err == nil {
		return fmt.Errorf("%s already exists", relativePath)
	}
	err = os.MkdirAll(filepath.Dir(target), 0o755)
	if err != nil {
		return err
	}
	return os.WriteFile(target, []byte(content), 0o644)
}

// Rewrite replaces an existing record file; a missing one is an error.
func (Service) Rewrite(orgClonePath string, relativePath string, content string) error {
	target := filepath.Join(orgClonePath, filepath.FromSlash(relativePath))
	_, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("%s: %w", relativePath, err)
	}
	return os.WriteFile(target, []byte(content), 0o644)
}

// Publish commits the paths and pushes main.
func (service Service) Publish(orgClonePath string, relativePaths []string, message string) error {
	err := service.Repository.CommitPaths(orgClonePath, relativePaths, message)
	if err != nil {
		return err
	}
	return service.Repository.Push(orgClonePath)
}

// Discard puts the clone back to what the remote holds: the compensation
// of a write or a commit whose publish failed.
func (service Service) Discard(orgClonePath string) error {
	return service.Repository.ResetToRemote(orgClonePath)
}
