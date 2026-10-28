package web

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// organizationID is the organisation's id, which a project clone's record
// names it by.
const organizationID = "9ec65182-ab15-4d1d-95b3-27c81b006b05"

// servePage opens the page over the real orchestrator, the fake adapters and
// a scratch home, with one organisation and one project clone, serves it on
// its port until the test ends, and returns it.
func servePage(t *testing.T, loggedIn bool) *Page {
	t.Helper()
	home := t.TempDir()
	state := fake.NewState()
	if loggedIn {
		state.Current.Logins = []string{"Danidani23"}
	}
	state.Current.Organizations = []machine.OrganizationRef{{ID: organizationID, Slug: "dani", Remote: "https://github.com/Danidani23/dani-agents.git", Login: "Danidani23"}}
	state.Current.Active = "dani"
	state.Current.ProjectClones = []machine.ProjectClone{{Organization: organizationID, Address: "organizations.dani.projects.shop", ClonePath: t.TempDir()}}
	keychain := &fake.Keychain{Tokens: map[string]ports.Token{"Danidani23": {Login: "Danidani23", AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}}}
	agents := []ports.Agent{&fake.Agent{Present: true, HomePath: t.TempDir()}}
	services := orchestration.NewServices(home, &fake.Host{Login: "Danidani23"}, &fake.Repository{}, state, agents, &fake.Judge{Unavailable: "no judge"}, &fake.Lock{}, keychain, &fake.Releases{Latest_: ports.Release{Version: "v0.1.0"}}, "v0.1.0", filepath.Join(home, "bin", "rein"))
	page, err := Open(orchestration.New(services), "v0.1.0")
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
	return page
}

// browser is a client that keeps cookies, as a browser does, and shows
// each redirect instead of following it.
func browser(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("jar: %v", err)
	}
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// get fetches address with client and returns the status and the body.
func get(t *testing.T, client *http.Client, address string) (*http.Response, string) {
	t.Helper()
	response, err := client.Get(address)
	if err != nil {
		t.Fatalf("get %s: %v", address, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s: %v", address, err)
	}
	return response, string(body)
}

// signIn follows the address the browser opens and returns the client that
// now holds the run's cookie, and the page's root without the token.
func signIn(t *testing.T, page *Page) (client *http.Client, root string) {
	t.Helper()
	client = browser(t)
	response, _ := get(t, client, page.Address())
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("the opened address answered %d, expected a redirect", response.StatusCode)
	}
	opened, _ := url.Parse(page.Address())
	return client, opened.Scheme + "://" + opened.Host + "/"
}

func TestTheAddressIsOnLoopbackWithATokenOfThisRun(t *testing.T) {
	first := servePage(t, true)
	second := servePage(t, true)
	pattern := regexp.MustCompile(`^http://127\.0\.0\.1:[0-9]+/\?token=[A-Z2-7]{26}$`)
	if !pattern.MatchString(first.Address()) {
		t.Fatalf("address %q", first.Address())
	}
	firstToken := strings.SplitN(first.Address(), "token=", 2)[1]
	secondToken := strings.SplitN(second.Address(), "token=", 2)[1]
	if firstToken == secondToken {
		t.Fatalf("two runs share the token %s", firstToken)
	}
}

func TestTheTokenIsTradedForACookieAndLeavesTheAddress(t *testing.T) {
	page := servePage(t, true)
	client := browser(t)
	response, _ := get(t, client, page.Address())
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/" {
		t.Fatalf("status %d location %q", response.StatusCode, response.Header.Get("Location"))
	}
	opened, _ := url.Parse(page.Address())
	setCookie := response.Header.Get("Set-Cookie")
	for _, expected := range []string{"rein_ui_" + opened.Port() + "=", "HttpOnly", "SameSite=Strict", "Path=/"} {
		if !strings.Contains(setCookie, expected) {
			t.Fatalf("cookie %q lacks %s", setCookie, expected)
		}
	}
	response, body := get(t, client, opened.Scheme+"://"+opened.Host+"/")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Danidani23") {
		t.Fatalf("with the cookie: status %d body %q", response.StatusCode, body)
	}
}

func TestARequestWithoutTheTokenIsRefused(t *testing.T) {
	page := servePage(t, true)
	opened, _ := url.Parse(page.Address())
	root := opened.Scheme + "://" + opened.Host + "/"
	response, body := get(t, browser(t), root)
	if response.StatusCode != http.StatusForbidden || !strings.Contains(body, "rein ui") {
		t.Fatalf("no token: status %d body %q", response.StatusCode, body)
	}
	response, _ = get(t, browser(t), root+"?token="+strings.Repeat("A", 26))
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("a wrong token: status %d", response.StatusCode)
	}
}

func TestARequestNamingAnotherHostIsRefused(t *testing.T) {
	page := servePage(t, true)
	client, root := signIn(t, page)
	rootAddress, _ := url.Parse(root)
	for _, host := range []string{"rebound.example:" + rootAddress.Port(), "localhost:" + rootAddress.Port()} {
		request, _ := http.NewRequest(http.MethodGet, root, nil)
		request.Host = host
		for _, held := range client.Jar.Cookies(rootAddress) {
			request.AddCookie(held)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("host %s: status %d", host, response.StatusCode)
		}
	}
}

func TestAWriteFromAnotherOriginIsRefused(t *testing.T) {
	accepted := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	guarded := guard(accepted, "127.0.0.1:4000", "secret")
	for _, origin := range []struct {
		header   string
		expected int
	}{
		{"http://127.0.0.1:4000", http.StatusNoContent},
		{"https://elsewhere.example", http.StatusForbidden},
		{"", http.StatusForbidden},
	} {
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4000/sync", nil)
		request.AddCookie(&http.Cookie{Name: "rein_ui_4000", Value: "secret"})
		if origin.header != "" {
			request.Header.Set("Origin", origin.header)
		}
		recorder := httptest.NewRecorder()
		guarded.ServeHTTP(recorder, request)
		if recorder.Code != origin.expected {
			t.Fatalf("origin %q: status %d, expected %d", origin.header, recorder.Code, origin.expected)
		}
	}
}

func TestTheMachinePageShowsWhatStatusReports(t *testing.T) {
	page := servePage(t, true)
	client, root := signIn(t, page)
	response, body := get(t, client, root)
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("status %d type %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	for _, expected := range []string{"v0.1.0", "Danidani23", "dani", "github.com/Danidani23/dani-agents", "shop", "never synced", `href="/static/rein.css"`} {
		if !strings.Contains(body, expected) {
			t.Errorf("the page lacks %q", expected)
		}
	}
	if strings.Contains(body, organizationID) || !strings.Contains(body, "<td>dani</td>") {
		t.Errorf("a project's organisation is not shown by its slug: %q", body)
	}
	if !strings.Contains(body, `<a href="/projects?org=dani&amp;project=shop">shop</a>`) {
		t.Errorf("a project does not lead to its page: %q", body)
	}
	response, stylesheet := get(t, client, root+"static/rein.css")
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/css") || stylesheet == "" {
		t.Fatalf("stylesheet: status %d type %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
}

func TestTheMachinePageWithoutALoginNamesTheWayIn(t *testing.T) {
	page := servePage(t, false)
	client, root := signIn(t, page)
	_, body := get(t, client, root)
	if !strings.Contains(body, "rein login") {
		t.Fatalf("the page without a login does not name rein login: %q", body)
	}
}

func TestThePageHasTheFrameWithTheLogoTheNameAndTheMenu(t *testing.T) {
	page := servePage(t, true)
	client, root := signIn(t, page)
	_, body := get(t, client, root)
	for _, expected := range []string{"Agents Rein", `src="/static/rein-mark.svg"`, `srcset="/static/rein-mark-dark.svg"`, `<nav class="menu"`, `href="/" aria-current="page"`, "This machine", `href="/projects"`, "Projects", "v0.1.0"} {
		if !strings.Contains(body, expected) {
			t.Errorf("the frame lacks %q", expected)
		}
	}
	for _, mark := range []string{"static/rein-mark.svg", "static/rein-mark-dark.svg"} {
		response, logo := get(t, client, root+mark)
		if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "image/svg+xml") || !strings.Contains(logo, "<svg") {
			t.Errorf("%s: status %d type %q", mark, response.StatusCode, response.Header.Get("Content-Type"))
		}
	}
}
