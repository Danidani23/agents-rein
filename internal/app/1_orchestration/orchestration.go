// Package orchestration holds the workflows and the queries: a workflow runs
// several services in order and compensates when a step fails; a query reads
// across services and changes nothing. Both take and return plain data and
// never print or ask, so a remote implementation could stand in for the
// local one (D16). A single operation on one service is not here: the
// gateway calls the service directly (D17).
package orchestration

import (
	"errors"

	"github.com/Danidani23/agents-rein/internal/app/2_services/clones"
	"github.com/Danidani23/agents-rein/internal/app/2_services/identity"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/organizations"
	"github.com/Danidani23/agents-rein/internal/app/2_services/placements"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Services are the services the orchestrator composes and the gateway may
// call directly.
type Services struct {
	Home          string
	Identity      identity.Service
	Organizations organizations.Service
	Clones        clones.Service
	Placements    placements.Service
	Agents        []ports.Agent
}

// NewServices wires the services over the ports.
func NewServices(home string, host ports.Host, repository ports.Repository, state ports.State, agents []ports.Agent) Services {
	return Services{
		Home:          home,
		Identity:      identity.Service{Host: host},
		Organizations: organizations.Service{State: state},
		Clones:        clones.Service{Home: home, Repository: repository},
		Placements:    placements.Service{Home: home, State: state, Agents: agents},
		Agents:        agents,
	}
}

// Workflows are the multi-step operations with compensation.
type Workflows interface {
	ConnectOrganization(remote string) (ConnectResult, error)
	DisconnectOrganization(slug string) (DisconnectResult, error)
	ResetMachine(plan ResetPlan) (ResetResult, error)
}

// Queries read across services and change nothing.
type Queries interface {
	Status() (StatusReport, error)
	PlanReset(all bool) (ResetPlan, error)
}

// OrganizationsService is what the gateway calls directly for the single
// operations on organisations.
type OrganizationsService interface {
	List() (organizations []machine.OrganizationRef, active string, err error)
	Use(slug string) error
}

// Orchestrator is everything the gateway can reach.
type Orchestrator struct {
	Workflows     Workflows
	Queries       Queries
	Organizations OrganizationsService
}

// NewLocal is the in-process orchestrator over the services.
func NewLocal(services Services) Orchestrator {
	implementation := &local{services: services}
	return Orchestrator{Workflows: implementation, Queries: implementation, Organizations: guarded{services: services}}
}

type local struct {
	services Services
}

// guarded is the organisations service behind the identity gate: a command
// that acts for a user refuses without one.
type guarded struct {
	services Services
}

func (service guarded) List() ([]machine.OrganizationRef, string, error) {
	_, err := service.services.Identity.Who()
	if err != nil {
		return nil, "", Refusal{Reason: err.Error()}
	}
	return service.services.Organizations.List()
}

func (service guarded) Use(slug string) error {
	_, err := service.services.Identity.Who()
	if err != nil {
		return Refusal{Reason: err.Error()}
	}
	return refused(service.services.Organizations.Use(slug))
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
