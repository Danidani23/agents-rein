package web

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/ports"
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
	_, err := actions.start("Sync shop", func(*run) (outcome, error) {
		<-release
		return outcome{}, nil
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err = actions.start("Reset shop", func(*run) (outcome, error) { return outcome{}, nil })
	if err == nil || !strings.Contains(err.Error(), "Sync shop is running") {
		t.Fatalf("second while the first runs: %v", err)
	}
	close(release)
	<-actions.latest().done
	_, err = actions.start("Reset shop", func(*run) (outcome, error) { return outcome{}, nil })
	if err != nil {
		t.Fatalf("after the first ended: %v", err)
	}
}

func TestCancelStopsTheRunningProgram(t *testing.T) {
	t.Run("a program running is stopped and the run says it was cancelled", func(t *testing.T) {
		stopped := 0
		actions := &runner{interrupt: Interrupt{Stop: func() { stopped++ }, Running: func() bool { return true }}}
		release := make(chan struct{})
		current, _ := actions.start("Sync shop", func(*run) (outcome, error) {
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
		current, _ := actions.start("Sync shop", func(*run) (outcome, error) {
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
	t.Run("an action waiting for its turn leaves the line, nothing done", func(t *testing.T) {
		stopped := 0
		actions := &runner{interrupt: Interrupt{Stop: func() { stopped++ }, Running: func() bool { return false }}}
		ahead := ports.LockHolder{Command: "rein sync --all", Caller: "a terminal", Folder: "/work", Since: time.Date(2026, 10, 29, 14, 2, 7, 0, time.Local)}
		lock := &fake.Lock{Ahead: &ahead, Free: make(chan struct{})}
		current, _ := actions.start("Sync shop", func(current *run) (outcome, error) {
			release, err := lockFor(current, orchestration.Orchestrator{Services: orchestration.Services{Lock: lock}}, "rein sync", "/work/shop")
			if err != nil {
				return outcome{}, err
			}
			release(nil)
			return outcome{Headline: "done"}, nil
		})
		for !current.isWaiting() {
			time.Sleep(time.Millisecond)
		}
		events, _, _ := current.since(0)
		if len(events) != 1 || events[0].Step != "waiting for rein sync --all in /work, run from a terminal since 14:02:07" {
			t.Fatalf("the wait is not a step: %+v", events)
		}
		message := actions.cancel()
		select {
		case <-current.done:
		case <-time.After(5 * time.Second):
			close(lock.Free)
			<-current.done
			t.Fatalf("Cancel did not take the action out of the line: %q", message)
		}
		if stopped != 0 || !strings.Contains(message, "leaving the line") || current.Outcome.Headline != "cancelled while waiting for its turn; nothing was done" || lock.Acquired != 0 {
			t.Fatalf("stopped %d message %q outcome %+v acquired %d", stopped, message, current.Outcome, lock.Acquired)
		}
		if lock.Holders[0] != (ports.LockHolder{Command: "rein sync", Caller: "the page", Folder: "/work/shop"}) {
			t.Fatalf("holder %+v", lock.Holders[0])
		}
	})
	t.Run("an action identical to one in line ends as that one did", func(t *testing.T) {
		actions := &runner{}
		ran := ports.LockHolder{Command: "rein sync", Caller: "a terminal", Folder: "/work/shop", Since: time.Date(2026, 10, 29, 14, 2, 7, 0, time.Local)}
		lock := &fake.Lock{Joined: &ports.Joined{Holder: ran}}
		current, _ := actions.start("Sync shop", func(current *run) (outcome, error) {
			_, err := lockFor(current, orchestration.Orchestrator{Services: orchestration.Services{Lock: lock}}, "rein sync", "/work/shop")
			if err != nil {
				return joinedOutcome(err)
			}
			return outcome{Headline: "ran twice"}, nil
		})
		<-current.done
		if current.Outcome.Failed || !strings.HasPrefix(current.Outcome.Headline, "done: the same command ran: rein sync in /work/shop, run from a terminal since 14:02:07") {
			t.Fatalf("outcome %+v", current.Outcome)
		}
	})
}
