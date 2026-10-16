package exchange

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// FromFolder reads a folder in either layout into level entries: the toolkit
// layout, global/AGENTS.md and skills/<id>/, or the declarations layout,
// rules/<id>.md and skills/<id>/. The layout is recognised by what is there.
func FromFolder(folder string) ([]Entry, error) {
	_, err := os.Stat(filepath.Join(folder, "global", "AGENTS.md"))
	if err == nil {
		return FromUserLevelSource(folder)
	}
	return fromDeclarationsLevel(folder)
}

func fromDeclarationsLevel(folder string) ([]Entry, error) {
	var entries []Entry
	rules, err := filepath.Glob(filepath.Join(folder, "rules", "*.md"))
	if err != nil {
		return nil, err
	}
	for _, rule := range rules {
		content, err := os.ReadFile(rule)
		if err != nil {
			return nil, err
		}
		entries = append(entries, Entry{Path: path.Join("rules", filepath.Base(rule)), Content: content})
	}
	skillFiles, err := filepath.Glob(filepath.Join(folder, "skills", "*", "SKILL.md"))
	if err != nil {
		return nil, err
	}
	for _, skillFile := range skillFiles {
		skillFolder := filepath.Dir(skillFile)
		err = filepath.WalkDir(skillFolder, func(current string, entry fs.DirEntry, walkErr error) error {
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
			relative, err := filepath.Rel(skillFolder, current)
			if err != nil {
				return err
			}
			entries = append(entries, Entry{Path: path.Join("skills", filepath.Base(skillFolder), filepath.ToSlash(relative)), Content: content})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s holds no level: neither global/AGENTS.md with skills/, nor rules/<id>.md or skills/<id>/SKILL.md", folder)
	}
	sortEntries(entries)
	return entries, nil
}
