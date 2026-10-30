package web

import (
	"testing"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
)

func TestTheUserMenuShowsTheProjectAsAnotherLogin(t *testing.T) {
	page, _ := serveProjects(t, "Danidani23", "Other")
	client, root := signIn(t, page)
	_, body := get(t, client, root+"projects?project=shop")
	containsAll(t, body, `href="/projects?account=Other&amp;project=shop"`, `href="/projects?account=Danidani23&amp;project=shop"`, `href="/projects?project=shop"`, "All accounts", "<code>mine</code>")
	_, body = get(t, client, root+"projects?project=shop&account=Other")
	containsAll(t, body, "User other", "the login Other", "<code>theirs</code>")
	containsNone(t, body, "<code>mine</code>")
}

func TestTheOrganisationMenuListsTheConnectedOrganisations(t *testing.T) {
	page, orchestrator := serveProjects(t, "Danidani23")
	_, err := orchestrator.CreateOrganization(orchestration.CreateOrganizationRequest{Name: "Acme", Owner: "Danidani23", RepositoryName: "acme-agents", Private: true})
	if err != nil {
		t.Fatalf("second organisation: %v", err)
	}
	client, root := signIn(t, page)
	_, body := get(t, client, root+"projects?org=dani&project=shop")
	containsAll(t, body, `href="/projects?org=acme"`, `href="/projects?org=dani"`, "Project shop")
	_, body = get(t, client, root+"projects?org=acme")
	containsAll(t, body, "No project in acme yet")
	containsNone(t, body, "Project shop")
}

func TestTheNotificationsAndTheAccountFilter(t *testing.T) {
	page, orchestrator := serveProjects(t, "Danidani23", "Second")
	err := orchestrator.Credentials.Logout("Second")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	client, root := signIn(t, page)
	t.Run("the menu counts what needs you, and each account in the top bar its own", func(t *testing.T) {
		_, body := get(t, client, root)
		containsAll(t, body, `Notifications <span class="count" aria-label="1 need you">1</span>`, `<summary>All accounts</summary>`, `All accounts <span class="muted">· 1 needs you</span>`, `Second <span class="muted">· 1 needs you</span>`)
	})
	t.Run("the page lists each with its account, narrowed to one account", func(t *testing.T) {
		_, body := get(t, client, root+"notifications")
		containsAll(t, body, `<td class="nowrap warning">logged-out</td><td>Second</td>`, `<a href="/accounts">no token in the keychain; log in again</a>`)
		_, body = get(t, client, root+"notifications?account=Danidani23")
		containsAll(t, body, "Nothing needs you on Danidani23")
	})
	t.Run("This machine shows one account's organisations and projects", func(t *testing.T) {
		_, body := get(t, client, root+"?account=Second")
		containsAll(t, body, "None connected.")
		containsNone(t, body, "<td>dani</td>")
		_, body = get(t, client, root+"?account=Danidani23")
		containsAll(t, body, `<a href="/projects?account=Danidani23&amp;org=dani&amp;project=shop">shop</a>`)
	})
}
