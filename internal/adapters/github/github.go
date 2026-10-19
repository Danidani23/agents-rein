// Package github is the Host port over the gh CLI: GitHub is the host (D8),
// identity is the active gh account, and gh is a prerequisite.
package github

import (
	"errors"
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

// GitProtocol reads gh's git_protocol setting; gh's own default is https.
func (Host) GitProtocol() (string, error) {
	result, err := execute.Run("", "gh", "config", "get", "git_protocol")
	if err != nil {
		return "", err
	}
	protocol := strings.TrimSpace(result.Stdout)
	if protocol == "" {
		protocol = "https"
	}
	return protocol, nil
}

// CreateRepository creates the repository through gh and asks gh for the
// URL that matches the user's protocol, so the first push uses the git
// authentication the user already has.
func (host Host) CreateRepository(repository ports.NewRepository) (remote string, err error) {
	visibility := "--public"
	if repository.Private {
		visibility = "--private"
	}
	fullName := repository.Owner + "/" + repository.Name
	_, err = execute.Run("", "gh", "repo", "create", fullName, visibility, "--description", repository.Description)
	if err != nil {
		return "", err
	}
	protocol, err := host.GitProtocol()
	if err != nil {
		return "", err
	}
	field := "url"
	if protocol == "ssh" {
		field = "sshUrl"
	}
	result, err := execute.Run("", "gh", "repo", "view", fullName, "--json", field, "--jq", "."+field)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

// DeleteRepository deletes the repository; gh needs the delete_repo scope
// for it, which gh auth refresh -s delete_repo grants.
func (Host) DeleteRepository(owner string, name string) error {
	_, err := execute.Run("", "gh", "repo", "delete", owner+"/"+name, "--yes")
	return err
}

// ProtectMainBranch puts the protection on main through the REST API. A 403
// is GitHub refusing the feature for this plan, private repositories on a
// free personal account, and comes back as the reason, not as an error.
func (Host) ProtectMainBranch(owner string, name string) (refusedReason string, err error) {
	_, err = execute.Run("", "gh", "api", "-X", "PUT", "repos/"+owner+"/"+name+"/branches/main/protection",
		"-F", "required_status_checks=null",
		"-F", "enforce_admins=false",
		"-F", "required_pull_request_reviews[require_code_owner_reviews]=true",
		"-F", "required_pull_request_reviews[required_approving_review_count]=1",
		"-F", "restrictions=null")
	var failure *execute.Failure
	if errors.As(err, &failure) && strings.Contains(failure.Stderr, "403") {
		return strings.TrimSpace(failure.Stderr), nil
	}
	return "", err
}

// SlugOf derives the address segment from a login: lowercase, because macOS
// and Windows file systems are case-insensitive and GitHub logins are unique
// regardless of case, so no two accounts can share a slug.
func SlugOf(login string) string {
	return strings.ToLower(login)
}
