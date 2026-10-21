// Package clones is the service that handles org repository clones on disk,
// under <home>/orgs/: fetching one into a temporary folder, verifying that it
// is an org repository, moving it into its place, and removing it.
package clones

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Service clones and removes org repositories.
type Service struct {
	Home       string
	Repository ports.Repository
}

// OrgClonesFolder is where every org clone lives.
func (service Service) OrgClonesFolder() string {
	return filepath.Join(service.Home, "orgs")
}

// OrgClonePath is where the clone of one organisation lives once placed.
func (service Service) OrgClonePath(organizationID string) string {
	return filepath.Join(service.OrgClonesFolder(), organizationID)
}

// staleAfter is how old a temporary folder may be before it counts as left
// behind by a run that was interrupted.
const staleAfter = time.Hour

// TemporaryFolder makes a fresh folder under the org clones folder for a
// repository being created before it has a remote to clone from, after
// removing the stale ones an interrupted run left.
func (service Service) TemporaryFolder() (string, error) {
	err := service.removeStale()
	if err != nil {
		return "", err
	}
	return os.MkdirTemp(service.OrgClonesFolder(), ".create-*")
}

// removeStale deletes the temporary folders older than staleAfter.
func (service Service) removeStale() error {
	err := os.MkdirAll(service.OrgClonesFolder(), 0o755)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(service.OrgClonesFolder())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !(strings.HasPrefix(entry.Name(), ".create-") || strings.HasPrefix(entry.Name(), ".connect-")) {
			continue
		}
		info, err := entry.Info()
		if err != nil || time.Since(info.ModTime()) < staleAfter {
			continue
		}
		err = os.RemoveAll(filepath.Join(service.OrgClonesFolder(), entry.Name()))
		if err != nil {
			return err
		}
	}
	return nil
}

// Preflight checks that version control is available.
func (service Service) Preflight() error {
	return service.Repository.Preflight()
}

// CloneTemporary clones remote into a fresh temporary folder under the org
// clones folder and returns it.
func (service Service) CloneTemporary(access ports.RemoteAccess, remote string) (temporaryOrgClonePath string, err error) {
	err = service.removeStale()
	if err != nil {
		return "", err
	}
	temporaryOrgClonePath, err = os.MkdirTemp(service.OrgClonesFolder(), ".connect-*")
	if err != nil {
		return "", err
	}
	// git wants a folder that does not exist yet
	os.Remove(temporaryOrgClonePath)
	err = service.Repository.Clone(access, remote, temporaryOrgClonePath)
	if err != nil {
		os.RemoveAll(temporaryOrgClonePath)
		return "", err
	}
	return temporaryOrgClonePath, nil
}

// Verify reads the one organisation record of a clone.
func (service Service) Verify(orgClonePath string) (declaration.Organization, error) {
	return declaration.FindOrganization(orgClonePath)
}

// Place moves a temporary clone to its final folder, named by the
// organisation's id, replacing a leftover from an earlier run.
func (service Service) Place(temporaryOrgClonePath string, organizationID string) (orgClonePath string, err error) {
	orgClonePath = service.OrgClonePath(organizationID)
	err = os.RemoveAll(orgClonePath)
	if err != nil {
		return "", err
	}
	err = os.Rename(temporaryOrgClonePath, orgClonePath)
	if err != nil {
		return "", err
	}
	return orgClonePath, nil
}

// Remove deletes one clone; a missing one is not an error.
func (service Service) Remove(orgClonePath string) error {
	if orgClonePath == "" {
		return nil
	}
	return os.RemoveAll(orgClonePath)
}

// RemoveAllOrgClones deletes the org clones folder with everything in it.
func (service Service) RemoveAllOrgClones() error {
	return os.RemoveAll(service.OrgClonesFolder())
}
