package declaration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validOrganization = "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\ndescription: The pilot.\nhost: github\n"

func TestParseOrganization(t *testing.T) {
	t.Run("a valid record parses", func(t *testing.T) {
		organization, problems := ParseOrganization("acme", []byte(validOrganization))
		if len(problems) != 0 || organization.Name != "Acme" {
			t.Fatalf("organization %+v problems %v", organization, problems)
		}
	})
	cases := []struct {
		name     string
		folder   string
		content  string
		expected string
	}{
		{"an id that is not a UUID is refused", "acme", "id: 123\nslug: acme\nname: Acme\nhost: github\n", "not a lowercase UUID"},
		{"a missing host is refused", "acme", "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\n", "host is required"},
		{"a planned host is refused as not yet supported", "acme", "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\nhost: gitlab\n", "not yet supported"},
		{"a slug must match its folder", "other", validOrganization, "does not match the folder"},
		{"a name is required", "acme", "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nhost: github\n", "name is required"},
		{"a field outside the record is refused", "acme", validOrganization + "colour: blue\n", "colour"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, problems := ParseOrganization(testCase.folder, []byte(testCase.content))
			found := false
			for _, problem := range problems {
				if strings.Contains(problem.Message, testCase.expected) {
					found = true
				}
			}
			if !found {
				t.Fatalf("problems %v, expected one containing %q", problems, testCase.expected)
			}
		})
	}
}

func writeOrganization(t *testing.T, clone string, slug string, content string) {
	t.Helper()
	folder := filepath.Join(clone, "declarations", "organizations", slug)
	err := os.MkdirAll(folder, 0o755)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err = os.WriteFile(filepath.Join(folder, "organization.yaml"), []byte(content), 0o644)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestFindOrganization(t *testing.T) {
	t.Run("the one record of a clone is found", func(t *testing.T) {
		clone := t.TempDir()
		writeOrganization(t, clone, "acme", validOrganization)
		organization, err := FindOrganization(clone)
		if err != nil || organization.Slug != "acme" {
			t.Fatalf("organization %+v err %v", organization, err)
		}
	})
	t.Run("a clone without a record is not an org repository", func(t *testing.T) {
		_, err := FindOrganization(t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "not an org repository") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("two records are refused", func(t *testing.T) {
		clone := t.TempDir()
		writeOrganization(t, clone, "acme", validOrganization)
		writeOrganization(t, clone, "other", strings.Replace(validOrganization, "slug: acme", "slug: other", 1))
		_, err := FindOrganization(clone)
		if err == nil || !strings.Contains(err.Error(), "holds 2 organisations") {
			t.Fatalf("err %v", err)
		}
	})
}
