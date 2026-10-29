package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// serveProjects opens the page over an organisation made through the
// workflows: dani with the team web and the projects shop and blog, a rule,
// a skill and a skip list at their levels, and shop cloned here, synced and
// one of its files edited by hand. logins are the logins the page then
// sees; the orchestrator is returned for a test that adds to the state.
func serveProjects(t *testing.T, logins ...string) (*Page, orchestration.Orchestrator) {
	t.Helper()
	home := t.TempDir()
	state := fake.NewState()
	state.Current.Logins = []string{"Danidani23"}
	tokens := map[string]ports.Token{}
	for _, login := range append([]string{"Danidani23"}, logins...) {
		tokens[login] = ports.Token{Login: login, AccessToken: "token-of-" + login, ExpiresAt: time.Now().Add(time.Hour)}
	}
	agent := &fake.Agent{Present: true, HomePath: t.TempDir(), HarnessFormat: "claude-code"}
	repository := &fake.Repository{Record: "id: " + organizationID + "\nslug: dani\nname: Dani\nhost: github\n"}
	services := orchestration.NewServices(home, &fake.Host{Login: "Danidani23"}, repository, state, fake.NewDocuments(), []ports.Agent{agent}, &fake.Judge{Unavailable: "no judge"}, &fake.Lock{}, &fake.Keychain{Tokens: tokens}, &fake.Releases{Latest_: ports.Release{Version: "v0.1.0"}}, "v0.1.0", filepath.Join(home, "bin", "rein"))
	orchestrator := orchestration.New(services)
	created, err := orchestrator.CreateOrganization(orchestration.CreateOrganizationRequest{Name: "Dani", Owner: "Danidani23", RepositoryName: "dani-agents", Private: true})
	if err != nil {
		t.Fatalf("organisation: %v", err)
	}
	_, err = orchestrator.CreateTeam(orchestration.CreateTeamRequest{Name: "Web"})
	if err != nil {
		t.Fatalf("team: %v", err)
	}
	for _, project := range []orchestration.CreateProjectRequest{{Name: "Shop", Teams: []string{"web"}, Labels: []string{"language:go"}}, {Name: "Blog", Teams: []string{"web"}, Labels: []string{"language:python"}}} {
		_, err = orchestrator.CreateProject(project)
		if err != nil {
			t.Fatalf("project %s: %v", project.Name, err)
		}
	}
	write := func(relative string, content string) {
		target := filepath.Join(created.Connected.OrgClonePath, "declarations", filepath.FromSlash(relative))
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		_ = os.WriteFile(target, []byte(content), 0o644)
	}
	write("rules/style.md", "---\nenforcement: required\n---\nHouse style.\n")
	write("rules/go.md", "---\npaths: [\"**/*.go\"]\napplyIf: [language:go]\n---\nTwo-line errors.\n")
	write("rules/testing.md", "Org tests.\n")
	write("rules/brief.md", "Be brief.\n")
	write("teams/web/rules/testing.md", "Web tests.\n")
	write("teams/web/skills/commit/SKILL.md", "---\nname: commit\ndescription: Draft commits.\n---\n")
	write("projects/shop/rules/testing.md", "Shop tests.\n")
	write("users/danidani23/rules/mine.md", "Mine.\n")
	write("users/other/rules/theirs.md", "Theirs.\n")
	projectRecord := filepath.Join(created.Connected.OrgClonePath, "declarations", "projects", "shop", "project.yaml")
	content, _ := os.ReadFile(projectRecord)
	_ = os.WriteFile(projectRecord, []byte(strings.Replace(string(content), "skip: []", "skip:\n    - organizations.dani.rules.brief", 1)), 0o644)
	folder, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	err = fake.NewProjectClone(folder, []string{strings.Repeat("a", 40)}, []string{"main.go"})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	_ = os.WriteFile(filepath.Join(folder, "main.go"), []byte("package main"), 0o644)
	_, err = orchestrator.InitializeProject(orchestration.InitializeProjectRequest{Directory: folder, Project: "shop"})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	_, err = orchestrator.SyncClone(orchestration.SyncRequest{Directory: folder, ReinExecutable: "/bin/rein"})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	_ = os.WriteFile(filepath.Join(folder, ".claude", "rules", "go.md"), []byte("edited"), 0o644)
	state.Current.Logins = logins
	page, err := Open(orchestrator, "v0.1.0", Interrupt{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	stopped, stop := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- page.Serve(stopped) }()
	t.Cleanup(func() {
		stop()
		err := <-served
		if err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	return page, orchestrator
}

// containsAll reports each expected text the body lacks.
func containsAll(t *testing.T, body string, expected ...string) {
	t.Helper()
	for _, text := range expected {
		if !strings.Contains(body, text) {
			t.Errorf("the page lacks %q", text)
		}
	}
}

// containsNone reports each text the body should not hold.
func containsNone(t *testing.T, body string, unexpected ...string) {
	t.Helper()
	for _, text := range unexpected {
		if strings.Contains(body, text) {
			t.Errorf("the page holds %q", text)
		}
	}
}

func TestTheProjectsPageShowsTheResolutionAsATree(t *testing.T) {
	page, _ := serveProjects(t, "Danidani23")
	client, root := signIn(t, page)
	response, body := get(t, client, root+"projects?project=shop")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", response.StatusCode, body)
	}
	containsAll(t, body,
		`href="/projects" aria-current="page"`,
		"Organisation dani", "every project",
		"Team web", "the project is on this team",
		"Project shop", "the project&#39;s own",
		"User danidani23", "the login Danidani23",
		"<code>style</code>", "required",
		"<code>go</code>", "selected by language:go",
		"<code>commit</code>", "Draft commits.",
		"replaces <code>rules.testing</code>, <code>teams.web.rules.testing</code>",
		"<code>brief</code>", "the skip list of project shop",
		"Files sync wrote", ".claude/rules/go.md", "edited by hand", "as written",
	)
	containsNone(t, body, "<code>theirs</code>", "<code>py</code>")
}

func TestTheProjectsPageListsEveryProjectAndOpensOnOneClonedHere(t *testing.T) {
	page, _ := serveProjects(t, "Danidani23")
	client, root := signIn(t, page)
	_, body := get(t, client, root+"projects")
	containsAll(t, body, "Project shop", "Files sync wrote", `href="/projects?project=blog"`, `href="/projects?project=shop"`, "cloned here")
	_, body = get(t, client, root+"projects?project=blog")
	containsAll(t, body, "Project blog", "not cloned on this machine")
	containsNone(t, body, "Files sync wrote", "<code>go</code>")
}

func TestTheProjectsPageNamesWhatItCannotShow(t *testing.T) {
	t.Run("without a login, the way in", func(t *testing.T) {
		page, _ := serveProjects(t)
		client, root := signIn(t, page)
		_, body := get(t, client, root+"projects?project=shop")
		containsAll(t, body, "rein login")
		containsNone(t, body, "<code>style</code>")
	})
	for _, unknown := range []struct {
		query    string
		expected string
	}{
		{"projects?project=nope", "nope is not a project of dani"},
		{"projects?org=nope", "nope is not an organisation connected here"},
		{"projects?user=nobody", "nobody is not logged in on this machine"},
	} {
		t.Run(unknown.query, func(t *testing.T) {
			page, _ := serveProjects(t, "Danidani23")
			client, root := signIn(t, page)
			response, body := get(t, client, root+unknown.query)
			if response.StatusCode != http.StatusNotFound {
				t.Errorf("status %d", response.StatusCode)
			}
			containsAll(t, body, unknown.expected)
		})
	}
}
