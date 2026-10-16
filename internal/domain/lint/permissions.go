package lint

import "strings"

// permissionRule is a rule such as Bash(git push*), split into its tool and
// its specifier.
type permissionRule struct {
	Tool         string
	Specifier    string
	HasSpecifier bool
}

func parsePermissionRule(text string) permissionRule {
	open := strings.Index(text, "(")
	if open < 0 || !strings.HasSuffix(text, ")") {
		return permissionRule{Tool: text}
	}
	return permissionRule{Tool: text[:open], Specifier: text[open+1 : len(text)-1], HasSpecifier: true}
}

// covers reports whether a deny rule makes an allow rule dead: everything the
// allow would permit, the deny refuses first. An allow broader than the deny
// is the usual "allow git except push" shape and is not covered.
func covers(deny string, allow string) bool {
	denied := parsePermissionRule(deny)
	allowed := parsePermissionRule(allow)
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
		return strings.TrimRight(strings.TrimSuffix(specifier, ":*"), " "), true
	case strings.HasSuffix(specifier, "*"):
		return strings.TrimRight(strings.TrimSuffix(specifier, "*"), " "), true
	}
	return specifier, false
}
