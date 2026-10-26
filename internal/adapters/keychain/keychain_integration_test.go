//go:build keychain

package keychain

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/ports"
)

// TestSystemKeychain round-trips a token and a key through this machine's
// real keychain, then removes them. It runs only with the keychain build
// tag, through task rein:test-keychain, because it writes to the keychain;
// REIN_KEYCHAIN_REQUIRED, as CI sets it, fails a machine without one.
func TestSystemKeychain(t *testing.T) {
	system := System{}
	reason, available := system.Available()
	if !available {
		if os.Getenv("REIN_KEYCHAIN_REQUIRED") != "" {
			t.Fatalf("no keychain: %s", reason)
		}
		t.Skipf("no keychain: %s", reason)
	}
	login := fmt.Sprintf("rein-test-%d", time.Now().UnixNano())
	name := fmt.Sprintf("REIN_TEST_KEY_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = system.Delete(login)
		_ = system.DeleteKey(name)
	})
	token := ports.Token{Login: login, AccessToken: "token-of-the-test", ExpiresAt: time.Now().Add(time.Hour).Truncate(time.Second).UTC()}
	err := system.Store(token)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	loaded, found, err := system.Load(login)
	if err != nil || !found || loaded.AccessToken != token.AccessToken || !loaded.ExpiresAt.Equal(token.ExpiresAt) {
		t.Fatalf("load %+v found %v err %v", loaded, found, err)
	}
	err = system.StoreKey(name, "sk-of-the-test")
	value, found, loadErr := system.LoadKey(name)
	if err != nil || loadErr != nil || !found || value != "sk-of-the-test" {
		t.Fatalf("key %q found %v err %v %v", value, found, err, loadErr)
	}
	// a second store replaces the first
	err = system.StoreKey(name, "sk-replaced")
	value, _, _ = system.LoadKey(name)
	if err != nil || value != "sk-replaced" {
		t.Fatalf("replace %q err %v", value, err)
	}
	err = system.Delete(login)
	_, found, _ = system.Load(login)
	if err != nil || found {
		t.Fatalf("delete token: %v found %v", err, found)
	}
	err = system.DeleteKey(name)
	_, found, _ = system.LoadKey(name)
	if err != nil || found {
		t.Fatalf("delete key: %v found %v", err, found)
	}
	// deleting what is not there is no error
	if err := system.DeleteKey(name); err != nil {
		t.Fatalf("delete twice: %v", err)
	}
}
