package orchestration

import (
	"context"
	"fmt"

	"github.com/Danidani23/agents-rein/internal/ports"
)

// LoginRequest is one login. Context ends the wait for the approval, as
// Ctrl-C or Cancel do; Holder is what the machine lock is told while the
// token is kept (D56).
type LoginRequest struct {
	Context context.Context
	Holder  ports.LockHolder
}

// LoginResult is the account that logged in.
type LoginResult struct {
	Login string
}

// Login runs GitHub's device flow (D25, D58). It asks for a code and
// reports it, with the address to enter it at, as the detail of its first
// step, so the terminal and the page both show it; waits for the
// developer's approval without the machine lock, which may take minutes
// (D59); and keeps the token in the keychain and the login in the state
// once its turn at the lock has come. Nothing is kept before the last
// step, so nothing is undone.
func (orchestrator Orchestrator) Login(request LoginRequest) (LoginResult, error) {
	services := orchestrator.Services
	ctx := request.Context
	if ctx == nil {
		ctx = context.Background()
	}
	report := func(step string, note string, link string) {
		if orchestrator.Progress != nil {
			orchestrator.Progress(ProgressEvent{Kind: ProgressDetail, Step: step, Note: note, Link: link})
		}
	}
	var code ports.DeviceCode
	var token ports.Token
	var result LoginResult
	const asking, waiting, keeping = "asking GitHub for a code", "waiting for your approval on GitHub", "keeping the token"
	err := runSteps(orchestrator.Progress, []Step{
		{
			Name: asking,
			Do: func() error {
				var err error
				code, err = services.Credentials.BeginLogin()
				if err != nil {
					return err
				}
				report(asking, fmt.Sprintf("open %s and enter the code %s; it is valid until %s", code.VerificationURI, code.UserCode, code.ExpiresAt.Local().Format("15:04")), code.VerificationURI)
				return nil
			},
		},
		{
			Name: waiting,
			Do: func() error {
				var err error
				token, _, err = services.Credentials.AwaitApproval(ctx, code)
				return err
			},
		},
		{
			Name: keeping,
			Do: func() error {
				release := func(error) {}
				if services.Lock != nil {
					// two logins of one account are one; of two accounts, two
					holder := request.Holder
					holder.Command += ", keeping the token of " + token.Login
					var err error
					release, err = services.Lock.Acquire(ctx, holder, func(wait ports.LockWait) {
						report(keeping, "waiting for "+wait.Holder.String(), "")
					})
					if err != nil {
						return err
					}
				}
				err := services.Credentials.KeepLogin(token)
				release(err)
				result.Login = token.Login
				return err
			},
		},
	})
	if err != nil {
		return LoginResult{}, err
	}
	return result, nil
}
