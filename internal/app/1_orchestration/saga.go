package orchestration

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/organizations"
)

// Step is one activity of a workflow with its compensation. Do performs it;
// Undo reverses it when a later step fails. A step without an Undo cannot be
// reversed, which is allowed for the last step and for reads.
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

// run performs the steps in order. When one fails, the Undo of every
// completed step runs in reverse order, and the step's error is returned,
// wrapped when a compensation failed as well.
func run(steps []Step) error {
	for index, step := range steps {
		err := step.Do()
		if err == nil {
			continue
		}
		var failures []string
		for undo := index - 1; undo >= 0; undo-- {
			if steps[undo].Undo == nil {
				continue
			}
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

// refused turns a service's own refusal, a precondition that failed, into a
// Refusal so the gateway maps it to exit 1; any other error passes through
// as the crash it is.
func refused(err error) error {
	if err == nil || errors.Is(err, ErrRefused) {
		return err
	}
	var notConnected organizations.ErrNotConnected
	var alreadyConnected organizations.ErrAlreadyConnected
	if errors.As(err, &notConnected) || errors.As(err, &alreadyConnected) {
		return Refusal{Reason: err.Error()}
	}
	return err
}
