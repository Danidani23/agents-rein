// Package keychain is the Keychain port over the operating system's own
// store: on macOS the login keychain through the security command, on
// Linux the desktop's keyring through secret-tool, which takes a secret on
// standard input, and on Windows Credential Manager through its own calls
// (D44). A token is one secret under the service agents-rein, with the
// GitHub login as the account (D25); a provider's key is one under the
// account key:<variable>. FileKeychain is the stand-in for tests and for a
// machine without a keychain, a file under rein's home readable by the user
// alone, chosen explicitly and never by default.
package keychain

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// service names rein's entries in the keychain.
const service = "agents-rein"

// backend keeps secrets of rein's service, one per account: the operating
// system's store, behind the encoding of tokens and keys.
type backend interface {
	available() (reason string, ok bool)
	put(account string, label string, secret string) error
	get(account string) (secret string, found bool, err error)
	remove(account string) error
}

// System is the operating system's keychain.
type System struct {
	// keeper replaces the operating system's store, for a test.
	keeper backend
}

var _ ports.Keychain = System{}

// store is the backend of this operating system.
func (system System) store() backend {
	if system.keeper != nil {
		return system.keeper
	}
	switch runtime.GOOS {
	case "darwin":
		return securityCommand{run: execute.RunWith}
	case "linux":
		return secretTool{run: execute.RunWith, look: execute.Preflight}
	case "windows":
		return credentialManager{}
	}
	return unsupported{}
}

// Available says whether this machine has a keychain rein can use.
func (system System) Available() (reason string, ok bool) {
	return system.store().available()
}

// Store writes the token, replacing an earlier one.
func (system System) Store(token ports.Token) error {
	encoded, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return system.store().put(token.Login, "Agents Rein: GitHub token of "+token.Login, string(encoded))
}

// Load reads the token of a login.
func (system System) Load(login string) (ports.Token, bool, error) {
	secret, found, err := system.store().get(login)
	if err != nil || !found {
		return ports.Token{}, false, err
	}
	var token ports.Token
	err = json.Unmarshal([]byte(strings.TrimSpace(secret)), &token)
	if err != nil {
		return ports.Token{}, false, err
	}
	return token, true, nil
}

// Delete removes the token of a login; a missing one is not an error.
func (system System) Delete(login string) error {
	return system.store().remove(login)
}

// keyAccount is the account a provider's key is kept under, apart from the
// logins, which hold no colon.
func keyAccount(name string) string {
	return "key:" + name
}

// StoreKey writes a provider's key, replacing an earlier one.
func (system System) StoreKey(name string, value string) error {
	return system.store().put(keyAccount(name), "Agents Rein: "+name, value)
}

// LoadKey reads a provider's key.
func (system System) LoadKey(name string) (string, bool, error) {
	secret, found, err := system.store().get(keyAccount(name))
	return strings.TrimSpace(secret), found, err
}

// DeleteKey removes a provider's key; a missing one is not an error.
func (system System) DeleteKey(name string) error {
	return system.store().remove(keyAccount(name))
}

// run starts a command, as execute.RunWith does; a test replaces it.
type run func(options execute.Options, name string, arguments ...string) (execute.Result, error)

// securityCommand is macOS's login keychain through the security command,
// a secret as a generic password.
type securityCommand struct {
	run run
}

func (securityCommand) available() (string, bool) {
	err := execute.Preflight("security")
	if err != nil {
		return err.Error(), false
	}
	return "", true
}

func (keychain securityCommand) put(account string, label string, secret string) error {
	_, err := keychain.run(execute.Options{}, "security", "add-generic-password", "-U", "-s", service, "-a", account, "-l", label, "-w", secret)
	return err
}

func (keychain securityCommand) get(account string) (string, bool, error) {
	result, err := keychain.run(execute.Options{}, "security", "find-generic-password", "-s", service, "-a", account, "-w")
	var failure *execute.Failure
	if errors.As(err, &failure) && strings.Contains(failure.Stderr, "could not be found") {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSuffix(result.Stdout, "\n"), true, nil
}

func (keychain securityCommand) remove(account string) error {
	_, err := keychain.run(execute.Options{}, "security", "delete-generic-password", "-s", service, "-a", account)
	var failure *execute.Failure
	if errors.As(err, &failure) && strings.Contains(failure.Stderr, "could not be found") {
		return nil
	}
	return err
}

// secretTool is the desktop's keyring on Linux, GNOME Keyring or KWallet,
// through the Secret Service's own command, which takes a secret on
// standard input, so it never shows among a process's arguments
// ([man page](https://manpages.ubuntu.com/manpages/noble/man1/secret-tool.1.html)).
type secretTool struct {
	run  run
	look func(name string) error
}

// silent says whether secret-tool failed without a word: what it does when
// nothing matches, unlike a keyring it cannot reach, which it names.
func silent(err error) bool {
	var failure *execute.Failure
	return errors.As(err, &failure) && strings.TrimSpace(failure.Stderr) == ""
}

// available needs secret-tool and a keyring it reaches: a lookup that
// finds nothing answers silently, one without a keyring says why.
func (keychain secretTool) available() (string, bool) {
	err := keychain.look("secret-tool")
	if err != nil {
		return fmt.Sprintf("rein keeps secrets in the desktop's keyring through secret-tool, from libsecret-tools on Debian and Ubuntu or libsecret on Fedora: %v", err), false
	}
	_, err = keychain.run(execute.Options{}, "secret-tool", "lookup", "service", service, "account", "rein-probe")
	if err != nil && !silent(err) {
		var failure *execute.Failure
		if errors.As(err, &failure) {
			return "no keyring to reach through secret-tool: " + strings.TrimSpace(failure.Stderr), false
		}
		return err.Error(), false
	}
	return "", true
}

func (keychain secretTool) put(account string, label string, secret string) error {
	_, err := keychain.run(execute.Options{Input: secret}, "secret-tool", "store", "--label="+label, "service", service, "account", account)
	return err
}

func (keychain secretTool) get(account string) (string, bool, error) {
	result, err := keychain.run(execute.Options{}, "secret-tool", "lookup", "service", service, "account", account)
	if silent(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return result.Stdout, true, nil
}

func (keychain secretTool) remove(account string) error {
	_, err := keychain.run(execute.Options{}, "secret-tool", "clear", "service", service, "account", account)
	if silent(err) {
		return nil
	}
	return err
}

// unsupported is an operating system rein knows no keychain of.
type unsupported struct{}

func (unsupported) available() (string, bool) {
	return fmt.Sprintf("rein knows no keychain on %s; export the variables, or set REIN_KEYCHAIN_FILE", runtime.GOOS), false
}

func (unsupported) put(string, string, string) error {
	return errors.New("no keychain on " + runtime.GOOS)
}

func (unsupported) get(string) (string, bool, error) {
	return "", false, errors.New("no keychain on " + runtime.GOOS)
}

func (unsupported) remove(string) error {
	return errors.New("no keychain on " + runtime.GOOS)
}

// FileKeychain keeps the tokens in one JSON file readable by the user only.
type FileKeychain struct {
	Path string
}

var _ ports.Keychain = FileKeychain{}

// Available is always true for a file.
func (FileKeychain) Available() (string, bool) {
	return "", true
}

func (keychain FileKeychain) load() (map[string]ports.Token, error) {
	tokens := map[string]ports.Token{}
	content, err := os.ReadFile(keychain.Path)
	if os.IsNotExist(err) {
		return tokens, nil
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(content, &tokens)
	return tokens, err
}

func (keychain FileKeychain) save(tokens map[string]ports.Token) error {
	err := os.MkdirAll(filepath.Dir(keychain.Path), 0o700)
	if err != nil {
		return err
	}
	content, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(keychain.Path, content, 0o600)
}

// Store writes the token.
func (keychain FileKeychain) Store(token ports.Token) error {
	tokens, err := keychain.load()
	if err != nil {
		return err
	}
	tokens[token.Login] = token
	return keychain.save(tokens)
}

// Load reads the token of a login.
func (keychain FileKeychain) Load(login string) (ports.Token, bool, error) {
	tokens, err := keychain.load()
	if err != nil {
		return ports.Token{}, false, err
	}
	token, found := tokens[login]
	return token, found, nil
}

// Delete removes the token of a login.
func (keychain FileKeychain) Delete(login string) error {
	tokens, err := keychain.load()
	if err != nil {
		return err
	}
	delete(tokens, login)
	return keychain.save(tokens)
}

// StoreKey writes a provider's key beside the tokens, under its own account.
func (keychain FileKeychain) StoreKey(name string, value string) error {
	return keychain.Store(ports.Token{Login: keyAccount(name), AccessToken: value})
}

// LoadKey reads a provider's key.
func (keychain FileKeychain) LoadKey(name string) (string, bool, error) {
	token, found, err := keychain.Load(keyAccount(name))
	return token.AccessToken, found, err
}

// DeleteKey removes a provider's key.
func (keychain FileKeychain) DeleteKey(name string) error {
	return keychain.Delete(keyAccount(name))
}
