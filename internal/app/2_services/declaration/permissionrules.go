package declaration

import "strings"

// PermissionRule is a Claude Code permission rule taken apart: the tool and
// its optional specifier.
type PermissionRule struct {
	Tool         string
	Specifier    string
	HasSpecifier bool
}

// ParsePermissionRule splits Tool(specifier).
func ParsePermissionRule(rule string) PermissionRule {
	open := strings.Index(rule, "(")
	if open < 0 || !strings.HasSuffix(rule, ")") {
		return PermissionRule{Tool: rule}
	}
	return PermissionRule{Tool: rule[:open], Specifier: rule[open+1 : len(rule)-1], HasSpecifier: true}
}

// CoversPermission says whether a deny rule covers an allow rule, so the
// allow is dead at runtime: the same tool, and the deny's specifier absent,
// equal, or a prefix the allow's specifier starts with.
func CoversPermission(deny string, allow string) bool {
	denied := ParsePermissionRule(deny)
	allowed := ParsePermissionRule(allow)
	if denied.Tool != allowed.Tool {
		return false
	}
	if !denied.HasSpecifier {
		return true
	}
	if !allowed.HasSpecifier {
		return false
	}
	denyBase, denyIsPrefix := specifierBase(denied.Specifier)
	if !denyIsPrefix {
		return allowed.Specifier == denied.Specifier
	}
	allowBase, _ := specifierBase(allowed.Specifier)
	return allowBase == denyBase || strings.HasPrefix(allowBase, denyBase+" ")
}

// specifierBase strips a trailing wildcard, "*" or ":*", and the space before
// it; isPrefix says whether there was one.
func specifierBase(specifier string) (base string, isPrefix bool) {
	switch {
	case strings.HasSuffix(specifier, ":*"):
		return strings.TrimSpace(strings.TrimSuffix(specifier, ":*")), true
	case strings.HasSuffix(specifier, "*"):
		return strings.TrimSpace(strings.TrimSuffix(specifier, "*")), true
	}
	return specifier, false
}
