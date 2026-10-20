// Package resolve decides which declarations apply to one project: the
// organisation's, its teams', its own and the user's, filtered by the
// project's labels, thinned by skip lists and exceptions, and settled per
// kind and id across levels, in the board's resolution order. It is a
// library: pure functions over what the org clone holds.
package resolve

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
)

// Declaration is one declaration of an org repository with its selectors
// and its files, by path relative to the declaration's own folder (one file
// for a rule, a tool server or a permission set; several for a skill or a
// hook).
type Declaration struct {
	Address   string
	Level     string
	Kind      string
	ID        string
	Selectors declaration.Selectors
	Files     map[string]string
}

// Context is the project a resolution is for.
type Context struct {
	OrganizationSlug string
	ProjectSlug      string
	Labels           []string
	Teams            []string
	// Skip lists by the address of the level that keeps them.
	Skips map[string][]string
	// UserSlug adds the user's level; empty leaves it out.
	UserSlug   string
	Exceptions []declaration.ExceptionRecord
	Now        time.Time
}

// Chosen is a declaration that applies, with what it replaced.
type Chosen struct {
	Declaration
	Replaced []string
}

// Skipped is a declaration a skip list took out.
type Skipped struct {
	Address string
	By      string
	// Exception is the exception's address when the declaration was required.
	Exception string
}

// Problem is a resolution error at an address: the harness is not written
// while one exists.
type Problem struct {
	Address string
	Message string
}

// Result is a resolution.
type Result struct {
	Chosen   []Chosen
	Skipped  []Skipped
	Problems []Problem
}

// Level ranks: a lower rank is a higher level.
const (
	rankOrganization = iota
	rankTeam
	rankProject
	rankUser
)

// LoadDeclarations reads every declaration of an org clone with its
// selectors: from YAML for tool servers, permission sets and hooks, from the
// frontmatter for rules, from the metadata for skills.
func LoadDeclarations(orgClonePath string) ([]Declaration, error) {
	base := filepath.Join(orgClonePath, "declarations")
	byKey := map[string]*Declaration{}
	var keys []string
	err := filepath.WalkDir(base, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) && filePath == base {
				return filepath.SkipDir
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(base, filePath)
		if err != nil {
			return err
		}
		segments := strings.Split(filepath.ToSlash(relative), "/")
		for index := len(segments) - 2; index >= 1; index-- {
			collection := segments[index]
			folderKind := collection == address.Skills || collection == address.Hooks
			fileKind := collection == address.Rules || collection == address.MCPServers || collection == address.Permissions
			if !folderKind && !fileKind {
				continue
			}
			if fileKind && index != len(segments)-2 {
				continue
			}
			if folderKind && index+1 >= len(segments)-1 {
				continue
			}
			content, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			levelPath := path.Join(segments[:index]...)
			var id, inside, key string
			if folderKind {
				id = segments[index+1]
				inside = path.Join(segments[index+2:]...)
				key = path.Join(levelPath, collection, id)
			} else {
				id = strings.TrimSuffix(strings.TrimSuffix(segments[len(segments)-1], ".md"), ".yaml")
				inside = segments[len(segments)-1]
				key = path.Join(levelPath, collection, id)
			}
			level, err := address.FromPath(levelPath)
			if err != nil {
				return nil
			}
			item, found := byKey[key]
			if !found {
				item = &Declaration{Address: level.String() + "." + collection + "." + id, Level: level.String(), Kind: collection, ID: id, Files: map[string]string{}}
				byKey[key] = item
				keys = append(keys, key)
			}
			item.Files[inside] = string(content)
			break
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	var declarations []Declaration
	for _, key := range keys {
		item := byKey[key]
		item.Selectors = selectorsOf(*item)
		declarations = append(declarations, *item)
	}
	return declarations, nil
}

// selectorsOf reads a declaration's selectors from its main file.
func selectorsOf(item Declaration) declaration.Selectors {
	switch item.Kind {
	case address.Rules:
		for _, content := range item.Files {
			_, selectors, _ := declaration.ParseRuleDeclaration("", []byte(content))
			return selectors
		}
	case address.Skills:
		content, found := item.Files["SKILL.md"]
		if !found {
			return declaration.Selectors{}
		}
		skill, _, parsed := declaration.ParseSkill("", item.ID, []byte(content))
		if !parsed {
			return declaration.Selectors{}
		}
		selectors, _ := declaration.SkillSelectors(skill)
		return selectors
	case address.Hooks:
		var selectors declaration.Selectors
		_ = yaml.Unmarshal([]byte(item.Files["hook.yaml"]), &selectors)
		return selectors
	default:
		for _, content := range item.Files {
			var selectors declaration.Selectors
			_ = yaml.Unmarshal([]byte(content), &selectors)
			return selectors
		}
	}
	return declaration.Selectors{}
}

// Resolve applies the resolution order to the declarations for the context.
func Resolve(declarations []Declaration, context Context) Result {
	organization := address.Organizations + "." + context.OrganizationSlug
	ranks := map[string]int{organization: rankOrganization}
	for _, team := range context.Teams {
		ranks[organization+".teams."+team] = rankTeam
	}
	ranks[organization+".projects."+context.ProjectSlug] = rankProject
	if context.UserSlug != "" {
		ranks[address.Users+"."+context.UserSlug] = rankUser
	}
	labels := map[string]bool{}
	for _, label := range context.Labels {
		labels[label] = true
	}
	var result Result
	var applicable []Declaration
	for _, item := range declarations {
		_, inScope := ranks[item.Level]
		if !inScope || !matches(item.Selectors, labels) {
			continue
		}
		applicable = append(applicable, item)
	}
	var survivors []Declaration
	for _, item := range applicable {
		skipped, problem := skipOf(item, ranks, context)
		if problem != nil {
			result.Problems = append(result.Problems, *problem)
		}
		if skipped != nil {
			result.Skipped = append(result.Skipped, *skipped)
			continue
		}
		survivors = append(survivors, item)
	}
	result.Chosen = settle(survivors, ranks, &result.Problems)
	sort.Slice(result.Chosen, func(left, right int) bool {
		if ranks[result.Chosen[left].Level] != ranks[result.Chosen[right].Level] {
			return ranks[result.Chosen[left].Level] < ranks[result.Chosen[right].Level]
		}
		return result.Chosen[left].Address < result.Chosen[right].Address
	})
	return result
}

// matches applies the selectors: doNotApplyIf wins; applyIf means any of
// the labels; no applyIf means everywhere.
func matches(selectors declaration.Selectors, labels map[string]bool) bool {
	for _, label := range selectors.DoNotApplyIf {
		if labels[label] {
			return false
		}
	}
	if len(selectors.ApplyIf) == 0 {
		return true
	}
	for _, label := range selectors.ApplyIf {
		if labels[label] {
			return true
		}
	}
	return false
}

// skipOf finds a skip list below the declaration's level that names it: a
// default declaration is skipped, a required one only with an exception
// granted to the skipper, else it stays and the skip is a problem.
func skipOf(item Declaration, ranks map[string]int, context Context) (*Skipped, *Problem) {
	var skippers []string
	for level := range context.Skips {
		skippers = append(skippers, level)
	}
	sort.Strings(skippers)
	for _, skipper := range skippers {
		rank, inScope := ranks[skipper]
		if !inScope || rank <= ranks[item.Level] {
			continue
		}
		named := false
		for _, entry := range context.Skips[skipper] {
			if entry == item.Address {
				named = true
			}
		}
		if !named {
			continue
		}
		if item.Selectors.Enforcement != declaration.EnforcementRequired {
			return &Skipped{Address: item.Address, By: skipper}, nil
		}
		for _, exception := range context.Exceptions {
			if exception.Declaration == item.Address && exception.Grantee == skipper && !exception.Expired(context.Now) {
				return &Skipped{Address: item.Address, By: skipper, Exception: exception.Address}, nil
			}
		}
		return nil, &Problem{Address: item.Address, Message: fmt.Sprintf("%s skips it, but it is required; an exception at %s granted to %s is needed", skipper, item.Level, skipper)}
	}
	return nil, nil
}

// settle keeps one declaration per kind and id across levels: a lower
// default replaces, a lower redeclaration of a required one is a problem,
// two teams with the same id are a problem unless the project settles it.
func settle(items []Declaration, ranks map[string]int, problems *[]Problem) []Chosen {
	groups := map[string][]Declaration{}
	var keys []string
	for _, item := range items {
		key := item.Kind + "/" + item.ID
		if _, seen := groups[key]; !seen {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], item)
	}
	sort.Strings(keys)
	var chosen []Chosen
	for _, key := range keys {
		group := groups[key]
		sort.Slice(group, func(left, right int) bool {
			if ranks[group[left].Level] != ranks[group[right].Level] {
				return ranks[group[left].Level] < ranks[group[right].Level]
			}
			return group[left].Address < group[right].Address
		})
		if conflicted := teamConflict(group, ranks); len(conflicted) > 0 {
			for _, item := range conflicted {
				var others []string
				for _, other := range conflicted {
					if other.Address != item.Address {
						others = append(others, other.Address)
					}
				}
				*problems = append(*problems, Problem{Address: item.Address, Message: fmt.Sprintf("the same %s %s is declared by %s; the project must declare its own to settle it", item.Kind, item.ID, strings.Join(others, " and "))})
			}
			continue
		}
		winner := Chosen{Declaration: group[0]}
		for _, candidate := range group[1:] {
			switch {
			case ranks[candidate.Level] == ranks[winner.Level]:
				// two teams, settled by the project below them
				winner.Replaced = append(winner.Replaced, candidate.Address)
			case winner.Selectors.Enforcement == declaration.EnforcementRequired:
				*problems = append(*problems, Problem{Address: candidate.Address, Message: fmt.Sprintf("redeclares %s, which is required; a lower level cannot replace it", winner.Address)})
			default:
				replaced := append(winner.Replaced, winner.Address)
				winner = Chosen{Declaration: candidate, Replaced: replaced}
			}
		}
		chosen = append(chosen, winner)
	}
	return chosen
}

// teamConflict lists the members of a group that two teams declare alike
// when no project declaration settles it; empty otherwise.
func teamConflict(group []Declaration, ranks map[string]int) []Declaration {
	var teams []Declaration
	for _, item := range group {
		switch ranks[item.Level] {
		case rankTeam:
			teams = append(teams, item)
		case rankProject:
			return nil
		}
	}
	if len(teams) < 2 {
		return nil
	}
	return teams
}

// ContextOf builds the context of a project from the tree it is recorded in.
func ContextOf(tree declaration.Tree, projectSlug string, userSlug string, now time.Time) (Context, bool) {
	project, found := tree.Projects[projectSlug]
	if !found {
		return Context{}, false
	}
	organization := address.Organizations + "." + tree.Organization.Slug
	context := Context{OrganizationSlug: tree.Organization.Slug, ProjectSlug: projectSlug, Labels: project.Labels, Teams: project.Teams, Skips: map[string][]string{}, UserSlug: userSlug, Exceptions: tree.Exceptions, Now: now}
	if len(project.Skip) > 0 {
		context.Skips[organization+".projects."+projectSlug] = project.Skip
	}
	for _, team := range project.Teams {
		if record, exists := tree.Teams[team]; exists && len(record.Skip) > 0 {
			context.Skips[organization+".teams."+team] = record.Skip
		}
	}
	return context, true
}
