// Package placement puts a user's global instructions and skills into the
// Claude home, as links on macOS and Linux and as copies on Windows, and
// refuses to touch anything it did not place itself.
package claudecode

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

// Mode is how a placement is made.
type Mode string

// The two modes.
const (
	ModeLink Mode = "link"
	ModeCopy Mode = "copy"
)

// Status is what happened to one target.
type Status string

// The outcomes of one target.
const (
	StatusLinked   Status = "linked"
	StatusCopied   Status = "copied"
	StatusReplaced Status = "replaced"
	StatusOk       Status = "ok"
	StatusRefused  Status = "refused"
)

// Pair is one target in the Claude home and the source it stands for.
type Pair struct {
	Path   string
	Target string
}

// Outcome is the result for one pair.
type Outcome struct {
	Pair
	Mode   Mode
	Status Status
	Reason string
}

// ClaudeHome is where Claude Code reads global instructions and skills:
// CLAUDE_CONFIG_DIR when set, else ~/.claude.
func ClaudeHome() (string, error) {
	value := os.Getenv("CLAUDE_CONFIG_DIR")
	if value != "" {
		return value, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userHome, ".claude"), nil
}

// ModeForSystem is a copy on Windows, where links need extra rights, and a
// link everywhere else.
func ModeForSystem() Mode {
	if runtime.GOOS == "windows" {
		return ModeCopy
	}
	return ModeLink
}

// Plan lists the pairs a user-level source yields: CLAUDE.md from
// global/AGENTS.md, and skills/<id> from each skill folder. The source must
// hold both.
func Plan(source string, claudeHome string) ([]Pair, error) {
	absoluteSource, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	instructions := filepath.Join(absoluteSource, "global", "AGENTS.md")
	_, err = os.Stat(instructions)
	if err != nil {
		return nil, fmt.Errorf("%s is not a user-level source: global/AGENTS.md is missing", source)
	}
	skillFolders, err := filepath.Glob(filepath.Join(absoluteSource, "skills", "*", "SKILL.md"))
	if err != nil {
		return nil, err
	}
	if len(skillFolders) == 0 {
		return nil, fmt.Errorf("%s is not a user-level source: skills/<id>/SKILL.md is missing", source)
	}
	sort.Strings(skillFolders)
	pairs := []Pair{{Path: filepath.Join(claudeHome, "CLAUDE.md"), Target: instructions}}
	for _, skillFile := range skillFolders {
		skillFolder := filepath.Dir(skillFile)
		pairs = append(pairs, Pair{Path: filepath.Join(claudeHome, "skills", filepath.Base(skillFolder)), Target: skillFolder})
	}
	return pairs, nil
}

// PlanLevel lists the pairs a level folder in the declarations layout yields
// for Claude Code: rules/<id>.md to rules/<id>.md in the Claude home, where a
// rule without paths loads in every session, and skills/<id> to skills/<id>.
// CLAUDE.md is never a target.
func PlanLevel(level string, claudeHome string) ([]Pair, error) {
	absoluteLevel, err := filepath.Abs(level)
	if err != nil {
		return nil, err
	}
	var pairs []Pair
	rules, err := filepath.Glob(filepath.Join(absoluteLevel, "rules", "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(rules)
	for _, rule := range rules {
		pairs = append(pairs, Pair{Path: filepath.Join(claudeHome, "rules", filepath.Base(rule)), Target: rule})
	}
	skills, err := filepath.Glob(filepath.Join(absoluteLevel, "skills", "*", "SKILL.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(skills)
	for _, skillFile := range skills {
		folder := filepath.Dir(skillFile)
		pairs = append(pairs, Pair{Path: filepath.Join(claudeHome, "skills", filepath.Base(folder)), Target: folder})
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("%s holds no rules/<id>.md and no skills/<id>/SKILL.md", level)
	}
	return pairs, nil
}

// Apply makes each claudecode. known maps the paths of the placements the
// calling command made before to their targets: those it may refresh, or
// replace with a new target, reporting StatusReplaced. Anything else at a
// path, another command's placement included, is refused, never replaced.
func Apply(pairs []Pair, mode Mode, known map[string]string) []Outcome {
	outcomes := make([]Outcome, 0, len(pairs))
	for _, pair := range pairs {
		outcomes = append(outcomes, apply(pair, mode, known))
	}
	return outcomes
}

func apply(pair Pair, mode Mode, known map[string]string) Outcome {
	outcome := Outcome{Pair: pair, Mode: mode}
	info, err := os.Lstat(pair.Path)
	ownedTarget, owned := known[pair.Path]
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return outcome.create()
	case err != nil:
		return outcome.refuse(err.Error())
	case mode == ModeLink && info.Mode()&os.ModeSymlink != 0:
		existing, err := os.Readlink(pair.Path)
		if err != nil {
			return outcome.refuse(err.Error())
		}
		if existing == pair.Target {
			outcome.Status = StatusOk
			return outcome
		}
		if owned && existing == ownedTarget {
			err = os.Remove(pair.Path)
			if err != nil {
				return outcome.refuse(err.Error())
			}
			return outcome.create().replaced()
		}
		return outcome.refuse("already a link to " + existing + "; placed from another source")
	case mode == ModeCopy && owned:
		err = os.RemoveAll(pair.Path)
		if err != nil {
			return outcome.refuse(err.Error())
		}
		created := outcome.create()
		if ownedTarget != pair.Target {
			return created.replaced()
		}
		return created
	}
	return outcome.refuse("already exists and was not placed by rein from this source")
}

// replaced marks a successful placement that stood in for an earlier one.
func (outcome Outcome) replaced() Outcome {
	if outcome.Status != StatusRefused {
		outcome.Status = StatusReplaced
	}
	return outcome
}

func (outcome Outcome) refuse(reason string) Outcome {
	outcome.Status = StatusRefused
	outcome.Reason = reason
	return outcome
}

func (outcome Outcome) create() Outcome {
	err := os.MkdirAll(filepath.Dir(outcome.Path), 0o755)
	if err != nil {
		return outcome.refuse(err.Error())
	}
	if outcome.Mode == ModeLink {
		err = os.Symlink(outcome.Target, outcome.Path)
		if err != nil {
			return outcome.refuse(err.Error())
		}
		outcome.Status = StatusLinked
		return outcome
	}
	err = copyTree(outcome.Target, outcome.Path)
	if err != nil {
		return outcome.refuse(err.Error())
	}
	outcome.Status = StatusCopied
	return outcome
}

// copyTree copies a file, or a folder with everything in it.
func copyTree(source string, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(source, destination, info.Mode().Perm())
	}
	return filepath.WalkDir(source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		return copyFile(current, target, entryInfo.Mode().Perm())
	})
}

func copyFile(source string, destination string, permissions fs.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, permissions)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
