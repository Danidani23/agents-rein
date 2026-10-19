package declaration

import (
	"fmt"
	"regexp"
	"strings"
)

// PermissionSet is a permissions declaration, permissions/<id>.yaml. The
// rule strings are an agent's own syntax, named by format, and the compiler
// emits them for that agent alone.
type PermissionSet struct {
	Selectors `yaml:",inline"`
	Format    string   `yaml:"format"`
	Allow     []string `yaml:"allow,omitempty"`
	Deny      []string `yaml:"deny,omitempty"`
	Ask       []string `yaml:"ask,omitempty"`
}

// Formats the lint knows the rule syntax of.
var Formats = []string{"claude-code"}

// claudeCodeRulePattern is Tool or Tool(specifier), Claude Code's permission
// rule syntax.
var claudeCodeRulePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\(.*\))?$`)

// ParsePermissionSet reads one permissions declaration; file is its name
// without the extension.
func ParsePermissionSet(file string, content []byte) (set PermissionSet, problems []Problem) {
	problems = decodeStrict(content, &set, file+".yaml")
	if problems != nil {
		return PermissionSet{}, problems
	}
	problems = append(problems, set.Selectors.check()...)
	problems = append(problems, checkFormat(set.Format)...)
	if len(set.Allow)+len(set.Deny)+len(set.Ask) == 0 {
		problems = append(problems, Problem{Severity: SeverityError, Message: "a permission set needs at least one rule under allow, deny or ask"})
	}
	if set.Format == "claude-code" {
		for list, rules := range map[string][]string{"allow": set.Allow, "deny": set.Deny, "ask": set.Ask} {
			for _, rule := range rules {
				if !claudeCodeRulePattern.MatchString(rule) {
					problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("%s rule %q is not Tool or Tool(specifier)", list, rule)})
				}
			}
		}
	}
	return set, problems
}

func checkFormat(format string) []Problem {
	if format == "" {
		return []Problem{{Severity: SeverityError, Message: "format is required: " + strings.Join(Formats, ", ")}}
	}
	for _, known := range Formats {
		if format == known {
			return nil
		}
	}
	return []Problem{{Severity: SeverityError, Message: fmt.Sprintf("format %q is unknown; this build knows %s", format, strings.Join(Formats, ", "))}}
}
