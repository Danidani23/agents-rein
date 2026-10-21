// Package hosting is the service that creates and protects org repositories
// on the host and publishes their first commit, with the token of the login
// that acts (D25). It owns no state. It never deletes a repository (D26).
package hosting

import (
	"strings"

	"github.com/Danidani23/agents-rein/internal/ports"
)

// Service acts on the host and on version control.
type Service struct {
	Host       ports.Host
	Repository ports.Repository
}

// Owners lists where the login may create repositories.
func (service Service) Owners(accessToken string) ([]string, error) {
	return service.Host.Owners(accessToken)
}

// DescribeRepository says what the host has at owner/name.
func (service Service) DescribeRepository(accessToken string, owner string, name string) (ports.RepositoryInfo, error) {
	return service.Host.Repository(accessToken, owner, name)
}

// CreateRepository creates the repository on the host and returns its clone URL.
func (service Service) CreateRepository(accessToken string, repository ports.NewRepository) (cloneURL string, err error) {
	return service.Host.CreateRepository(accessToken, repository)
}

// ProtectMainBranch asks the host for the protection of main; a refusal by
// the host for the account's plan is a reason, not an error.
func (service Service) ProtectMainBranch(accessToken string, owner string, name string) (refusedReason string, err error) {
	return service.Host.ProtectMainBranch(accessToken, owner, name)
}

// Publish turns directory into a repository with one commit and pushes it.
func (service Service) Publish(access ports.RemoteAccess, directory string, remote string, message string) error {
	return service.Repository.Publish(access, directory, remote, message)
}

// InstallURL is where the app is installed on an account.
func (service Service) InstallURL() string {
	return service.Host.InstallURL()
}

// RepositorySettingsURL is the page where a person deletes owner/name by
// hand; rein never does (D26).
func (service Service) RepositorySettingsURL(owner string, name string) string {
	return service.Host.RepositorySettingsURL(owner, name)
}

// RepositoryOf splits a clone URL of the host into owner and name; false
// for a remote that is not one, such as a local path.
func RepositoryOf(cloneURL string) (owner string, name string, ok bool) {
	trimmed := strings.TrimSuffix(cloneURL, ".git")
	for _, prefix := range []string{"https://github.com/", "git@github.com:", "ssh://git@github.com/"} {
		if strings.HasPrefix(trimmed, prefix) {
			parts := strings.Split(strings.TrimPrefix(trimmed, prefix), "/")
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				return parts[0], parts[1], true
			}
		}
	}
	return "", "", false
}
