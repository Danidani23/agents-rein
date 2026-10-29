package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Interrupt is what Cancel does, the way Ctrl-C does it in a terminal: Stop
// stops the program an action waits for, so the workflow's step fails and
// the steps done are undone; Running reports whether a program runs now.
// The CLI passes the execute adapter's.
type Interrupt struct {
	Stop    func()
	Running func() bool
}

// runner runs the page's actions, one at a time, as the machine lock
// allows one writer, and keeps the latest so its page can be shown again.
type runner struct {
	mutex     sync.Mutex
	current   *run
	interrupt Interrupt
}

// run is one action: its name, its progress as the workflow reports it,
// and its outcome once done is closed.
type run struct {
	Name    string
	done    chan struct{}
	mutex   sync.Mutex
	events  []orchestration.ProgressEvent
	changed chan struct{}
	// finished, cancelAsked, cancelMessage, waitingStep and leftLine are
	// guarded by mutex; Outcome is written before done is closed.
	finished      bool
	cancelAsked   bool
	cancelMessage string
	// waitingStep is the step that waits for the machine lock, "" when the
	// action does not wait; leftLine is set when Cancel ended the wait.
	waitingStep string
	leftLine    bool
	Outcome     outcome
	// ctx ends when Cancel is pressed while the action waits for its turn.
	ctx  context.Context
	stop context.CancelFunc
}

// outcome is what an action ends with: a headline, the lines the CLI would
// print, and the next step.
type outcome struct {
	Headline string
	Failed   bool
	Lines    []outcomeLine
	Next     string
}

// outcomeLine is one line of an outcome; Warning marks what needs the
// developer's eye.
type outcomeLine struct {
	Text    string
	Warning bool
}

// work is what an action does, reporting its steps through the run's
// record and waiting for its turn at the machine lock through lockFor.
type work func(current *run) (outcome, error)

// start runs work as the action name unless another action is running.
func (actions *runner) start(name string, action work) (*run, error) {
	actions.mutex.Lock()
	defer actions.mutex.Unlock()
	if actions.current != nil && !actions.current.isFinished() {
		return nil, refusal.New(actions.current.Name + " is running; one action runs at a time")
	}
	started := &run{Name: name, done: make(chan struct{}), changed: make(chan struct{})}
	started.ctx, started.stop = context.WithCancel(context.Background())
	actions.current = started
	go func() {
		defer started.stop()
		result, err := action(started)
		started.finish(result, err)
	}()
	return started, nil
}

// latest is the action running now or the one that ran last, nil before
// the first.
func (actions *runner) latest() *run {
	actions.mutex.Lock()
	defer actions.mutex.Unlock()
	return actions.current
}

// cancel stops the program the running action waits for and says what it
// did. Between two programs there is nothing to stop: the steps there are
// quick, and the action finishes on its own.
func (actions *runner) cancel() string {
	current := actions.latest()
	message := ""
	switch {
	case current == nil || current.isFinished():
		return "nothing is running"
	case current.isWaiting():
		message = "leaving the line; nothing was done"
		current.mutex.Lock()
		current.leftLine = true
		current.mutex.Unlock()
		current.stop()
	case actions.interrupt.Running != nil && actions.interrupt.Running():
		message = "stopping the running program; the steps already done are undone"
		current.mutex.Lock()
		current.cancelAsked = true
		current.mutex.Unlock()
		actions.interrupt.Stop()
	default:
		message = "nothing to stop right now: no program is running, and the steps between programs are quick, so the action finishes on its own"
	}
	current.mutex.Lock()
	current.cancelMessage = message
	current.mutex.Unlock()
	return message
}

// runningName is the name of the action running now, "" when none runs.
func (actions *runner) runningName() string {
	current := actions.latest()
	if current == nil || current.isFinished() {
		return ""
	}
	return current.Name
}

func (current *run) record(event orchestration.ProgressEvent) {
	current.mutex.Lock()
	defer current.mutex.Unlock()
	current.events = append(current.events, event)
	close(current.changed)
	current.changed = make(chan struct{})
}

// finish keeps the outcome, headed by what stopped it when err is set;
// the next step of a success does not follow a failure, whose message
// names the way on.
func (current *run) finish(result outcome, err error) {
	current.mutex.Lock()
	if err != nil {
		result.Next = ""
	}
	switch {
	case err == nil:
	case current.leftLine:
		result.Headline, result.Failed = "cancelled while waiting for its turn; nothing was done", true
	case current.cancelAsked:
		result.Headline, result.Failed = "cancelled; the steps already done were undone: "+err.Error(), true
	case errors.Is(err, refusal.ErrRefused):
		result.Headline, result.Failed = "refused: "+err.Error(), true
	default:
		result.Headline, result.Failed = "failed: "+err.Error(), true
	}
	current.Outcome = result
	current.finished = true
	close(current.changed)
	current.mutex.Unlock()
	close(current.done)
}

// waitFor shows what the action waits for as a step of its own, the step
// before it done once it changes.
func (current *run) waitFor(wait ports.LockWait) {
	step := "waiting for " + wait.Holder.String()
	if wait.Joining {
		step = "waiting for the same action, which runs: " + wait.Holder.String()
	}
	current.mutex.Lock()
	before := current.waitingStep
	current.waitingStep = step
	current.mutex.Unlock()
	if before != "" {
		current.record(orchestration.ProgressEvent{Kind: orchestration.ProgressDone, Step: before})
	}
	current.record(orchestration.ProgressEvent{Kind: orchestration.ProgressStarted, Step: step})
}

// waited ends the waiting step, if the action waited.
func (current *run) waited() {
	current.mutex.Lock()
	step := current.waitingStep
	current.waitingStep = ""
	current.mutex.Unlock()
	if step != "" {
		current.record(orchestration.ProgressEvent{Kind: orchestration.ProgressDone, Step: step})
	}
}

func (current *run) isWaiting() bool {
	current.mutex.Lock()
	defer current.mutex.Unlock()
	return current.waitingStep != ""
}

func (current *run) isFinished() bool {
	current.mutex.Lock()
	defer current.mutex.Unlock()
	return current.finished
}

// since is the events from index from on, whether the run has finished,
// and what is closed at the next change.
func (current *run) since(from int) (events []orchestration.ProgressEvent, finished bool, changed chan struct{}) {
	current.mutex.Lock()
	defer current.mutex.Unlock()
	if from < len(current.events) {
		events = append(events, current.events[from:]...)
	}
	return events, current.finished, current.changed
}

// stepLine is one step as the page shows it, its outcome replacing its
// start as the CLI's line does; Kind is its class: running, done, failed
// or undone.
type stepLine struct {
	Key     string
	Kind    string
	Text    string
	Note    string
	Details []string
}

// streamedEvent is one progress event as the page's script receives it.
type streamedEvent struct {
	Key    string `json:"key"`
	Kind   string `json:"kind"`
	Text   string `json:"text"`
	Detail string `json:"detail,omitempty"`
	Note   string `json:"note,omitempty"`
}

// streamedEventOf words an event as the CLI prints it and keys it by its
// step, an undoing by the step it undoes.
func streamedEventOf(event orchestration.ProgressEvent) streamedEvent {
	elapsed := event.Elapsed.Round(100 * time.Millisecond).String()
	switch event.Kind {
	case orchestration.ProgressDone:
		return streamedEvent{Key: event.Step, Kind: "done", Text: "✓ " + event.Step + " (" + elapsed + ")"}
	case orchestration.ProgressFailed:
		return streamedEvent{Key: event.Step, Kind: "failed", Text: "✗ " + event.Step + ": " + event.Error}
	case orchestration.ProgressUndoing:
		return streamedEvent{Key: "undo " + event.Step, Kind: "running", Text: "↩ undoing " + event.Step + " …"}
	case orchestration.ProgressUndone:
		return streamedEvent{Key: "undo " + event.Step, Kind: "undone", Text: "↩ undone " + event.Step + " (" + elapsed + ")"}
	case orchestration.ProgressDetail:
		return streamedEvent{Key: event.Step, Kind: "detail", Detail: event.Note}
	case orchestration.ProgressNote:
		return streamedEvent{Key: event.Step, Kind: "note", Note: event.Note}
	}
	return streamedEvent{Key: event.Step, Kind: "running", Text: event.Step + " …"}
}

// stepsOf folds the events into one line per step, as the page shows them
// before its script takes over.
func stepsOf(events []orchestration.ProgressEvent) []stepLine {
	var lines []stepLine
	index := map[string]int{}
	for _, event := range events {
		streamed := streamedEventOf(event)
		position, seen := index[streamed.Key]
		if !seen {
			lines = append(lines, stepLine{Key: streamed.Key})
			position = len(lines) - 1
			index[streamed.Key] = position
		}
		switch streamed.Kind {
		case "detail":
			lines[position].Details = append(lines[position].Details, streamed.Detail)
		case "note":
			lines[position].Note = streamed.Note
		default:
			lines[position].Kind, lines[position].Text = streamed.Kind, streamed.Text
		}
	}
	return lines
}

// runView is the page of the latest action: its steps so far, Cancel while
// it runs, its outcome once it has finished. From is how many events the
// page holds, where its script picks up.
type runView struct {
	frame
	Started       bool
	Finished      bool
	Steps         []stepLine
	From          int
	CancelMessage string
	Outcome       outcome
}

// runPage shows the latest action.
func runPage(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		current := actions.latest()
		title := "Action"
		if current != nil {
			title = current.Name
		}
		view := runView{frame: frameOf(request, report, version, title, "run", actions)}
		if current != nil {
			events, finished, _ := current.since(0)
			current.mutex.Lock()
			view.CancelMessage = current.cancelMessage
			current.mutex.Unlock()
			view.Started, view.Finished, view.Steps, view.From = true, finished, stepsOf(events), len(events)
			if finished {
				view.Outcome = current.Outcome
			}
		}
		render(writer, page, view, http.StatusOK)
	})
}

// runEvents streams the latest action's events from the index the page
// names on, as server-sent events, and an end event once it has finished.
func runEvents(actions *runner) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		current := actions.latest()
		if current == nil {
			http.Error(writer, "no action has run since rein ui started", http.StatusNotFound)
			return
		}
		from, err := strconv.Atoi(request.URL.Query().Get("from"))
		if err != nil || from < 0 {
			from = 0
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.Header().Set("Cache-Control", "no-cache")
		controller := http.NewResponseController(writer)
		for {
			events, finished, changed := current.since(from)
			for _, event := range events {
				encoded, err := json.Marshal(streamedEventOf(event))
				if err != nil {
					return
				}
				fmt.Fprintf(writer, "event: step\ndata: %s\n\n", encoded)
				from++
			}
			if finished {
				fmt.Fprint(writer, "event: end\ndata: {}\n\n")
			}
			err = controller.Flush()
			if err != nil || finished {
				return
			}
			select {
			case <-changed:
			case <-request.Context().Done():
				return
			}
		}
	})
}

// cancelRun stops the running action's program, as Ctrl-C does, and shows
// the action again with what Cancel did.
func cancelRun(actions *runner) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		actions.cancel()
		http.Redirect(writer, request, "/run", http.StatusSeeOther)
	})
}
