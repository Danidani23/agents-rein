// Package orchestration holds the workflows and the queries: a workflow runs
// several services in order and compensates when a step fails; a query reads
// across services and changes nothing. Both take and return plain data and
// never print or ask. A single operation on one service is not here: the
// gateway calls the service directly (D17). Every workflow is its own file,
// workflow_<name>.go with its result types, so the folder lists them; the
// queries share queries.go; saga.go is the runner.
package orchestration

import (
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/clones"
	"github.com/Danidani23/agents-rein/internal/app/2_services/hosting"
	"github.com/Danidani23/agents-rein/internal/app/2_services/identity"
	"github.com/Danidani23/agents-rein/internal/app/2_services/ledger"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/organizations"
	"github.com/Danidani23/agents-rein/internal/app/2_services/placements"
	"github.com/Danidani23/agents-rein/internal/app/2_services/records"
	"github.com/Danidani23/agents-rein/internal/app/2_services/snapshots"
	"github.com/Danidani23/agents-rein/internal/app/2_services/workingcopy"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Services are the services the workflows compose and the gateway may call
// directly, each over the ports it needs.
type Services struct {
	Home          string
	Identity      identity.Service
	Organizations organizations.Service
	Clones        clones.Service
	Placements    placements.Service
	Hosting       hosting.Service
	Snapshots     snapshots.Service
	Records       records.Service
	WorkingCopy   workingcopy.Service
	Ledger        ledger.Service
	// Clock stamps every record a workflow writes (D20).
	Clock func() time.Time
}

// NewServices builds the services over the ports: identity over the host,
// organisations over the state, clones over the repository, placements over
// the state and the agents, hosting over the host and the repository,
// snapshots over the agents. Nothing runs here; main calls it once with the
// real adapters and the tests with the fakes.
func NewServices(home string, host ports.Host, repository ports.Repository, state ports.State, agents []ports.Agent) Services {
	return Services{
		Home:          home,
		Identity:      identity.Service{Host: host},
		Organizations: organizations.Service{State: state, Clock: time.Now},
		Clones:        clones.Service{Home: home, Repository: repository},
		Placements:    placements.Service{Home: home, State: state, Agents: agents},
		Hosting:       hosting.Service{Host: host, Repository: repository},
		Snapshots:     snapshots.Service{Home: home, Agents: agents, Clock: time.Now},
		Records:       records.Service{Repository: repository},
		WorkingCopy:   workingcopy.Service{Repository: repository},
		Ledger:        ledger.Service{Home: home, Clock: time.Now},
		Clock:         time.Now,
	}
}

// resolveOrganization is the organisation a command works in: the one named,
// or the active one when the name is empty.
func resolveOrganization(services Services, slug string) (organization machine.OrganizationRef, orgClonePath string, err error) {
	if slug == "" {
		return services.Organizations.Active()
	}
	return services.Organizations.Lookup(slug)
}

// Orchestrator is the services plus the workflows and queries across them;
// the gateway holds one. It is a concrete type with one implementation: a
// backend, should one ever exist, is called from the gateway, and an
// interface can be drawn around this type the day it is needed (D18).
type Orchestrator struct {
	Services
}

// New is the orchestrator over the services.
func New(services Services) Orchestrator {
	return Orchestrator{Services: services}
}
