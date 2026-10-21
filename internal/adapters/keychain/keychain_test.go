package keychain

import (
	"os"
	"path/filepath"
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
	if info.Mode().Perm() != 0o600 {
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
