package claudecode

import (
	"errors"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
)

func TestJudgeArgumentsAndTimeout(t *testing.T) {
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
