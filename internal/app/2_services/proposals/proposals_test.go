package proposals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/app/2_services/exchange"
	"github.com/Danidani23/agents-rein/internal/ports"
)

func TestReadFolderAndZip(t *testing.T) {
	service := Service{}
	folder := t.TempDir()
	_ = os.MkdirAll(filepath.Join(folder, "skills", "commit"), 0o755)
	_ = os.WriteFile(filepath.Join(folder, "rules", "brief.md"), []byte("Be brief.\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(folder, "rules"), 0o755)
	_ = os.WriteFile(filepath.Join(folder, "rules", "brief.md"), []byte("Be brief.\n"), 0o644)
	_ = os.WriteFile(filepath.Join(folder, "skills", "commit", "SKILL.md"), []byte("---\nname: commit\ndescription: Draft commits.\n---\n"), 0o644)
	candidates, err := service.Read(folder)
	if err != nil || len(candidates.Files) != 2 || candidates.Files["rules/brief.md"] != "Be brief.\n" || candidates.Report != nil {
		t.Fatalf("folder %+v err %v", candidates, err)
	}
	zipPath := filepath.Join(t.TempDir(), "export.zip")
	err = exchange.Write(zipPath, exchange.Manifest{FormatVersion: exchange.FormatVersion, Kind: "user-level"}, []exchange.Entry{{Path: "rules/zipped.md", Content: []byte("From a zip.\n")}})
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	candidates, err = service.Read(zipPath)
	if err != nil || len(candidates.Files) != 1 || candidates.Files["rules/zipped.md"] != "From a zip.\n" {
		t.Fatalf("zip %+v err %v", candidates, err)
	}
	_, err = service.Read(filepath.Join(t.TempDir(), "nowhere"))
	if err == nil {
		t.Fatalf("a missing source is an error")
	}
}

func TestBranchWritePublishAndAbandon(t *testing.T) {
	repository := &fake.Repository{}
	host := &fake.Host{Login: "Danidani23"}
	service := Service{Repository: repository, Host: host}
	clone := filepath.Join(t.TempDir(), "clone")
	_ = os.MkdirAll(filepath.Join(clone, "declarations"), 0o755)
	_ = os.WriteFile(filepath.Join(clone, "declarations", "organization.yaml"), []byte("id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\nhost: github\n"), 0o644)
	err := repository.Publish(ports.RemoteAccess{}, clone, "/remote/acme.git", "first")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	err = service.Branch(clone, "rein/propose-x")
	if err != nil || repository.Current != "rein/propose-x" {
		t.Fatalf("branch: err %v current %q", err, repository.Current)
	}
	written, err := service.Write(clone, "teams/web", map[string]string{"rules/brief.md": "Be brief.\n", "skills/commit/SKILL.md": "---\nname: commit\ndescription: d\n---\n"})
	if err != nil || len(written) != 2 || !strings.Contains(strings.Join(written, ","), "declarations/teams/web/rules/brief.md") {
		t.Fatalf("written %v err %v", written, err)
	}
	err = service.Publish(ports.RemoteAccess{}, clone, "rein/propose-x", written, "propose")
	if err != nil || repository.Published["/remote/acme.git#rein/propose-x"] == "" {
		t.Fatalf("publish: err %v published %v", err, repository.Published)
	}
	url, err := service.OpenPullRequest("token", "Danidani23", "acme-agent-settings", ports.PullRequest{Branch: "rein/propose-x", Title: "t", Body: "b"})
	if err != nil || url != "https://github.com/Danidani23/acme-agent-settings/pull/1" || len(host.PullRequests) != 1 {
		t.Fatalf("pull request %q err %v", url, err)
	}
	err = service.BackToMain(clone, "rein/propose-x")
	if err != nil || repository.Current != "main" || strings.Join(repository.Deleted, ",") != "rein/propose-x" {
		t.Fatalf("back to main: err %v current %q deleted %v", err, repository.Current, repository.Deleted)
	}
	_, err = os.Stat(filepath.Join(clone, "declarations", "teams"))
	if err == nil {
		t.Fatalf("main still holds the branch's files")
	}
	_ = service.Branch(clone, "rein/propose-y")
	_, _ = service.Write(clone, "", map[string]string{"rules/x.md": "x\n"})
	_ = service.Publish(ports.RemoteAccess{}, clone, "rein/propose-y", []string{"declarations/rules/x.md"}, "y")
	err = service.Abandon(ports.RemoteAccess{}, clone, "rein/propose-y", true)
	if err != nil || repository.Current != "main" || strings.Join(repository.RemoteDeleted, ",") != "rein/propose-y" || repository.Published["/remote/acme.git#rein/propose-y"] != "" {
		t.Fatalf("abandon: err %v current %q remote deleted %v", err, repository.Current, repository.RemoteDeleted)
	}
	_, err = os.Stat(filepath.Join(clone, "declarations", "rules", "x.md"))
	if err == nil {
		t.Fatalf("the abandoned branch's file stayed in the clone")
	}
	_ = os.MkdirAll(filepath.Join(clone, "declarations", "skills", "old", "references"), 0o755)
	_ = os.WriteFile(filepath.Join(clone, "declarations", "skills", "old", "SKILL.md"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(clone, "declarations", "skills", "old", "references", "a.md"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(clone, "declarations", "skills", "keep.md"), []byte("x"), 0o644)
	err = service.Remove(clone, []string{"declarations/skills/old/SKILL.md", "declarations/skills/old/references/a.md", "declarations/skills/gone.md"})
	_, oldErr := os.Stat(filepath.Join(clone, "declarations", "skills", "old"))
	_, keepErr := os.Stat(filepath.Join(clone, "declarations", "skills", "keep.md"))
	if err != nil || oldErr == nil || keepErr != nil {
		t.Fatalf("remove: err %v old %v keep %v", err, oldErr, keepErr)
	}
}
