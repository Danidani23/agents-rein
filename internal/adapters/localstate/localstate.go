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
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
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

const header = "# Written by rein: what it knows about this machine. Edit through rein commands, not by hand;\n# rein status shows it, rein reset machine empties it. The previous copy is state.yaml.previous.\n"

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
	content, err = migrate(content)
	if err != nil {
		return machine.State{}, fmt.Errorf("%s: %w; the previous copy is %s", path, err, previousFile)
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

// legacyKeys are the keys earlier builds wrote, by their names today; a
// file holding them is read as if it used the new names and is written with
// the new names at the next save.
var legacyKeys = map[string]string{"clones": "org_clone_paths", "projects": "project_clones"}

// migrate renames the legacy keys of an old state file.
func migrate(content []byte) ([]byte, error) {
	var generic map[string]any
	err := yaml.Unmarshal(content, &generic)
	if err != nil || generic == nil {
		return content, nil
	}
	changed := false
	for old, current := range legacyKeys {
		value, present := generic[old]
		if !present {
			continue
		}
		if _, taken := generic[current]; !taken {
			generic[current] = value
		}
		delete(generic, old)
		changed = true
	}
	if !changed {
		return content, nil
	}
	return yaml.Marshal(generic)
}

// errEmptyDocument lets an empty file count as an empty state.
func errEmptyDocument(err error) error {
	if err.Error() == "EOF" {
		return err
	}
	return errors.New("not empty")
}

// Save writes state.yaml whole: the previous copy is kept and the new
// content goes to a temporary file first and is renamed into place. The
// lock against another rein is the command's, taken through Acquire.
func (store Store) Save(state machine.State) error {
	err := os.MkdirAll(store.Home, 0o755)
	if err != nil {
		return err
	}
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

// lockRetry is how often Acquire tries again while it waits.
const lockRetry = 200 * time.Millisecond

var _ ports.MachineLock = Store{}

// Acquire takes the lock file for the rest of the command, waiting up to
// wait for another rein to release it; a lock older than lockStale is taken
// over as left behind by a rein that died.
func (store Store) Acquire(wait time.Duration) (release func(), err error) {
	err = os.MkdirAll(store.Home, 0o755)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(store.Home, lockFile)
	deadline := time.Now().Add(wait)
	for {
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
		if pid, parseErr := strconv.Atoi(strings.TrimSpace(string(holder))); parseErr == nil && !processAlive(pid) {
			os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			holder, _ := os.ReadFile(path)
			return nil, fmt.Errorf("another rein is running (pid %s holds %s); wait for it, or remove the lock if that rein is gone", strings.TrimSpace(string(holder)), path)
		}
		time.Sleep(lockRetry)
	}
}

// processAlive says whether a process with the pid is running: a signal of
// zero reaches a live one, and a live one of another user answers that the
// permission is missing, which is still alive. Windows knows no signal of
// zero; there finding the process opens it, which fails once it is gone.
func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		_ = process.Release()
		return true
	}
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
