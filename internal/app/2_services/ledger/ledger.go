// Package ledger is the service that keeps the record of what rein removed
// from this machine (D12): one file, <home>/removals.yaml, with a section per
// agent, each removal pointing at the snapshot that holds the content. The
// inserts, what rein placed, get a file per organisation with sync. The
// file is written whole through a temporary file and a rename (D19).
package ledger

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// Service owns the removals file.
type Service struct {
	Home  string
	Clock func() time.Time
}

// Removal is one path rein took out of an agent's home or a project clone.
// Tracked says whether the repository tracked it, so restore knows what to
// commit again; RestoredAt is set once restore put it back (D20).
type Removal struct {
	Path       string    `yaml:"path"`
	Scope      string    `yaml:"scope"`
	Project    string    `yaml:"project,omitempty"`
	Snapshot   string    `yaml:"snapshot"`
	Tracked    bool      `yaml:"tracked,omitempty"`
	RemovedAt  time.Time `yaml:"removed_at"`
	RestoredAt time.Time `yaml:"restored_at,omitempty"`
}

// Entry is a removal with the agent it belongs to.
type Entry struct {
	Agent string
	Removal
}

// Scopes of a removal.
const (
	ScopeHome    = "home"
	ScopeProject = "project"
)

// Removals is the file: a version for later migrations and the removals
// per agent.
type Removals struct {
	Version int                  `yaml:"version"`
	Agents  map[string][]Removal `yaml:"agents"`
}

const removalsFile = "removals.yaml"

const header = "# Written by rein: what it removed from this machine, with the snapshot that holds each file.\n# Edit through rein commands; rein reset project reads this.\n"

// Path is the removals file.
func (service Service) Path() string {
	return filepath.Join(service.Home, removalsFile)
}

func (service Service) now() time.Time {
	if service.Clock == nil {
		return time.Now().UTC()
	}
	return service.Clock()
}

// Load reads the file; a missing one is empty.
func (service Service) Load() (Removals, error) {
	removals := Removals{Version: 1, Agents: map[string][]Removal{}}
	content, err := os.ReadFile(service.Path())
	if os.IsNotExist(err) {
		return removals, nil
	}
	if err != nil {
		return Removals{}, err
	}
	err = yaml.Unmarshal(content, &removals)
	if err != nil {
		return Removals{}, err
	}
	if removals.Agents == nil {
		removals.Agents = map[string][]Removal{}
	}
	return removals, nil
}

// RecordRemovals appends removals for one agent, each stamped now.
func (service Service) RecordRemovals(agent string, removals []Removal) error {
	all, err := service.Load()
	if err != nil {
		return err
	}
	removedAt := service.now()
	for _, removal := range removals {
		removal.RemovedAt = removedAt
		all.Agents[agent] = append(all.Agents[agent], removal)
	}
	return service.save(all)
}

// Pending lists the removals of a project not put back yet, oldest first.
func (service Service) Pending(project string) ([]Entry, error) {
	all, err := service.Load()
	if err != nil {
		return nil, err
	}
	var pending []Entry
	for agent, removals := range all.Agents {
		for _, removal := range removals {
			if removal.Project == project && removal.RestoredAt.IsZero() {
				pending = append(pending, Entry{Agent: agent, Removal: removal})
			}
		}
	}
	sort.Slice(pending, func(left, right int) bool {
		if pending[left].Agent != pending[right].Agent {
			return pending[left].Agent < pending[right].Agent
		}
		return pending[left].Path < pending[right].Path
	})
	return pending, nil
}

// MarkRestored stamps every pending removal of the snapshots as put back.
func (service Service) MarkRestored(snapshotPaths []string) error {
	return service.stamp(snapshotPaths, service.now())
}

// UnmarkRestored takes the stamp off again: the compensation of a restore
// that failed after marking.
func (service Service) UnmarkRestored(snapshotPaths []string) error {
	return service.stamp(snapshotPaths, time.Time{})
}

func (service Service) stamp(snapshotPaths []string, restoredAt time.Time) error {
	all, err := service.Load()
	if err != nil {
		return err
	}
	named := map[string]bool{}
	for _, snapshotPath := range snapshotPaths {
		named[snapshotPath] = true
	}
	for agent, removals := range all.Agents {
		for index := range removals {
			if named[removals[index].Snapshot] {
				removals[index].RestoredAt = restoredAt
			}
		}
		all.Agents[agent] = removals
	}
	return service.save(all)
}

// Delete removes the file: the end of a reset, after everything is put back.
func (service Service) Delete() error {
	err := os.Remove(service.Path())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ForgetSnapshot drops every removal that points at the snapshot: the
// compensation of a move-out that was put back.
func (service Service) ForgetSnapshot(snapshotPath string) error {
	all, err := service.Load()
	if err != nil {
		return err
	}
	for agent, removals := range all.Agents {
		var kept []Removal
		for _, removal := range removals {
			if removal.Snapshot != snapshotPath {
				kept = append(kept, removal)
			}
		}
		if len(kept) == 0 {
			delete(all.Agents, agent)
			continue
		}
		all.Agents[agent] = kept
	}
	return service.save(all)
}

// save writes the file whole, through a temporary file and a rename.
func (service Service) save(all Removals) error {
	content, err := yaml.Marshal(all)
	if err != nil {
		return err
	}
	err = os.MkdirAll(service.Home, 0o755)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(service.Home, ".removals-*")
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
	return os.Rename(temporary.Name(), service.Path())
}
