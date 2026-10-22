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
		organization, problems := ParseOrganization([]byte(validOrganization))
		if len(problems) != 0 || organization.Name != "Acme" {
			t.Fatalf("organization %+v problems %v", organization, problems)
		}
	})
	cases := []struct {
		name     string
		content  string
		expected string
	}{
		{"an id that is not a UUID is refused", "id: 123\nslug: acme\nname: Acme\nhost: github\n", "not a lowercase UUID"},
		{"a missing host is refused", "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\n", "host is required"},
		{"a planned host is refused as not yet supported", "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\nhost: gitlab\n", "not yet supported"},
		{"a name is required", "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nhost: github\n", "name is required"},
		{"a field outside the record is refused", validOrganization + "colour: blue\n", "colour"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, problems := ParseOrganization([]byte(testCase.content))
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

func writeOrganization(t *testing.T, clone string, content string) {
	t.Helper()
	folder := filepath.Join(clone, "declarations")
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
		writeOrganization(t, clone, validOrganization)
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
}
