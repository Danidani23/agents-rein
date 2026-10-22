package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
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

// envelope is the JSON claude -p prints around its answer, on success and
// on failure alike.
type envelope struct {
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
	Subtype string `json:"subtype"`
}

// Ask runs claude -p with JSON output and returns the result text. It runs
// in an empty folder without tools, so no project's instructions, hooks or
// tools colour the verdict, and keeps no session; a brief of many
// candidates takes a minute or more, so the limit is the network one. Never
// --bare: it skips the login with the hooks.
func (Judge) Ask(brief string) (string, error) {
	scratch, err := os.MkdirTemp("", "rein-judge-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	result, err := execute.RunWith(execute.Options{Directory: scratch, Input: brief, Timeout: execute.NetworkTimeout}, "claude", "-p", "--output-format", "json", "--max-turns", "1", "--tools", "", "--no-session-persistence")
	var output envelope
	parsed := json.Unmarshal([]byte(result.Stdout), &output) == nil
	if err != nil {
		if parsed && strings.TrimSpace(output.Result) != "" {
			return "", fmt.Errorf("claude reported: %s (%s)", strings.TrimSpace(output.Result), output.Subtype)
		}
		if tail := lastLines(result.Stdout, 3); tail != "" {
			return "", fmt.Errorf("%w; it printed: %s", err, tail)
		}
		return "", err
	}
	if !parsed {
		return "", fmt.Errorf("claude answered with something other than its JSON envelope: %s", lastLines(result.Stdout, 3))
	}
	if output.IsError {
		return "", fmt.Errorf("claude reported an error: %s", strings.TrimSpace(output.Result))
	}
	return output.Result, nil
}

// lastLines is the last few non-empty lines of a text, joined by " | ".
func lastLines(text string, count int) string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return strings.Join(lines, " | ")
}
