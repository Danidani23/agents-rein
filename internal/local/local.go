// Package local holds what reins keeps on one machine: config.yaml, what the
// user chose, and state.yaml, what the tool learned. Both live under the
// folder REINS_HOME names, by default ~/.reins. Neither ever holds a secret.
package local

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// HomeVariable overrides the folder of the two files.
const HomeVariable = "REINS_HOME"

const (
	configFile = "config.yaml"
	stateFile  = "state.yaml"
)

// ErrNotConfigured says reins config has never run on this machine.
var ErrNotConfigured = errors.New("reins is not configured on this machine; run reins config first")

// Home is the folder of config.yaml and state.yaml.
func Home() (string, error) {
	value := os.Getenv(HomeVariable)
	if value != "" {
		return value, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userHome, ".reins"), nil
}

// OrganizationRef is a connected organisation as config records it.
type OrganizationRef struct {
	ID     string `yaml:"id"`
	Slug   string `yaml:"slug"`
	Remote string `yaml:"remote"`
}

// AgentClaudeCode is the one agent this build writes for.
const AgentClaudeCode = "claude-code"

// SupportedAgents lists the agents reins can write settings for.
var SupportedAgents = []string{AgentClaudeCode}

// Config is what the user chose.
type Config struct {
	User            string            `yaml:"user"`
	Agent           string            `yaml:"agent,omitempty"`
	UserLevelSource string            `yaml:"user_level_source,omitempty"`
	Organizations   []OrganizationRef `yaml:"organizations"`
	Active          string            `yaml:"active,omitempty"`
}

// Organization finds a connected organisation by slug.
func (config Config) Organization(slug string) (ref OrganizationRef, found bool) {
	for _, candidate := range config.Organizations {
		if candidate.Slug == slug {
			return candidate, true
		}
	}
	return OrganizationRef{}, false
}

// Placement is one link or copy reins made in the Claude home. Owner is the
// command that made it, config or import; a record without one is config's,
// from before the field existed.
type Placement struct {
	Path   string `yaml:"path"`
	Target string `yaml:"target"`
	Mode   string `yaml:"mode"`
	Owner  string `yaml:"owner,omitempty"`
}

// The commands that place files.
const (
	OwnerConfig = "config"
	OwnerImport = "import"
)

// OwnedBy maps path to target for the placements of one owner.
func (state State) OwnedBy(owner string) map[string]string {
	known := map[string]string{}
	for _, placed := range state.Placements {
		recorded := placed.Owner
		if recorded == "" {
			recorded = OwnerConfig
		}
		if recorded == owner {
			known[placed.Path] = placed.Target
		}
	}
	return known
}

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
	Imported string `yaml:"imported"`
}

// State is what the tool learned and owns.
type State struct {
	Clones     map[string]string `yaml:"clones"`
	Projects   []Project         `yaml:"projects"`
	Placements []Placement       `yaml:"placements"`
	Imports    []Import          `yaml:"imports,omitempty"`
}

// LoadConfig reads config.yaml; found is false when it does not exist yet.
func LoadConfig(home string) (config Config, found bool, err error) {
	content, err := os.ReadFile(filepath.Join(home, configFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, err
	}
	err = yaml.Unmarshal(content, &config)
	if err != nil {
		return Config{}, false, fmt.Errorf("%s: %w", filepath.Join(home, configFile), err)
	}
	return config, true, nil
}

// SaveConfig writes config.yaml, creating the home folder.
func SaveConfig(home string, config Config) error {
	return write(filepath.Join(home, configFile), config)
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
	return write(filepath.Join(home, stateFile), state)
}

// Require loads the config and refuses when reins config has never run.
func Require(home string) (Config, error) {
	config, found, err := LoadConfig(home)
	if err != nil {
		return Config{}, err
	}
	if !found {
		return Config{}, ErrNotConfigured
	}
	return config, nil
}

func write(path string, value any) error {
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return err
	}
	content, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
