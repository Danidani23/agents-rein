package claudecode

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Judge is the agent-cli backend of the judge port (D23): Claude Code run
// headless with the brief on its standard input, one turn, no tools, on the
// developer's own subscription. It is the fallback when an organisation
// declares no provider.
type Judge struct{}

var _ ports.Judge = Judge{}

// Name is the backend's name for the report.
func (Judge) Name() string {
	return "agent-cli:claude"
}

// Available needs the claude command on the PATH.
func (Judge) Available() (string, bool) {
	_, err := exec.LookPath("claude")
	if err != nil {
		return "claude is not on the PATH; install Claude Code, or declare a provider in the organisation", false
	}
	return "", true
}

// Ask runs claude -p with JSON output and returns the result text.
func (Judge) Ask(brief string) (string, error) {
	result, err := execute.RunWithInput("", brief, "claude", "-p", "--output-format", "json", "--max-turns", "1")
	if err != nil {
		return "", err
	}
	var output struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	err = json.Unmarshal([]byte(result.Stdout), &output)
	if err != nil {
		return "", fmt.Errorf("claude answered with something other than its JSON envelope: %v", err)
	}
	if output.IsError {
		return "", fmt.Errorf("claude reported an error: %s", strings.TrimSpace(output.Result))
	}
	return output.Result, nil
}
