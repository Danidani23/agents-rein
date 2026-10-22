package records

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/ports"
)

const organizationRecord = "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\nhost: github\n"

func TestRecords(t *testing.T) {
	repository := &fake.Repository{}
	service := Service{Repository: repository}
	clone := filepath.Join(t.TempDir(), "clone")
	// a clone published once, so a discard has something to go back to
	_ = os.MkdirAll(filepath.Join(clone, "declarations"), 0o755)
	_ = os.WriteFile(filepath.Join(clone, "declarations", "organization.yaml"), []byte(organizationRecord), 0o644)
	err := repository.Publish(ports.RemoteAccess{}, clone, "/remote/acme.git", "first")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	tree, err := service.Tree(clone)
	if err != nil || tree.Organization.Slug != "acme" {
		t.Fatalf("tree %+v err %v", tree.Organization, err)
	}
	err = service.Write(clone, "declarations/teams/web/team.yaml", "id: 1b4e28ba-2fa1-11d2-883f-0016d3cca427\nslug: web\nname: Web\n")
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	err = service.Write(clone, "declarations/teams/web/team.yaml", "again")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("a record is never overwritten: %v", err)
	}
	err = service.Rewrite(clone, "declarations/teams/nope/team.yaml", "x")
	if err == nil || !strings.Contains(err.Error(), "declarations/teams/nope/team.yaml") {
		t.Fatalf("a missing record cannot be rewritten: %v", err)
	}
	err = service.Rewrite(clone, "declarations/teams/web/team.yaml", "id: 1b4e28ba-2fa1-11d2-883f-0016d3cca427\nslug: web\nname: Web\ndescription: the web\n")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	tree, _ = service.Tree(clone)
	if tree.Teams["web"].Description != "the web" {
		t.Fatalf("tree after rewrite %+v", tree.Teams)
	}
	err = service.Publish(ports.RemoteAccess{}, clone, []string{"declarations/teams/web/team.yaml"}, "team web")
	if err != nil || strings.Join(repository.Commits, ",") != "first,team web" {
		t.Fatalf("publish: err %v commits %v", err, repository.Commits)
	}
	published, _ := os.ReadFile(filepath.Join(repository.Published["/remote/acme.git"], "declarations", "teams", "web", "team.yaml"))
	if !strings.Contains(string(published), "the web") {
		t.Fatalf("the push did not carry the record: %q", published)
	}
	_ = os.WriteFile(filepath.Join(clone, "declarations", "teams", "web", "team.yaml"), []byte("broken"), 0o644)
	err = service.Discard(clone)
	restored, _ := os.ReadFile(filepath.Join(clone, "declarations", "teams", "web", "team.yaml"))
	if err != nil || !strings.Contains(string(restored), "the web") {
		t.Fatalf("discard: err %v record %q", err, restored)
	}
	if err := service.Pull(ports.RemoteAccess{}, clone); err != nil {
		t.Fatalf("pull: %v", err)
	}
	repository.FailPush = true
	err = service.Publish(ports.RemoteAccess{}, clone, []string{"declarations/teams/web/team.yaml"}, "again")
	if err == nil || !strings.Contains(err.Error(), "git push failed") {
		t.Fatalf("a rejected push is reported: %v", err)
	}
}
