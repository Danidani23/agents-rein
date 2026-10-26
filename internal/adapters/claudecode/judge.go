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

// envelope is the result claude -p ends its stream with, on success and on
// failure alike.
type envelope struct {
	Type    string `json:"type"`
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
	Subtype string `json:"subtype"`
}

// streamEvent is a line of claude's stream that carries a piece of the
// answer: a text delta, or a thinking one
// ([docs](https://code.claude.com/docs/en/headless#stream-responses)).
type streamEvent struct {
	Type  string `json:"type"`
	Event struct {
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`
}

// judgeStream follows claude's stream as it comes and tells progress what
// the answer holds so far.
type judgeStream struct {
	progress func(ports.JudgeProgress)
	written  strings.Builder
}

// line reads one line of the stream; a line that is no event is skipped.
func (stream *judgeStream) line(text string) {
	var event streamEvent
	if stream.progress == nil || json.Unmarshal([]byte(text), &event) != nil || event.Type != "stream_event" {
		return
	}
	switch event.Event.Delta.Type {
	case "thinking_delta":
		if stream.written.Len() == 0 {
			stream.progress(ports.JudgeProgress{Thinking: true})
		}
	case "text_delta":
		stream.written.WriteString(event.Event.Delta.Text)
		stream.progress(ports.JudgeProgress{Written: stream.written.String()})
	}
}

// resultOf finds the result that ends claude's stream.
func resultOf(stdout string) (envelope, bool) {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		var final envelope
		if json.Unmarshal([]byte(lines[index]), &final) == nil && final.Type == "result" {
			return final, true
		}
	}
	return envelope{}, false
}

// judgeTimeout is how long the distiller may think: a large harness with
// many units takes several minutes on a large model.
const judgeTimeout = 15 * time.Minute

// Ask runs claude -p with JSON output and returns the result text and the
// session's id, which claude --resume opens again. It runs in an empty
// folder without tools, so no project's instructions, hooks or tools
// colour the verdict; model names the model, "" for the developer's
// default. Never --bare: it skips the login with the hooks.
func (Judge) Ask(brief string, model string, progress func(ports.JudgeProgress)) (string, string, error) {
	scratch, err := os.MkdirTemp("", "rein-judge-*")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(scratch)
	session := newSessionID()
	stream := &judgeStream{progress: progress}
	result, err := execute.RunWith(execute.Options{Directory: scratch, Input: brief, Timeout: judgeTimeout, Lines: stream.line}, "claude", judgeArguments(model, session)...)
	err = explainFailure(err)
	output, parsed := resultOf(result.Stdout)
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
		return "", session, fmt.Errorf("claude's stream ended without its result: %s", lastLines(result.Stdout, 3))
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
// the answer streamed as it is written (P2.54), the session's id, and the
// model when one is named.
func judgeArguments(model string, session string) []string {
	arguments := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--max-turns", "1", "--tools", "", "--session-id", session}
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
