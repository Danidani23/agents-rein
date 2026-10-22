// Package address names where a declaration is stored in an org repository,
// for instance organizations.dani.teams.web.skills.commit. An address is a
// sequence of collection and slug pairs; the first pair is the organisation,
// the last pair is the kind and the id. The org repository holds one
// organisation, so its folders leave the first pair out: the address above
// is the path teams/web/skills/commit under declarations/.
package address

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// The collections an address may contain.
const (
	Organizations = "organizations"
	Users         = "users"
	Teams         = "teams"
	Projects      = "projects"
	Labels        = "labels"
	Exceptions    = "exceptions"
	Skills        = "skills"
	Rules         = "rules"
	MCPServers    = "mcpservers"
	Hooks         = "hooks"
	Permissions   = "permissions"
	Agents        = "agents"
)

// declarationKinds are terminal: nothing nests below a declaration.
var declarationKinds = map[string]bool{
	Labels: true, Exceptions: true, Skills: true, Rules: true,
	MCPServers: true, Hooks: true, Permissions: true, Agents: true,
}

// allowedBelow lists the collections that may follow a pair of each
// collection; the empty key is the root.
var allowedBelow = map[string][]string{
	"":            {Organizations},
	Organizations: {Teams, Projects, Users, Labels, Exceptions, Skills, Rules, MCPServers, Hooks, Permissions, Agents},
	Teams:         {Exceptions, Skills, Rules, MCPServers, Hooks, Permissions, Agents},
	Projects:      {Skills, Rules, MCPServers, Hooks, Permissions, Agents},
	Users:         {Skills, Rules, MCPServers, Hooks, Permissions, Agents},
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// IsOrganizationCollection says whether a folder name directly under
// declarations/ belongs to the layout: teams, projects, users, labels,
// exceptions or a declaration kind.
func IsOrganizationCollection(name string) bool {
	return contains(allowedBelow[Organizations], name)
}

// IsSlug reports whether text is lowercase letters, digits and single hyphens,
// with no hyphen at either end.
func IsSlug(text string) bool {
	return slugPattern.MatchString(text)
}

// Address locates a level, a record or a declaration in the org repository.
type Address struct {
	pairs []pair
}

type pair struct {
	collection string
	slug       string
}

// Parse reads a dotted address such as organizations.dani.skills.commit.
func Parse(text string) (Address, error) {
	if text == "" {
		return Address{}, fmt.Errorf("address is empty")
	}
	return fromSegments(strings.Split(text, "."), text)
}

// FromPath reads an address from a directory path relative to the
// declarations root of the organisation's repository, such as
// teams/web/skills/commit; the empty path is the organisation itself.
func FromPath(relativePath string, organizationSlug string) (Address, error) {
	if !IsSlug(organizationSlug) {
		return Address{}, fmt.Errorf("organisation %q is not a slug", organizationSlug)
	}
	cleaned := filepath.ToSlash(filepath.Clean(relativePath))
	segments := []string{Organizations, organizationSlug}
	if cleaned != "." && cleaned != "" {
		segments = append(segments, strings.Split(cleaned, "/")...)
	}
	return fromSegments(segments, relativePath)
}

// LevelPath is the folder of a level under declarations/: "" for the
// organisation, teams/web, projects/shop or users/jane.
func LevelPath(level string) (string, error) {
	parsed, err := Parse(level)
	if err != nil {
		return "", err
	}
	return parsed.Path(), nil
}

func fromSegments(segments []string, source string) (Address, error) {
	if len(segments)%2 != 0 {
		return Address{}, fmt.Errorf("address %q: expected collection and slug pairs, got %d segments", source, len(segments))
	}
	var address Address
	previous := ""
	for index := 0; index < len(segments); index += 2 {
		collection := segments[index]
		slug := segments[index+1]
		if !contains(allowedBelow[previous], collection) {
			return Address{}, fmt.Errorf("address %q: %q may not follow %q", source, collection, describe(previous))
		}
		if !IsSlug(slug) {
			return Address{}, fmt.Errorf("address %q: %q is not a slug (lowercase letters, digits and single hyphens)", source, slug)
		}
		address.pairs = append(address.pairs, pair{collection: collection, slug: slug})
		previous = collection
	}
	return address, nil
}

func describe(collection string) string {
	if collection == "" {
		return "the root"
	}
	return collection
}

func contains(list []string, item string) bool {
	for _, candidate := range list {
		if candidate == item {
			return true
		}
	}
	return false
}

// String renders the dotted form.
func (address Address) String() string {
	parts := make([]string, 0, len(address.pairs)*2)
	for _, current := range address.pairs {
		parts = append(parts, current.collection, current.slug)
	}
	return strings.Join(parts, ".")
}

// Path is the address as a folder under declarations/, the organisation's
// pair left out: "" for the organisation, teams/web/skills/commit for a
// team's skill.
func (address Address) Path() string {
	parts := make([]string, 0, len(address.pairs)*2)
	for _, current := range address.pairs[min(1, len(address.pairs)):] {
		parts = append(parts, current.collection, current.slug)
	}
	return strings.Join(parts, "/")
}

// OrganizationSlug is the organisation the address belongs to.
func (address Address) OrganizationSlug() string {
	if len(address.pairs) == 0 {
		return ""
	}
	return address.pairs[0].slug
}

// Kind is the last collection, for instance "skills".
func (address Address) Kind() string {
	if len(address.pairs) == 0 {
		return ""
	}
	return address.pairs[len(address.pairs)-1].collection
}

// ID is the last slug, for instance "commit".
func (address Address) ID() string {
	if len(address.pairs) == 0 {
		return ""
	}
	return address.pairs[len(address.pairs)-1].slug
}

// Parent is the address without its last pair; found is false at the root.
func (address Address) Parent() (parent Address, found bool) {
	if len(address.pairs) <= 1 {
		return Address{}, false
	}
	return Address{pairs: address.pairs[:len(address.pairs)-1]}, true
}

// IsDeclaration reports whether the address names a declaration rather than
// an organisation, a team, a project or a user.
func (address Address) IsDeclaration() bool {
	return declarationKinds[address.Kind()]
}
