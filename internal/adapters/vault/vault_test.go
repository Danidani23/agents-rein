package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
)

func newStore(t *testing.T) (Store, *fake.Keychain) {
	t.Helper()
	keychain := &fake.Keychain{}
	return Store{Home: t.TempDir(), Keychain: keychain}, keychain
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
}

// rawValue is a document's value as it lies in the file.
func rawValue(t *testing.T, store Store, name string) []byte {
	t.Helper()
	database, err := bolt.Open(store.path(), 0o600, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()
	var value []byte
	_ = database.View(func(transaction *bolt.Tx) error {
		value = append(value, transaction.Bucket(documentsBucket).Get([]byte(name))...)
		return nil
	})
	return value
}

func setRawValue(t *testing.T, store Store, name string, value []byte) {
	t.Helper()
	database, err := bolt.Open(store.path(), 0o600, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()
	err = database.Update(func(transaction *bolt.Tx) error {
		return transaction.Bucket(documentsBucket).Put([]byte(name), value)
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
}

func TestStore(t *testing.T) {
	t.Run("a read of a store never made finds nothing and makes nothing", func(t *testing.T) {
		store, keychain := newStore(t)
		_, found, err := store.Read("state.yaml")
		names, listErr := store.List("snapshots/")
		if err != nil || found || listErr != nil || len(names) != 0 {
			t.Fatalf("found %v err %v names %v list err %v", found, err, names, listErr)
		}
		err = store.Remove("state.yaml")
		_, statErr := os.Stat(store.path())
		if err != nil || statErr == nil || len(keychain.Keys) != 0 {
			t.Fatalf("remove err %v, store made %v, keys %v", err, statErr == nil, keychain.Keys)
		}
	})
	t.Run("a document written is read back, listed by its prefix and removed", func(t *testing.T) {
		store, keychain := newStore(t)
		for name, content := range map[string]string{"state.yaml": "version: 3\n", "snapshots/shop-a.json": "{}", "snapshots/shop-b.json": "[]", "inserts/id-1.yaml": "clones: {}\n"} {
			err := store.Write(name, []byte(content))
			if err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}
		content, found, err := store.Read("state.yaml")
		if err != nil || !found || string(content) != "version: 3\n" {
			t.Fatalf("read %q %v %v", content, found, err)
		}
		names, err := store.List("snapshots/")
		if err != nil || strings.Join(names, " ") != "snapshots/shop-a.json snapshots/shop-b.json" {
			t.Fatalf("names %v err %v", names, err)
		}
		err = store.Remove("snapshots/shop-a.json")
		names, _ = store.List("snapshots/")
		if err != nil || len(names) != 1 {
			t.Fatalf("after remove: %v %v", names, err)
		}
		if len(keychain.Keys) != 1 || keychain.Keys[store.keyName()] == "" {
			t.Fatalf("the key is not in the keychain: %v", keychain.Keys)
		}
		if runtime.GOOS != "windows" {
			info, _ := os.Stat(store.path())
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("mode %v", info.Mode())
			}
		}
	})
	t.Run("every value is encrypted, and one changed or moved under another name fails to open", func(t *testing.T) {
		store, _ := newStore(t)
		_ = store.Write("state.yaml", []byte("active: acme\n"))
		_ = store.Write("removals.yaml", []byte("version: 1\n"))
		file, _ := os.ReadFile(store.path())
		if bytes.Contains(file, []byte("active: acme")) {
			t.Fatalf("the store holds the text in the clear")
		}
		changed := rawValue(t, store, "state.yaml")
		changed[len(changed)-1] ^= 1
		setRawValue(t, store, "state.yaml", changed)
		_, _, err := store.Read("state.yaml")
		if err == nil || !strings.Contains(err.Error(), "the document state.yaml in rein's store was changed outside rein") {
			t.Fatalf("changed: %v", err)
		}
		setRawValue(t, store, "state.yaml", rawValue(t, store, "removals.yaml"))
		_, _, err = store.Read("state.yaml")
		if err == nil || !strings.Contains(err.Error(), "changed outside rein") {
			t.Fatalf("moved: %v", err)
		}
	})
	t.Run("the files of an earlier rein move in, are read back, and only then go", func(t *testing.T) {
		store, _ := newStore(t)
		writeFile(t, filepath.Join(store.Home, "state.yaml"), "active: acme\n")
		writeFile(t, filepath.Join(store.Home, "state.yaml.previous"), "active: old\n")
		writeFile(t, filepath.Join(store.Home, "removals.yaml"), "version: 1\n")
		writeFile(t, filepath.Join(store.Home, "inserts", "id-1.yaml"), "clones: {}\n")
		writeFile(t, filepath.Join(store.Home, "snapshots", "shop-claude-code-20261029-100000.json"), `{"agent":"claude-code"}`)
		writeFile(t, filepath.Join(store.Home, "state.lock"), "4242")
		writeFile(t, filepath.Join(store.Home, "orgs", "id-1", "README.md"), "a clone")
		content, found, err := store.Read("state.yaml")
		if err != nil || !found || string(content) != "active: acme\n" {
			t.Fatalf("state %q %v %v", content, found, err)
		}
		for name, expected := range map[string]string{"removals.yaml": "version: 1\n", "inserts/id-1.yaml": "clones: {}\n", "snapshots/shop-claude-code-20261029-100000.json": `{"agent":"claude-code"}`} {
			content, found, err := store.Read(name)
			if err != nil || !found || string(content) != expected {
				t.Fatalf("%s: %q %v %v", name, content, found, err)
			}
		}
		for _, gone := range []string{"state.yaml", "state.yaml.previous", "removals.yaml", "inserts", "snapshots", newStoreFile} {
			_, err := os.Stat(filepath.Join(store.Home, gone))
			if err == nil {
				t.Fatalf("%s stayed", gone)
			}
		}
		for _, kept := range []string{"state.lock", filepath.Join("orgs", "id-1", "README.md")} {
			_, err := os.Stat(filepath.Join(store.Home, kept))
			if err != nil {
				t.Fatalf("%s went: %v", kept, err)
			}
		}
	})
	t.Run("a store gone, without its key, or not matching it is named, never replaced by an empty one", func(t *testing.T) {
		store, keychain := newStore(t)
		_ = store.Write("state.yaml", []byte("active: acme\n"))
		forget(store)
		err := os.Remove(store.path())
		if err != nil {
			t.Fatalf("remove: %v", err)
		}
		_, _, err = store.Read("state.yaml")
		_, statErr := os.Stat(store.path())
		if err == nil || !strings.Contains(err.Error(), "is gone, though its key is in the keychain") || statErr == nil {
			t.Fatalf("gone: %v, made again %v", err, statErr == nil)
		}
		err = store.Write("state.yaml", []byte("active: acme\n"))
		if err == nil || !strings.Contains(err.Error(), "is gone") {
			t.Fatalf("a write over a lost store: %v", err)
		}

		store, keychain = newStore(t)
		_ = store.Write("state.yaml", []byte("active: acme\n"))
		forget(store)
		keychain.Keys = nil
		_, _, err = store.Read("state.yaml")
		if err == nil || !strings.Contains(err.Error(), "has no key in the keychain") {
			t.Fatalf("no key: %v", err)
		}

		store, keychain = newStore(t)
		_ = store.Write("state.yaml", []byte("active: acme\n"))
		forget(store)
		keychain.Keys[store.keyName()] = "ZmFrZS1rZXktb2YtdGhpcnR5LXR3by1ieXRlcy0tLS0="
		_, _, err = store.Read("state.yaml")
		if err == nil || !strings.Contains(err.Error(), "does not match its key") {
			t.Fatalf("another key: %v", err)
		}
	})
	t.Run("erase removes the store and its key, and the next write makes new ones", func(t *testing.T) {
		store, keychain := newStore(t)
		_ = store.Write("state.yaml", []byte("active: acme\n"))
		before := keychain.Keys[store.keyName()]
		err := store.Erase()
		_, statErr := os.Stat(store.path())
		if err != nil || statErr == nil || len(keychain.Keys) != 0 {
			t.Fatalf("erase %v, store stayed %v, keys %v", err, statErr == nil, keychain.Keys)
		}
		_, found, err := store.Read("state.yaml")
		if err != nil || found {
			t.Fatalf("after erase: %v %v", found, err)
		}
		err = store.Write("state.yaml", []byte("active: other\n"))
		if err != nil || keychain.Keys[store.keyName()] == before {
			t.Fatalf("write after erase: %v, the same key %v", err, keychain.Keys[store.keyName()] == before)
		}
	})
	t.Run("without a keychain the key is a file only the developer reads", func(t *testing.T) {
		keychain := &fake.Keychain{Unavailable: "no keyring in this session"}
		store := Store{Home: t.TempDir(), Keychain: keychain}
		err := store.Write("state.yaml", []byte("active: acme\n"))
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		info, err := os.Stat(filepath.Join(store.Home, keyFile))
		if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
			t.Fatalf("key file %v %v", info, err)
		}
		forget(store)
		content, found, err := store.Read("state.yaml")
		if err != nil || !found || string(content) != "active: acme\n" {
			t.Fatalf("read %q %v %v", content, found, err)
		}
		err = store.Erase()
		_, keyErr := os.Stat(filepath.Join(store.Home, keyFile))
		if err != nil || keyErr == nil {
			t.Fatalf("erase %v, key file stayed %v", err, keyErr == nil)
		}
	})
}

// forget drops the key this process read, as a new process starts.
func forget(store Store) {
	store.process().key = nil
}
