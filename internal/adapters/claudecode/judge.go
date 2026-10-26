package claudecode

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

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

// judgeTimeout is how long the distiller may think: a large harness with
// many units takes several minutes on a large model.
const judgeTimeout = 15 * time.Minute

// Ask runs claude -p with JSON output and returns the result text and the
// session's id, which claude --resume opens again. It runs in an empty
// folder without tools, so no project's instructions, hooks or tools
// colour the verdict; model names the model, "" for the developer's
// default. Never --bare: it skips the login with the hooks.
func (Judge) Ask(brief string, model string) (string, string, error) {
	scratch, err := os.MkdirTemp("", "rein-judge-*")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(scratch)
	session := newSessionID()
	result, err := execute.RunWith(execute.Options{Directory: scratch, Input: brief, Timeout: judgeTimeout}, "claude", judgeArguments(model, session)...)
	err = explainFailure(err)
	var output envelope
	parsed := json.Unmarshal([]byte(result.Stdout), &output) == nil
	if err != nil {
		if parsed && strings.TrimSpace(output.Result) != "" {
			return "", session, fmt.Errorf("claude reported: %s (%s)", strings.TrimSpace(output.Result), output.Subtype)
		}
		if tail := lastLines(result.Stdout, 3); tail != "" {
			return "", session, fmt.Errorf("%w; it printed: %s", err, tail)
		}
		return "", session, err
	}
	if !parsed {
		return "", session, fmt.Errorf("claude answered with something other than its JSON envelope: %s", lastLines(result.Stdout, 3))
	}
	if output.IsError {
		return "", session, fmt.Errorf("claude reported an error: %s", strings.TrimSpace(output.Result))
	}
	return output.Result, session, nil
}

// ResumeCommand opens the conversation from any folder: claude keeps a
// claude -p run out of its session picker but finds it by its id.
func (Judge) ResumeCommand(session string) string {
	return "claude --resume " + session
}

// judgeArguments are claude's arguments for one brief: one turn, no tools,
// JSON out, the session's id, and the model when one is named.
func judgeArguments(model string, session string) []string {
	arguments := []string{"-p", "--output-format", "json", "--max-turns", "1", "--tools", "", "--session-id", session}
	if model != "" {
		arguments = append(arguments, "--model", model)
	}
	return arguments
}

// explainFailure says a timeout as it is: claude had the brief and did not
// answer in time, which a faster model or another try mends.
func explainFailure(err error) error {
	if errors.Is(err, execute.ErrTimedOut) {
		return fmt.Errorf("claude did not answer within %s; distill again, or name a faster model with --model", judgeTimeout)
	}
	return err
}

// newSessionID is a fresh UUID, version 4, for claude --session-id.
func newSessionID() string {
	var bytes [16]byte
	_, err := rand.Read(bytes[:])
	if err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	bytes[6] = bytes[6]&0x0f | 0x40
	bytes[8] = bytes[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
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
