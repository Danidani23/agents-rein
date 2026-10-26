//go:build !windows

package keychain

import "errors"

// credentialManager is Windows's; elsewhere it is never chosen.
type credentialManager struct{}

var errNotWindows = errors.New("Credential Manager is Windows's")

func (credentialManager) available() (string, bool) {
	return errNotWindows.Error(), false
}

func (credentialManager) put(string, string, string) error {
	return errNotWindows
}

func (credentialManager) get(string) (string, bool, error) {
	return "", false, errNotWindows
}

func (credentialManager) remove(string) error {
	return errNotWindows
}
