package orchestration

import (
	"context"
	"fmt"
	"slices"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// AddAccountRequest is one account to add: the host it is on, github or
// a planned one, which is refused. Context and Holder are a login's.
type AddAccountRequest struct {
	Host    string
	Context context.Context
	Holder  ports.LockHolder
}

// AddAccountResult is the account added; Known says it was on this machine
// already, and only its token was renewed.
type AddAccountResult struct {
	Login string
	Host  string
	Known bool
}

// AddAccount adds the host account that approves the device flow to this
// machine (D61): the account a developer adds first, before anything else.
func (orchestrator Orchestrator) AddAccount(request AddAccountRequest) (AddAccountResult, error) {
	services := orchestrator.Services
	supported := services.Credentials.Host.Name()
	switch {
	case slices.Contains(declaration.PlannedHosts, request.Host):
		return AddAccountResult{}, refusal.New(fmt.Sprintf("%s is planned, not yet supported; this build adds accounts on %s", request.Host, supported))
	case request.Host != supported:
		return AddAccountResult{}, refusal.New(fmt.Sprintf("%q is not a host rein knows; this build adds accounts on %s", request.Host, supported))
	}
	before, err := services.Credentials.Logins()
	if err != nil {
		return AddAccountResult{}, err
	}
	login, err := deviceFlow(request.Context, orchestrator, request.Holder, "")
	if err != nil {
		return AddAccountResult{}, err
	}
	return AddAccountResult{Login: login, Host: request.Host, Known: slices.Contains(before, login)}, nil
}
