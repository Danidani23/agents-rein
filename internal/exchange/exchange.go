// Package exchange is the format in which agent settings travel between
// people and machines: a zip holding one level in the declarations layout,
// rules/<id>.md and skills/<id>/, plus a manifest with a hash per file. The
// receiver places it as their own level, so no owner is baked in.
package exchange

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ManifestName is the file at the zip root that describes the export.
const ManifestName = "reins-export.yaml"

// FormatVersion is the version of the layout this build writes and reads.
const FormatVersion = 1

// Manifest describes an export.
type Manifest struct {
	FormatVersion int               `yaml:"format_version"`
	Kind          string            `yaml:"kind"`
	User          string            `yaml:"user"`
	Source        string            `yaml:"source"`
	Created       string            `yaml:"created"`
	ToolVersion   string            `yaml:"tool_version"`
	Files         map[string]string `yaml:"files"`
}

// Entry is one file of the level, with a slash-separated path.
type Entry struct {
	Path    string
	Content []byte
}

// fixedTime keeps the zip bytes independent of when they were written.
var fixedTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// FromUserLevelSource reads a user-level source folder, the toolkit layout
// with global/AGENTS.md and skills/<id>/, into level entries: the global
// instructions become rules/global.md, a rule without paths that loads in
// every session, and each skill folder copies over.
func FromUserLevelSource(source string) ([]Entry, error) {
	instructions, err := os.ReadFile(filepath.Join(source, "global", "AGENTS.md"))
	if err != nil {
		return nil, fmt.Errorf("%s is not a user-level source: global/AGENTS.md is missing", source)
	}
	entries := []Entry{{Path: "rules/global.md", Content: instructions}}
	skillFiles, err := filepath.Glob(filepath.Join(source, "skills", "*", "SKILL.md"))
	if err != nil {
		return nil, err
	}
	if len(skillFiles) == 0 {
		return nil, fmt.Errorf("%s is not a user-level source: skills/<id>/SKILL.md is missing", source)
	}
	for _, skillFile := range skillFiles {
		folder := filepath.Dir(skillFile)
		err = filepath.WalkDir(folder, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			content, err := os.ReadFile(current)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(folder, current)
			if err != nil {
				return err
			}
			entries = append(entries, Entry{Path: path.Join("skills", filepath.Base(folder), filepath.ToSlash(relative)), Content: content})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sortEntries(entries)
	return entries, nil
}

// Count says how many rules and skills a set of entries holds.
func Count(entries []Entry) (rules int, skills int) {
	seen := map[string]bool{}
	for _, entry := range entries {
		switch {
		case strings.HasPrefix(entry.Path, "rules/"):
			rules++
		case strings.HasPrefix(entry.Path, "skills/"):
			parts := strings.Split(entry.Path, "/")
			if len(parts) >= 2 && !seen[parts[1]] {
				seen[parts[1]] = true
				skills++
			}
		}
	}
	return rules, skills
}

// Hash identifies the content of an export: the paths and their hashes,
// sorted, so the same files give the same hash whenever they were written.
func Hash(entries []Entry) string {
	sorted := append([]Entry(nil), entries...)
	sortEntries(sorted)
	digest := sha256.New()
	for _, entry := range sorted {
		fmt.Fprintf(digest, "%s\n%s\n", entry.Path, hashOf(entry.Content))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// Write writes the zip: the manifest first, then every entry in path order
// with a fixed timestamp.
func Write(out string, manifest Manifest, entries []Entry) error {
	sorted := append([]Entry(nil), entries...)
	sortEntries(sorted)
	manifest.FormatVersion = FormatVersion
	manifest.Files = map[string]string{}
	for _, entry := range sorted {
		manifest.Files[entry.Path] = hashOf(entry.Content)
	}
	manifestContent, err := yaml.Marshal(manifest)
	if err != nil {
		return err
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	err = add(writer, ManifestName, manifestContent)
	if err != nil {
		return err
	}
	for _, entry := range sorted {
		err = add(writer, entry.Path, entry.Content)
		if err != nil {
			return err
		}
	}
	err = writer.Close()
	if err != nil {
		return err
	}
	err = os.MkdirAll(filepath.Dir(out), 0o755)
	if err != nil {
		return err
	}
	return os.WriteFile(out, buffer.Bytes(), 0o644)
}

func add(writer *zip.Writer, name string, content []byte) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: fixedTime}
	file, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = file.Write(content)
	return err
}

// Read opens a zip, checks the manifest, the format version and every hash,
// and returns the entries.
func Read(zipPath string) (Manifest, []Entry, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("%s: not a zip file", zipPath)
	}
	defer reader.Close()
	contents := map[string][]byte{}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := path.Clean(file.Name)
		if strings.HasPrefix(name, "../") || path.IsAbs(name) {
			return Manifest{}, nil, fmt.Errorf("%s: entry %q escapes the archive", zipPath, file.Name)
		}
		opened, err := file.Open()
		if err != nil {
			return Manifest{}, nil, err
		}
		content, err := io.ReadAll(opened)
		opened.Close()
		if err != nil {
			return Manifest{}, nil, err
		}
		contents[name] = content
	}
	manifestContent, found := contents[ManifestName]
	if !found {
		return Manifest{}, nil, fmt.Errorf("%s is not a reins export: %s is missing", zipPath, ManifestName)
	}
	var manifest Manifest
	err = yaml.Unmarshal(manifestContent, &manifest)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("%s: %s: %w", zipPath, ManifestName, err)
	}
	if manifest.FormatVersion != FormatVersion {
		return Manifest{}, nil, fmt.Errorf("%s: format version %d; this reins reads version %d", zipPath, manifest.FormatVersion, FormatVersion)
	}
	delete(contents, ManifestName)
	var entries []Entry
	for name, content := range contents {
		expected, listed := manifest.Files[name]
		if !listed {
			return Manifest{}, nil, fmt.Errorf("%s: %s is not listed in the manifest", zipPath, name)
		}
		if hashOf(content) != expected {
			return Manifest{}, nil, fmt.Errorf("%s: %s does not match its hash in the manifest", zipPath, name)
		}
		entries = append(entries, Entry{Path: name, Content: content})
	}
	for name := range manifest.Files {
		if _, present := contents[name]; !present {
			return Manifest{}, nil, fmt.Errorf("%s: %s is listed in the manifest but missing", zipPath, name)
		}
	}
	sortEntries(entries)
	return manifest, entries, nil
}

// Unpack writes the entries under directory.
func Unpack(directory string, entries []Entry) error {
	for _, entry := range entries {
		target := filepath.Join(directory, filepath.FromSlash(entry.Path))
		err := os.MkdirAll(filepath.Dir(target), 0o755)
		if err != nil {
			return err
		}
		err = os.WriteFile(target, entry.Content, 0o644)
		if err != nil {
			return err
		}
	}
	return nil
}

func hashOf(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func sortEntries(entries []Entry) {
	sort.Slice(entries, func(left, right int) bool { return entries[left].Path < entries[right].Path })
}
