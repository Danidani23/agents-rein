// Package app holds the use cases: what each command does, written against
// the ports and the domain only. A use case returns a result the CLI prints;
// it never writes to a stream itself, and it never asks a question, which is
// the CLI's job through the Prompter port.
package app

import (
	"errors"

	"github.com/Danidani23/agents-rein/internal/ports"
)

// Dependencies is everything a use case may touch.
type Dependencies struct {
	Home       string
	Host       ports.Host
	Repository ports.Repository
	Agents     []ports.Agent
	State      ports.State
}

// ErrRefused marks a refusal: a precondition failed, nothing changed. The
// CLI maps it to exit code 1; any other error is a crash.
var ErrRefused = errors.New("refused")

// Refusal is a refusal with its reason.
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

func refuse(reason string) error {
	return Refusal{Reason: reason}
}

// InstalledAgents lists the agents found on this machine.
func InstalledAgents(dependencies Dependencies) []string {
	var found []string
	for _, agent := range dependencies.Agents {
		if agent.Installed() {
			found = append(found, agent.Name())
		}
	}
	return found
}
