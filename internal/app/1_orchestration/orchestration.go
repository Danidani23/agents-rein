// Package orchestration holds the workflows: what each command does, written
// against the ports and the services only, behind one interface per area. A
// workflow takes and returns plain data and never prints or asks; a decision
// that needs a person comes back as a result the gateway acts on, which is
// what lets a remote implementation stand in for the local one some day.
package orchestration

import (
	"errors"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Dependencies is everything the local implementation may touch.
type Dependencies struct {
	Home       string
	Host       ports.Host
	Repository ports.Repository
	Agents     []ports.Agent
	State      ports.State
}

// Organizations is the area of org repositories on this machine.
type Organizations interface {
	Connect(remote string) (ConnectResult, error)
	List() (organizations []machine.OrganizationRef, active string, err error)
	Use(slug string) error
	Disconnect(slug string) (DisconnectResult, error)
}

// Machine is the area of this machine itself: what rein knows and did here.
type Machine interface {
	Status() (StatusReport, error)
	PlanReset(all bool) (ResetPlan, error)
	ApplyReset(plan ResetPlan) (ResetResult, error)
}

// Orchestrator is every area the gateway can call.
type Orchestrator struct {
	Organizations Organizations
	Machine       Machine
}

// NewLocal is the in-process implementation over the given dependencies.
func NewLocal(dependencies Dependencies) Orchestrator {
	implementation := &local{dependencies: dependencies}
	return Orchestrator{Organizations: implementation, Machine: implementation}
}

type local struct {
	dependencies Dependencies
}

// ErrRefused marks a refusal: a precondition failed, nothing changed. The
// gateway maps it to exit code 1; any other error is a crash.
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

// installedAgents lists the agents found on this machine.
func (implementation *local) installedAgents() []string {
	var found []string
	for _, agent := range implementation.dependencies.Agents {
		if agent.Installed() {
			found = append(found, agent.Name())
		}
	}
	return found
}
