// Package github is the Host port over the gh CLI: GitHub is the host (D8),
// identity is the active gh account, and gh is a prerequisite.
package github

import (
	"fmt"
	"strings"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Host talks to GitHub through gh.
type Host struct{}

var _ ports.Host = Host{}

// Identity reads the active account. gh holds several accounts and marks
// one active; that one is the identity. Logged out is a refusal that names
// gh's own command.
func (Host) Identity() (ports.Identity, error) {
	err := execute.Preflight("gh")
	if err != nil {
		return ports.Identity{}, fmt.Errorf("the GitHub CLI is required: install gh (https://cli.github.com), run gh auth login, then try again")
	}
	result, err := execute.Run("", "gh", "api", "user", "--jq", ".login")
	if err != nil {
		return ports.Identity{}, fmt.Errorf("gh is not logged in to GitHub; run gh auth login, then try again")
	}
	login := strings.TrimSpace(result.Stdout)
	if login == "" {
		return ports.Identity{}, fmt.Errorf("gh returned no account; run gh auth login, then try again")
	}
	return ports.Identity{Login: login, Slug: SlugOf(login)}, nil
}

// SlugOf derives the address segment from a login: lowercase, because macOS
// and Windows file systems are case-insensitive and GitHub logins are unique
// regardless of case, so no two accounts can share a slug.
func SlugOf(login string) string {
	return strings.ToLower(login)
}
