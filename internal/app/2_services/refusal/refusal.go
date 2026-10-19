// Package refusal is the one error every layer agrees on: a precondition
// failed and nothing changed. A service raises it for its own preconditions,
// a workflow for a step that must not go on, the gateway for a missing
// terminal or a declined confirmation. The gateway maps it to exit code 1;
// any other error is a crash and exits 2.
package refusal

import "errors"

// ErrRefused is what errors.Is matches for every refusal.
var ErrRefused = errors.New("refused")

// Refusal is a refusal with its reason, printed as is.
type Refusal struct {
	Reason string
}

func (refusal Refusal) Error() string {
	return refusal.Reason
}

// Is makes every Refusal match ErrRefused.
func (Refusal) Is(target error) bool {
	return target == ErrRefused
}

// New is a refusal with the given reason.
func New(reason string) error {
	return Refusal{Reason: reason}
}
