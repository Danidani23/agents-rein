// Package snapshots is the service that records what an agent's home or a
// project clone held before rein touched it (D11): the harness files, path
// to content, as JSON under <home>/snapshots/. Never transcripts, memory or
// caches. A snapshot is the raw material of migrate and the content restore
// puts back.
package snapshots

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/Danidani23/agents-rein/internal/ports"
)

// maxFileSize keeps a snapshot to settings; anything larger is not a
// settings file.
const maxFileSize = 1 << 20

// Service takes snapshots. Clock stamps them (D20); nil means the wall clock.
type Service struct {
	Home   string
	Agents []ports.Agent
	Clock  func() time.Time
}

// Snapshot is one folder's harness files at one time: an agent's home, or a
// project's folder when Project is set.
type Snapshot struct {
	Agent   string            `json:"agent"`
	Folder  string            `json:"folder"`
	Project string            `json:"project,omitempty"`
	TakenAt time.Time         `json:"taken_at"`
	Files   map[string]string `json:"files"`
}

// Folder is where the snapshots live.
func (service Service) Folder() string {
	return filepath.Join(service.Home, "snapshots")
}

func (service Service) now() time.Time {
	if service.Clock == nil {
		return time.Now().UTC()
	}
	return service.Clock()
}

// TakeAgentHomes snapshots the home of every agent that has one with
// settings in it, and returns the files written.
func (service Service) TakeAgentHomes() (snapshotPaths []string, err error) {
	for _, agent := range service.Agents {
		agentHome, err := agent.Home()
		if err != nil {
			return nil, err
		}
		snapshotPath, files, err := service.Take(agent.Name(), "", agentHome, agent.HarnessPaths())
		if err != nil {
			return nil, err
		}
		if len(files) > 0 {
			snapshotPaths = append(snapshotPaths, snapshotPath)
		}
	}
	return snapshotPaths, nil
}

// Take snapshots the harness paths under folder for one agent, named after
// the project when there is one. Nothing is written when no file is there;
// the files are returned either way, so the caller knows what was taken.
func (service Service) Take(agent string, project string, folder string, harnessPaths []string) (snapshotPath string, files map[string]string, err error) {
	files, err = collect(folder, harnessPaths)
	if err != nil {
		return "", nil, err
	}
	if len(files) == 0 {
		return "", files, nil
	}
	name := agent
	if project != "" {
		name = project + "-" + agent
	}
	snapshotPath, err = service.write(name, Snapshot{Agent: agent, Folder: folder, Project: project, TakenAt: service.now(), Files: files})
	return snapshotPath, files, err
}

// Stored is a snapshot on disk with its path.
type Stored struct {
	Path string
	Snapshot
}

// List reads every snapshot, oldest first.
func (service Service) List() ([]Stored, error) {
	entries, err := os.ReadDir(service.Folder())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var stored []Stored
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		snapshotPath := filepath.Join(service.Folder(), entry.Name())
		content, err := os.ReadFile(snapshotPath)
		if err != nil {
			return nil, err
		}
		var snapshot Snapshot
		err = json.Unmarshal(content, &snapshot)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", snapshotPath, err)
		}
		stored = append(stored, Stored{Path: snapshotPath, Snapshot: snapshot})
	}
	sort.Slice(stored, func(left, right int) bool { return stored[left].TakenAt.Before(stored[right].TakenAt) })
	return stored, nil
}

// Read loads one snapshot file.
func (Service) Read(snapshotPath string) (Stored, error) {
	content, err := os.ReadFile(snapshotPath)
	if err != nil {
		return Stored{}, err
	}
	var snapshot Snapshot
	err = json.Unmarshal(content, &snapshot)
	if err != nil {
		return Stored{}, fmt.Errorf("%s: %w", snapshotPath, err)
	}
	return Stored{Path: snapshotPath, Snapshot: snapshot}, nil
}

// Latest is the newest snapshot per agent that matches: the project's when
// project is set, the agents' homes when it is empty.
func (service Service) Latest(project string) ([]Stored, error) {
	stored, err := service.List()
	if err != nil {
		return nil, err
	}
	newest := map[string]Stored{}
	for _, candidate := range stored {
		if candidate.Project == project {
			newest[candidate.Agent] = candidate
		}
	}
	var latest []Stored
	for _, candidate := range newest {
		latest = append(latest, candidate)
	}
	sort.Slice(latest, func(left, right int) bool { return latest[left].Agent < latest[right].Agent })
	return latest, nil
}

// RemoveAll deletes every snapshot: the end of a reset, after restore.
func (service Service) RemoveAll() error {
	return os.RemoveAll(service.Folder())
}

// Remove deletes a snapshot file: the compensation of a failed workflow.
func (Service) Remove(snapshotPath string) error {
	if snapshotPath == "" {
		return nil
	}
	return os.Remove(snapshotPath)
}

// write stores the snapshot as <name>-<time>.json, readable by the user only.
func (service Service) write(name string, snapshot Snapshot) (string, error) {
	err := os.MkdirAll(service.Folder(), 0o700)
	if err != nil {
		return "", err
	}
	content, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", err
	}
	stamp := snapshot.TakenAt.UTC().Format("20060102-150405")
	for attempt := 0; ; attempt++ {
		fileName := fmt.Sprintf("%s-%s.json", name, stamp)
		if attempt > 0 {
			fileName = fmt.Sprintf("%s-%s-%d.json", name, stamp, attempt)
		}
		snapshotPath := filepath.Join(service.Folder(), fileName)
		file, err := os.OpenFile(snapshotPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = file.Write(content)
		if err != nil {
			file.Close()
			return "", err
		}
		return snapshotPath, file.Close()
	}
}

// collect reads the text files under the harness paths of a folder, keyed by
// their path relative to it with forward slashes. A missing path is skipped;
// so is a file that is not text or larger than settings get.
func collect(folder string, harnessPaths []string) (map[string]string, error) {
	files := map[string]string{}
	for _, harnessPath := range harnessPaths {
		root := filepath.Join(folder, harnessPath)
		info, err := os.Stat(root)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			err = collectFile(folder, root, files)
			if err != nil {
				return nil, err
			}
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			resolved, err := os.Stat(path)
			if errors.Is(err, fs.ErrNotExist) || (err == nil && resolved.IsDir()) {
				// a dangling link, or a link to a folder, which is not followed
				return nil
			}
			if err != nil {
				return err
			}
			return collectFile(folder, path, files)
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func collectFile(folder string, path string, files map[string]string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() > maxFileSize {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !utf8.Valid(content) {
		return nil
	}
	relative, err := filepath.Rel(folder, path)
	if err != nil {
		return err
	}
	files[filepath.ToSlash(relative)] = string(content)
	return nil
}
