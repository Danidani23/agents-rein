// Package workingcopy is the service that acts on a project's working copy:
// where it is and what its fingerprint is, the move-out of the agent's files,
// the marked block in .gitignore, and the commit of exactly those paths
// (D14). It touches no org repository and no state.
package workingcopy

import (
	"crypto/sha256"
	"encoding/hex"
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

// Verification is the state of the files sync placed into a folder.
type Verification struct {
	Unchanged []string
	Modified  []string
	Missing   []string
}

// Verify hashes the inserted files under folder against the hashes
// recorded for them; a path is a relative path with forward slashes.
func (Service) Verify(folder string, recorded map[string]string) Verification {
	var verification Verification
	var paths []string
	for relative := range recorded {
		paths = append(paths, relative)
	}
	sort.Strings(paths)
	for _, relative := range paths {
		content, err := os.ReadFile(filepath.Join(folder, filepath.FromSlash(relative)))
		switch {
		case err != nil:
			verification.Missing = append(verification.Missing, relative)
		case hashOf(content) != recorded[relative]:
			verification.Modified = append(verification.Modified, relative)
		default:
			verification.Unchanged = append(verification.Unchanged, relative)
		}
	}
	return verification
}

// WriteFiles writes files, path to content, under folder, and returns the
// previous content of every path that existed, so a failure can put it back.
func (Service) WriteFiles(folder string, files map[string]string) (previous map[string]string, err error) {
	previous = map[string]string{}
	for relative, content := range files {
		target := filepath.Join(folder, filepath.FromSlash(relative))
		existing, readErr := os.ReadFile(target)
		if readErr == nil {
			previous[relative] = string(existing)
		}
		err = os.MkdirAll(filepath.Dir(target), 0o755)
		if err != nil {
			return previous, err
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(relative, ".sh") {
			mode = 0o755
		}
		err = os.WriteFile(target, []byte(content), mode)
		if err != nil {
			return previous, err
		}
	}
	return previous, nil
}

// ReadFile reads one file under folder, empty when it does not exist.
func (Service) ReadFile(folder string, relative string) string {
	content, err := os.ReadFile(filepath.Join(folder, filepath.FromSlash(relative)))
	if err != nil {
		return ""
	}
	return string(content)
}

// TrackedFiles lists every file the repository tracks under folder, sorted.
func (service Service) TrackedFiles(folder string) ([]string, error) {
	listed, err := service.Repository.TrackedPaths(folder, []string{"."})
	if err != nil {
		return nil, err
	}
	sort.Strings(listed)
	return listed, nil
}

// TrackedMarkdown lists the Markdown files the repository tracks under
// folder, sorted; an empty list, never nil, for a repository without any.
func (service Service) TrackedMarkdown(folder string) ([]string, error) {
	listed, err := service.TrackedFiles(folder)
	if err != nil {
		return nil, err
	}
	documents := []string{}
	for _, relative := range listed {
		if strings.HasSuffix(relative, ".md") {
			documents = append(documents, relative)
		}
	}
	sort.Strings(documents)
	return documents, nil
}

// FilesUnder lists the files below the relative folders under folder, as
// relative paths with forward slashes; a missing folder lists nothing.
func (Service) FilesUnder(folder string, relativeFolders []string) []string {
	var listed []string
	for _, relativeFolder := range relativeFolders {
		root := filepath.Join(folder, filepath.FromSlash(relativeFolder))
		_ = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(folder, current)
			if err != nil {
				return nil
			}
			listed = append(listed, filepath.ToSlash(relative))
			return nil
		})
	}
	sort.Strings(listed)
	return listed
}

// RemoveEmptyFolders deletes the folders under folder that the removed paths
// left empty, deepest first, never folder itself.
func (Service) RemoveEmptyFolders(folder string, relativePaths []string) {
	var parents []string
	seen := map[string]bool{}
	for _, relative := range relativePaths {
		for parent := filepath.Dir(filepath.FromSlash(relative)); parent != "." && parent != string(filepath.Separator); parent = filepath.Dir(parent) {
			if !seen[parent] {
				seen[parent] = true
				parents = append(parents, parent)
			}
		}
	}
	sort.Slice(parents, func(left, right int) bool { return len(parents[left]) > len(parents[right]) })
	for _, parent := range parents {
		entries, err := os.ReadDir(filepath.Join(folder, parent))
		if err == nil && len(entries) == 0 {
			_ = os.Remove(filepath.Join(folder, parent))
		}
	}
}

// UnsetVariables names the environment variables among names that are not set.
func (Service) UnsetVariables(names []string) []string {
	var unset []string
	for _, name := range names {
		_, found := os.LookupEnv(name)
		if !found {
			unset = append(unset, name)
		}
	}
	return unset
}

func hashOf(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
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

// RepositoryName is what the repository is called: the last segment of its
// origin URL without .git, or the root folder's name without a remote.
func (service Service) RepositoryName(root string) string {
	remote, err := service.Repository.RemoteURL(root)
	if err == nil && remote != "" {
		name := strings.TrimSuffix(strings.TrimRight(remote, "/"), ".git")
		if cut := strings.LastIndexAny(name, "/:"); cut >= 0 {
			name = name[cut+1:]
		}
		if name != "" {
			return name
		}
	}
	return filepath.Base(root)
}
