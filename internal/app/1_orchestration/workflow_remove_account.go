package orchestration

import (
	"fmt"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
)

// RemoveAccountResult is the account removed.
type RemoveAccountResult struct {
	Login string
}

// RemoveAccount deletes an account's token and forgets the account, under
// the machine lock its caller holds (D61). While an organisation acts with
// it, it is refused, naming each one, so no organisation is left without
// the account it acts with.
func (orchestrator Orchestrator) RemoveAccount(login string) (RemoveAccountResult, error) {
	services := orchestrator.Services
	plan, err := orchestrator.PlanRemoveAccount(login)
	if err != nil {
		return RemoveAccountResult{}, err
	}
	if len(plan.Acting) > 0 {
		return RemoveAccountResult{}, refusal.New(fmt.Sprintf("%s acts for %s; disconnect each first, rein org disconnect %s", login, strings.Join(plan.Acting, ", "), plan.Acting[0]))
	}
	token, hadToken, err := services.Credentials.Token(login)
	if err != nil {
		return RemoveAccountResult{}, err
	}
	err = runSteps(orchestrator.Progress, []Step{
		{
			Name: "deleting the token of " + login,
			Do:   func() error { return services.Credentials.Logout(login) },
			Undo: func() error {
				if !hadToken {
					return nil
				}
				return services.Credentials.KeepLogin(token)
			},
		},
		{
			Name: "forgetting the account " + login,
			Do:   func() error { return services.Credentials.Forget(login) },
			Undo: func() error { return services.Credentials.Remember(plan.Account) },
		},
	})
	if err != nil {
		return RemoveAccountResult{}, err
	}
	return RemoveAccountResult{Login: login}, nil
}
