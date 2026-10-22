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
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/resolve"
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
	kindOrganization
	kindTeam
	kindProject
	kindUser
	kindLabels
	kindToolServer
	kindPermissionSet
	kindHook
	kindException
)

// classify says what a file is by its name and its folder: a SKILL.md is a
// skill, a Markdown file in a "rules" folder is a rule, settings.json or
// settings.local.json holds permissions, a YAML file under mcpservers or
// permissions is that declaration, and hooks/<id>/hook.yaml is a hook.
func classify(filePath string) fileKind {
	base := filepath.Base(filePath)
	parent := filepath.Base(filepath.Dir(filePath))
	switch {
	case base == "SKILL.md":
		return kindSkill
	case base == "settings.json" || base == "settings.local.json":
		return kindSettings
	case strings.HasSuffix(base, ".md") && parent == "rules":
		return kindRule
	case base == "organization.yaml":
		return kindOrganization
	case base == "team.yaml":
		return kindTeam
	case base == "project.yaml":
		return kindProject
	case base == "user.yaml":
		return kindUser
	case strings.HasSuffix(base, ".yaml") && parent == "labels":
		return kindLabels
	case strings.HasSuffix(base, ".yaml") && parent == "mcpservers":
		return kindToolServer
	case strings.HasSuffix(base, ".yaml") && parent == "permissions":
		return kindPermissionSet
	case base == "hook.yaml" && filepath.Base(filepath.Dir(filepath.Dir(filePath))) == "hooks":
		return kindHook
	case strings.HasSuffix(base, ".yaml") && parent == "exceptions":
		return kindException
	}
	return kindOther
}

type run struct {
	root         string
	absoluteRoot string
	report       Report
	skillsByName map[string][]string
	trees        []string
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
		switch kind := classify(filePath); kind {
		case kindSkill:
			state.lintSkill(filePath)
		case kindRule:
			state.lintRule(filePath)
		case kindSettings:
			state.lintSettings(filePath)
		case kindOrganization, kindTeam, kindProject, kindUser, kindLabels:
			state.lintRecord(filePath, kind)
		case kindToolServer, kindPermissionSet, kindHook, kindException:
			state.lintDeclaration(filePath, kind)
		}
		return nil
	})
	if err != nil {
		return Report{}, err
	}
	state.reportDuplicateSkills()
	state.crossCheckTrees()
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
		if addressText != "" {
			_, selectorProblems := declaration.SkillSelectors(skill)
			problems = append(problems, selectorProblems...)
		}
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
	var problems []declaration.Problem
	if addressText != "" {
		_, _, problems = declaration.ParseRuleDeclaration(state.displayPath(filePath), content)
	} else {
		_, problems = declaration.ParseRule(state.displayPath(filePath), content)
	}
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

// lintDeclaration parses a tool server, a permission set or a hook; a
// permission set also gets the dead-allow check a settings file gets.
func (state *run) lintDeclaration(filePath string, kind fileKind) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		state.add(filePath, "", declaration.Problem{Severity: declaration.SeverityError, Message: "cannot read: " + err.Error()})
		return
	}
	addressText, addressProblem := addressOf(filePath, kind)
	if addressProblem != nil {
		state.add(filePath, "", *addressProblem)
	}
	stem := strings.TrimSuffix(filepath.Base(filePath), ".yaml")
	var problems []declaration.Problem
	switch kind {
	case kindToolServer:
		_, problems = declaration.ParseToolServer(stem, content)
	case kindPermissionSet:
		var set declaration.PermissionSet
		set, problems = declaration.ParsePermissionSet(stem, content)
		for _, allow := range set.Allow {
			for _, deny := range set.Deny {
				if covers(deny, allow) {
					problems = append(problems, declaration.Problem{Severity: declaration.SeverityError, Message: fmt.Sprintf("allow rule %q is covered by deny rule %q; deny wins at runtime, so the allow is dead", allow, deny)})
				}
			}
		}
	case kindHook:
		entries, _ := os.ReadDir(filepath.Dir(filePath))
		var files []string
		for _, entry := range entries {
			files = append(files, entry.Name())
		}
		_, problems = declaration.ParseHook(content, files)
	case kindException:
		_, problems = declaration.ParseException(stem, content, time.Now())
	}
	for _, problem := range problems {
		state.add(filePath, addressText, problem)
	}
}

// lintRecord parses one record file; the per-record checks live with the
// records, the cross-checks run once per tree afterwards.
func (state *run) lintRecord(filePath string, kind fileKind) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		state.add(filePath, "", declaration.Problem{Severity: declaration.SeverityError, Message: "cannot read: " + err.Error()})
		return
	}
	folder := filepath.Base(filepath.Dir(filePath))
	var problems []declaration.Problem
	switch kind {
	case kindOrganization:
		_, problems = declaration.ParseOrganization(content)
	case kindTeam:
		_, problems = declaration.ParseTeam(folder, content)
	case kindProject:
		_, problems = declaration.ParseProject(folder, content)
	case kindUser:
		_, problems = declaration.ParseUser(folder, content)
	case kindLabels:
		_, problems = declaration.ParseLabelCategory(strings.TrimSuffix(filepath.Base(filePath), ".yaml"), content)
	}
	addressText, _ := recordAddress(filePath, kind)
	for _, problem := range problems {
		state.add(filePath, addressText, problem)
	}
	if kind == kindOrganization {
		state.trees = append(state.trees, filepath.Dir(filepath.Dir(filePath)))
	}
}

// crossCheckTrees runs the cross-checks of every org repository the walk
// found, once each; a tree that cannot load has already been reported
// record by record.
func (state *run) crossCheckTrees() {
	for _, clone := range state.trees {
		tree, problems, err := declaration.LoadTree(clone)
		if err != nil {
			continue
		}
		for _, problem := range problems {
			if !problem.Cross {
				continue
			}
			if problem.Path == "" {
				problem.Path = clone
			}
			state.add(problem.Path, problem.Address, problem.Problem)
		}
		declarations, err := resolve.LoadDeclarations(clone)
		if err != nil {
			state.add(clone, "", declaration.Problem{Severity: declaration.SeverityError, Message: "cannot read the declarations: " + err.Error()})
			continue
		}
		for _, slug := range sortedProjectSlugs(tree) {
			context, _ := resolve.ContextOf(tree, slug, "", time.Now())
			for _, problem := range resolve.Resolve(declarations, context).Problems {
				state.add(clone, problem.Address, declaration.Problem{Severity: declaration.SeverityError, Message: fmt.Sprintf("for project %s: %s", slug, problem.Message)})
			}
		}
	}
}

func sortedProjectSlugs(tree declaration.Tree) []string {
	var slugs []string
	for slug := range tree.Projects {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	return slugs
}

// recordAddress is the level a record describes: its folder.
func recordAddress(absolutePath string, kind fileKind) (text string, problem *declaration.Problem) {
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
	if !address.IsOrganizationCollection(inside[0]) && inside[0] != "organization.yaml" {
		return "", nil
	}
	organizationSlug := organizationSlugOf(strings.Join(segments[:start+1], "/"))
	var relative string
	if kind == kindLabels {
		relative = path.Join(append(inside[:len(inside)-1], strings.TrimSuffix(inside[len(inside)-1], ".yaml"))...)
	} else {
		relative = path.Join(inside[:len(inside)-1]...)
	}
	parsed, err := address.FromPath(relative, organizationSlug)
	if err != nil {
		return "", &declaration.Problem{Severity: declaration.SeverityError, Message: "path does not form an address: " + err.Error()}
	}
	return parsed.String(), nil
}

// organizationSlugOf reads the organisation's slug from the record at the
// root of a declarations folder; a tree without one, such as the scratch
// tree of a proposal, is addressed under "organization".
func organizationSlugOf(declarationsFolder string) string {
	content, err := os.ReadFile(filepath.Join(filepath.FromSlash(declarationsFolder), "organization.yaml"))
	if err == nil {
		organization, _ := declaration.ParseOrganization(content)
		if address.IsSlug(organization.Slug) {
			return organization.Slug
		}
	}
	return "organization"
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
	if !address.IsOrganizationCollection(inside[0]) {
		return "", nil
	}
	organizationSlug := organizationSlugOf(strings.Join(segments[:start+1], "/"))
	var relative string
	switch kind {
	case kindSkill, kindHook:
		relative = path.Join(inside[:len(inside)-1]...)
	case kindRule, kindToolServer, kindPermissionSet, kindException:
		stem := strings.TrimSuffix(strings.TrimSuffix(inside[len(inside)-1], ".md"), ".yaml")
		relative = path.Join(append(inside[:len(inside)-1], stem)...)
	default:
		return "", nil
	}
	parsed, err := address.FromPath(relative, organizationSlug)
	if err != nil {
		return "", &declaration.Problem{Severity: declaration.SeverityError, Message: "path does not form an address: " + err.Error()}
	}
	return parsed.String(), nil
}
