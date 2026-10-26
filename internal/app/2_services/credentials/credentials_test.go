package credentials

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/ports"
)

func newService(host *fake.Host) (Service, *fake.State, *fake.Keychain) {
	state := fake.NewState()
	keychain := &fake.Keychain{}
	return Service{Keychain: keychain, Host: host, State: state, Clock: func() time.Time { return time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC) }, Sleep: func(time.Duration) {}}, state, keychain
}

func TestLogin(t *testing.T) {
	t.Run("the device flow ends with the token in the keychain and the login in the state", func(t *testing.T) {
		host := &fake.Host{Login: "Danidani23", Pending: 2}
		service, state, keychain := newService(host)
		code, err := service.BeginLogin()
		if err != nil || code.UserCode != "ABCD-1234" {
			t.Fatalf("code %+v err %v", code, err)
		}
		identity, err := service.CompleteLogin(code)
		if err != nil || identity.Login != "Danidani23" || identity.Slug != "danidani23" {
			t.Fatalf("identity %+v err %v", identity, err)
		}
		if strings.Join(state.Current.Logins, ",") != "Danidani23" || keychain.Tokens["Danidani23"].AccessToken != "token-of-Danidani23" {
			t.Fatalf("state %v keychain %v", state.Current.Logins, keychain.Tokens)
		}
		_, err = service.CompleteLogin(code)
		logins, _ := service.Logins()
		if err != nil || len(logins) != 1 {
			t.Fatalf("a second login of the same account duplicated it: %v %v", logins, err)
		}
	})
	t.Run("a declined flow and a missing keychain are refusals", func(t *testing.T) {
		service, _, _ := newService(&fake.Host{})
		code, _ := service.BeginLogin()
		_, err := service.CompleteLogin(code)
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "declined") {
			t.Fatalf("err %v", err)
		}
		service, _, keychain := newService(&fake.Host{Login: "x"})
		keychain.Unavailable = "no keychain on this machine"
		_, err = service.BeginLogin()
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "no keychain") {
			t.Fatalf("err %v", err)
		}
	})
}

func TestAccessToken(t *testing.T) {
	host := &fake.Host{Login: "Danidani23"}
	service, state, keychain := newService(host)
	_, err := service.AccessToken("")
	if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err %v", err)
	}
	state.Current.Logins = []string{"Danidani23"}
	_ = keychain.Store(ports.Token{Login: "Danidani23", AccessToken: "fresh", ExpiresAt: time.Date(2026, 10, 21, 20, 0, 0, 0, time.UTC)})
	token, err := service.AccessToken("")
	if err != nil || token != "fresh" || host.Refreshed != 0 {
		t.Fatalf("token %q err %v", token, err)
	}
	_ = keychain.Store(ports.Token{Login: "Danidani23", AccessToken: "stale", RefreshToken: "r", ExpiresAt: time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)})
	token, err = service.AccessToken("Danidani23")
	if err != nil || token != "renewed-stale" || host.Refreshed != 1 || keychain.Tokens["Danidani23"].AccessToken != "renewed-stale" {
		t.Fatalf("token %q err %v refreshed %d", token, err, host.Refreshed)
	}
	_ = keychain.Store(ports.Token{Login: "Danidani23", AccessToken: "dead", ExpiresAt: time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)})
	_, err = service.AccessToken("Danidani23")
	if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "cannot be renewed") {
		t.Fatalf("err %v", err)
	}
	state.Current.Logins = []string{"Danidani23", "Other"}
	_, err = service.AccessToken("")
	if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "several accounts") {
		t.Fatalf("err %v", err)
	}
	_, err = service.AccessToken("Nobody")
	if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "Nobody is not logged in") {
		t.Fatalf("err %v", err)
	}
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_TOKEN", "job-token")
	token, err = service.AccessToken("")
	if err != nil || token != "job-token" {
		t.Fatalf("inside Actions: token %q err %v", token, err)
	}
}

func TestResolveAndLogout(t *testing.T) {
	service, state, keychain := newService(&fake.Host{Login: "Danidani23"})
	state.Current.Logins = []string{"Danidani23"}
	_ = keychain.Store(ports.Token{Login: "Danidani23", AccessToken: "t"})
	identity, err := service.Resolve("")
	if err != nil || identity.Slug != "danidani23" {
		t.Fatalf("identity %+v err %v", identity, err)
	}
	err = service.Logout("Danidani23")
	logins, _ := service.Logins()
	if err != nil || len(logins) != 0 || len(keychain.Tokens) != 0 {
		t.Fatalf("logout err %v logins %v tokens %v", err, logins, keychain.Tokens)
	}
	err = service.Logout("Danidani23")
	if !errors.Is(err, refusal.ErrRefused) {
		t.Fatalf("err %v", err)
	}
}

func TestKeys(t *testing.T) {
	service, _, keychain := newService(&fake.Host{})
	service.Environment = func(name string) (string, bool) { return "from-environment", name == "DEEPSEEK_API_KEY" }
	// the environment answers until the keychain holds the key, as on CI (Q59)
	value, found := service.Key("DEEPSEEK_API_KEY")
	if !found || value != "from-environment" {
		t.Fatalf("from the environment %q %v", value, found)
	}
	err := service.StoreKey("DEEPSEEK_API_KEY", "sk-keychain\n")
	value, found = service.Key("DEEPSEEK_API_KEY")
	if err != nil || !found || value != "sk-keychain" {
		t.Fatalf("from the keychain %q %v err %v", value, found, err)
	}
	for expected, call := range map[string]func() error{
		"sk-123 is not the name of an environment variable": func() error { return service.StoreKey("sk-123", "x") },
		"no key came": func() error { return service.StoreKey("SOME_KEY", "  \n") },
	} {
		err := call()
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), expected) {
			t.Fatalf("%s: %v", expected, err)
		}
	}
	err = service.DeleteKey("DEEPSEEK_API_KEY")
	value, _ = service.Key("DEEPSEEK_API_KEY")
	if err != nil || value != "from-environment" {
		t.Fatalf("after delete %q err %v", value, err)
	}
	// without a keychain, keeping a key is refused and the environment still answers
	keychain.Unavailable = "no keychain on this machine"
	err = service.StoreKey("DEEPSEEK_API_KEY", "sk")
	if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "no keychain on this machine") || !strings.Contains(err.Error(), "export DEEPSEEK_API_KEY") {
		t.Fatalf("no keychain: %v", err)
	}
	if value, found := service.Key("DEEPSEEK_API_KEY"); !found || value != "from-environment" {
		t.Fatalf("no keychain, environment %q %v", value, found)
	}
}
