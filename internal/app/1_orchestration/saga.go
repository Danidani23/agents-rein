package orchestration

import (
	"fmt"
	"strings"
)

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
func runSteps(progress func(string), steps []Step) error {
	report := func(message string) {
		if progress != nil {
			progress(message)
		}
	}
	for index, step := range steps {
		report(step.Name)
		err := step.Do()
		if err == nil {
			continue
		}
		report("failed: " + err.Error())
		var failures []string
		for undo := index - 1; undo >= 0; undo-- {
			if steps[undo].Undo == nil {
				continue
			}
			report("undoing: " + steps[undo].Name)
			undoErr := steps[undo].Undo()
			if undoErr != nil {
				failures = append(failures, steps[undo].Name+": "+undoErr.Error())
			}
		}
		if len(failures) > 0 {
			return &CompensationError{Cause: err, Failures: failures}
		}
		return err
	}
	return nil
}
