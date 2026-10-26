package declaration

import (
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
)

// Team is the record of a team, team.yaml under teams/<slug>/.
type Team struct {
	ID          string    `yaml:"id"`
	Slug        string    `yaml:"slug"`
	Name        string    `yaml:"name"`
	Description string    `yaml:"description"`
	CreatedAt   time.Time `yaml:"created_at,omitempty"`
	// Skip lists the addresses of default declarations above this level
	// that do not apply to it; a required one needs an exception.
	Skip []string `yaml:"skip"`
}

// Project is the record of a project, project.yaml under projects/<slug>/.
// Teams are referenced, so several teams can share a project and be added or
// removed without moving files; labels decide which declarations apply; path
// is where the project lives inside its repository, "." for the root;
// fingerprint is the sorted root commit hashes of its repository, empty
// until the first init.
type Project struct {
	ID          string    `yaml:"id"`
	Slug        string    `yaml:"slug"`
	Name        string    `yaml:"name"`
	Description string    `yaml:"description"`
	Teams       []string  `yaml:"teams"`
	Labels      []string  `yaml:"labels"`
	Path        string    `yaml:"path"`
	Fingerprint []string  `yaml:"fingerprint"`
	CreatedAt   time.Time `yaml:"created_at,omitempty"`
	UpdatedAt   time.Time `yaml:"updated_at,omitempty"`
	// Skip lists the addresses of default declarations above this level
	// that do not apply to it; a required one needs an exception.
	Skip []string `yaml:"skip"`
}

// User is the record of a person, user.yaml under users/<slug>/. GitHub is
// the login exactly as the host has it (D8); the slug is its lowercase form
// and the folder name.
type User struct {
	Slug      string    `yaml:"slug"`
	GitHub    string    `yaml:"github"`
	Name      string    `yaml:"name"`
	CreatedAt time.Time `yaml:"created_at,omitempty"`
}

// LabelCategory is one file of the label registry, labels/<category>.yaml:
// the values a project may carry under that category, each with a short
// description.
type LabelCategory struct {
	Category string `yaml:"category"`
	// Required means every project carries at least one value of it; the
	// creators insist, the lint warns.
	Required bool `yaml:"required"`
	// Multiple lets a project carry several values of it; without it one
	// value is the most, and two are a contradiction.
	Multiple bool              `yaml:"multiple"`
	Labels   map[string]string `yaml:"labels"`
}

func decodeStrict(content []byte, into any, file string) []Problem {
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	err := decoder.Decode(into)
	if err != nil {
		return []Problem{{Severity: SeverityError, Message: file + ": " + err.Error()}}
	}
	return nil
}

func checkSlug(slug string, folder string) []Problem {
	switch {
	case slug == "":
		return []Problem{{Severity: SeverityError, Message: "slug is required"}}
	case !address.IsSlug(slug):
		return []Problem{{Severity: SeverityError, Message: fmt.Sprintf("slug %q may only contain lowercase letters, digits and single hyphens", slug)}}
	case slug != folder:
		return []Problem{{Severity: SeverityError, Message: fmt.Sprintf("slug %q does not match the folder name %q", slug, folder)}}
	}
	return nil
}

func checkID(id string) []Problem {
	if !IsUUID(id) {
		return []Problem{{Severity: SeverityError, Message: fmt.Sprintf("id %q is not a lowercase UUID", id)}}
	}
	return nil
}

// ParseTeam reads a team record; folder is the slug folder holding it.
func ParseTeam(folder string, content []byte) (team Team, problems []Problem) {
	problems = decodeStrict(content, &team, "team.yaml")
	if problems != nil {
		return Team{}, problems
	}
	problems = append(problems, checkID(team.ID)...)
	problems = append(problems, checkSlug(team.Slug, folder)...)
	if team.Name == "" {
		problems = append(problems, Problem{Severity: SeverityError, Message: "name is required"})
	}
	problems = append(problems, checkSkip(team.Skip)...)
	return team, problems
}

// ParseProject reads a project record; folder is the slug folder holding it.
func ParseProject(folder string, content []byte) (project Project, problems []Problem) {
	problems = decodeStrict(content, &project, "project.yaml")
	if problems != nil {
		return Project{}, problems
	}
	problems = append(problems, checkID(project.ID)...)
	problems = append(problems, checkSlug(project.Slug, folder)...)
	if project.Name == "" {
		problems = append(problems, Problem{Severity: SeverityError, Message: "name is required"})
	}
	if len(project.Teams) == 0 {
		problems = append(problems, Problem{Severity: SeverityError, Message: "teams is required: a project belongs to at least one team"})
	}
	for _, team := range project.Teams {
		if !address.IsSlug(team) {
			problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("team %q is not a slug", team)})
		}
	}
	for _, label := range project.Labels {
		_, _, ok := SplitLabel(label)
		if !ok {
			problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("label %q is not category:value", label)})
		}
	}
	if project.Path == "" {
		project.Path = "."
	}
	cleaned := path.Clean(strings.TrimPrefix(project.Path, "./"))
	if path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("path %q must be a relative path inside the repository", project.Path)})
	} else {
		project.Path = cleaned
	}
	for _, hash := range project.Fingerprint {
		if len(hash) != 40 || strings.Trim(hash, "0123456789abcdef") != "" {
			problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("fingerprint %q is not a commit hash", hash)})
		}
	}
	problems = append(problems, checkSkip(project.Skip)...)
	return project, problems
}

// checkSkip reports skip entries that are not declaration addresses.
func checkSkip(skip []string) (problems []Problem) {
	for _, entry := range skip {
		parsed, err := address.Parse(entry)
		if err != nil || !parsed.IsDeclaration() {
			problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("skip entry %q is not the address of a declaration", entry)})
		}
	}
	return problems
}

// ParseUser reads a user record; folder is the slug folder holding it.
func ParseUser(folder string, content []byte) (user User, problems []Problem) {
	problems = decodeStrict(content, &user, "user.yaml")
	if problems != nil {
		return User{}, problems
	}
	problems = append(problems, checkSlug(user.Slug, folder)...)
	switch {
	case user.GitHub == "":
		problems = append(problems, Problem{Severity: SeverityError, Message: "github is required: the login exactly as GitHub has it"})
	case strings.ToLower(user.GitHub) != user.Slug:
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("slug %q must be the lowercase form of the github login %q", user.Slug, user.GitHub)})
	}
	return user, problems
}

// ParseLabelCategory reads one registry file; file is its name without the
// extension, which the category must equal.
func ParseLabelCategory(file string, content []byte) (category LabelCategory, problems []Problem) {
	problems = decodeStrict(content, &category, file+".yaml")
	if problems != nil {
		return LabelCategory{}, problems
	}
	switch {
	case category.Category == "":
		problems = append(problems, Problem{Severity: SeverityError, Message: "category is required"})
	case !address.IsSlug(category.Category):
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("category %q is not a slug", category.Category)})
	case category.Category != file:
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("category %q does not match the file name %q", category.Category, file)})
	}
	if len(category.Labels) == 0 {
		problems = append(problems, Problem{Severity: SeverityError, Message: "labels is required: at least one value"})
	}
	for value := range category.Labels {
		if !address.IsSlug(value) {
			problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("label value %q is not a slug", value)})
		}
	}
	return category, problems
}

// SplitLabel splits category:value.
func SplitLabel(label string) (category string, value string, ok bool) {
	parts := strings.SplitN(label, ":", 2)
	if len(parts) != 2 || !address.IsSlug(parts[0]) || !address.IsSlug(parts[1]) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// SeededLabels is the registry a new organisation starts with: enough for a
// project to say what it is, edited freely afterwards.
func SeededLabels() []LabelCategory {
	return []LabelCategory{
		{Category: "language", Multiple: true, Labels: map[string]string{
			"go": "Go", "typescript": "TypeScript", "python": "Python", "dart": "Dart", "java": "Java", "rust": "Rust",
		}},
		{Category: "kind", Required: true, Labels: map[string]string{
			"static-site": "a static website", "web-app": "a web application", "service": "a backend service",
			"cli": "a command-line tool", "library": "a library", "infrastructure": "infrastructure as code",
		}},
		{Category: "data", Labels: map[string]string{
			"public": "no data that must stay inside the organisation", "internal": "internal data", "regulated": "data under a regulation",
		}},
		// what a project uses, beside what it is: several at once (D40)
		{Category: "stack", Multiple: true, Labels: map[string]string{
			"opentofu": "OpenTofu or Terraform, which are compatible", "docker": "containers built with Docker",
			"kubernetes": "deployed to Kubernetes", "react": "a React front end",
		}},
	}
}

// LabelValues lists a category's values in order.
func (category LabelCategory) LabelValues() []string {
	values := make([]string, 0, len(category.Labels))
	for value := range category.Labels {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

// MissingRequiredCategories lists the required categories, in order, that
// none of the labels belongs to.
func MissingRequiredCategories(registry map[string]LabelCategory, labels []string) []string {
	present := map[string]bool{}
	for _, label := range labels {
		category, _, ok := SplitLabel(label)
		if ok {
			present[category] = true
		}
	}
	var missing []string
	for _, name := range sortedKeys(registry) {
		if registry[name].Required && !present[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// SingleValueConflicts says, one sentence per category, where the labels
// carry several values of a category that allows one.
func SingleValueConflicts(registry map[string]LabelCategory, labels []string) []string {
	valuesByCategory := map[string][]string{}
	for _, label := range labels {
		category, value, ok := SplitLabel(label)
		if ok {
			valuesByCategory[category] = append(valuesByCategory[category], value)
		}
	}
	var conflicts []string
	for _, name := range sortedKeys(valuesByCategory) {
		registered, found := registry[name]
		if found && !registered.Multiple && len(valuesByCategory[name]) > 1 {
			conflicts = append(conflicts, fmt.Sprintf("%s takes one value; given %s", name, strings.Join(valuesByCategory[name], ", ")))
		}
	}
	return conflicts
}
