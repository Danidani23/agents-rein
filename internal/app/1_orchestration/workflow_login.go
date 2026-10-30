package orchestration

import (
	"context"
	"fmt"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// LoginRequest is one login of an account on this machine. Account names
// it; "" means the only one, or the first one on a machine without any.
// Context ends the wait for the approval, as Ctrl-C or Cancel do; Holder
// is what the machine lock is told while the token is kept (D56).
type LoginRequest struct {
	Account string
	Context context.Context
	Holder  ports.LockHolder
}

// LoginResult is the account that logged in, and whether it was added,
// on a machine that had none.
type LoginResult struct {
	Login string
	Added bool
}

// Login logs an account of this machine in again through the host's device
// flow (D25, D58, D61), renewing its token; on a machine without any
// account it adds the one that approves. The code GitHub asks for is the
// detail of the first step, so the terminal and the page both show it; the
// wait for the approval takes no lock (D59); a code approved as another
// account than the one named keeps nothing.
func (orchestrator Orchestrator) Login(request LoginRequest) (LoginResult, error) {
	accounts, err := orchestrator.Services.Credentials.Accounts()
	if err != nil {
		return LoginResult{}, err
	}
	expected := request.Account
	switch {
	case expected != "":
		_, err = orchestrator.Services.Credentials.Account(expected)
		if err != nil {
			return LoginResult{}, err
		}
	case len(accounts) == 1:
		expected = accounts[0].Login
	case len(accounts) > 1:
		var logins []string
		for _, account := range accounts {
			logins = append(logins, account.Login)
		}
		return LoginResult{}, refusal.New(fmt.Sprintf("several accounts are on this machine, %s; name the one to log in: rein login <account>", strings.Join(logins, ", ")))
	}
	login, err := deviceFlow(request.Context, orchestrator, request.Holder, expected)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Login: login, Added: len(accounts) == 0}, nil
}

// deviceFlow runs the host's device flow (D25): it asks for a code and
// reports it, with the address to enter it at, as the detail of its first
// step; waits for the developer's approval without the machine lock, which
// may take minutes (D59); and keeps the token in the keychain and the
// account in the state once its turn at the lock has come. Expected, when
// set, is the only account the approval may come from. Nothing is kept
// before the last step, so nothing is undone.
func deviceFlow(ctx context.Context, orchestrator Orchestrator, holder ports.LockHolder, expected string) (login string, err error) {
	services := orchestrator.Services
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
	const asking, waiting, keeping = "asking GitHub for a code", "waiting for your approval on GitHub", "keeping the token"
	err = runSteps(orchestrator.Progress, []Step{
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
				if err != nil {
					return err
				}
				if expected != "" && token.Login != expected {
					return refusal.New(fmt.Sprintf("GitHub approved the code as %s, not %s; sign in to GitHub as %s in your browser and log in again, or add %s with rein account add", token.Login, expected, expected, token.Login))
				}
				return nil
			},
		},
		{
			Name: keeping,
			Do: func() error {
				release := func(error) {}
				if services.Lock != nil {
					// two logins of one account are one; of two accounts, two
					lockHolder := holder
					lockHolder.Command += ", keeping the token of " + token.Login
					var err error
					release, err = services.Lock.Acquire(ctx, lockHolder, func(wait ports.LockWait) {
						report(keeping, "waiting for "+wait.Holder.String(), "")
					})
					if err != nil {
						return err
					}
				}
				err := services.Credentials.KeepLogin(token)
				if err == nil {
					// a refused token the check found is replaced
					err = services.Findings.Drop(machine.FindingLoggedOut, token.Login)
				}
				release(err)
				return err
			},
		},
	})
	if err != nil {
		return "", err
	}
	return token.Login, nil
}
