package web

import (
	"html/template"
	"net/http"
	"slices"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/resolve"
)

// projectsPage shows one project of the chosen organisation at a time:
// its record, what it resolves as a tree of levels, kinds and settings,
// each saying why it applies, what a skip list took out, and the files
// sync wrote into its clones here. The user level is the chosen login's.
// It reads the organisation's clone as it is, behind the login gate as
// rein resolve is.
func projectsPage(orchestrator orchestration.Orchestrator, version string, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		view := projectsView{frame: frameOf(request, report, version, "Projects", "projects"), LoggedIn: len(report.Logins) > 0}
		if !view.LoggedIn || view.Problem != "" || view.ChosenOrganization == "" {
			render(writer, page, view, statusOf(view.frame))
			return
		}
		description, err := orchestrator.DescribeOrganization(view.ChosenOrganization)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		clonesHere := map[string][]orchestration.CloneStatus{}
		for _, clone := range report.Clones {
			clonesHere[clone.Address] = append(clonesHere[clone.Address], clone)
		}
		named := view.query.Get("project")
		chosen := named
		for _, project := range description.Projects {
			if chosen == "" && len(clonesHere[addressOfProject(description.Slug, project.Slug)]) > 0 {
				chosen = project.Slug
			}
		}
		if chosen == "" && len(description.Projects) > 0 {
			chosen = description.Projects[0].Slug
		}
		var record orchestration.ProjectSummary
		for _, project := range description.Projects {
			target := carry(view.query, "org", "user")
			target.Set("project", project.Slug)
			entry := choice{Label: project.Name, Link: link("/projects", target), Chosen: project.Slug == chosen}
			if len(clonesHere[addressOfProject(description.Slug, project.Slug)]) > 0 {
				entry.Note = "cloned here"
			}
			if entry.Chosen {
				record = project
			}
			view.Projects = append(view.Projects, entry)
		}
		if named != "" && record.Slug == "" {
			view.Problem = named + " is not a project of " + description.Slug
		}
		if record.Slug == "" {
			render(writer, page, view, statusOf(view.frame))
			return
		}
		resolution, err := orchestrator.ResolveProject(description.Slug, record.Slug, view.userLogin)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		detail := detailOf(resolution, record, view.userLogin)
		for _, clone := range clonesHere[resolution.Address] {
			files, err := orchestrator.SyncedFiles(clone.ClonePath)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
				return
			}
			detail.Clones = append(detail.Clones, cloneFilesOf(clone, files, description.Slug))
		}
		view.Detail = &detail
		render(writer, page, view, http.StatusOK)
	})
}

// statusOf is 404 for an address that names what this machine does not
// have, 200 otherwise.
func statusOf(shown frame) int {
	if shown.Problem != "" {
		return http.StatusNotFound
	}
	return http.StatusOK
}

// projectsView is the Projects page: the menu of the organisation's
// projects and the chosen one's detail, nil when there is none.
type projectsView struct {
	frame
	LoggedIn bool
	Projects []choice
	Detail   *projectDetail
}

// projectDetail is one project as the page shows it.
type projectDetail struct {
	Slug     string
	Name     string
	Address  string
	Teams    []string
	Labels   []string
	User     string
	Settings int
	Skips    int
	Levels   []levelNode
	Skipped  []skippedRow
	Problems []resolve.Problem
	Clones   []cloneFiles
}

// levelNode is one level of the tree: what it is, how it is tied to the
// project, how many settings it gives and a skip list took out, and its
// settings by kind.
type levelNode struct {
	Heading  string
	Tie      string
	Settings int
	Skipped  int
	Kinds    []kindNode
}

// kindNode is one kind of setting under a level.
type kindNode struct {
	Name     string
	Settings []settingRow
}

// settingRow is one setting that applies and why: required or default, the
// labels that selected it, the ones that would leave it out, and the
// settings of higher levels it replaced, by address within the
// organisation.
type settingRow struct {
	ID          string
	Address     string
	Description string
	Enforcement string
	SelectedBy  string
	LeftOutBy   string
	Replaces    []string
}

// skippedRow is one setting a skip list took out: the list's level and,
// for a required one, the exception that allows it.
type skippedRow struct {
	ID        string
	Kind      string
	Address   string
	By        string
	Exception string
}

// cloneFiles is one clone of the project on this machine and the files
// sync wrote into it.
type cloneFiles struct {
	Folder   string
	LastSync string
	Files    []fileRow
}

// fileRow is one file sync wrote; Changed marks one edited by hand or
// missing.
type fileRow struct {
	Path    string
	From    string
	Written string
	State   string
	Changed bool
}

// kindNames are the kinds as the page names them.
var kindNames = map[string]string{
	address.Rules:       "Rules",
	address.Skills:      "Skills",
	address.Hooks:       "Hooks",
	address.Permissions: "Permissions",
	address.MCPServers:  "Tool servers",
	address.Agents:      "Agents",
}

// levelNames are the collections of a level's address as the page names
// them.
var levelNames = map[string]string{
	address.Organizations: "organisation",
	address.Teams:         "team",
	address.Projects:      "project",
	address.Users:         "user",
}

func addressOfProject(organizationSlug string, projectSlug string) string {
	return address.Organizations + "." + organizationSlug + "." + address.Projects + "." + projectSlug
}

// detailOf builds the tree: the organisation, the project's teams, the
// project and the chosen login's user level, in that order, each with the
// settings it gives.
func detailOf(resolution orchestration.Resolution, record orchestration.ProjectSummary, userLogin string) projectDetail {
	detail := projectDetail{Slug: record.Slug, Name: record.Name, Address: resolution.Address, Teams: resolution.Teams, Labels: resolution.Labels, User: resolution.User, Settings: len(resolution.Result.Chosen), Skips: len(resolution.Result.Skipped), Problems: resolution.Result.Problems}
	organization := address.Organizations + "." + resolution.Organization
	levels := []string{organization}
	detail.Levels = []levelNode{{Heading: "Organisation " + resolution.Organization, Tie: "every project"}}
	for _, team := range resolution.Teams {
		levels = append(levels, organization+"."+address.Teams+"."+team)
		detail.Levels = append(detail.Levels, levelNode{Heading: "Team " + team, Tie: "the project is on this team"})
	}
	levels = append(levels, resolution.Address, organization+"."+address.Users+"."+resolution.User)
	detail.Levels = append(detail.Levels, levelNode{Heading: "Project " + resolution.Project, Tie: "the project's own"}, levelNode{Heading: "User " + resolution.User, Tie: "the login " + userLogin})
	levelOf := func(level string) int {
		index := slices.Index(levels, level)
		if index < 0 {
			levels = append(levels, level)
			detail.Levels = append(detail.Levels, levelNode{Heading: level})
			index = len(levels) - 1
		}
		return index
	}
	labels := map[string]bool{}
	for _, label := range resolution.Labels {
		labels[label] = true
	}
	for _, chosen := range resolution.Result.Chosen {
		node := &detail.Levels[levelOf(chosen.Level)]
		node.Settings++
		row := settingRow{ID: chosen.ID, Address: chosen.Address, Description: chosen.Selectors.Description, Enforcement: chosen.Selectors.Enforcement, LeftOutBy: strings.Join(chosen.Selectors.DoNotApplyIf, ", ")}
		if row.Enforcement == "" {
			row.Enforcement = "default"
		}
		// a skill says what it is for in its own frontmatter
		skillFile, isSkill := chosen.Files["SKILL.md"]
		if chosen.Kind == address.Skills && isSkill {
			skill, _, _ := declaration.ParseSkill("SKILL.md", chosen.ID, []byte(skillFile))
			row.Description = skill.Description
		}
		var selectedBy []string
		for _, label := range chosen.Selectors.ApplyIf {
			if labels[label] {
				selectedBy = append(selectedBy, label)
			}
		}
		row.SelectedBy = strings.Join(selectedBy, ", ")
		for _, replaced := range chosen.Replaced {
			row.Replaces = append(row.Replaces, strings.TrimPrefix(replaced, organization+"."))
		}
		kindIndex := slices.IndexFunc(node.Kinds, func(kind kindNode) bool { return kind.Name == kindNameOf(chosen.Kind) })
		if kindIndex < 0 {
			node.Kinds = append(node.Kinds, kindNode{Name: kindNameOf(chosen.Kind)})
			kindIndex = len(node.Kinds) - 1
		}
		node.Kinds[kindIndex].Settings = append(node.Kinds[kindIndex].Settings, row)
	}
	for _, skipped := range resolution.Result.Skipped {
		row := skippedRow{Address: skipped.Address, By: levelNameOf(skipped.By), Exception: skipped.Exception}
		parsed, err := address.Parse(skipped.Address)
		if err == nil {
			row.ID, row.Kind = parsed.ID(), kindNameOf(parsed.Kind())
			level, found := parsed.Parent()
			if found {
				detail.Levels[levelOf(level.String())].Skipped++
			}
		}
		detail.Skipped = append(detail.Skipped, row)
	}
	return detail
}

func kindNameOf(kind string) string {
	name, known := kindNames[kind]
	if !known {
		return kind
	}
	return name
}

// levelNameOf names a level by its address: "project shop" for
// organizations.dani.projects.shop.
func levelNameOf(level string) string {
	parsed, err := address.Parse(level)
	if err != nil {
		return level
	}
	name, known := levelNames[parsed.Kind()]
	if !known {
		return level
	}
	return name + " " + parsed.ID()
}

// cloneFilesOf is one clone's files as the page lists them, the
// declaration each came from named within the organisation.
func cloneFilesOf(clone orchestration.CloneStatus, files []orchestration.SyncedFile, organizationSlug string) cloneFiles {
	listed := cloneFiles{Folder: shortPath(clone.ClonePath), LastSync: localTimeOf(clone.LastSync)}
	for _, file := range files {
		listed.Files = append(listed.Files, fileRow{Path: file.Path, From: strings.TrimPrefix(file.Address, address.Organizations+"."+organizationSlug+"."), Written: file.WrittenAt.Local().Format(timeLayout), State: file.State, Changed: file.State != orchestration.SyncedAsWritten})
	}
	return listed
}
