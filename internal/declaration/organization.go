package declaration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/reins/internal/address"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IsUUID reports whether text is a lowercase UUID.
func IsUUID(text string) bool {
	return uuidPattern.MatchString(text)
}

// Organization is the record of an organisation, organization.yaml under
// declarations/organizations/<slug>/. The id never changes; the slug is the
// address segment; name and description are free to change.
type Organization struct {
	ID          string `yaml:"id"`
	Slug        string `yaml:"slug"`
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

// ParseOrganization reads the record; folder is the slug folder holding it.
func ParseOrganization(folder string, content []byte) (organization Organization, problems []Problem) {
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	err := decoder.Decode(&organization)
	if err != nil {
		return Organization{}, []Problem{{Severity: SeverityError, Message: "organization.yaml: " + err.Error()}}
	}
	if !IsUUID(organization.ID) {
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("id %q is not a lowercase UUID", organization.ID)})
	}
	switch {
	case organization.Slug == "":
		problems = append(problems, Problem{Severity: SeverityError, Message: "slug is required"})
	case !address.IsSlug(organization.Slug):
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("slug %q may only contain lowercase letters, digits and single hyphens", organization.Slug)})
	case organization.Slug != folder:
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("slug %q does not match the folder name %q", organization.Slug, folder)})
	}
	if organization.Name == "" {
		problems = append(problems, Problem{Severity: SeverityError, Message: "name is required"})
	}
	return organization, problems
}

// FindOrganization locates the one organisation record in an org repository
// clone, at declarations/organizations/<slug>/organization.yaml.
func FindOrganization(clonePath string) (Organization, error) {
	pattern := filepath.Join(clonePath, "declarations", "organizations", "*", "organization.yaml")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return Organization{}, err
	}
	switch len(matches) {
	case 0:
		return Organization{}, fmt.Errorf("not an org repository: no declarations/organizations/<slug>/organization.yaml in %s", clonePath)
	case 1:
	default:
		return Organization{}, fmt.Errorf("%s holds %d organisations; an org repository holds exactly one", clonePath, len(matches))
	}
	content, err := os.ReadFile(matches[0])
	if err != nil {
		return Organization{}, err
	}
	folder := filepath.Base(filepath.Dir(matches[0]))
	organization, problems := ParseOrganization(folder, content)
	if len(problems) > 0 {
		var messages []string
		for _, problem := range problems {
			messages = append(messages, problem.Message)
		}
		return Organization{}, fmt.Errorf("%s: %s", matches[0], strings.Join(messages, "; "))
	}
	return organization, nil
}
