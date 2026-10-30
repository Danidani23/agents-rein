package web

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
)

// post sends a form from the page's own origin, as the browser does.
func post(t *testing.T, client *http.Client, root string, path string, form url.Values) (*http.Response, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, root+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("request %s: %v", path, err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", strings.TrimSuffix(root, "/"))
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return response, string(body)
}

// cloneOf is the folder of the one project clone the fixture made.
func cloneOf(t *testing.T, orchestrator orchestration.Orchestrator) string {
	t.Helper()
	clones, err := orchestrator.Organizations.ProjectClones()
	if err != nil || len(clones) != 1 {
		t.Fatalf("clones %+v err %v", clones, err)
	}
	return clones[0].ClonePath
}

// waitForRun waits until the page's current action has finished.
func waitForRun(t *testing.T, page *Page) {
	t.Helper()
	current := page.actions.latest()
	if current == nil {
		t.Fatalf("no action was started")
	}
	select {
	case <-current.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("the action %s did not finish", current.Name)
	}
}

// runAction confirms an action's form and returns the page of its run once
// it has finished.
func runAction(t *testing.T, page *Page, client *http.Client, root string, path string, form url.Values) string {
	t.Helper()
	response, body := post(t, client, root, path, form)
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/run" {
		t.Fatalf("confirm %s: status %d location %q body %q", path, response.StatusCode, response.Header.Get("Location"), body)
	}
	waitForRun(t, page)
	_, body = get(t, client, root+"run")
	return body
}

func TestSyncRunsFromTheProjectsPage(t *testing.T) {
	t.Run("the Projects page offers sync and reset for a clone here", func(t *testing.T) {
		page, orchestrator := serveProjects(t, "Danidani23")
		client, root := signIn(t, page)
		_, body := get(t, client, root+"projects?project=shop")
		folder := url.QueryEscape(cloneOf(t, orchestrator))
		containsAll(t, body, `href="/actions/sync?folder=`+folder+`"`, `href="/actions/sync?all=on"`, `href="/actions/reset-project?folder=`+folder+`"`)
	})
	t.Run("the preview says what sync does and names the files edited by hand", func(t *testing.T) {
		page, orchestrator := serveProjects(t, "Danidani23")
		client, root := signIn(t, page)
		response, body := get(t, client, root+"actions/sync?folder="+url.QueryEscape(cloneOf(t, orchestrator)))
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status %d: %s", response.StatusCode, body)
		}
		containsAll(t, body, "This will pull", ".claude/rules/go.md", `<form method="post" action="/actions/sync"`, `name="force"`)
	})
	t.Run("without force, the file edited by hand stops it and the run says so", func(t *testing.T) {
		page, orchestrator := serveProjects(t, "Danidani23")
		client, root := signIn(t, page)
		body := runAction(t, page, client, root, "actions/sync", url.Values{"folder": {cloneOf(t, orchestrator)}})
		containsAll(t, body, "Sync shop", "was edited by hand since the last sync: .claude/rules/go.md", "refused")
		containsNone(t, body, "Next:", "<h2>Steps</h2>")
	})
	t.Run("with force, it syncs, keeps the edit in a snapshot and names the next step", func(t *testing.T) {
		page, orchestrator := serveProjects(t, "Danidani23")
		client, root := signIn(t, page)
		body := runAction(t, page, client, root, "actions/sync", url.Values{"folder": {cloneOf(t, orchestrator)}, "force": {"on"}})
		containsAll(t, body, "synced organizations.dani.projects.shop", "were kept in the snapshot organizations.dani.projects.shop-", "Next:")
		containsNone(t, body, "refused")
	})
	t.Run("every clone, as sync --all does", func(t *testing.T) {
		page, _ := serveProjects(t, "Danidani23")
		client, root := signIn(t, page)
		_, body := get(t, client, root+"actions/sync?all=on")
		containsAll(t, body, "every project clone on this machine", "shop")
		body = runAction(t, page, client, root, "actions/sync", url.Values{"all": {"on"}, "force": {"on"}})
		containsAll(t, body, "Sync every clone", "synced organizations.dani.projects.shop")
	})
}

func TestResetProjectRunsFromTheProjectsPage(t *testing.T) {
	page, orchestrator := serveProjects(t, "Danidani23")
	client, root := signIn(t, page)
	folder := cloneOf(t, orchestrator)
	_, body := get(t, client, root+"actions/reset-project?folder="+url.QueryEscape(folder))
	containsAll(t, body, "This will put back the agent&#39;s files", `<form method="post" action="/actions/reset-project"`, `name="no-commit"`)
	body = runAction(t, page, client, root, "actions/reset-project", url.Values{"folder": {folder}})
	containsAll(t, body, "Reset shop", "restored", "Next:")
	clones, _ := orchestrator.Organizations.ProjectClones()
	if len(clones) != 0 {
		t.Fatalf("the clone is still registered: %+v", clones)
	}
}

func TestAnActionIsRefusedOutsideWhatThisMachineHas(t *testing.T) {
	t.Run("a folder that is not a clone rein initialised", func(t *testing.T) {
		page, _ := serveProjects(t, "Danidani23")
		client, root := signIn(t, page)
		elsewhere := t.TempDir()
		response, body := get(t, client, root+"actions/sync?folder="+url.QueryEscape(elsewhere))
		if response.StatusCode != http.StatusNotFound || !strings.Contains(body, "is not a project clone") {
			t.Fatalf("preview: status %d body %q", response.StatusCode, body)
		}
		response, _ = post(t, client, root, "actions/reset-project", url.Values{"folder": {elsewhere}})
		if response.StatusCode != http.StatusNotFound || page.actions.latest() != nil {
			t.Fatalf("confirm: status %d, run %+v", response.StatusCode, page.actions.latest())
		}
	})
	t.Run("without a login", func(t *testing.T) {
		page, orchestrator := serveProjects(t)
		client, root := signIn(t, page)
		response, body := post(t, client, root, "actions/sync", url.Values{"folder": {cloneOf(t, orchestrator)}})
		if response.StatusCode != http.StatusForbidden || !strings.Contains(body, "rein login") || page.actions.latest() != nil {
			t.Fatalf("status %d body %q", response.StatusCode, body)
		}
	})
	t.Run("a form from another origin", func(t *testing.T) {
		page, orchestrator := serveProjects(t, "Danidani23")
		client, root := signIn(t, page)
		request, _ := http.NewRequest(http.MethodPost, root+"actions/sync", strings.NewReader(url.Values{"folder": {cloneOf(t, orchestrator)}}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Origin", "https://elsewhere.example")
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusForbidden || page.actions.latest() != nil {
			t.Fatalf("status %d", response.StatusCode)
		}
	})
}

func TestTheCheckInThePages(t *testing.T) {
	t.Run("this machine shows what the check found, and the clone what concerns it", func(t *testing.T) {
		page, orchestrator := serveProjects(t, "Danidani23")
		client, root := signIn(t, page)
		_, body := get(t, client, root)
		containsAll(t, body, "What the check found", "The check has not run yet")
		folder := cloneOf(t, orchestrator)
		_, err := orchestrator.Findings.Keep([]machine.Finding{{Kind: machine.FindingBehind, Subject: folder, Detail: "dani changed on GitHub since this clone's last sync", Page: "/projects?org=dani&project=shop"}}, true)
		if err != nil {
			t.Fatalf("keep: %v", err)
		}
		_, body = get(t, client, root)
		containsAll(t, body, "behind", `<a href="/projects?org=dani&amp;project=shop">dani changed on GitHub since this clone&#39;s last sync</a>`, "Checked ")
		_, body = get(t, client, root+"projects?org=dani&project=shop")
		containsAll(t, body, `<p class="notice">dani changed on GitHub since this clone&#39;s last sync</p>`, "Automatic sync: off, turn it on")
	})
	t.Run("the switch turns a clone's automatic sync on and leads back", func(t *testing.T) {
		page, orchestrator := serveProjects(t, "Danidani23")
		client, root := signIn(t, page)
		folder := cloneOf(t, orchestrator)
		response, body := post(t, client, root, "actions/auto-sync", url.Values{"folder": {folder}, "on": {"on"}})
		clones, _ := orchestrator.Organizations.ProjectClones()
		if response.StatusCode != http.StatusSeeOther || !clones[0].AutoSync {
			t.Fatalf("status %d body %q clones %+v", response.StatusCode, body, clones)
		}
		response, _ = post(t, client, root, "actions/auto-sync", url.Values{"folder": {t.TempDir()}, "on": {"on"}})
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("a folder that is no clone: status %d", response.StatusCode)
		}
	})
}
