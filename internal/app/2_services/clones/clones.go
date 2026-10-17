// Package clones is the service that handles org repository clones on disk,
// under <home>/orgs/: fetching one into a temporary folder, verifying that it
// is an org repository, moving it into its place, and removing it.
package clones

import (
	"os"
	"path/filepath"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Service clones and removes org repositories.
type Service struct {
	Home       string
	Repository ports.Repository
}

// Folder is where the clones live.
func (service Service) Folder() string {
	return filepath.Join(service.Home, "orgs")
}

// Preflight checks that version control is available.
func (service Service) Preflight() error {
	return service.Repository.Preflight()
}

// CloneTemporary clones remote into a fresh temporary folder under the
// clones folder and returns it.
func (service Service) CloneTemporary(remote string) (temporary string, err error) {
	err = os.MkdirAll(service.Folder(), 0o755)
	if err != nil {
		return "", err
	}
	temporary, err = os.MkdirTemp(service.Folder(), ".connect-*")
	if err != nil {
		return "", err
	}
	// git wants a folder that does not exist yet
	os.Remove(temporary)
	err = service.Repository.Clone(remote, temporary)
	if err != nil {
		os.RemoveAll(temporary)
		return "", err
	}
	return temporary, nil
}

// Verify reads the one organisation record of a clone.
func (service Service) Verify(clone string) (declaration.Organization, error) {
	return declaration.FindOrganization(clone)
}

// Path is where the clone of an organisation lives once placed.
func (service Service) Path(id string) string {
	return filepath.Join(service.Folder(), id)
}

// Place moves a temporary clone to its final folder, named by the
// organisation's id, replacing a leftover from an earlier run.
func (service Service) Place(temporary string, id string) (final string, err error) {
	final = service.Path(id)
	err = os.RemoveAll(final)
	if err != nil {
		return "", err
	}
	err = os.Rename(temporary, final)
	if err != nil {
		return "", err
	}
	return final, nil
}

// Remove deletes a clone folder; a missing one is not an error.
func (service Service) Remove(clone string) error {
	if clone == "" {
		return nil
	}
	return os.RemoveAll(clone)
}

// RemoveAll deletes every clone.
func (service Service) RemoveAll() error {
	return os.RemoveAll(service.Folder())
}
