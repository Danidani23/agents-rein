// Package credentials is the service that owns who rein acts as on this
// machine: the accounts in the state, their tokens in the keychain, the
// device flow that obtains one, and the renewal of an expired one (D25,
// D61).
// In a GitHub Actions job it hands out the job's own token instead, the
// one exception to the keychain (D22).
package credentials

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Service owns the logins.
type Service struct {
	Keychain ports.Keychain
	Host     ports.Host
	State    ports.State
	Clock    func() time.Time
	// Sleep waits between polls of the device flow; nil means time.Sleep.
	Sleep func(time.Duration)
	// Environment reads a variable; nil means os.LookupEnv.
	Environment func(name string) (string, bool)
}

// ErrNotLoggedIn is the refusal when no login exists.
type ErrNotLoggedIn struct{}

func (ErrNotLoggedIn) Error() string {
	return "not logged in; run rein login, which opens the GitHub device flow"
}

// Is makes it a refusal.
func (ErrNotLoggedIn) Is(target error) bool {
	return target == refusal.ErrRefused
}

// ErrUnknownLogin is the refusal for an account this machine does not have.
type ErrUnknownLogin struct {
	Login string
}

func (err ErrUnknownLogin) Error() string {
	return fmt.Sprintf("%s is not an account on this machine; rein account add adds one", err.Login)
}

// ErrLoggedOut is the refusal for an account whose token is gone.
type ErrLoggedOut struct {
	Login string
}

func (err ErrLoggedOut) Error() string {
	return fmt.Sprintf("%s is logged out on this machine; rein login %s logs it in again", err.Login, err.Login)
}

// Is makes it a refusal.
func (ErrLoggedOut) Is(target error) bool {
	return target == refusal.ErrRefused
}

// Is makes it a refusal.
func (ErrUnknownLogin) Is(target error) bool {
	return target == refusal.ErrRefused
}

func (service Service) now() time.Time {
	if service.Clock == nil {
		return time.Now().UTC()
	}
	return service.Clock()
}

func (service Service) sleep(duration time.Duration) {
	if service.Sleep == nil {
		time.Sleep(duration)
		return
	}
	service.Sleep(duration)
}

// Accounts lists the accounts on this machine, logged in or not, sorted by
// login.
func (service Service) Accounts() ([]machine.Account, error) {
	state, err := service.State.Load()
	if err != nil {
		return nil, err
	}
	accounts := append([]machine.Account(nil), state.Accounts...)
	sort.Slice(accounts, func(left int, right int) bool { return accounts[left].Login < accounts[right].Login })
	return accounts, nil
}

// Logins lists the logins of the accounts on this machine, sorted.
func (service Service) Logins() ([]string, error) {
	accounts, err := service.Accounts()
	if err != nil {
		return nil, err
	}
	var logins []string
	for _, account := range accounts {
		logins = append(logins, account.Login)
	}
	return logins, nil
}

// Standing says whether an account is logged in: its token is in the
// keychain and is current or can be renewed. Otherwise reason says why
// not. It asks no host, so a token the host refuses still stands here.
func (service Service) Standing(login string) (loggedIn bool, reason string, err error) {
	token, found, err := service.Keychain.Load(login)
	if err != nil {
		return false, "", err
	}
	switch {
	case !found:
		return false, "no token in the keychain", nil
	case token.Expired(service.now()) && (token.RefreshToken == "" || (!token.RefreshExpiresAt.IsZero() && token.RefreshExpiresAt.Before(service.now()))):
		return false, "its token expired and cannot be renewed", nil
	}
	return true, "", nil
}

// Token is an account's token as the keychain holds it, for a workflow
// that deletes it to put back.
func (service Service) Token(login string) (token ports.Token, found bool, err error) {
	return service.Keychain.Load(login)
}

// BeginLogin starts the device flow and returns the code to show; without
// a usable keychain there is nowhere to keep the token, so it refuses first.
func (service Service) BeginLogin() (ports.DeviceCode, error) {
	reason, ok := service.Keychain.Available()
	if !ok {
		return ports.DeviceCode{}, refusal.New(reason)
	}
	return service.Host.StartDeviceFlow()
}

// CompleteLogin waits for the approval and keeps the login, as
// AwaitApproval and KeepLogin do one after the other.
func (service Service) CompleteLogin(code ports.DeviceCode) (ports.Identity, error) {
	token, identity, err := service.AwaitApproval(context.Background(), code)
	if err != nil {
		return ports.Identity{}, err
	}
	return identity, service.KeepLogin(token)
}

// AwaitApproval waits until the code is approved on the host, it expires,
// or ctx ends, and reads the user the token acts as. It changes nothing on
// this machine, so it needs no lock (D59).
func (service Service) AwaitApproval(ctx context.Context, code ports.DeviceCode) (token ports.Token, identity ports.Identity, err error) {
	for {
		err = ctx.Err()
		if err != nil {
			return ports.Token{}, ports.Identity{}, err
		}
		token, pending, err := service.Host.PollDeviceFlow(code)
		if err != nil {
			return ports.Token{}, ports.Identity{}, refusal.New(err.Error())
		}
		if pending {
			if service.now().After(code.ExpiresAt) {
				return ports.Token{}, ports.Identity{}, refusal.New("the code expired before it was entered; log in again")
			}
			service.sleep(code.Interval)
			continue
		}
		identity, err = service.Host.Identity(token.AccessToken)
		if err != nil {
			return ports.Token{}, ports.Identity{}, err
		}
		token.Login = identity.Login
		return token, identity, nil
	}
}

// KeepLogin stores the token under its login in the keychain and
// remembers the account on the host the token came from.
func (service Service) KeepLogin(token ports.Token) error {
	err := service.Keychain.Store(token)
	if err != nil {
		return err
	}
	return service.Remember(machine.Account{Host: service.Host.Name(), Login: token.Login, AddedAt: service.now()})
}

// Remember adds an account to the state once: at a login, and when a
// reset of the machine keeps the accounts in a new store. An account known
// already keeps when it was added.
func (service Service) Remember(account machine.Account) error {
	state, err := service.State.Load()
	if err != nil {
		return err
	}
	for _, known := range state.Accounts {
		if known.Login == account.Login {
			return nil
		}
	}
	state.Accounts = append(state.Accounts, account)
	return service.State.Save(state)
}

// Logout deletes an account's token from the keychain; the account stays,
// logged out, until it is removed (D61). An account logged out already
// stays so.
func (service Service) Logout(login string) error {
	_, err := service.Account(login)
	if err != nil {
		return err
	}
	_, found, err := service.Keychain.Load(login)
	if err != nil || !found {
		return err
	}
	return service.Keychain.Delete(login)
}

// Forget removes an account from the state; its token is the caller's.
func (service Service) Forget(login string) error {
	state, err := service.State.Load()
	if err != nil {
		return err
	}
	var kept []machine.Account
	found := false
	for _, known := range state.Accounts {
		if known.Login == login {
			found = true
			continue
		}
		kept = append(kept, known)
	}
	if !found {
		return ErrUnknownLogin{Login: login}
	}
	state.Accounts = kept
	return service.State.Save(state)
}

// Account is the account with login on this machine, or the refusal that
// names it unknown.
func (service Service) Account(login string) (machine.Account, error) {
	accounts, err := service.Accounts()
	if err != nil {
		return machine.Account{}, err
	}
	for _, account := range accounts {
		if account.Login == login {
			return account, nil
		}
	}
	return machine.Account{}, ErrUnknownLogin{Login: login}
}

// AccessToken is the token of a login, renewed when expired. An empty login
// means the only one, and is a refusal when there are several or none.
func (service Service) AccessToken(login string) (string, error) {
	if token, found := service.actionsToken(); found {
		return token, nil
	}
	if login == "" {
		logins, err := service.Logins()
		if err != nil {
			return "", err
		}
		switch len(logins) {
		case 0:
			return "", ErrNotLoggedIn{}
		case 1:
			login = logins[0]
		default:
			return "", refusal.New(fmt.Sprintf("several accounts are logged in, %v; say which with --account", logins))
		}
	}
	_, err := service.Account(login)
	if err != nil {
		return "", err
	}
	token, found, err := service.Keychain.Load(login)
	if err != nil {
		return "", err
	}
	if !found {
		return "", ErrLoggedOut{Login: login}
	}
	if !token.Expired(service.now()) {
		return token.AccessToken, nil
	}
	if token.RefreshToken == "" || (!token.RefreshExpiresAt.IsZero() && token.RefreshExpiresAt.Before(service.now())) {
		return "", refusal.New(fmt.Sprintf("the token of %s expired and cannot be renewed; run rein login again", login))
	}
	renewed, err := service.Host.RefreshToken(token)
	if err != nil {
		return "", err
	}
	err = service.Keychain.Store(renewed)
	if err != nil {
		return "", err
	}
	return renewed.AccessToken, nil
}

// Resolve picks the login a command acts with: the one named, else the
// only one, and the identity it stands for, derived from the login without
// a call.
func (service Service) Resolve(login string) (ports.Identity, error) {
	if login == "" {
		logins, err := service.Logins()
		if err != nil {
			return ports.Identity{}, err
		}
		switch len(logins) {
		case 0:
			return ports.Identity{}, ErrNotLoggedIn{}
		case 1:
			login = logins[0]
		default:
			return ports.Identity{}, refusal.New(fmt.Sprintf("several accounts are logged in, %v; say which with --account", logins))
		}
	}
	logins, err := service.Logins()
	if err != nil {
		return ports.Identity{}, err
	}
	for _, known := range logins {
		if known == login {
			return ports.Identity{Login: login, Slug: slugOf(login)}, nil
		}
	}
	return ports.Identity{}, ErrUnknownLogin{Login: login}
}

// actionsToken is the job's own token inside GitHub Actions.
func (service Service) actionsToken() (string, bool) {
	inActions, _ := service.lookup("GITHUB_ACTIONS")
	if inActions == "" {
		return "", false
	}
	token, _ := service.lookup("GITHUB_TOKEN")
	return token, token != ""
}

// lookup reads a variable of the caller's environment.
func (service Service) lookup(name string) (string, bool) {
	if service.Environment == nil {
		return os.LookupEnv(name)
	}
	return service.Environment(name)
}

func slugOf(login string) string {
	var builder []rune
	for _, character := range login {
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		builder = append(builder, character)
	}
	return string(builder)
}

// Key is the provider's key a variable names: from the keychain, where
// rein key set keeps it, else from the environment, as on CI where the
// pipeline's secret store sets it (Q59).
func (service Service) Key(name string) (string, bool) {
	if _, available := service.Keychain.Available(); available {
		value, found, err := service.Keychain.LoadKey(name)
		if err == nil && found && value != "" {
			return value, true
		}
	}
	value, found := service.lookup(name)
	return value, found && value != ""
}

// StoreKey keeps a provider's key in the keychain under the variable's
// name; the value is trimmed of the newline it came with.
func (service Service) StoreKey(name string, value string) error {
	if !declaration.IsVariableName(name) {
		return refusal.New(fmt.Sprintf("%s is not the name of an environment variable, such as DEEPSEEK_API_KEY; name the variable, never the key", name))
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return refusal.New("no key came through standard input")
	}
	reason, available := service.Keychain.Available()
	if !available {
		return refusal.New(fmt.Sprintf("%s; export %s in your shell instead", reason, name))
	}
	return service.Keychain.StoreKey(name, value)
}

// DeleteKey removes a provider's key from the keychain.
func (service Service) DeleteKey(name string) error {
	if !declaration.IsVariableName(name) {
		return refusal.New(fmt.Sprintf("%s is not the name of an environment variable", name))
	}
	reason, available := service.Keychain.Available()
	if !available {
		return refusal.New(reason)
	}
	return service.Keychain.DeleteKey(name)
}
