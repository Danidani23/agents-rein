package lint

import "github.com/Danidani23/agents-rein/internal/app/2_services/declaration"

// covers reports whether a deny rule makes an allow rule dead: everything the
// allow would permit, the deny refuses first. An allow broader than the deny
// is the usual "allow git except push" shape and is not covered.
func covers(deny string, allow string) bool {
	return declaration.CoversPermission(deny, allow)
}
