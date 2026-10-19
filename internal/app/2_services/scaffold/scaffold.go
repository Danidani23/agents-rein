// Package scaffold writes the first content of an org repository: the
// organisation record, the seeded label registry, CODEOWNERS and a README.
// It is a library: pure functions from a record to files.
package scaffold

import (
	"crypto/rand"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
)

const header = "# Written by rein org create. Change it through a pull request; rein lint checks it.\n"

// NewID is a fresh lowercase UUID, version 4.
func NewID() string {
	var bytes [16]byte
	_, err := rand.Read(bytes[:])
	if err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	bytes[6] = bytes[6]&0x0f | 0x40
	bytes[8] = bytes[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}

var notSlugCharacters = regexp.MustCompile(`[^a-z0-9]+`)

// SlugOf derives an address slug from a display name: lowercase, every run
// of other characters a single hyphen, none at the ends. Empty when the name
// has no letter or digit.
func SlugOf(name string) string {
	return strings.Trim(notSlugCharacters.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// Files are the first files of an org repository, by path relative to its
// root, with forward slashes. codeowner is the host login that reviews
// every change.
func Files(organization declaration.Organization, codeowner string) (map[string]string, error) {
	files := map[string]string{}
	record, err := yaml.Marshal(organization)
	if err != nil {
		return nil, err
	}
	organizationFolder := path.Join("declarations", "organizations", organization.Slug)
	files[path.Join(organizationFolder, "organization.yaml")] = header + string(record)
	for _, category := range declaration.SeededLabels() {
		content, err := yaml.Marshal(category)
		if err != nil {
			return nil, err
		}
		files[path.Join(organizationFolder, "labels", category.Category+".yaml")] = header + string(content)
	}
	files["CODEOWNERS"] = "# Every change is reviewed by a code owner. Written by rein org create.\n* @" + codeowner + "\n"
	files["README.md"] = "# " + organization.Name + "\n\n" +
		"The agent settings of " + organization.Name + ", managed with Agents Rein: instructions, rules,\n" +
		"skills, tool servers, hooks and permissions as declarations under `declarations/`, at the\n" +
		"organisation, team, project and user levels. Change them through a pull request; `rein lint`\n" +
		"checks every change, and `rein sync` delivers them to each project clone.\n"
	return files, nil
}

// TeamRecord is the file of a team record, by path relative to the org
// repository's root.
func TeamRecord(organizationSlug string, team declaration.Team) (relativePath string, content string, err error) {
	record, err := yaml.Marshal(team)
	if err != nil {
		return "", "", err
	}
	return path.Join("declarations", "organizations", organizationSlug, "teams", team.Slug, "team.yaml"), header + string(record), nil
}

// ProjectRecord is the file of a project record, by path relative to the
// org repository's root.
func ProjectRecord(organizationSlug string, project declaration.Project) (relativePath string, content string, err error) {
	record, err := yaml.Marshal(project)
	if err != nil {
		return "", "", err
	}
	return path.Join("declarations", "organizations", organizationSlug, "projects", project.Slug, "project.yaml"), header + string(record), nil
}

// Write puts files under root.
func Write(root string, files map[string]string) error {
	for relative, content := range files {
		target := filepath.Join(root, filepath.FromSlash(relative))
		err := os.MkdirAll(filepath.Dir(target), 0o755)
		if err != nil {
			return err
		}
		err = os.WriteFile(target, []byte(content), 0o644)
		if err != nil {
			return err
		}
	}
	return nil
}
