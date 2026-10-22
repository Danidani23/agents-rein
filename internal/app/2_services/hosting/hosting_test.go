package hosting

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/ports"
)

func TestHosting(t *testing.T) {
	host := &fake.Host{Login: "Danidani23", Organizations: []string{"acme-inc"}, Repositories: map[string]ports.RepositoryInfo{"Danidani23/old": {Exists: true, CloneURL: "https://github.com/Danidani23/old.git"}}}
	repository := &fake.Repository{}
	service := Service{Host: host, Repository: repository}
	owners, err := service.Owners("token-of-Danidani23")
	if err != nil || strings.Join(owners, ",") != "Danidani23,acme-inc" {
		t.Fatalf("owners %v err %v", owners, err)
	}
	described, err := service.DescribeRepository("token", "Danidani23", "old")
	if err != nil || !described.Exists || described.CloneURL != "https://github.com/Danidani23/old.git" {
		t.Fatalf("described %+v err %v", described, err)
	}
	missing, _ := service.DescribeRepository("token", "Danidani23", "nope")
	if missing.Exists {
		t.Fatalf("a missing repository exists: %+v", missing)
	}
	cloneURL, err := service.CreateRepository("token", ports.NewRepository{Owner: "acme-inc", Name: "settings", Private: true})
	if err != nil || cloneURL != "https://github.com/acme-inc/settings.git" || len(host.Created) != 1 || !host.Created[0].Private {
		t.Fatalf("created %q err %v", cloneURL, err)
	}
	reason, err := service.ProtectMainBranch("token", "acme-inc", "settings")
	if err != nil || reason != "" || strings.Join(host.ProtectedBranches, ",") != "acme-inc/settings/main" {
		t.Fatalf("protect: reason %q err %v", reason, err)
	}
	host.ProtectionRefusal = "the free plan protects no private branches"
	reason, err = service.ProtectMainBranch("token", "acme-inc", "settings")
	if err != nil || reason != host.ProtectionRefusal {
		t.Fatalf("refused protection: reason %q err %v", reason, err)
	}
	folder := t.TempDir()
	_ = os.WriteFile(filepath.Join(folder, "README.md"), []byte("x"), 0o644)
	err = service.Publish(ports.RemoteAccess{}, folder, cloneURL, "first")
	if err != nil || repository.Published[cloneURL] == "" {
		t.Fatalf("publish: err %v published %v", err, repository.Published)
	}
	if !strings.Contains(service.InstallURL(), "installations/new") || service.RepositorySettingsURL("acme-inc", "settings") != "https://github.com/acme-inc/settings/settings" {
		t.Fatalf("urls %q %q", service.InstallURL(), service.RepositorySettingsURL("acme-inc", "settings"))
	}
	for cloneURL, expected := range map[string]string{
		"https://github.com/acme-inc/settings.git": "acme-inc/settings",
		"git@github.com:acme-inc/settings.git":     "acme-inc/settings",
		"/tmp/remotes/settings.git":                "",
	} {
		owner, name, ok := RepositoryOf(cloneURL)
		if (expected == "" && ok) || (expected != "" && (!ok || owner+"/"+name != expected)) {
			t.Fatalf("%s: %s/%s %v", cloneURL, owner, name, ok)
		}
	}
}
