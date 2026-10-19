// Package snapshots is the service that records what an agent's home held
// before rein touched it (D11): the harness files, path to content, as JSON
// under <home>/snapshots/. Never transcripts, memory or caches. A snapshot
// is the raw material of migrate and the record restore relies on.
package snapshots

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/Danidani23/agents-rein/internal/ports"
)

// maxFileSize keeps a snapshot to settings; anything larger is not a
// settings file.
const maxFileSize = 1 << 20

// Service takes snapshots of the agents' homes. Clock stamps them (D20);
// nil means the wall clock.
type Service struct {
	Home   string
	Agents []ports.Agent
	Clock  func() time.Time
}

// Snapshot is one agent home at one time.
type Snapshot struct {
	Agent   string            `json:"agent"`
	Home    string            `json:"home"`
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
		files, err := collect(agentHome, agent.HarnessPaths())
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			continue
		}
		takenAt := service.now()
		snapshotPath, err := service.write(Snapshot{Agent: agent.Name(), Home: agentHome, TakenAt: takenAt, Files: files})
		if err != nil {
			return nil, err
		}
		snapshotPaths = append(snapshotPaths, snapshotPath)
	}
	return snapshotPaths, nil
}

// write stores the snapshot as <agent>-<time>.json, readable by the user only.
func (service Service) write(snapshot Snapshot) (string, error) {
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
		name := fmt.Sprintf("%s-%s.json", snapshot.Agent, stamp)
		if attempt > 0 {
			name = fmt.Sprintf("%s-%s-%d.json", snapshot.Agent, stamp, attempt)
		}
		snapshotPath := filepath.Join(service.Folder(), name)
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

// collect reads the text files under the harness paths of a home, keyed by
// their path relative to the home with forward slashes. A missing path is
// skipped; so is a file that is not text or larger than settings get.
func collect(agentHome string, harnessPaths []string) (map[string]string, error) {
	files := map[string]string{}
	for _, harnessPath := range harnessPaths {
		root := filepath.Join(agentHome, harnessPath)
		info, err := os.Stat(root)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			err = collectFile(agentHome, root, files)
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
			return collectFile(agentHome, path, files)
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func collectFile(agentHome string, path string, files map[string]string) error {
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
	relative, err := filepath.Rel(agentHome, path)
	if err != nil {
		return err
	}
	files[filepath.ToSlash(relative)] = string(content)
	return nil
}
