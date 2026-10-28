package web

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
)

func TestTheRunsStepsStreamToThePage(t *testing.T) {
	page, orchestrator := serveProjects(t, "Danidani23")
	client, root := signIn(t, page)
	runAction(t, page, client, root, "actions/sync", url.Values{"folder": {cloneOf(t, orchestrator)}, "force": {"on"}})
	response, body := get(t, client, root+"run/events?from=0")
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("type %q", response.Header.Get("Content-Type"))
	}
	containsAll(t, body, "event: step\ndata: {", `"kind":"done"`, "event: end\n")
}

func TestOneActionRunsAtATime(t *testing.T) {
	actions := &runner{}
	release := make(chan struct{})
	_, err := actions.start("Sync shop", func(func(orchestration.ProgressEvent)) (outcome, error) {
		<-release
		return outcome{}, nil
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err = actions.start("Reset shop", func(func(orchestration.ProgressEvent)) (outcome, error) { return outcome{}, nil })
	if err == nil || !strings.Contains(err.Error(), "Sync shop is running") {
		t.Fatalf("second while the first runs: %v", err)
	}
	close(release)
	<-actions.latest().done
	_, err = actions.start("Reset shop", func(func(orchestration.ProgressEvent)) (outcome, error) { return outcome{}, nil })
	if err != nil {
		t.Fatalf("after the first ended: %v", err)
	}
}

func TestCancelStopsTheRunningProgram(t *testing.T) {
	t.Run("a program running is stopped and the run says it was cancelled", func(t *testing.T) {
		stopped := 0
		actions := &runner{interrupt: Interrupt{Stop: func() { stopped++ }, Running: func() bool { return true }}}
		release := make(chan struct{})
		current, _ := actions.start("Sync shop", func(func(orchestration.ProgressEvent)) (outcome, error) {
			<-release
			return outcome{}, errors.New("git pull: signal: killed")
		})
		message := actions.cancel()
		close(release)
		<-current.done
		if stopped != 1 || !strings.Contains(message, "undo") || !strings.Contains(current.Outcome.Headline, "cancelled") {
			t.Fatalf("stopped %d message %q outcome %+v", stopped, message, current.Outcome)
		}
	})
	t.Run("between programs there is nothing to stop", func(t *testing.T) {
		stopped := 0
		actions := &runner{interrupt: Interrupt{Stop: func() { stopped++ }, Running: func() bool { return false }}}
		release := make(chan struct{})
		current, _ := actions.start("Sync shop", func(func(orchestration.ProgressEvent)) (outcome, error) {
			<-release
			return outcome{}, nil
		})
		message := actions.cancel()
		close(release)
		<-current.done
		if stopped != 0 || !strings.Contains(message, "nothing to stop") {
			t.Fatalf("stopped %d message %q", stopped, message)
		}
	})
}
