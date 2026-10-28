package web

import (
	"testing"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
)

func TestTheUserMenuShowsTheProjectAsAnotherLogin(t *testing.T) {
	page, _ := serveProjects(t, "Danidani23", "Other")
	client, root := signIn(t, page)
	_, body := get(t, client, root+"projects?project=shop")
	containsAll(t, body, `href="/projects?project=shop&amp;user=Other"`, `href="/projects?project=shop&amp;user=Danidani23"`, "<code>mine</code>")
	_, body = get(t, client, root+"projects?project=shop&user=Other")
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
