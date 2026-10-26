package claudecode

import (
	"errors"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/ports"
)

func TestJudgeStream(t *testing.T) {
	var seen []ports.JudgeProgress
	stream := &judgeStream{progress: func(update ports.JudgeProgress) { seen = append(seen, update) }}
	for _, line := range []string{
		`{"type":"system","subtype":"init","session_id":"s"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"hmm"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"{\"version\":2,"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"\"candidates\":[]}"}}}`,
		`not json`,
		`{"type":"result","subtype":"success","is_error":false,"result":"{\"version\":2,\"candidates\":[]}","session_id":"s"}`,
	} {
		stream.line(line)
	}
	if len(seen) != 3 || !seen[0].Thinking || seen[1].Written != `{"version":2,` || seen[2].Written != `{"version":2,"candidates":[]}` {
		t.Fatalf("progress %+v", seen)
	}
	final, found := resultOf(strings.Join([]string{`{"type":"system"}`, `{"type":"result","is_error":true,"result":"no login","subtype":"error"}`}, "\n"))
	if !found || !final.IsError || final.Result != "no login" {
		t.Fatalf("result %+v found %v", final, found)
	}
	if _, found := resultOf("nothing like it"); found {
		t.Fatalf("a result in no stream")
	}
}

func TestJudgeArgumentsAndTimeout(t *testing.T) {
	// the answer streams, so the distiller's progress can be shown (P2.54)
	if !strings.Contains(strings.Join(judgeArguments("", "session"), " "), "--output-format stream-json --verbose --include-partial-messages") {
		t.Fatalf("arguments %v", judgeArguments("", "session"))
	}
	if strings.Contains(strings.Join(judgeArguments("", "session"), " "), "--model") {
		t.Fatalf("a model with none asked for: %v", judgeArguments("", "session"))
	}
	if !strings.HasSuffix(strings.Join(judgeArguments("sonnet", "session"), " "), "--session-id session --model sonnet") {
		t.Fatalf("arguments %v", judgeArguments("sonnet", "session"))
	}
	// running out of time is said as it is, with what to do about it
	timedOut := explainFailure(&execute.Failure{Command: "claude -p", Cause: execute.ErrTimedOut})
	if !strings.Contains(timedOut.Error(), "claude did not answer within 15m0s") || !strings.Contains(timedOut.Error(), "--model") || strings.Contains(timedOut.Error(), "waiting for input") {
		t.Fatalf("timeout %v", timedOut)
	}
	other := errors.New("claude failed: no login")
	if explainFailure(other) != other {
		t.Fatalf("another failure changed: %v", explainFailure(other))
	}
}
