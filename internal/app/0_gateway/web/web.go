// Package web is rein's third gateway: rein ui, what rein knows shown as
// pages in the developer's browser, over the same concrete Orchestrator as
// the CLI and the MCP gateway (D45). It listens on 127.0.0.1 only. Every
// request must name that address as its Host and carry the token of this
// run: once in the address the browser opened, which trades it for a
// cookie, then as that cookie. A request that changes something must also
// come from the page's own origin. So no other page in the browser and no
// other machine reaches it. Its pages read without the machine lock; each
// action it runs takes the lock for itself, one action at a time. The
// decision is docs/decisions/2026-10-27_rein-ui-a-gateway-served-to-the-browser.md.
package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
)

// assets are the templates and the stylesheet, built into the binary.
//
//go:embed templates static
var assets embed.FS

// Page is rein ui for one run: the port it listens on, the token the
// opened address carries, the routes behind the guard, and the actions it
// runs.
type Page struct {
	listener net.Listener
	token    string
	handler  http.Handler
	actions  *runner
}

// Open listens on a free port of 127.0.0.1 and makes the token of this run.
// version is what rein version prints; interrupt is what Cancel does.
func Open(orchestrator orchestration.Orchestrator, version string, interrupt Interrupt) (*Page, error) {
	routes, actions, err := routesOf(orchestrator, version, interrupt, nil)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	token := rand.Text()
	return &Page{listener: listener, token: token, handler: guard(routes, listener.Addr().String(), token), actions: actions}, nil
}

// Window is the pages for the app's own window, which reaches them through
// its webview and never over a port, so no other page or machine can, and
// no token guards them (D50). Only they show the confirmations commands
// wait on (D53).
func Window(orchestrator orchestration.Orchestrator, version string, interrupt Interrupt, confirmations *Confirmations) (http.Handler, error) {
	routes, _, err := routesOf(orchestrator, version, interrupt, confirmations)
	if err != nil {
		return nil, err
	}
	return withoutRedirects(routes), nil
}

// withoutRedirects answers a redirect with a page that leads on by itself:
// the window's webview shows an HTTP redirect from the app's own handler as
// an empty page, since WebKit does not follow one there, as Wails' asset
// server notes. Everything else passes through, a stream included.
func withoutRedirects(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		next.ServeHTTP(&leadingOn{ResponseWriter: writer}, request)
	})
}

// leadingOn turns a redirect into a page that leads to its address.
type leadingOn struct {
	http.ResponseWriter
	redirected bool
}

func (writer *leadingOn) WriteHeader(code int) {
	location := writer.Header().Get("Location")
	if code < 300 || code >= 400 || location == "" {
		writer.ResponseWriter.WriteHeader(code)
		return
	}
	writer.redirected = true
	writer.Header().Del("Location")
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.ResponseWriter.WriteHeader(http.StatusOK)
	escaped := template.HTMLEscapeString(location)
	_, _ = fmt.Fprintf(writer.ResponseWriter, `<!doctype html><meta http-equiv="refresh" content="0; url=%s"><a href="%s">Continue</a>`, escaped, escaped)
}

// Write drops the redirect's own body, which no one would see.
func (writer *leadingOn) Write(content []byte) (int, error) {
	if writer.redirected {
		return len(content), nil
	}
	return writer.ResponseWriter.Write(content)
}

// Flush passes a stream on as it is written.
func (writer *leadingOn) Flush() {
	flusher, ok := writer.ResponseWriter.(http.Flusher)
	if ok {
		flusher.Flush()
	}
}

func (writer *leadingOn) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

// routesOf is every page and action, with the runner of the actions; the
// window's routes, when confirmations is set, also the confirmations.
func routesOf(orchestrator orchestration.Orchestrator, version string, interrupt Interrupt, confirmations *Confirmations) (http.Handler, *runner, error) {
	machineTemplate, err := template.New("layout.html").Funcs(templateFunctions).ParseFS(assets, "templates/layout.html", "templates/machine.html")
	if err != nil {
		return nil, nil, err
	}
	projectsTemplate, err := template.New("layout.html").Funcs(templateFunctions).ParseFS(assets, "templates/layout.html", "templates/projects.html")
	if err != nil {
		return nil, nil, err
	}
	actionTemplate, err := template.New("layout.html").Funcs(templateFunctions).ParseFS(assets, "templates/layout.html", "templates/action.html")
	if err != nil {
		return nil, nil, err
	}
	runTemplate, err := template.New("layout.html").Funcs(templateFunctions).ParseFS(assets, "templates/layout.html", "templates/run.html")
	if err != nil {
		return nil, nil, err
	}
	routes := http.NewServeMux()
	routes.Handle("GET /static/", http.FileServerFS(assets))
	actions := &runner{interrupt: interrupt, confirmations: confirmations}
	if confirmations != nil {
		confirmationsTemplate, err := template.New("layout.html").Funcs(templateFunctions).ParseFS(assets, "templates/layout.html", "templates/confirmations.html")
		if err != nil {
			return nil, nil, err
		}
		routes.Handle("GET /confirmations", confirmationsPage(orchestrator, version, actions, confirmationsTemplate))
		routes.Handle("POST /confirmations/answer", answerConfirmation(actions))
	}
	routes.Handle("GET /{$}", machinePage(orchestrator, version, actions, machineTemplate))
	routes.Handle("GET /projects", projectsPage(orchestrator, version, actions, projectsTemplate))
	routes.Handle("GET /actions/sync", syncPreview(orchestrator, version, actions, actionTemplate))
	routes.Handle("POST /actions/sync", syncAction(orchestrator, version, actions, actionTemplate))
	routes.Handle("GET /actions/reset-project", resetPreview(orchestrator, version, actions, actionTemplate))
	routes.Handle("POST /actions/reset-project", resetAction(orchestrator, version, actions, actionTemplate))
	routes.Handle("GET /run", runPage(orchestrator, version, actions, runTemplate))
	routes.Handle("GET /run/events", runEvents(actions))
	routes.Handle("POST /run/cancel", cancelRun(actions))
	return routes, actions, nil
}

// Address is what the browser opens: the page's root with the token, which
// the first request trades for the cookie.
func (page *Page) Address() string {
	return "http://" + page.listener.Addr().String() + "/?token=" + page.token
}

// Serve answers until ctx ends, which closes the port.
func (page *Page) Serve(ctx context.Context) error {
	server := &http.Server{Handler: page.handler, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = server.Close()
		case <-served:
		}
	}()
	err := server.Serve(page.listener)
	close(served)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// guard lets a request through only from the tab rein opened: its Host is
// the address the page listens on, it carries the run's token, and a
// request that is not a read comes from the page's own origin. The token in
// the address is traded for a cookie named after the port, so two runs keep
// apart, and the address is cleaned of it.
func guard(next http.Handler, host string, token string) http.Handler {
	_, port, _ := net.SplitHostPort(host)
	cookieName := "rein_ui_" + port
	origin := "http://" + host
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Host != host {
			http.Error(writer, "rein ui answers only at "+origin, http.StatusForbidden)
			return
		}
		offered := request.URL.Query().Get("token")
		if offered != "" {
			if !sameToken(offered, token) {
				http.Error(writer, "this is not the token of this rein ui; open the address it printed", http.StatusForbidden)
				return
			}
			http.SetCookie(writer, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			cleaned := *request.URL
			query := cleaned.Query()
			query.Del("token")
			cleaned.RawQuery = query.Encode()
			http.Redirect(writer, request, cleaned.RequestURI(), http.StatusSeeOther)
			return
		}
		held, err := request.Cookie(cookieName)
		if err != nil || !sameToken(held.Value, token) {
			http.Error(writer, "open the address rein ui printed; it carries the token of this run", http.StatusForbidden)
			return
		}
		if request.Method != http.MethodGet && request.Method != http.MethodHead && request.Header.Get("Origin") != origin {
			http.Error(writer, "a change comes only from rein ui's own page", http.StatusForbidden)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

// sameToken compares in constant time, so the time an answer takes says
// nothing about how much of a guess was right.
func sameToken(offered string, token string) bool {
	return subtle.ConstantTimeCompare([]byte(offered), []byte(token)) == 1
}

// render writes the page whole with status, or not at all: a template that
// fails halfway is an error, not half a page.
func render(writer http.ResponseWriter, page *template.Template, view any, status int) {
	var rendered bytes.Buffer
	err := page.ExecuteTemplate(&rendered, "layout", view)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(status)
	_, _ = writer.Write(rendered.Bytes())
}
