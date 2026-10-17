// Package identity is the service that says who rein acts for: the active
// account of the host, read every time and never stored (D8, D9).
package identity

import "github.com/Danidani23/agents-rein/internal/ports"

// Service answers who the user is.
type Service struct {
	Host ports.Host
}

// Who returns the host's active account, or the host's own error saying how
// to log in.
func (service Service) Who() (ports.Identity, error) {
	return service.Host.Identity()
}
