package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
)

func TestSlugOf(t *testing.T) {
	cases := map[string]string{"Acme Corp": "acme-corp", "  --Acme!! Corp 2 ": "acme-corp-2", "!!!": "", "dani": "dani"}
	for name, expected := range cases {
		if SlugOf(name) != expected {
			t.Errorf("SlugOf(%q) = %q, expected %q", name, SlugOf(name), expected)
		}
	}
}

func TestNewID(t *testing.T) {
	first, second := NewID(), NewID()
	if !declaration.IsUUID(first) || !declaration.IsUUID(second) || first == second {
		t.Fatalf("ids %q %q", first, second)
	}
}

func TestFilesMakeAnOrgRepositoryTheLintAccepts(t *testing.T) {
	organization := declaration.Organization{ID: NewID(), Slug: "acme", Name: "Acme", Description: "Agent settings of Acme", Host: "github"}
	files, err := Files(organization, "Danidani23")
	if err != nil {
		t.Fatalf("files: %v", err)
	}
	root := t.TempDir()
	err = Write(root, files)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	tree, problems, err := declaration.LoadTree(root)
	if err != nil || len(problems) != 0 || len(tree.Labels) != 3 {
		t.Fatalf("tree err %v problems %+v labels %d", err, problems, len(tree.Labels))
	}
	codeowners, _ := os.ReadFile(filepath.Join(root, "CODEOWNERS"))
	readme, _ := os.ReadFile(filepath.Join(root, "README.md"))
	record, _ := os.ReadFile(filepath.Join(root, "declarations", "organizations", "acme", "organization.yaml"))
	if !strings.Contains(string(codeowners), "* @Danidani23") || !strings.HasPrefix(string(readme), "# Acme") || !strings.Contains(string(record), "host: github") || !strings.HasPrefix(string(record), "# Written by rein org create") {
		t.Fatalf("codeowners %q readme %q record %q", codeowners, readme, record)
	}
}
