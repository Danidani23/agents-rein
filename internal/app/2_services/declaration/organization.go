package declaration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IsUUID reports whether text is a lowercase UUID.
func IsUUID(text string) bool {
	return uuidPattern.MatchString(text)
}

// Hosts rein can talk to, the first being the only one this build supports
// (D8). An organisation names its host in its record; the choice is made at
// org create and never on a machine.
var Hosts = []string{"github"}

// PlannedHosts are listed so people see where the tool is going; a record
// naming one is refused.
var PlannedHosts = []string{"gitlab"}

// Organization is the record of an organisation, organization.yaml under
// declarations/organizations/<slug>/. The id never changes; the slug is the
// address segment; name and description are free to change; host says which
// forge holds the org repository and its identities.
type Organization struct {
	ID          string `yaml:"id"`
	Slug        string `yaml:"slug"`
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Host        string `yaml:"host"`
	// MinimumReinVersion makes an older rein refuse to sync (P2.8).
	MinimumReinVersion string `yaml:"minimum_rein_version,omitempty"`
	// CreatedAt is when the record was written (D20).
	CreatedAt time.Time `yaml:"created_at,omitempty"`
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
	problems = append(problems, checkHost(organization.Host)...)
	return organization, problems
}

func checkHost(host string) []Problem {
	if host == "" {
		return []Problem{{Severity: SeverityError, Message: "host is required; this build supports " + strings.Join(Hosts, ", ")}}
	}
	for _, supported := range Hosts {
		if host == supported {
			return nil
		}
	}
	for _, planned := range PlannedHosts {
		if host == planned {
			return []Problem{{Severity: SeverityError, Message: fmt.Sprintf("host %q is planned but not yet supported; this build supports %s", host, strings.Join(Hosts, ", "))}}
		}
	}
	return []Problem{{Severity: SeverityError, Message: fmt.Sprintf("host %q is unknown; this build supports %s", host, strings.Join(Hosts, ", "))}}
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
