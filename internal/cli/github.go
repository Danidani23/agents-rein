package cli

import (
	"fmt"
	"strings"

	"github.com/Danidani23/agents-rein/internal/execute"
)

// githubLogin returns the account gh is logged in as. GitHub is the host
// (D8): identity is the GitHub login, read from gh, never typed. gh is a
// prerequisite, and an account that is not logged in is a refusal that
// names gh's own login command.
func githubLogin() (string, error) {
	err := execute.Preflight("gh")
	if err != nil {
		return "", fmt.Errorf("the GitHub CLI is required: install gh (https://cli.github.com), run gh auth login, then try again")
	}
	result, err := execute.Run("", "gh", "api", "user", "--jq", ".login")
	if err != nil {
		return "", fmt.Errorf("gh is not logged in to GitHub; run gh auth login, then try again")
	}
	login := strings.TrimSpace(result.Stdout)
	if login == "" {
		return "", fmt.Errorf("gh returned no account; run gh auth login, then try again")
	}
	return login, nil
}

// slugOf derives the address segment from a GitHub login: lowercase, because
// macOS and Windows file systems are case-insensitive and GitHub logins are
// unique regardless of case, so no two accounts can share a slug.
func slugOf(login string) string {
	return strings.ToLower(login)
}
