package keychain

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

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
