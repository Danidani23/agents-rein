// Package localstate persists the machine model as state.yaml under the
// folder REIN_HOME names, by default ~/.rein, which also holds the clones
// under orgs/ and the snapshots.
package localstate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
)

// HomeVariable overrides the folder.
const HomeVariable = "REIN_HOME"

const stateFile = "state.yaml"

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

// Load reads state.yaml; a missing file is an empty state.
func (store Store) Load() (machine.State, error) {
	state := machine.Empty()
	content, err := os.ReadFile(filepath.Join(store.Home, stateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return machine.State{}, err
	}
	err = yaml.Unmarshal(content, &state)
	if err != nil {
		return machine.State{}, fmt.Errorf("%s: %w", filepath.Join(store.Home, stateFile), err)
	}
	if state.Clones == nil {
		state.Clones = map[string]string{}
	}
	return state, nil
}

// Save writes state.yaml, creating the home folder.
func (store Store) Save(state machine.State) error {
	path := filepath.Join(store.Home, stateFile)
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return err
	}
	content, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
