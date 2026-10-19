// Package hosting is the service that creates, protects and deletes org
// repositories on the host, and publishes their first commit. It owns no
// state; its side effects are on the host and in the repository it pushes.
package hosting

import "github.com/Danidani23/agents-rein/internal/ports"

// Service acts on the host and on version control.
type Service struct {
	Host       ports.Host
	Repository ports.Repository
}

// CreateRepository creates the repository on the host and returns its remote.
func (service Service) CreateRepository(repository ports.NewRepository) (remote string, err error) {
	return service.Host.CreateRepository(repository)
}

// DeleteRepository deletes owner/name on the host.
func (service Service) DeleteRepository(owner string, name string) error {
	return service.Host.DeleteRepository(owner, name)
}

// ProtectMainBranch asks the host for the protection of main; a refusal by
// the host for the account's plan is a reason, not an error.
func (service Service) ProtectMainBranch(owner string, name string) (refusedReason string, err error) {
	return service.Host.ProtectMainBranch(owner, name)
}

// Publish turns directory into a repository with one commit and pushes it.
func (service Service) Publish(directory string, remote string, message string) error {
	return service.Repository.Publish(directory, remote, message)
}
