package orchestration

import (
	"fmt"
	"strings"
	"time"
)

// ProgressKind says what a progress event reports.
type ProgressKind string

// The kinds of progress event.
const (
	ProgressStarted ProgressKind = "started"
	ProgressDone    ProgressKind = "done"
	ProgressFailed  ProgressKind = "failed"
	ProgressUndoing ProgressKind = "undoing"
	ProgressUndone  ProgressKind = "undone"
	// ProgressDetail says once what a running step does, and ProgressNote
	// how far it has come; a step reports both itself (P2.54).
	ProgressDetail ProgressKind = "detail"
	ProgressNote   ProgressKind = "note"
)

// ProgressEvent is one thing the saga runner reports: a step starting,
// finishing with its duration, failing with its error, or being undone;
// or a running step's detail or note.
type ProgressEvent struct {
	Kind    ProgressKind
	Step    string
	Elapsed time.Duration
	Error   string
	Note    string
}

// Step is one activity of a workflow with its compensation. Do performs it;
// Undo reverses it when a later step fails. A step without an Undo cannot be
// reversed, which is allowed for a read and for the last step.
type Step struct {
	Name string
	Do   func() error
	Undo func() error
}

// CompensationError says a workflow failed and some of its compensations
// failed too, so the machine may be left between two states.
type CompensationError struct {
	Cause    error
	Failures []string
}

func (err *CompensationError) Error() string {
	return fmt.Sprintf("%v; compensation failed: %s", err.Cause, strings.Join(err.Failures, "; "))
}

func (err *CompensationError) Unwrap() error {
	return err.Cause
}

// runSteps performs the steps in order, reporting each as it starts
// through progress, which may be nil. When one fails, the Undo of every
// completed step runs in reverse order, each reported, and the step's error
// is returned, wrapped when a compensation failed as well. Nothing a
// workflow does is silent (P2.25).
func runSteps(progress func(ProgressEvent), steps []Step) error {
	report := func(event ProgressEvent) {
		if progress != nil {
			progress(event)
		}
	}
	for index, step := range steps {
		started := time.Now()
		report(ProgressEvent{Kind: ProgressStarted, Step: step.Name})
		err := step.Do()
		if err == nil {
			report(ProgressEvent{Kind: ProgressDone, Step: step.Name, Elapsed: time.Since(started)})
			continue
		}
		report(ProgressEvent{Kind: ProgressFailed, Step: step.Name, Elapsed: time.Since(started), Error: err.Error()})
		var failures []string
		for undo := index - 1; undo >= 0; undo-- {
			if steps[undo].Undo == nil {
				continue
			}
			undoStarted := time.Now()
			report(ProgressEvent{Kind: ProgressUndoing, Step: steps[undo].Name})
			undoErr := steps[undo].Undo()
			if undoErr != nil {
				failures = append(failures, steps[undo].Name+": "+undoErr.Error())
				report(ProgressEvent{Kind: ProgressFailed, Step: "undoing " + steps[undo].Name, Elapsed: time.Since(undoStarted), Error: undoErr.Error()})
				continue
			}
			report(ProgressEvent{Kind: ProgressUndone, Step: steps[undo].Name, Elapsed: time.Since(undoStarted)})
		}
		if len(failures) > 0 {
			return &CompensationError{Cause: err, Failures: failures}
		}
		return err
	}
	return nil
}
