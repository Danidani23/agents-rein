// Package vault is rein's encrypted store (D51): one bbolt file, store.db in
// rein's home, that holds rein's own records as documents under the names
// their files had. Every value is encrypted with AES-GCM under a key kept in
// the system's keychain, bound to its document's name, so a value changed or
// moved under another name fails to open. On a machine without a keychain
// the key is a file beside the store, readable by the developer alone.
//
// The store is opened for each operation and closed after it, one operation
// at a time in this process, so another process, such as a background
// process of another build finishing its commands, waits only for an
// operation, and no file stays open. The first operation moves the files of
// an earlier rein in: it writes them into a new store, reads them back,
// puts the store in place, and only then removes the files. A store that is
// gone, or that does not match its key, is named, never replaced by an
// empty one.
package vault

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/Danidani23/agents-rein/internal/ports"
)

const (
	storeFile = "store.db"
	// newStoreFile is the store being made by a move, until it is complete.
	newStoreFile = "store.db.new"
	// keyFile holds the key where the machine has no keychain.
	keyFile = "store.key"
	// openWait is how long an operation waits for another process to close
	// the store.
	openWait = time.Minute
)

var (
	documentsBucket = []byte("documents")
	metaBucket      = []byte("meta")
	checkKey        = []byte("check")
	// checkText is what the check value holds, encrypted, so a wrong key is
	// told from a changed document.
	checkText = []byte("rein's store")
)

// legacyFiles are the files of an earlier rein that move into the store,
// each under its name; the folders move with every file in them.
var legacyFiles = []string{"state.yaml", "removals.yaml"}

var legacyFolders = []string{"inserts", "snapshots"}

// leftBehind are files of an earlier rein that no longer serve once the
// store holds what they held.
var leftBehind = []string{"state.yaml.previous"}

// Store is the encrypted store of one rein home. Keychain keeps its key.
type Store struct {
	Home     string
	Keychain ports.Keychain
}

var _ ports.Documents = Store{}

// processes keeps, per store, the lock that lets one operation of this
// process at a time open it, and its key once read.
var processes = struct {
	mutex  sync.Mutex
	stores map[string]*opened
}{stores: map[string]*opened{}}

type opened struct {
	mutex sync.Mutex
	key   []byte
}

func (store Store) path() string {
	return filepath.Join(store.Home, storeFile)
}

// Where names the store's file.
func (store Store) Where() string {
	return store.path()
}

// keyName is the keychain's name for this home's key, so two homes on one
// machine, such as a test's, never share a key.
func (store Store) keyName() string {
	sum := sha256.Sum256([]byte(store.Home))
	return "REIN_STORE_" + strings.ToUpper(hex.EncodeToString(sum[:4]))
}

func (store Store) process() *opened {
	processes.mutex.Lock()
	defer processes.mutex.Unlock()
	current, found := processes.stores[store.path()]
	if !found {
		current = &opened{}
		processes.stores[store.path()] = current
	}
	return current
}

// Read returns a document.
func (store Store) Read(name string) (content []byte, found bool, err error) {
	err = store.within(false, func(database *bolt.DB, sealer cipher.AEAD) error {
		return database.View(func(transaction *bolt.Tx) error {
			sealed := transaction.Bucket(documentsBucket).Get([]byte(name))
			if sealed == nil {
				return nil
			}
			found = true
			content, err = open(sealer, name, sealed)
			return err
		})
	})
	return content, found, err
}

// Write creates or replaces a document.
func (store Store) Write(name string, content []byte) error {
	return store.within(true, func(database *bolt.DB, sealer cipher.AEAD) error {
		return database.Update(func(transaction *bolt.Tx) error {
			sealed, err := seal(sealer, name, content)
			if err != nil {
				return err
			}
			return transaction.Bucket(documentsBucket).Put([]byte(name), sealed)
		})
	})
}

// Remove deletes a document.
func (store Store) Remove(name string) error {
	return store.within(false, func(database *bolt.DB, sealer cipher.AEAD) error {
		return database.Update(func(transaction *bolt.Tx) error {
			return transaction.Bucket(documentsBucket).Delete([]byte(name))
		})
	})
}

// List names the documents under prefix.
func (store Store) List(prefix string) (names []string, err error) {
	err = store.within(false, func(database *bolt.DB, sealer cipher.AEAD) error {
		return database.View(func(transaction *bolt.Tx) error {
			cursor := transaction.Bucket(documentsBucket).Cursor()
			for name, _ := cursor.Seek([]byte(prefix)); name != nil && bytes.HasPrefix(name, []byte(prefix)); name, _ = cursor.Next() {
				names = append(names, string(name))
			}
			return nil
		})
	})
	return names, err
}

// Erase removes the store, a move left unfinished and the key.
func (store Store) Erase() error {
	current := store.process()
	current.mutex.Lock()
	defer current.mutex.Unlock()
	for _, name := range []string{storeFile, newStoreFile, keyFile} {
		err := os.Remove(filepath.Join(store.Home, name))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	current.key = nil
	if store.Keychain == nil {
		return nil
	}
	_, available := store.Keychain.Available()
	if !available {
		return nil
	}
	return store.Keychain.DeleteKey(store.keyName())
}

// within runs work on the open store. A store that is not there yet is
// made when work writes, or when there are files to move in; a read of a
// store that was never made runs on nothing and finds nothing.
func (store Store) within(writes bool, work func(database *bolt.DB, sealer cipher.AEAD) error) error {
	current := store.process()
	current.mutex.Lock()
	defer current.mutex.Unlock()
	_, statErr := os.Stat(store.path())
	if errors.Is(statErr, fs.ErrNotExist) {
		made, err := store.make(current, writes)
		if err != nil {
			return err
		}
		if !made {
			return nil
		}
	} else if statErr != nil {
		return statErr
	}
	key, err := store.key(current)
	if err != nil {
		return err
	}
	if key == nil {
		return fmt.Errorf("rein's store %s has no key in the keychain, so it cannot be read; rein reset machine removes it and starts over", store.path())
	}
	database, sealer, err := openStore(store.path(), key)
	if err != nil {
		return err
	}
	defer database.Close()
	return work(database, sealer)
}

// make makes the store when it is missing: with the files of an earlier
// rein moved in when there are some, empty when writes asks for it. A key
// with no store and nothing to move means the store was lost.
func (store Store) make(current *opened, writes bool) (made bool, err error) {
	legacy, err := store.legacyDocuments()
	if err != nil {
		return false, err
	}
	key, err := store.key(current)
	if err != nil {
		return false, err
	}
	_, unfinished := os.Stat(filepath.Join(store.Home, newStoreFile))
	if key != nil && len(legacy) == 0 && unfinished != nil {
		return false, fmt.Errorf("rein's store %s is gone, though its key is in the keychain; rein reset machine starts over, after which init and connect record the machine again", store.path())
	}
	if len(legacy) == 0 && !writes {
		return false, nil
	}
	if key == nil {
		key, err = store.newKey(current)
		if err != nil {
			return false, err
		}
	}
	err = store.moveIn(key, legacy)
	return err == nil, err
}

// moveIn writes the documents into a new store, reads them back, puts the
// store in place and removes the files they came from.
func (store Store) moveIn(key []byte, legacy map[string][]byte) error {
	newPath := filepath.Join(store.Home, newStoreFile)
	_ = os.Remove(newPath)
	database, sealer, err := createStore(newPath, key)
	if err != nil {
		return err
	}
	err = database.Update(func(transaction *bolt.Tx) error {
		for name, content := range legacy {
			sealed, err := seal(sealer, name, content)
			if err != nil {
				return err
			}
			err = transaction.Bucket(documentsBucket).Put([]byte(name), sealed)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		err = database.View(func(transaction *bolt.Tx) error {
			for name, content := range legacy {
				readBack, err := open(sealer, name, transaction.Bucket(documentsBucket).Get([]byte(name)))
				if err != nil {
					return err
				}
				if !bytes.Equal(readBack, content) {
					return fmt.Errorf("%s read back differently from the store", name)
				}
			}
			return nil
		})
	}
	closeErr := database.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("moving rein's files into its store: %w; the files are as they were", err)
	}
	err = os.Rename(newPath, store.path())
	if err != nil {
		return err
	}
	for name := range legacy {
		err = os.Remove(filepath.Join(store.Home, filepath.FromSlash(name)))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("moved into rein's store %s, but %w; remove the file", store.path(), err)
		}
	}
	for _, folder := range legacyFolders {
		_ = os.Remove(filepath.Join(store.Home, folder))
	}
	for _, name := range leftBehind {
		_ = os.Remove(filepath.Join(store.Home, name))
	}
	return nil
}

// legacyDocuments reads the files of an earlier rein, by document name.
func (store Store) legacyDocuments() (map[string][]byte, error) {
	documents := map[string][]byte{}
	for _, name := range legacyFiles {
		content, err := os.ReadFile(filepath.Join(store.Home, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		documents[name] = content
	}
	for _, folder := range legacyFolders {
		entries, err := os.ReadDir(filepath.Join(store.Home, folder))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			content, err := os.ReadFile(filepath.Join(store.Home, folder, entry.Name()))
			if err != nil {
				return nil, err
			}
			documents[folder+"/"+entry.Name()] = content
		}
	}
	return documents, nil
}

// key is the store's key, from the keychain or, without one, the key file;
// nil when there is none yet.
func (store Store) key(current *opened) ([]byte, error) {
	if current.key != nil {
		return current.key, nil
	}
	var encoded string
	if store.keychainAvailable() {
		value, found, err := store.Keychain.LoadKey(store.keyName())
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, nil
		}
		encoded = value
	} else {
		content, err := os.ReadFile(filepath.Join(store.Home, keyFile))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		encoded = string(content)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("the key of rein's store %s is not one rein made; rein reset machine starts over", store.path())
	}
	current.key = key
	return key, nil
}

// newKey makes a key and keeps it.
func (store Store) newKey(current *opened) ([]byte, error) {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	if err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	if store.keychainAvailable() {
		err = store.Keychain.StoreKey(store.keyName(), encoded)
	} else {
		err = os.MkdirAll(store.Home, 0o755)
		if err == nil {
			err = os.WriteFile(filepath.Join(store.Home, keyFile), []byte(encoded+"\n"), 0o600)
		}
	}
	if err != nil {
		return nil, err
	}
	current.key = key
	return key, nil
}

func (store Store) keychainAvailable() bool {
	if store.Keychain == nil {
		return false
	}
	_, available := store.Keychain.Available()
	return available
}

// createStore makes a store at path with its buckets and the check value.
func createStore(path string, key []byte) (*bolt.DB, cipher.AEAD, error) {
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return nil, nil, err
	}
	sealer, err := newSealer(key)
	if err != nil {
		return nil, nil, err
	}
	database, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: openWait})
	if err != nil {
		return nil, nil, err
	}
	err = database.Update(func(transaction *bolt.Tx) error {
		meta, err := transaction.CreateBucketIfNotExists(metaBucket)
		if err != nil {
			return err
		}
		_, err = transaction.CreateBucketIfNotExists(documentsBucket)
		if err != nil {
			return err
		}
		sealed, err := seal(sealer, string(checkKey), checkText)
		if err != nil {
			return err
		}
		return meta.Put(checkKey, sealed)
	})
	if err != nil {
		database.Close()
		return nil, nil, err
	}
	return database, sealer, nil
}

// openStore opens the store at path and proves the key opens it.
func openStore(path string, key []byte) (*bolt.DB, cipher.AEAD, error) {
	sealer, err := newSealer(key)
	if err != nil {
		return nil, nil, err
	}
	database, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: openWait})
	if errors.Is(err, bolt.ErrTimeout) {
		return nil, nil, fmt.Errorf("rein's store %s stayed open in another rein for %s; its log is owner.log in rein's home", path, openWait)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("rein's store %s cannot be opened: %w; rein reset machine starts over", path, err)
	}
	err = database.View(func(transaction *bolt.Tx) error {
		meta := transaction.Bucket(metaBucket)
		if meta == nil || transaction.Bucket(documentsBucket) == nil {
			return errors.New("it is not a store rein made")
		}
		checked, err := open(sealer, string(checkKey), meta.Get(checkKey))
		if err != nil || !bytes.Equal(checked, checkText) {
			return errors.New("it does not match its key")
		}
		return nil
	})
	if err != nil {
		database.Close()
		return nil, nil, fmt.Errorf("rein's store %s cannot be read: %w; rein reset machine starts over", path, err)
	}
	return database, sealer, nil
}

func newSealer(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// seal encrypts content bound to name: a fresh nonce, then the ciphertext.
func seal(sealer cipher.AEAD, name string, content []byte) ([]byte, error) {
	nonce := make([]byte, sealer.NonceSize())
	_, err := rand.Read(nonce)
	if err != nil {
		return nil, err
	}
	return sealer.Seal(nonce, nonce, content, []byte(name)), nil
}

// open decrypts a value sealed for name; a value changed, or sealed for
// another name, is an error.
func open(sealer cipher.AEAD, name string, sealed []byte) ([]byte, error) {
	if len(sealed) < sealer.NonceSize() {
		return nil, fmt.Errorf("the document %s in rein's store is damaged", name)
	}
	content, err := sealer.Open(nil, sealed[:sealer.NonceSize()], sealed[sealer.NonceSize():], []byte(name))
	if err != nil {
		return nil, fmt.Errorf("the document %s in rein's store was changed outside rein, or is damaged", name)
	}
	return content, nil
}
