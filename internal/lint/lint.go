// Package lint checks a harness tree: every SKILL.md against the Agent Skills
// specification, every rule file's frontmatter, every settings file's
// permission rules, and the skills that share a name. Inside an org
// repository's declarations tree each finding also carries the address.
package lint

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Danidani23/ai-assisted-dev-tool/internal/address"
	"github.com/Danidani23/ai-assisted-dev-tool/internal/declaration"
)

// Finding is one problem the lint found.
type Finding struct {
	Path     string
	Address  string // empty outside a declarations tree
	Severity declaration.Severity
	Message  string
}

// Report holds every finding of one run, sorted by path then message.
type Report struct {
	Findings []Finding
}

// Errors counts the findings that fail the lint.
func (report Report) Errors() (count int) {
	for _, finding := range report.Findings {
		if finding.Severity == declaration.SeverityError {
			count++
		}
	}
	return count
}

// Warnings counts the findings that do not fail the lint.
func (report Report) Warnings() (count int) {
	for _, finding := range report.Findings {
		if finding.Severity == declaration.SeverityWarning {
			count++
		}
	}
	return count
}

type fileKind int

const (
	kindOther fileKind = iota
	kindSkill
	kindRule
	kindSettings
)

// classify says what a file is by its name and its folder: a SKILL.md is a
// skill, a Markdown file in a "rules" folder is a rule, and settings.json or
// settings.local.json holds permissions.
func classify(filePath string) fileKind {
	base := filepath.Base(filePath)
	switch {
	case base == "SKILL.md":
		return kindSkill
	case base == "settings.json" || base == "settings.local.json":
		return kindSettings
	case strings.HasSuffix(base, ".md") && filepath.Base(filepath.Dir(filePath)) == "rules":
		return kindRule
	}
	return kindOther
}

type run struct {
	root         string
	absoluteRoot string
	report       Report
	skillsByName map[string][]string
}

// Run lints every harness file under root.
func Run(root string) (Report, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return Report{}, err
	}
	info, err := os.Stat(absoluteRoot)
	if err != nil {
		return Report{}, err
	}
	if !info.IsDir() {
		return Report{}, fmt.Errorf("%s is not a directory", root)
	}
	state := &run{root: root, absoluteRoot: absoluteRoot, skillsByName: map[string][]string{}}
	err = filepath.WalkDir(absoluteRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		switch classify(filePath) {
		case kindSkill:
			state.lintSkill(filePath)
		case kindRule:
			state.lintRule(filePath)
		case kindSettings:
			state.lintSettings(filePath)
		}
		return nil
	})
	if err != nil {
		return Report{}, err
	}
	state.reportDuplicateSkills()
	sort.Slice(state.report.Findings, func(left, right int) bool {
		if state.report.Findings[left].Path != state.report.Findings[right].Path {
			return state.report.Findings[left].Path < state.report.Findings[right].Path
		}
		return state.report.Findings[left].Message < state.report.Findings[right].Message
	})
	return state.report, nil
}

// displayPath renders a file relative to the root the user gave.
func (state *run) displayPath(filePath string) string {
	relative, err := filepath.Rel(state.absoluteRoot, filePath)
	if err != nil {
		return filePath
	}
	return filepath.Join(state.root, relative)
}

func (state *run) add(filePath string, addressText string, problem declaration.Problem) {
	state.report.Findings = append(state.report.Findings, Finding{
		Path:     state.displayPath(filePath),
		Address:  addressText,
		Severity: problem.Severity,
		Message:  problem.Message,
	})
}

func (state *run) lintSkill(filePath string) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		state.add(filePath, "", declaration.Problem{Severity: declaration.SeverityError, Message: "cannot read: " + err.Error()})
		return
	}
	addressText, addressProblem := addressOf(filePath, kindSkill)
	if addressProblem != nil {
		state.add(filePath, "", *addressProblem)
	}
	directory := filepath.Base(filepath.Dir(filePath))
	skill, problems, parsed := declaration.ParseSkill(state.displayPath(filePath), directory, content)
	if parsed {
		problems = append(problems, skill.Validate()...)
	}
	for _, problem := range problems {
		state.add(filePath, addressText, problem)
	}
	if skill.Name != "" {
		state.skillsByName[skill.Name] = append(state.skillsByName[skill.Name], filePath)
	}
}

func (state *run) lintRule(filePath string) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		state.add(filePath, "", declaration.Problem{Severity: declaration.SeverityError, Message: "cannot read: " + err.Error()})
		return
	}
	addressText, addressProblem := addressOf(filePath, kindRule)
	if addressProblem != nil {
		state.add(filePath, "", *addressProblem)
	}
	_, problems := declaration.ParseRule(state.displayPath(filePath), content)
	for _, problem := range problems {
		state.add(filePath, addressText, problem)
	}
}

func (state *run) lintSettings(filePath string) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		state.add(filePath, "", declaration.Problem{Severity: declaration.SeverityError, Message: "cannot read: " + err.Error()})
		return
	}
	settings, problems := declaration.ParseSettings(state.displayPath(filePath), content)
	for _, problem := range problems {
		state.add(filePath, "", problem)
	}
	for _, allow := range settings.Permissions.Allow {
		for _, deny := range settings.Permissions.Deny {
			if covers(deny, allow) {
				state.add(filePath, "", declaration.Problem{
					Severity: declaration.SeverityError,
					Message:  fmt.Sprintf("allow rule %q is covered by deny rule %q; deny wins at runtime, so the allow is dead", allow, deny),
				})
			}
		}
	}
}

// reportDuplicateSkills warns about skills that share a name across the run:
// Claude Code loads one of them, and a personal skill wins over a project one.
func (state *run) reportDuplicateSkills() {
	for name, paths := range state.skillsByName {
		if len(paths) < 2 {
			continue
		}
		sort.Strings(paths)
		for _, filePath := range paths {
			var others []string
			for _, other := range paths {
				if other != filePath {
					others = append(others, state.displayPath(other))
				}
			}
			state.add(filePath, "", declaration.Problem{
				Severity: declaration.SeverityWarning,
				Message:  fmt.Sprintf("skill name %q is also declared at %s; Claude Code loads one of them, and a personal skill wins over a project skill", name, strings.Join(others, ", ")),
			})
		}
	}
}

// addressOf derives the address of a file inside a declarations tree; outside
// one it is empty. A skill's address is its folder; a rule's is the rules
// folder plus the file's stem.
func addressOf(absolutePath string, kind fileKind) (text string, problem *declaration.Problem) {
	segments := strings.Split(filepath.ToSlash(absolutePath), "/")
	start := -1
	for index, segment := range segments {
		if segment == "declarations" {
			start = index
		}
	}
	if start < 0 || start+1 >= len(segments) {
		return "", nil
	}
	inside := segments[start+1:]
	if inside[0] != address.Organizations && inside[0] != address.Users {
		return "", nil
	}
	var relative string
	switch kind {
	case kindSkill:
		relative = path.Join(inside[:len(inside)-1]...)
	case kindRule:
		stem := strings.TrimSuffix(inside[len(inside)-1], ".md")
		relative = path.Join(append(inside[:len(inside)-1], stem)...)
	default:
		return "", nil
	}
	parsed, err := address.FromPath(relative)
	if err != nil {
		return "", &declaration.Problem{Severity: declaration.SeverityError, Message: "path does not form an address: " + err.Error()}
	}
	return parsed.String(), nil
}
