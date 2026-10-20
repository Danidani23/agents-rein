package declaration

import "fmt"

// Rule is a Markdown rule file. Without frontmatter it loads in every
// session; with a paths list it loads when a matching file is touched.
type Rule struct {
	Path  string
	Paths []string
}

// ruleDeclarationFields are the frontmatter fields a rule declaration in an
// org repository may carry: Claude Code's paths and rein's selectors, which
// the compiler strips before writing the rule into a clone.
var ruleDeclarationFields = []string{"paths", "applyIf", "doNotApplyIf", "enforcement"}

// ParseRuleDeclaration reads a rule as an org repository holds it: paths and
// the selectors; any other field is a warning.
func ParseRuleDeclaration(path string, content []byte) (rule Rule, selectors Selectors, problems []Problem) {
	rule = Rule{Path: path}
	frontmatter, _, found, err := SplitFrontmatter(string(content))
	if err != nil {
		return rule, selectors, []Problem{{Severity: SeverityError, Message: err.Error()}}
	}
	if !found {
		return rule, selectors, nil
	}
	mapping, err := parseMapping(frontmatter)
	if err != nil {
		return rule, selectors, []Problem{{Severity: SeverityError, Message: err.Error()}}
	}
	for field := range mapping {
		known := false
		for _, candidate := range ruleDeclarationFields {
			if field == candidate {
				known = true
			}
		}
		if !known {
			problems = append(problems, Problem{Severity: SeverityWarning, Message: fmt.Sprintf("frontmatter field %q is neither paths nor a selector (applyIf, doNotApplyIf, enforcement)", field)})
		}
	}
	rule.Paths, problems = stringList(mapping, "paths", problems)
	selectors.ApplyIf, problems = stringList(mapping, "applyIf", problems)
	selectors.DoNotApplyIf, problems = stringList(mapping, "doNotApplyIf", problems)
	if raw, present := mapping["enforcement"]; present {
		text, isString := raw.(string)
		if !isString {
			problems = append(problems, Problem{Severity: SeverityError, Message: "enforcement must be required or default"})
		} else {
			selectors.Enforcement = text
		}
	}
	problems = append(problems, selectors.check()...)
	return rule, selectors, problems
}

// stringList reads an optional list of strings from the frontmatter.
func stringList(mapping map[string]any, field string, problems []Problem) ([]string, []Problem) {
	raw, present := mapping[field]
	if !present {
		return nil, problems
	}
	entries, ok := raw.([]any)
	if !ok {
		return nil, append(problems, Problem{Severity: SeverityError, Message: field + " must be a list"})
	}
	var values []string
	for _, entry := range entries {
		text, isString := entry.(string)
		if !isString {
			problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("%s entry %v is not a string", field, entry)})
			continue
		}
		values = append(values, text)
	}
	return values, problems
}

// ParseRule reads a rule file as an agent reads it. Only "paths" is a field
// Claude Code reads; any other field is reported as a warning, since it is
// ignored silently.
func ParseRule(path string, content []byte) (rule Rule, problems []Problem) {
	rule = Rule{Path: path}
	frontmatter, _, found, err := SplitFrontmatter(string(content))
	if err != nil {
		return rule, []Problem{{Severity: SeverityError, Message: err.Error()}}
	}
	if !found {
		return rule, nil
	}
	mapping, err := parseMapping(frontmatter)
	if err != nil {
		return rule, []Problem{{Severity: SeverityError, Message: err.Error()}}
	}
	for field := range mapping {
		if field != "paths" {
			problems = append(problems, Problem{Severity: SeverityWarning, Message: fmt.Sprintf("frontmatter field %q is not one Claude Code reads in a rule; only paths is", field)})
		}
	}
	raw, present := mapping["paths"]
	if !present {
		return rule, problems
	}
	entries, ok := raw.([]any)
	if !ok {
		return rule, append(problems, Problem{Severity: SeverityError, Message: "paths must be a list of globs"})
	}
	for _, entry := range entries {
		glob, isString := entry.(string)
		if !isString {
			problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("paths entry %v is not a string", entry)})
			continue
		}
		rule.Paths = append(rule.Paths, glob)
	}
	return rule, problems
}
