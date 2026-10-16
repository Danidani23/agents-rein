package cli

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/Danidani23/agents-rein/internal/execute"
	"github.com/Danidani23/agents-rein/internal/local"
)

// identity is who rein acts for: the active GitHub account of gh (D8), read
// every time and never stored (D9). gh keeps several accounts and marks one
// active; that one is the identity.
type identity struct {
	Login string
	Slug  string
}

// githubIdentity reads the active account. gh is a prerequisite, and an
// account that is not logged in is a refusal that names gh's own command.
func githubIdentity() (identity, error) {
	err := execute.Preflight("gh")
	if err != nil {
		return identity{}, fmt.Errorf("the GitHub CLI is required: install gh (https://cli.github.com), run gh auth login, then try again")
	}
	result, err := execute.Run("", "gh", "api", "user", "--jq", ".login")
	if err != nil {
		return identity{}, fmt.Errorf("gh is not logged in to GitHub; run gh auth login, then try again")
	}
	login := strings.TrimSpace(result.Stdout)
	if login == "" {
		return identity{}, fmt.Errorf("gh returned no account; run gh auth login, then try again")
	}
	return identity{Login: login, Slug: slugOf(login)}, nil
}

// slugOf derives the address segment from a GitHub login: lowercase, because
// macOS and Windows file systems are case-insensitive and GitHub logins are
// unique regardless of case, so no two accounts can share a slug.
func slugOf(login string) string {
	return strings.ToLower(login)
}

// installedAgents detects the supported agents present on this machine, by
// their command on the PATH. Which agents a project allows is a declaration
// of its levels (P2.4); which are installed is this.
func installedAgents() []string {
	var found []string
	_, err := exec.LookPath("claude")
	if err == nil {
		found = append(found, local.AgentClaudeCode)
	}
	return found
}
