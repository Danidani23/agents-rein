//go:build windows

package keychain

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// Credential Manager's calls in advapi32, reached through the standard
// library alone
// ([docs](https://learn.microsoft.com/en-us/windows/win32/api/wincred/nf-wincred-credwritew)).
var (
	advapi32   = syscall.NewLazyDLL("advapi32.dll")
	credWrite  = advapi32.NewProc("CredWriteW")
	credRead   = advapi32.NewProc("CredReadW")
	credDelete = advapi32.NewProc("CredDeleteW")
	credFree   = advapi32.NewProc("CredFree")
)

// The values of wincred.h rein uses
// ([docs](https://learn.microsoft.com/en-us/windows/win32/api/wincred/ns-wincred-credentialw)).
const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	// maxBlobSize is CRED_MAX_CREDENTIAL_BLOB_SIZE, 5*512 bytes.
	maxBlobSize = 5 * 512
	// errorNotFound is ERROR_NOT_FOUND, what a missing credential returns.
	errorNotFound = syscall.Errno(1168)
)

// credential is CREDENTIALW, field for field.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

// credentialManager is Windows's Credential Manager: a secret as a generic
// credential named agents-rein:<account>, kept on this machine for this
// user.
type credentialManager struct{}

func target(account string) (*uint16, error) {
	return syscall.UTF16PtrFromString(service + ":" + account)
}

func (credentialManager) available() (string, bool) {
	err := credRead.Find()
	if err != nil {
		return "Credential Manager cannot be reached: " + err.Error(), false
	}
	return "", true
}

func (credentialManager) put(account string, label string, secret string) error {
	if len(secret) > maxBlobSize {
		return fmt.Errorf("Credential Manager holds at most %d bytes in one credential; this secret has %d", maxBlobSize, len(secret))
	}
	name, err := target(account)
	if err != nil {
		return err
	}
	comment, err := syscall.UTF16PtrFromString(label)
	if err != nil {
		return err
	}
	blob := []byte(secret)
	entry := credential{Type: credTypeGeneric, TargetName: name, Comment: comment, CredentialBlobSize: uint32(len(blob)), Persist: credPersistLocalMachine}
	if len(blob) > 0 {
		entry.CredentialBlob = &blob[0]
	}
	written, _, callErr := credWrite.Call(uintptr(unsafe.Pointer(&entry)), 0)
	if written == 0 {
		return fmt.Errorf("Credential Manager did not keep %s:%s: %w", service, account, callErr)
	}
	return nil
}

func (credentialManager) get(account string) (string, bool, error) {
	name, err := target(account)
	if err != nil {
		return "", false, err
	}
	var found *credential
	read, _, callErr := credRead.Call(uintptr(unsafe.Pointer(name)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&found)))
	if read == 0 {
		if errors.Is(callErr, errorNotFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("Credential Manager did not read %s:%s: %w", service, account, callErr)
	}
	defer credFree.Call(uintptr(unsafe.Pointer(found)))
	if found.CredentialBlobSize == 0 {
		return "", true, nil
	}
	return string(unsafe.Slice(found.CredentialBlob, found.CredentialBlobSize)), true, nil
}

func (credentialManager) remove(account string) error {
	name, err := target(account)
	if err != nil {
		return err
	}
	deleted, _, callErr := credDelete.Call(uintptr(unsafe.Pointer(name)), credTypeGeneric, 0)
	if deleted == 0 && !errors.Is(callErr, errorNotFound) {
		return fmt.Errorf("Credential Manager did not remove %s:%s: %w", service, account, callErr)
	}
	return nil
}
