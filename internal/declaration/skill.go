package declaration

import (
	"fmt"
	"maps"
	"slices"

	"github.com/Danidani23/ai-assisted-dev-tool/internal/address"
)

// The limits the Agent Skills specification sets.
const (
	maxNameLength          = 64
	maxDescriptionLength   = 1024
	maxCompatibilityLength = 500
)

// skillFields are the frontmatter fields the Agent Skills specification allows.
var skillFields = []string{"name", "description", "license", "compatibility", "metadata", "allowed-tools"}

// Skill is a SKILL.md as the Agent Skills specification defines it.
type Skill struct {
	Path          string
	Directory     string // the folder holding SKILL.md; the name must match it
	Name          string
	Description   string
	License       string
	Compatibility string
	Metadata      map[string]string
	AllowedTools  string
}

// ParseSkill reads a SKILL.md. When the frontmatter cannot be read at all,
// parsed is false and the one problem says why; otherwise the skill is
// returned with every field problem found.
func ParseSkill(path string, directory string, content []byte) (skill Skill, problems []Problem, parsed bool) {
	skill = Skill{Path: path, Directory: directory}
	frontmatter, _, found, err := SplitFrontmatter(string(content))
	if err != nil {
		return skill, []Problem{{Severity: SeverityError, Message: err.Error()}}, false
	}
	if !found {
		return skill, []Problem{{Severity: SeverityError, Message: "SKILL.md has no frontmatter; the specification requires name and description"}}, false
	}
	mapping, err := parseMapping(frontmatter)
	if err != nil {
		return skill, []Problem{{Severity: SeverityError, Message: err.Error()}}, false
	}
	for _, field := range slices.Sorted(maps.Keys(mapping)) {
		if !slices.Contains(skillFields, field) {
			problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("frontmatter field %q is not in the Agent Skills specification (name, description, license, compatibility, metadata, allowed-tools)", field)})
		}
	}
	skill.Name, problems = stringField(mapping, "name", problems)
	skill.Description, problems = stringField(mapping, "description", problems)
	skill.License, problems = stringField(mapping, "license", problems)
	skill.Compatibility, problems = stringField(mapping, "compatibility", problems)
	skill.AllowedTools, problems = stringField(mapping, "allowed-tools", problems)
	skill.Metadata, problems = metadataField(mapping, problems)
	return skill, problems, true
}

// metadataField reads the optional metadata mapping of string to string.
func metadataField(mapping map[string]any, problems []Problem) (metadata map[string]string, updated []Problem) {
	raw, present := mapping["metadata"]
	if !present {
		return nil, problems
	}
	entries, ok := raw.(map[string]any)
	if !ok {
		return nil, append(problems, Problem{Severity: SeverityError, Message: "frontmatter field \"metadata\" must be a mapping of string keys to string values"})
	}
	metadata = map[string]string{}
	for _, key := range slices.Sorted(maps.Keys(entries)) {
		text, isString := entries[key].(string)
		if !isString {
			problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("metadata value for %q must be a string", key)})
			continue
		}
		metadata[key] = text
	}
	return metadata, problems
}

// Validate checks the specification's constraints on the parsed fields.
func (skill Skill) Validate() []Problem {
	var problems []Problem
	switch {
	case skill.Name == "":
		problems = append(problems, Problem{Severity: SeverityError, Message: "name is required"})
	case len(skill.Name) > maxNameLength:
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("name is %d characters; the maximum is %d", len(skill.Name), maxNameLength)})
	case !address.IsSlug(skill.Name):
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("name %q may only contain lowercase letters, digits and single hyphens, with none at the ends", skill.Name)})
	case skill.Name != skill.Directory:
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("name %q does not match the directory name %q", skill.Name, skill.Directory)})
	}
	switch {
	case skill.Description == "":
		problems = append(problems, Problem{Severity: SeverityError, Message: "description is required"})
	case len(skill.Description) > maxDescriptionLength:
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("description is %d characters; the maximum is %d", len(skill.Description), maxDescriptionLength)})
	}
	if len(skill.Compatibility) > maxCompatibilityLength {
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("compatibility is %d characters; the maximum is %d", len(skill.Compatibility), maxCompatibilityLength)})
	}
	return problems
}
