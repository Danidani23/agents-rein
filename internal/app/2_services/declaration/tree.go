package declaration

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Tree is an org repository read whole: its records keyed by slug. Problems
// found while reading carry the address of the record.
type Tree struct {
	Organization Organization
	Teams        map[string]Team
	Projects     map[string]Project
	Users        map[string]User
	Labels       map[string]LabelCategory
}

// TreeProblem is a problem at an address. Cross marks the ones only the
// whole tree can find, as opposed to a record's own.
type TreeProblem struct {
	Address string
	Path    string
	Problem Problem
	Cross   bool
}

// LoadTree reads declarations/ under clone: the one organisation, its teams,
// projects, labels and the users, then cross-checks them. A missing or
// unreadable organisation is an error; everything else is a problem.
func LoadTree(clone string) (Tree, []TreeProblem, error) {
	matches := glob(filepath.Join(clone, "declarations", "organizations", "*", "organization.yaml"))
	switch len(matches) {
	case 0:
		return Tree{}, nil, fmt.Errorf("not an org repository: no declarations/organizations/<slug>/organization.yaml in %s", clone)
	case 1:
	default:
		return Tree{}, nil, fmt.Errorf("%s holds %d organisations; an org repository holds exactly one", clone, len(matches))
	}
	var problems []TreeProblem
	add := func(addressText string, path string, found []Problem) {
		for _, problem := range found {
			problems = append(problems, TreeProblem{Address: addressText, Path: path, Problem: problem})
		}
	}
	// The organisation's own problems are reported like any record's, and
	// the tree is still read and cross-checked, keyed by the folder name.
	orgFolder := filepath.Dir(matches[0])
	orgSlug := filepath.Base(orgFolder)
	orgAddress := "organizations." + orgSlug
	content, err := os.ReadFile(matches[0])
	if err != nil {
		return Tree{}, nil, err
	}
	organization, found := ParseOrganization(orgSlug, content)
	add(orgAddress, matches[0], found)
	tree := Tree{Organization: organization, Teams: map[string]Team{}, Projects: map[string]Project{}, Users: map[string]User{}, Labels: map[string]LabelCategory{}}
	base := filepath.Join(clone, "declarations")
	for _, file := range glob(filepath.Join(orgFolder, "labels", "*.yaml")) {
		name := strings.TrimSuffix(filepath.Base(file), ".yaml")
		content, err := os.ReadFile(file)
		if err != nil {
			return Tree{}, nil, err
		}
		category, found := ParseLabelCategory(name, content)
		add(orgAddress+".labels."+name, file, found)
		if len(found) == 0 {
			tree.Labels[category.Category] = category
		}
	}
	for _, file := range glob(filepath.Join(orgFolder, "teams", "*", "team.yaml")) {
		folder := filepath.Base(filepath.Dir(file))
		content, err := os.ReadFile(file)
		if err != nil {
			return Tree{}, nil, err
		}
		team, found := ParseTeam(folder, content)
		add(orgAddress+".teams."+folder, file, found)
		if len(found) == 0 {
			tree.Teams[team.Slug] = team
		}
	}
	for _, file := range glob(filepath.Join(orgFolder, "projects", "*", "project.yaml")) {
		folder := filepath.Base(filepath.Dir(file))
		content, err := os.ReadFile(file)
		if err != nil {
			return Tree{}, nil, err
		}
		project, found := ParseProject(folder, content)
		add(orgAddress+".projects."+folder, file, found)
		if len(found) == 0 {
			tree.Projects[project.Slug] = project
		}
	}
	for _, file := range glob(filepath.Join(base, "users", "*", "user.yaml")) {
		folder := filepath.Base(filepath.Dir(file))
		content, err := os.ReadFile(file)
		if err != nil {
			return Tree{}, nil, err
		}
		user, found := ParseUser(folder, content)
		add("users."+folder, file, found)
		if len(found) == 0 {
			tree.Users[user.Slug] = user
		}
	}
	problems = append(problems, tree.crossCheck(orgAddress, orgFolder)...)
	sort.Slice(problems, func(left, right int) bool {
		if problems[left].Address != problems[right].Address {
			return problems[left].Address < problems[right].Address
		}
		return problems[left].Problem.Message < problems[right].Problem.Message
	})
	return tree, problems, nil
}

// crossCheck finds what no single record can: references between records,
// ids shared, and two projects claiming one repository.
func (tree Tree) crossCheck(orgAddress string, orgFolder string) []TreeProblem {
	var problems []TreeProblem
	ids := map[string]string{tree.Organization.ID: orgAddress}
	claim := func(id string, addressText string) {
		if other, taken := ids[id]; taken {
			problems = append(problems, TreeProblem{Address: addressText, Cross: true, Problem: Problem{Severity: SeverityError, Message: fmt.Sprintf("id %s is also the id of %s; every record has its own UUID", id, other)}})
			return
		}
		ids[id] = addressText
	}
	for _, slug := range sortedKeys(tree.Teams) {
		claim(tree.Teams[slug].ID, orgAddress+".teams."+slug)
	}
	repositories := map[string]string{}
	for _, slug := range sortedKeys(tree.Projects) {
		project := tree.Projects[slug]
		addressText := orgAddress + ".projects." + slug
		path := filepath.Join(orgFolder, "projects", slug, "project.yaml")
		claim(project.ID, addressText)
		for _, team := range project.Teams {
			if _, exists := tree.Teams[team]; !exists {
				problems = append(problems, TreeProblem{Address: addressText, Path: path, Cross: true, Problem: Problem{Severity: SeverityError, Message: fmt.Sprintf("team %q does not exist; teams are %s", team, strings.Join(sortedKeys(tree.Teams), ", "))}})
			}
		}
		for _, label := range project.Labels {
			category, value, ok := SplitLabel(label)
			if !ok {
				continue
			}
			registered, exists := tree.Labels[category]
			if !exists {
				problems = append(problems, TreeProblem{Address: addressText, Path: path, Cross: true, Problem: Problem{Severity: SeverityError, Message: fmt.Sprintf("label %q names no category of the registry; categories are %s", label, strings.Join(sortedKeys(tree.Labels), ", "))}})
				continue
			}
			if _, exists := registered.Labels[value]; !exists {
				problems = append(problems, TreeProblem{Address: addressText, Path: path, Cross: true, Problem: Problem{Severity: SeverityError, Message: fmt.Sprintf("label %q is not in the registry; %s has %s", label, category, strings.Join(registered.LabelValues(), ", "))}})
			}
		}
		if len(project.Fingerprint) > 0 {
			key := strings.Join(project.Fingerprint, ",") + " " + project.Path
			if other, taken := repositories[key]; taken {
				problems = append(problems, TreeProblem{Address: addressText, Path: path, Cross: true, Problem: Problem{Severity: SeverityError, Message: fmt.Sprintf("the same repository and path as %s; two projects cannot share both", other)}})
			} else {
				repositories[key] = addressText
			}
		}
	}
	return problems
}

func glob(pattern string) []string {
	matches, _ := filepath.Glob(pattern)
	sort.Strings(matches)
	return matches
}

func sortedKeys[Value any](entries map[string]Value) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
