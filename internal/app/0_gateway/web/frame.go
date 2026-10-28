package web

import (
	"html/template"
	"net/http"
	"net/url"
	"slices"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
)

// frame is what every page shows around its content: its title, its entry
// in the menu, the version, and the two menus of the top bar, the
// organisation and the login the page is seen as. The choice travels in
// the address as org and user and changes only what is shown, never the
// active organisation or the login that acts.
type frame struct {
	Title   string
	Current string
	Version string
	// MachineLink and ProjectsLink are the menu's entries, the choice
	// carried.
	MachineLink         string
	ProjectsLink        string
	OrganizationChoices []choice
	ChosenOrganization  string
	UserChoices         []choice
	ChosenUser          string
	// Problem says why the address asks for something this machine does
	// not have.
	Problem string
	// RunName is the action running now or run last, RunActive whether it
	// still runs; the menu leads to it.
	RunName   string
	RunActive bool
	// organization is the chosen organisation, userLogin the chosen login,
	// query the address's own.
	organization machine.OrganizationRef
	userLogin    string
	query        url.Values
}

// choice is one entry of a menu: what it shows, where it leads, a note
// beside it, and whether it is the one shown now.
type choice struct {
	Label  string
	Link   string
	Note   string
	Chosen bool
}

// switchView is one menu: its label, the entry shown now, and every entry.
type switchView struct {
	Label   string
	Chosen  string
	Choices []choice
}

// templateFunctions are what the templates call: switchOf builds a menu,
// chosenOf names the entry of a menu shown now.
var templateFunctions = template.FuncMap{
	"switchOf": func(label string, chosen string, choices []choice) switchView {
		return switchView{Label: label, Chosen: chosen, Choices: choices}
	},
	"chosenOf": func(choices []choice) string {
		for _, entry := range choices {
			if entry.Chosen {
				return entry.Label
			}
		}
		return ""
	},
}

// frameOf reads the choice from the request: the organisation named, or
// the active one; the login named, or the one that acts on the
// organisation.
func frameOf(request *http.Request, report orchestration.StatusReport, version string, title string, current string, actions *runner) frame {
	query := request.URL.Query()
	shown := frame{Title: title, Current: current, Version: version, query: query}
	latest := actions.latest()
	if latest != nil {
		shown.RunName, shown.RunActive = latest.Name, !latest.isFinished()
	}
	carried := carry(query, "org", "user")
	shown.MachineLink = link("/", carried)
	shown.ProjectsLink = link("/projects", carried)
	named := query.Get("org")
	for _, organization := range report.Organizations {
		if organization.Slug == named || (named == "" && organization.Slug == report.Active) {
			shown.organization = organization
		}
	}
	if named != "" && shown.organization.Slug == "" {
		shown.Problem = named + " is not an organisation connected here"
	}
	shown.ChosenOrganization = shown.organization.Slug
	for _, organization := range report.Organizations {
		target := carry(query, "user")
		target.Set("org", organization.Slug)
		shown.OrganizationChoices = append(shown.OrganizationChoices, choice{Label: organization.Slug, Link: link(request.URL.Path, target), Chosen: organization.Slug == shown.ChosenOrganization})
	}
	shown.userLogin = query.Get("user")
	if shown.userLogin == "" {
		shown.userLogin = shown.organization.Login
	}
	if query.Get("user") != "" && !slices.Contains(report.Logins, shown.userLogin) {
		shown.Problem = shown.userLogin + " is not logged in on this machine"
	}
	shown.ChosenUser = shown.userLogin
	for _, login := range report.Logins {
		target := carry(query, "org", "project")
		target.Set("user", login)
		shown.UserChoices = append(shown.UserChoices, choice{Label: login, Link: link(request.URL.Path, target), Chosen: login == shown.ChosenUser})
	}
	return shown
}

// carry keeps the named values of the address that are set.
func carry(query url.Values, keys ...string) url.Values {
	carried := url.Values{}
	for _, key := range keys {
		value := query.Get(key)
		if value != "" {
			carried.Set(key, value)
		}
	}
	return carried
}

// link is path with values as its query, or path alone without any.
func link(path string, values url.Values) string {
	encoded := values.Encode()
	if encoded == "" {
		return path
	}
	return path + "?" + encoded
}
