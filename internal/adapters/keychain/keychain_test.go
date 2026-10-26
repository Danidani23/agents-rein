package keychain

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/ports"
)

func TestFileKeychain(t *testing.T) {
	store := FileKeychain{Path: filepath.Join(t.TempDir(), "tokens.json")}
	_, found, err := store.Load("x")
	if err != nil || found {
		t.Fatalf("empty: %v %v", found, err)
	}
	err = store.Store(ports.Token{Login: "x", AccessToken: "secret"})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	info, _ := os.Stat(store.Path)
	// Windows keeps no Unix permission bits
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("the token file is %v, not 0600", info.Mode())
	}
	token, found, err := store.Load("x")
	if err != nil || !found || token.AccessToken != "secret" {
		t.Fatalf("load %+v %v %v", token, found, err)
	}
	err = store.Delete("x")
	_, found, _ = store.Load("x")
	if err != nil || found {
		t.Fatalf("delete %v found %v", err, found)
	}
	if _, ok := store.Available(); !ok {
		t.Fatalf("a file keychain is always available")
	}
}

func TestFileKeychainKeys(t *testing.T) {
	store := FileKeychain{Path: filepath.Join(t.TempDir(), "tokens.json")}
	_ = store.Store(ports.Token{Login: "x", AccessToken: "token"})
	err := store.StoreKey("DEEPSEEK_API_KEY", "sk-1")
	if err != nil {
		t.Fatalf("store key: %v", err)
	}
	value, found, err := store.LoadKey("DEEPSEEK_API_KEY")
	if err != nil || !found || value != "sk-1" {
		t.Fatalf("load key %q %v %v", value, found, err)
	}
	// a key is no login, and the login's token is untouched
	_, found, _ = store.Load("DEEPSEEK_API_KEY")
	token, _, _ := store.Load("x")
	if found || token.AccessToken != "token" {
		t.Fatalf("a key read as a login %v, token %+v", found, token)
	}
	err = store.DeleteKey("DEEPSEEK_API_KEY")
	_, found, _ = store.LoadKey("DEEPSEEK_API_KEY")
	token, _, _ = store.Load("x")
	if err != nil || found || token.AccessToken != "token" {
		t.Fatalf("delete key %v found %v token %+v", err, found, token)
	}
}

// memory is a backend in a map, for the layer above the operating system.
type memory struct {
	secrets map[string]string
	labels  map[string]string
}

func (memory) available() (string, bool) { return "", true }

func (store memory) put(account string, label string, secret string) error {
	store.secrets[account], store.labels[account] = secret, label
	return nil
}

func (store memory) get(account string) (string, bool, error) {
	secret, found := store.secrets[account]
	return secret, found, nil
}

func (store memory) remove(account string) error {
	delete(store.secrets, account)
	return nil
}

func TestSystemOverABackend(t *testing.T) {
	store := memory{secrets: map[string]string{}, labels: map[string]string{}}
	system := System{keeper: store}
	err := system.Store(ports.Token{Login: "Danidani23", AccessToken: "token"})
	token, found, loadErr := system.Load("Danidani23")
	if err != nil || loadErr != nil || !found || token.AccessToken != "token" || store.labels["Danidani23"] != "Agents Rein: GitHub token of Danidani23" {
		t.Fatalf("token %+v found %v err %v %v labels %v", token, found, err, loadErr, store.labels)
	}
	// a key sits apart from the logins, under key:<name>
	err = system.StoreKey("DEEPSEEK_API_KEY", "sk-1")
	value, found, loadErr := system.LoadKey("DEEPSEEK_API_KEY")
	if err != nil || loadErr != nil || !found || value != "sk-1" || store.secrets["key:DEEPSEEK_API_KEY"] != "sk-1" || store.labels["key:DEEPSEEK_API_KEY"] != "Agents Rein: DEEPSEEK_API_KEY" {
		t.Fatalf("key %q found %v err %v %v secrets %v", value, found, err, loadErr, store.secrets)
	}
	_ = system.DeleteKey("DEEPSEEK_API_KEY")
	_ = system.Delete("Danidani23")
	if len(store.secrets) != 0 {
		t.Fatalf("left %v", store.secrets)
	}
}

// call is one run of a command the stub recorded.
type call struct {
	input     string
	arguments []string
}

func TestSecretTool(t *testing.T) {
	var calls []call
	answer := func(result execute.Result, err error) func(execute.Options, string, ...string) (execute.Result, error) {
		return func(options execute.Options, name string, arguments ...string) (execute.Result, error) {
			calls = append(calls, call{input: options.Input, arguments: append([]string{name}, arguments...)})
			return result, err
		}
	}
	installed := func(string) error { return nil }
	// the secret travels on standard input, never as an argument (D44)
	err := secretTool{run: answer(execute.Result{}, nil), look: installed}.put("key:DEEPSEEK_API_KEY", "Agents Rein: DEEPSEEK_API_KEY", "sk-1")
	if err != nil || calls[0].input != "sk-1" || strings.Join(calls[0].arguments, " ") != "secret-tool store --label=Agents Rein: DEEPSEEK_API_KEY service agents-rein account key:DEEPSEEK_API_KEY" {
		t.Fatalf("store %+v err %v", calls, err)
	}
	calls = nil
	secret, found, err := secretTool{run: answer(execute.Result{Stdout: "sk-1"}, nil), look: installed}.get("key:DEEPSEEK_API_KEY")
	if err != nil || !found || secret != "sk-1" || strings.Join(calls[0].arguments, " ") != "secret-tool lookup service agents-rein account key:DEEPSEEK_API_KEY" {
		t.Fatalf("lookup %q %v %+v err %v", secret, found, calls, err)
	}
	// nothing stored: secret-tool fails without a word, which is no error
	missing := &execute.Failure{Command: "secret-tool lookup", Cause: errors.New("exit status 1")}
	_, found, err = secretTool{run: answer(execute.Result{}, missing), look: installed}.get("Danidani23")
	if err != nil || found {
		t.Fatalf("missing: found %v err %v", found, err)
	}
	err = secretTool{run: answer(execute.Result{}, missing), look: installed}.remove("Danidani23")
	if err != nil || strings.Join(calls[len(calls)-1].arguments, " ") != "secret-tool clear service agents-rein account Danidani23" {
		t.Fatalf("clear %+v err %v", calls, err)
	}
	// no keyring service to reach is unavailable, with secret-tool's own words
	unreachable := &execute.Failure{Command: "secret-tool lookup", Stderr: "Cannot autolaunch D-Bus without X11 $DISPLAY\n", Cause: errors.New("exit status 1")}
	reason, available := secretTool{run: answer(execute.Result{}, unreachable), look: installed}.available()
	if available || !strings.Contains(reason, "Cannot autolaunch D-Bus") {
		t.Fatalf("unreachable: %v %q", available, reason)
	}
	_, _, err = secretTool{run: answer(execute.Result{}, unreachable), look: installed}.get("Danidani23")
	if err == nil {
		t.Fatalf("an unreachable keyring read as an empty one")
	}
	reason, available = secretTool{run: answer(execute.Result{}, missing), look: installed}.available()
	if !available {
		t.Fatalf("a reachable keyring without the probe entry: %q", reason)
	}
	reason, available = secretTool{run: answer(execute.Result{}, nil), look: func(string) error { return errors.New("secret-tool is not on the PATH") }}.available()
	if available || !strings.Contains(reason, "libsecret-tools") {
		t.Fatalf("not installed: %v %q", available, reason)
	}
}
