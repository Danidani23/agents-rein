// Package workingcopy is the service that acts on a project's working copy:
// where it is and what its fingerprint is, the move-out of the agent's files,
// the marked block in .gitignore, and the commit of exactly those paths
// (D14). It touches no org repository and no state.
package workingcopy

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Danidani23/agents-rein/internal/ports"
)

// Service acts on working copies through version control and the file system.
type Service struct {
	Repository ports.Repository
}

// Location is a project's place in its repository: the repository's root,
// the project's path under it, "." at the root, the folder the two make,
// and the fingerprint, the root commits sorted.
type Location struct {
	Root        string
	Path        string
	Folder      string
	Fingerprint []string
}

const (
	ignoreBlockStart = "# >>> rein: agent settings are not repository settings; rein sync writes these, keep this block"
	ignoreBlockEnd   = "# <<< rein"
)

// Locate finds the repository around directory and fingerprints it.
func (service Service) Locate(directory string) (Location, error) {
	root, err := service.Repository.Root(directory)
	if err != nil {
		return Location{}, err
	}
	// git reports the root with every link resolved; the folder is compared the same way
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Location{}, err
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return Location{}, err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return Location{}, err
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil {
		return Location{}, err
	}
	fingerprint, err := service.Repository.RootCommits(root)
	if err != nil {
		return Location{}, err
	}
	sort.Strings(fingerprint)
	return Location{Root: root, Path: filepath.ToSlash(relative), Folder: filepath.Join(root, relative), Fingerprint: fingerprint}, nil
}

// Present says which of the paths exist under folder.
func (Service) Present(folder string, relativePaths []string) []string {
	var present []string
	for _, relativePath := range relativePaths {
		_, err := os.Lstat(filepath.Join(folder, relativePath))
		if err == nil {
			present = append(present, relativePath)
		}
	}
	return present
}

// TrackedAmong says which of the paths, relative to root, the repository
// tracks, a folder counting when anything under it is tracked.
func (service Service) TrackedAmong(root string, relativePaths []string) (map[string]bool, error) {
	tracked := map[string]bool{}
	if len(relativePaths) == 0 {
		return tracked, nil
	}
	listed, err := service.Repository.TrackedPaths(root, relativePaths)
	if err != nil {
		return nil, err
	}
	for _, relativePath := range relativePaths {
		for _, candidate := range listed {
			if candidate == relativePath || strings.HasPrefix(candidate, strings.TrimSuffix(relativePath, "/")+"/") {
				tracked[relativePath] = true
			}
		}
	}
	return tracked, nil
}

// HasIgnoreBlock says whether folder's .gitignore holds the marked block.
func (Service) HasIgnoreBlock(folder string) bool {
	content, err := os.ReadFile(filepath.Join(folder, ".gitignore"))
	return err == nil && strings.Contains(string(content), ignoreBlockStart)
}

// Remove deletes the paths under folder, folders with their content.
func (Service) Remove(folder string, relativePaths []string) error {
	for _, relativePath := range relativePaths {
		err := os.RemoveAll(filepath.Join(folder, relativePath))
		if err != nil {
			return err
		}
	}
	return nil
}

// Restore writes files, path to content, back under folder: the
// compensation of a move-out.
func (Service) Restore(folder string, files map[string]string) error {
	for relativePath, content := range files {
		target := filepath.Join(folder, filepath.FromSlash(relativePath))
		err := os.MkdirAll(filepath.Dir(target), 0o755)
		if err != nil {
			return err
		}
		err = os.WriteFile(target, []byte(content), 0o644)
		if err != nil {
			return err
		}
	}
	return nil
}

// WriteIgnoreBlock puts the marked block with the entries into folder's
// .gitignore, replacing an earlier block, and says whether the file changed.
func (Service) WriteIgnoreBlock(folder string, entries []string) (changed bool, err error) {
	ignorePath := filepath.Join(folder, ".gitignore")
	existing, err := os.ReadFile(ignorePath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	block := ignoreBlockStart + "\n" + strings.Join(entries, "\n") + "\n" + ignoreBlockEnd + "\n"
	without := withoutIgnoreBlock(string(existing))
	updated := without
	if updated != "" && !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	if updated != "" {
		updated += "\n"
	}
	updated += block
	if updated == string(existing) {
		return false, nil
	}
	return true, os.WriteFile(ignorePath, []byte(updated), 0o644)
}

// RemoveIgnoreBlock takes the marked block out again, deleting .gitignore
// when nothing else is in it.
func (Service) RemoveIgnoreBlock(folder string) error {
	ignorePath := filepath.Join(folder, ".gitignore")
	existing, err := os.ReadFile(ignorePath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	without := strings.TrimRight(withoutIgnoreBlock(string(existing)), "\n")
	if without == "" {
		return os.Remove(ignorePath)
	}
	return os.WriteFile(ignorePath, []byte(without+"\n"), 0o644)
}

// withoutIgnoreBlock strips the marked block, markers included.
func withoutIgnoreBlock(content string) string {
	start := strings.Index(content, ignoreBlockStart)
	if start < 0 {
		return content
	}
	end := strings.Index(content[start:], ignoreBlockEnd)
	if end < 0 {
		return content
	}
	after := start + end + len(ignoreBlockEnd)
	rest := strings.TrimLeft(content[after:], "\n")
	before := strings.TrimRight(content[:start], "\n")
	if before == "" {
		return rest
	}
	if rest == "" {
		return before + "\n"
	}
	return before + "\n\n" + rest
}

// Commit stages the paths that the repository tracks, the removed ones
// included, plus the extra paths, and commits them when there is anything
// to commit. Paths are relative to root.
func (service Service) Commit(root string, removedPaths []string, extraPaths []string, message string) (hash string, committed bool, err error) {
	var toStage []string
	if len(removedPaths) > 0 {
		tracked, err := service.Repository.TrackedPaths(root, removedPaths)
		if err != nil {
			return "", false, err
		}
		toStage = append(toStage, tracked...)
	}
	toStage = append(toStage, extraPaths...)
	if len(toStage) == 0 {
		return "", false, nil
	}
	err = service.Repository.StagePaths(root, toStage)
	if err != nil {
		return "", false, err
	}
	staged, err := service.Repository.HasStagedChanges(root)
	if err != nil || !staged {
		return "", false, err
	}
	hash, err = service.Repository.CommitStaged(root, message)
	if err != nil {
		return "", false, err
	}
	return hash, true, nil
}
