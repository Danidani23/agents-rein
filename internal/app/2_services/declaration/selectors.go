package declaration

import (
	"fmt"
	"regexp"
	"strings"
)

// Selectors are the fields every declaration carries: what it is for, which
// projects it applies to by their labels, and how firmly (the board's
// "Selectors, enforcement, skip lists and exceptions").
type Selectors struct {
	Description  string   `yaml:"description,omitempty"`
	ApplyIf      []string `yaml:"applyIf,omitempty"`
	DoNotApplyIf []string `yaml:"doNotApplyIf,omitempty"`
	// Enforcement is required or default; empty means default.
	Enforcement string `yaml:"enforcement,omitempty"`
}

// Enforcements a declaration may name.
const (
	EnforcementRequired = "required"
	EnforcementDefault  = "default"
)

// check reports malformed labels and an unknown enforcement.
func (selectors Selectors) check() (problems []Problem) {
	for _, label := range append(append([]string{}, selectors.ApplyIf...), selectors.DoNotApplyIf...) {
		_, _, ok := SplitLabel(label)
		if !ok {
			problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("label %q is not category:value", label)})
		}
	}
	switch selectors.Enforcement {
	case "", EnforcementRequired, EnforcementDefault:
	default:
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("enforcement %q is not required or default", selectors.Enforcement)})
	}
	return problems
}

// VariablePrefix is what every environment variable a declaration references
// starts with, so a shell profile shows what rein's declarations need and
// nothing collides with another program's variables (D22).
const VariablePrefix = "REIN_"

var referencePattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// References lists the ${VARIABLE} references in a value.
func References(value string) (names []string) {
	for _, match := range referencePattern.FindAllStringSubmatch(value, -1) {
		names = append(names, match[1])
	}
	return names
}

var secretPrefixes = []string{"ghp_", "gho_", "ghu_", "ghs_", "github_pat_", "sk-", "sk_live_", "sk_test_", "xoxb-", "xoxp-", "AKIA", "glpat-", "AIza"}

// LooksLikeSecret guesses whether a literal value is a credential: a known
// token prefix, or a long unspaced string of letters and digits.
func LooksLikeSecret(value string) bool {
	for _, prefix := range secretPrefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	if len(value) < 32 || strings.ContainsAny(value, " /.:") {
		return false
	}
	hasLetter, hasDigit := false, false
	for _, character := range value {
		switch {
		case character >= '0' && character <= '9':
			hasDigit = true
		case (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z'):
			hasLetter = true
		}
	}
	return hasLetter && hasDigit
}

// checkValues checks the values of env or headers: a literal that looks like
// a secret is an error, a reference must carry the prefix and be described
// in variables.
func checkValues(kind string, values map[string]string, variables map[string]string, referenced map[string]bool) (problems []Problem) {
	for key, value := range values {
		names := References(value)
		if len(names) == 0 && LooksLikeSecret(value) {
			problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("%s %s looks like a secret; reference a variable, ${%sNAME}, and describe it under variables", kind, key, VariablePrefix)})
			continue
		}
		for _, name := range names {
			referenced[name] = true
			if !strings.HasPrefix(name, VariablePrefix) {
				problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("%s %s references ${%s}; variables rein's declarations reference start with %s", kind, key, name, VariablePrefix)})
			}
			_, described := variables[name]
			if !described {
				problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("%s %s references ${%s}, which variables does not describe", kind, key, name)})
			}
		}
	}
	return problems
}
