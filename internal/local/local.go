// Package local holds what rein keeps on one machine: state.yaml under the
// folder REIN_HOME names, by default ~/.rein. It is a cache (D9): the active
// organisation, the connected organisations, the project registrations and
// the placements rein made, all rebuildable from the clones and from sync.
// Identity is never stored; it comes from gh each time.
package local

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// HomeVariable overrides the folder of the state file.
const HomeVariable = "REIN_HOME"

const stateFile = "state.yaml"

// AgentClaudeCode is the one agent this build writes for.
const AgentClaudeCode = "claude-code"

// SupportedAgents lists the agents rein can write settings for.
var SupportedAgents = []string{AgentClaudeCode}

// Home is the folder of state.yaml, the clones and the imports.
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

// OrganizationRef is a connected organisation.
type OrganizationRef struct {
	ID     string `yaml:"id"`
	Slug   string `yaml:"slug"`
	Remote string `yaml:"remote"`
}

// Placement is one link or copy rein made in the agent's home. Owner is the
// command that made it and Source what it placed it from, the folder or the
// export; a command may replace only its own placements from the same
// source, so re-importing an edited folder refreshes it while a different
// level that claims the same id is refused.
type Placement struct {
	Path   string `yaml:"path"`
	Target string `yaml:"target"`
	Mode   string `yaml:"mode"`
	Owner  string `yaml:"owner,omitempty"`
	Source string `yaml:"source,omitempty"`
}

// OwnerImport marks a placement made by rein import.
const OwnerImport = "import"

// Project is a project repository registered on this machine.
type Project struct {
	Organization string `yaml:"organization"`
	Address      string `yaml:"address"`
	ClonePath    string `yaml:"clone_path"`
	Fingerprint  string `yaml:"fingerprint,omitempty"`
	LastSync     string `yaml:"last_sync,omitempty"`
}

// Import is an export unpacked on this machine.
type Import struct {
	Hash     string `yaml:"hash"`
	Path     string `yaml:"path"`
	FromUser string `yaml:"from_user,omitempty"`
	Source   string `yaml:"source,omitempty"`
	Imported string `yaml:"imported"`
}

// State is everything rein remembers about this machine.
type State struct {
	Organizations []OrganizationRef `yaml:"organizations"`
	Active        string            `yaml:"active,omitempty"`
	Clones        map[string]string `yaml:"clones"`
	Projects      []Project         `yaml:"projects"`
	Placements    []Placement       `yaml:"placements"`
	Imports       []Import          `yaml:"imports,omitempty"`
}

// Organization finds a connected organisation by slug.
func (state State) Organization(slug string) (ref OrganizationRef, found bool) {
	for _, candidate := range state.Organizations {
		if candidate.Slug == slug {
			return candidate, true
		}
	}
	return OrganizationRef{}, false
}

// OwnedBy maps path to target for the placements of one owner from one
// source.
func (state State) OwnedBy(owner string, source string) map[string]string {
	known := map[string]string{}
	for _, placed := range state.Placements {
		if placed.Owner == owner && placed.Source == source {
			known[placed.Path] = placed.Target
		}
	}
	return known
}

// LoadState reads state.yaml; a missing file is an empty state.
func LoadState(home string) (State, error) {
	state := State{Clones: map[string]string{}}
	content, err := os.ReadFile(filepath.Join(home, stateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return State{}, err
	}
	err = yaml.Unmarshal(content, &state)
	if err != nil {
		return State{}, fmt.Errorf("%s: %w", filepath.Join(home, stateFile), err)
	}
	if state.Clones == nil {
		state.Clones = map[string]string{}
	}
	return state, nil
}

// SaveState writes state.yaml, creating the home folder.
func SaveState(home string, state State) error {
	path := filepath.Join(home, stateFile)
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
