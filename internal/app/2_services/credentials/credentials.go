// Package credentials is the service that owns who is logged in on this
// machine: the logins in the state, their tokens in the keychain, the
// device flow that obtains one, and the renewal of an expired one (D25).
// In a GitHub Actions job it hands out the job's own token instead, the
// one exception to the keychain (D22).
package credentials

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
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

// ErrUnknownLogin is the refusal for a login this machine does not have.
type ErrUnknownLogin struct {
	Login string
}

func (err ErrUnknownLogin) Error() string {
	return fmt.Sprintf("%s is not logged in on this machine; run rein login", err.Login)
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

// Logins lists the accounts logged in on this machine, sorted.
func (service Service) Logins() ([]string, error) {
	state, err := service.State.Load()
	if err != nil {
		return nil, err
	}
	logins := append([]string(nil), state.Logins...)
	sort.Strings(logins)
	return logins, nil
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

// CompleteLogin waits for the approval, reads the user the token acts as,
// stores the token under that login and remembers the login.
func (service Service) CompleteLogin(code ports.DeviceCode) (ports.Identity, error) {
	for {
		token, pending, err := service.Host.PollDeviceFlow(code)
		if err != nil {
			return ports.Identity{}, refusal.New(err.Error())
		}
		if pending {
			if service.now().After(code.ExpiresAt) {
				return ports.Identity{}, refusal.New("the code expired before it was entered; run rein login again")
			}
			service.sleep(code.Interval)
			continue
		}
		identity, err := service.Host.Identity(token.AccessToken)
		if err != nil {
			return ports.Identity{}, err
		}
		token.Login = identity.Login
		err = service.Keychain.Store(token)
		if err != nil {
			return ports.Identity{}, err
		}
		return identity, service.Remember(identity.Login)
	}
}

// Remember adds a login to the state once: at a login, and when a reset
// of the machine keeps the logins in a new store.
func (service Service) Remember(login string) error {
	state, err := service.State.Load()
	if err != nil {
		return err
	}
	for _, known := range state.Logins {
		if known == login {
			return nil
		}
	}
	state.Logins = append(state.Logins, login)
	return service.State.Save(state)
}

// Logout forgets a login and deletes its token.
func (service Service) Logout(login string) error {
	state, err := service.State.Load()
	if err != nil {
		return err
	}
	var kept []string
	found := false
	for _, known := range state.Logins {
		if known == login {
			found = true
			continue
		}
		kept = append(kept, known)
	}
	if !found {
		return ErrUnknownLogin{Login: login}
	}
	state.Logins = kept
	err = service.State.Save(state)
	if err != nil {
		return err
	}
	return service.Keychain.Delete(login)
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
			return "", refusal.New(fmt.Sprintf("several accounts are logged in, %v; say which with --login", logins))
		}
	}
	token, found, err := service.Keychain.Load(login)
	if err != nil {
		return "", err
	}
	if !found {
		return "", ErrUnknownLogin{Login: login}
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
			return ports.Identity{}, refusal.New(fmt.Sprintf("several accounts are logged in, %v; say which with --login", logins))
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
