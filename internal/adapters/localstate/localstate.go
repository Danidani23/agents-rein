// Package localstate persists the machine model as state.yaml under the
// folder REIN_HOME names, by default ~/.rein, which also holds the clones,
// the snapshots, the ledger and the distillations. The file is rein's, not
// the user's (D19): it carries a version, is validated when read, is written
// whole through a temporary file and a rename under a lock, and the previous
// copy is kept beside it.
package localstate

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
)

// HomeVariable overrides the folder.
const HomeVariable = "REIN_HOME"

const (
	stateFile    = "state.yaml"
	previousFile = "state.yaml.previous"
	lockFile     = "state.lock"
	// lockStale is how old a lock may be before it counts as left behind by
	// a rein that died.
	lockStale = 10 * time.Minute
)

const header = "# Written by rein: what it knows about this machine. Edit through rein commands, not by hand;\n# rein status shows it, rein reset empties it. The previous copy is state.yaml.previous.\n"

// Home is the folder of state.yaml, the clones and the snapshots.
func Home() (string, error) {
	value := os.Getenv(HomeVariable)
	if value != "" {
		return value, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userHome, ".rein"), nil
}

// Store reads and writes the state of one home folder. It is the State port.
type Store struct {
	Home string
}

// Load reads state.yaml; a missing file is an empty state, an unknown field
// or a newer version is an error that names the file.
func (store Store) Load() (machine.State, error) {
	state := machine.Empty()
	path := filepath.Join(store.Home, stateFile)
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return machine.State{}, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	err = decoder.Decode(&state)
	if err != nil && !errors.Is(err, errEmptyDocument(err)) {
		return machine.State{}, fmt.Errorf("%s: %w; the previous copy is %s", path, err, previousFile)
	}
	if state.Version == 0 {
		// a file from before the version field (P2.23) is the first version
		state.Version = machine.StateVersion
	}
	if state.Version > machine.StateVersion {
		return machine.State{}, fmt.Errorf("%s is version %d, written by a newer rein; this build reads version %d", path, state.Version, machine.StateVersion)
	}
	if state.OrgClonePaths == nil {
		state.OrgClonePaths = map[string]string{}
	}
	return state, nil
}

// errEmptyDocument lets an empty file count as an empty state.
func errEmptyDocument(err error) error {
	if err.Error() == "EOF" {
		return err
	}
	return errors.New("not empty")
}

// Save writes state.yaml whole: the previous copy is kept, the new content
// goes to a temporary file first and is renamed into place, and a lock
// keeps two rein processes from writing at once.
func (store Store) Save(state machine.State) error {
	err := os.MkdirAll(store.Home, 0o755)
	if err != nil {
		return err
	}
	unlock, err := store.lock()
	if err != nil {
		return err
	}
	defer unlock()
	state.Version = machine.StateVersion
	content, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	path := filepath.Join(store.Home, stateFile)
	current, err := os.ReadFile(path)
	if err == nil {
		err = os.WriteFile(filepath.Join(store.Home, previousFile), current, 0o644)
		if err != nil {
			return err
		}
	}
	temporary, err := os.CreateTemp(store.Home, ".state-*")
	if err != nil {
		return err
	}
	_, err = temporary.Write(append([]byte(header), content...))
	if err != nil {
		temporary.Close()
		os.Remove(temporary.Name())
		return err
	}
	err = temporary.Close()
	if err != nil {
		os.Remove(temporary.Name())
		return err
	}
	return os.Rename(temporary.Name(), path)
}

// lock takes the lock file, or says which rein holds it; a lock older than
// lockStale is taken over.
func (store Store) lock() (unlock func(), err error) {
	path := filepath.Join(store.Home, lockFile)
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_, _ = file.WriteString(strconv.Itoa(os.Getpid()))
			file.Close()
			return func() { os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		info, statErr := os.Stat(path)
		if statErr == nil && time.Since(info.ModTime()) > lockStale {
			os.Remove(path)
			continue
		}
		holder, _ := os.ReadFile(path)
		return nil, fmt.Errorf("another rein is writing the state (pid %s holds %s); try again, or remove the lock if that rein is gone", string(holder), path)
	}
	return nil, fmt.Errorf("could not take %s", path)
}
