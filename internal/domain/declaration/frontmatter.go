// Package declaration reads the files a harness is made of: SKILL.md skills,
// Markdown rules and Claude Code settings files. Each parser returns the
// problems it found instead of stopping at the first, so a lint run can
// report a file completely.
package declaration

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Severity says how much a problem matters: an error fails the lint, a
// warning is reported and lets it pass.
type Severity string

// The two severities.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Problem is one thing wrong with a declaration file.
type Problem struct {
	Severity Severity
	Message  string
}

// SplitFrontmatter separates a YAML frontmatter block, fenced by "---" lines
// at the top of the file, from the Markdown body. A file that does not open
// with "---" has no frontmatter and found is false.
func SplitFrontmatter(content string) (frontmatter string, body string, found bool, err error) {
	normalised := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalised, "---\n") {
		return "", normalised, false, nil
	}
	lines := strings.Split(normalised[len("---\n"):], "\n")
	for index, line := range lines {
		if line == "---" {
			return strings.Join(lines[:index], "\n"), strings.Join(lines[index+1:], "\n"), true, nil
		}
	}
	return "", "", true, fmt.Errorf("frontmatter opens with --- but never closes")
}

// parseMapping decodes frontmatter into a mapping; a scalar or a sequence at
// the top level is refused.
func parseMapping(frontmatter string) (map[string]any, error) {
	var decoded any
	err := yaml.Unmarshal([]byte(frontmatter), &decoded)
	if err != nil {
		return nil, fmt.Errorf("frontmatter is not valid YAML: %w", err)
	}
	if decoded == nil {
		return map[string]any{}, nil
	}
	mapping, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("frontmatter must be a mapping of field to value")
	}
	return mapping, nil
}

// stringField reads an optional string field; a value of another type is a
// problem and reads as empty.
func stringField(mapping map[string]any, field string, problems []Problem) (value string, updated []Problem) {
	raw, present := mapping[field]
	if !present {
		return "", problems
	}
	text, ok := raw.(string)
	if !ok {
		return "", append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("frontmatter field %q must be a string", field)})
	}
	return text, problems
}
