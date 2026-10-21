// Package keychain is the Keychain port over the operating system's own
// store: on macOS the login keychain through the security command. A token
// is one generic password under the service agents-rein, with the GitHub
// login as the account (D25). FileKeychain is the stand-in for tests and
// for a machine without a keychain, a file under rein's home readable by
// the user alone, chosen explicitly and never by default.
package keychain

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// service names rein's entries in the keychain.
const service = "agents-rein"

// System is the operating system's keychain.
type System struct{}

var _ ports.Keychain = System{}

// Available says whether this machine has a keychain rein can use.
func (System) Available() (reason string, ok bool) {
	if runtime.GOOS != "darwin" {
		return "rein keeps tokens in the operating system's keychain, which it can use on macOS only so far; Linux and Windows follow", false
	}
	err := execute.Preflight("security")
	if err != nil {
		return err.Error(), false
	}
	return "", true
}

// Store writes the token as a generic password, replacing an earlier one.
func (System) Store(token ports.Token) error {
	encoded, err := json.Marshal(token)
	if err != nil {
		return err
	}
	_, err = execute.Run("", "security", "add-generic-password", "-U", "-s", service, "-a", token.Login, "-l", "Agents Rein: GitHub token of "+token.Login, "-w", string(encoded))
	return err
}

// Load reads the token of a login.
func (System) Load(login string) (ports.Token, bool, error) {
	result, err := execute.Run("", "security", "find-generic-password", "-s", service, "-a", login, "-w")
	var failure *execute.Failure
	if errors.As(err, &failure) && strings.Contains(failure.Stderr, "could not be found") {
		return ports.Token{}, false, nil
	}
	if err != nil {
		return ports.Token{}, false, err
	}
	var token ports.Token
	err = json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &token)
	if err != nil {
		return ports.Token{}, false, err
	}
	return token, true, nil
}

// Delete removes the token of a login; a missing one is not an error.
func (System) Delete(login string) error {
	_, err := execute.Run("", "security", "delete-generic-password", "-s", service, "-a", login)
	var failure *execute.Failure
	if errors.As(err, &failure) && strings.Contains(failure.Stderr, "could not be found") {
		return nil
	}
	return err
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
