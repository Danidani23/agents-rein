package web

import (
	"html/template"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
)

// machinePage shows what rein status shows of the machine: the logins, the
// agents, what rein placed in the agent's home, the connected
// organisations, the project clones and the proposals waiting. It runs
// without a login, as status does.
func machinePage(orchestrator orchestration.Orchestrator, version string, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		render(writer, page, machineViewOf(report, frameOf(request, report, version, "This machine", "machine")), http.StatusOK)
	})
}

// machineView is what the machine's page shows in its frame.
type machineView struct {
	frame
	Logins            []string
	Agents            []string
	Home              string
	PlacedFiles       int
	MissingPlacements []machine.Placement
	Organizations     []organizationRow
	Projects          []projectRow
	Pending           []pendingRow
	Imports           []importRow
}

// organizationRow is one connected organisation; Link is its repository on
// the web, "" for a remote that is not a web address.
type organizationRow struct {
	Slug       string
	Active     bool
	Repository string
	Link       string
	Login      string
}

// projectRow is one project clone with the drift of what sync wrote and
// what became of its latest snapshot; Link is its Projects page,
// PullRequest is set once the snapshot went as one.
type projectRow struct {
	Project          string
	Link             string
	Organization     string
	Folder           string
	SyncedFiles      int
	EditedByHand     int
	Missing          int
	LastSync         string
	Snapshot         string
	PullRequest      string
	PullRequestLabel string
}

// pendingRow is one proposal distill wrote and propose has not sent.
type pendingRow struct {
	Folder      string
	Level       string
	Candidates  int
	DistilledAt string
}

// importRow is one level unpacked by the import of 2026-10-15.
type importRow struct {
	Hash   string
	Origin string
	Path   string
}

// timeLayout is how the page writes a moment, in the machine's time zone.
const timeLayout = "2006-01-02 15:04"

// proposedPrefix starts the snapshot text of a project whose snapshot went
// as a pull request, the request's address following it.
const proposedPrefix = "snapshot distilled and proposed, "

func machineViewOf(report orchestration.StatusReport, shown frame) machineView {
	view := machineView{frame: shown, Logins: report.Logins, Agents: report.Agents, Home: shortPath(report.Home), PlacedFiles: len(report.Placements)}
	for _, placed := range report.Placements {
		if !placed.TargetExists {
			view.MissingPlacements = append(view.MissingPlacements, placed.Placement)
		}
	}
	// a project clone names its organisation by id; the page names it by slug
	slugByID := map[string]string{}
	for _, organization := range report.Organizations {
		slugByID[organization.ID] = organization.Slug
		repository := strings.TrimSuffix(strings.TrimPrefix(organization.Remote, "https://"), ".git")
		link := ""
		if strings.HasPrefix(organization.Remote, "https://") {
			link = "https://" + repository
		}
		view.Organizations = append(view.Organizations, organizationRow{Slug: organization.Slug, Active: organization.Slug == report.Active, Repository: repository, Link: link, Login: organization.Login})
	}
	for _, clone := range report.Clones {
		organization, connected := slugByID[clone.Organization]
		if !connected {
			organization = clone.Organization
		}
		project := clone.Address[strings.LastIndex(clone.Address, ".")+1:]
		target := carry(shown.query, "user")
		target.Set("org", organization)
		target.Set("project", project)
		row := projectRow{Project: project, Link: link("/projects", target), Organization: organization, Folder: shortPath(clone.ClonePath), SyncedFiles: clone.Inserts, EditedByHand: clone.Modified, Missing: clone.Missing, Snapshot: clone.Snapshot}
		row.LastSync = localTimeOf(clone.LastSync)
		pullRequest := strings.TrimPrefix(clone.Snapshot, proposedPrefix)
		if strings.HasPrefix(clone.Snapshot, proposedPrefix) && strings.HasPrefix(pullRequest, "https://") {
			row.Snapshot = "distilled and proposed as"
			row.PullRequest = pullRequest
			row.PullRequestLabel = "pull request #" + path.Base(pullRequest)
		}
		view.Projects = append(view.Projects, row)
	}
	for _, pending := range report.Pending {
		view.Pending = append(view.Pending, pendingRow{Folder: shortPath(pending.Path), Level: pending.Level, Candidates: pending.Candidates, DistilledAt: pending.DistilledAt.Local().Format(timeLayout)})
	}
	for _, imported := range report.Imports {
		origin := imported.Source
		if imported.FromUser != "" {
			origin = "from " + imported.FromUser
		}
		view.Imports = append(view.Imports, importRow{Hash: imported.Hash[:min(12, len(imported.Hash))], Origin: origin, Path: imported.Path})
	}
	return view
}

// localTimeOf writes a moment recorded in RFC 3339 in the machine's time
// zone, "" for one that does not parse.
func localTimeOf(moment string) string {
	parsed, err := time.Parse(time.RFC3339, moment)
	if err != nil {
		return ""
	}
	return parsed.Local().Format(timeLayout)
}

// shortPath writes the home folder as ~.
func shortPath(folder string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !strings.HasPrefix(folder, home+string(filepath.Separator)) {
		return folder
	}
	return "~" + strings.TrimPrefix(folder, home)
}
