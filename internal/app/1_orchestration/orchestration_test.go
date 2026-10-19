package orchestration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// The services are real; only the ports are faked.

const validRecord = "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\nhost: github\n"

type harness struct {
	home       string
	host       *fake.Host
	repository *fake.Repository
	state      *fake.State
	agent      *fake.Agent
	judge      *fake.Judge
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return &harness{
		home:       t.TempDir(),
		host:       &fake.Host{Login: "Danidani23"},
		repository: &fake.Repository{Record: validRecord},
		state:      fake.NewState(),
		agent:      &fake.Agent{Present: true, HomePath: t.TempDir()},
		judge:      &fake.Judge{Unavailable: "no judge in this test"},
	}
}

func (fixture *harness) orchestrator() Orchestrator {
	return New(NewServices(fixture.home, fixture.host, fixture.repository, fixture.state, []ports.Agent{fixture.agent}, fixture.judge))
}

func (fixture *harness) orgCloneFolders(t *testing.T) []string {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(fixture.home, "orgs"))
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestConnectOrganization(t *testing.T) {
	t.Run("a clone with one record is kept under orgs and becomes active", func(t *testing.T) {
		fixture := newHarness(t)
		result, err := fixture.orchestrator().ConnectOrganization("git@example:acme.git")
		if err != nil || result.Slug != "acme" || result.Name != "Acme" || !result.BecameActive {
			t.Fatalf("result %+v err %v", result, err)
		}
		if result.OrgClonePath != filepath.Join(fixture.home, "orgs", "0f8fad5b-d9cb-469f-a165-70867728950e") || fixture.state.Current.Active != "acme" {
			t.Fatalf("clone %q active %q", result.OrgClonePath, fixture.state.Current.Active)
		}
		if fixture.state.Current.OrgClonePaths["0f8fad5b-d9cb-469f-a165-70867728950e"] != result.OrgClonePath {
			t.Fatalf("clones %v", fixture.state.Current.OrgClonePaths)
		}
	})
	t.Run("the agents' homes are snapshotted on the way", func(t *testing.T) {
		fixture := newHarness(t)
		_ = os.WriteFile(filepath.Join(fixture.agent.HomePath, "INSTRUCTIONS.md"), []byte("be brief"), 0o644)
		result, err := fixture.orchestrator().ConnectOrganization("x")
		if err != nil || len(result.SnapshotPaths) != 1 || !strings.HasPrefix(result.SnapshotPaths[0], filepath.Join(fixture.home, "snapshots")) {
			t.Fatalf("result %+v err %v", result, err)
		}
	})
	t.Run("a second connect of the same organisation is refused and leaves the first clone alone", func(t *testing.T) {
		fixture := newHarness(t)
		first, err := fixture.orchestrator().ConnectOrganization("x")
		if err != nil || first.OrgClonePath == "" {
			t.Fatalf("first connect: %+v err %v", first, err)
		}
		marker := filepath.Join(first.OrgClonePath, "marker")
		_ = os.WriteFile(marker, []byte("x"), 0o644)
		_, err = fixture.orchestrator().ConnectOrganization("x")
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "already connected") {
			t.Fatalf("err %v", err)
		}
		_, err = os.Stat(marker)
		if err != nil || len(fixture.orgCloneFolders(t)) != 1 || len(fixture.state.Current.Organizations) != 1 {
			t.Fatalf("marker %v clones %v organisations %v", err, fixture.orgCloneFolders(t), fixture.state.Current.Organizations)
		}
	})
	t.Run("a clone without a record is refused and removed", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.repository = &fake.Repository{}
		_, err := fixture.orchestrator().ConnectOrganization("x")
		if !errors.Is(err, refusal.ErrRefused) || len(fixture.orgCloneFolders(t)) != 0 {
			t.Fatalf("err %v clones %v", err, fixture.orgCloneFolders(t))
		}
	})
	t.Run("a failed clone is not a refusal", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.repository = &fake.Repository{Fail: true}
		_, err := fixture.orchestrator().ConnectOrganization("x")
		if err == nil || errors.Is(err, refusal.ErrRefused) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("a failure after cloning compensates: the clone is removed and nothing is registered", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.FailSaves = 1
		_, err := fixture.orchestrator().ConnectOrganization("x")
		if err == nil || errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "disk full") {
			t.Fatalf("err %v", err)
		}
		if len(fixture.orgCloneFolders(t)) != 0 || len(fixture.state.Current.Organizations) != 0 {
			t.Fatalf("clones %v organisations %v", fixture.orgCloneFolders(t), fixture.state.Current.Organizations)
		}
	})
}

func TestDisconnectOrganization(t *testing.T) {
	t.Run("forgets the organisation and removes its clone", func(t *testing.T) {
		fixture := newHarness(t)
		_, _ = fixture.orchestrator().ConnectOrganization("x")
		result, err := fixture.orchestrator().DisconnectOrganization("acme")
		if err != nil || result.ActiveAfter != "" || len(fixture.state.Current.Organizations) != 0 {
			t.Fatalf("result %+v err %v state %+v", result, err, fixture.state.Current)
		}
		_, err = os.Stat(result.OrgClonePath)
		if err == nil {
			t.Fatalf("clone still there")
		}
	})
	t.Run("an unknown slug is refused", func(t *testing.T) {
		fixture := newHarness(t)
		_, err := fixture.orchestrator().DisconnectOrganization("nobody")
		if !errors.Is(err, refusal.ErrRefused) {
			t.Fatalf("err %v", err)
		}
	})
}

func TestStatus(t *testing.T) {
	t.Run("a logged-out host is reported, not refused", func(t *testing.T) {
		fixture := newHarness(t)
		orchestrator := New(NewServices(fixture.home, &fake.Host{}, fixture.repository, fixture.state, []ports.Agent{fixture.agent}, fixture.judge))
		report, err := orchestrator.Status()
		if err != nil || !strings.Contains(report.IdentityError, "gh auth login") || report.Identity.Login != "" {
			t.Fatalf("report %+v err %v", report, err)
		}
	})
	t.Run("placements are checked for their targets and agents are listed", func(t *testing.T) {
		fixture := newHarness(t)
		present := filepath.Join(t.TempDir(), "present")
		_ = os.WriteFile(present, []byte("x"), 0o644)
		fixture.state.Current.Placements = []machine.Placement{{Path: "/a", Target: present}, {Path: "/b", Target: "/nowhere/gone"}}
		fixture.state.Current.Projects = []machine.Project{{Organization: "id-1"}}
		report, err := fixture.orchestrator().Status()
		if err != nil || report.Missing != 1 || len(report.Placements) != 2 || !report.Placements[0].TargetExists || report.Placements[1].TargetExists {
			t.Fatalf("report %+v err %v", report, err)
		}
		if len(report.Agents) != 1 || report.Agents[0] != "fake-agent" || len(report.Projects) != 1 {
			t.Fatalf("agents %v projects %v", report.Agents, report.Projects)
		}
		fixture.agent.Present = false
		report, _ = fixture.orchestrator().Status()
		if len(report.Agents) != 0 {
			t.Fatalf("agents %v", report.Agents)
		}
	})
}

func TestReset(t *testing.T) {
	t.Run("the plan lists what the state holds", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Placements = []machine.Placement{{Path: "/a"}}
		fixture.state.Current.Organizations = []machine.OrganizationRef{{Slug: "acme"}}
		plan, err := fixture.orchestrator().PlanReset(true)
		if err != nil || len(plan.Placements) != 1 || len(plan.Organizations) != 1 || !plan.All {
			t.Fatalf("plan %+v err %v", plan, err)
		}
		if plan.OrgClonesFolder != filepath.Join(fixture.home, "orgs") || plan.ImportsFolder != filepath.Join(fixture.home, "imports") {
			t.Fatalf("plan %+v", plan)
		}
	})
	t.Run("reset unplaces through the agent, keeps what was skipped, and with all forgets the machine", func(t *testing.T) {
		fixture := newHarness(t)
		orgClonePath := filepath.Join(fixture.home, "orgs", "id-1")
		_ = os.MkdirAll(orgClonePath, 0o755)
		_ = os.MkdirAll(filepath.Join(fixture.home, "imports"), 0o755)
		fixture.state.Current.Placements = []machine.Placement{{Path: "/a"}, {Path: "/foreign"}}
		fixture.state.Current.Organizations = []machine.OrganizationRef{{ID: "id-1", Slug: "acme"}}
		fixture.state.Current.OrgClonePaths = map[string]string{"id-1": orgClonePath}
		fixture.state.Current.Active = "acme"
		fixture.state.Current.Projects = []machine.Project{{Organization: "id-1"}}
		fixture.state.Current.Imports = []machine.Import{{}}
		plan, _ := fixture.orchestrator().PlanReset(true)
		result, err := fixture.orchestrator().ResetMachine(plan)
		if err != nil || result.Kept != 1 || len(result.RemovedOrgClones) != 1 || len(fixture.agent.Unplaced) != 2 {
			t.Fatalf("result %+v err %v unplaced %v", result, err, fixture.agent.Unplaced)
		}
		state := fixture.state.Current
		if len(state.Placements) != 1 || state.Placements[0].Path != "/foreign" || state.Active != "" || len(state.Organizations) != 0 || len(state.Projects) != 0 || len(state.Imports) != 0 {
			t.Fatalf("state %+v", state)
		}
		for _, gone := range []string{orgClonePath, filepath.Join(fixture.home, "imports")} {
			_, err = os.Stat(gone)
			if err == nil {
				t.Fatalf("%s still there", gone)
			}
		}
	})
	t.Run("without all the organisations stay", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Organizations = []machine.OrganizationRef{{ID: "id-1", Slug: "acme"}}
		fixture.state.Current.Active = "acme"
		plan, _ := fixture.orchestrator().PlanReset(false)
		_, err := fixture.orchestrator().ResetMachine(plan)
		if err != nil || len(fixture.state.Current.Organizations) != 1 || fixture.state.Current.Active != "acme" {
			t.Fatalf("err %v state %+v", err, fixture.state.Current)
		}
	})
}

func TestCreateOrganization(t *testing.T) {
	request := CreateOrganizationRequest{Name: "Acme", Description: "Agent settings of Acme", Owner: "Danidani23", RepositoryName: "acme-agent-settings", Private: true, Codeowner: "Danidani23"}
	t.Run("creates, publishes, protects and connects; the scaffold folder is gone afterwards", func(t *testing.T) {
		fixture := newHarness(t)
		_ = os.WriteFile(filepath.Join(fixture.agent.HomePath, "INSTRUCTIONS.md"), []byte("be brief"), 0o644)
		result, err := fixture.orchestrator().CreateOrganization(request)
		if err != nil || result.Remote != "fake://Danidani23/acme-agent-settings" || result.ProtectionRefused != "" || result.Connected.Slug != "acme" || !result.Connected.BecameActive {
			t.Fatalf("result %+v err %v", result, err)
		}
		if len(fixture.host.Created) != 1 || !fixture.host.Created[0].Private || fixture.host.Created[0].Description != "Agent settings of Acme" || len(fixture.host.ProtectedBranches) != 1 {
			t.Fatalf("host %+v", fixture.host)
		}
		for _, relative := range []string{"CODEOWNERS", "README.md", filepath.Join("declarations", "organizations", "acme", "organization.yaml"), filepath.Join("declarations", "organizations", "acme", "labels", "language.yaml")} {
			_, err = os.Stat(filepath.Join(result.Connected.OrgClonePath, relative))
			if err != nil {
				t.Fatalf("%s: %v", relative, err)
			}
		}
		if folders := fixture.orgCloneFolders(t); len(folders) != 1 || len(result.Connected.SnapshotPaths) != 1 {
			t.Fatalf("folders %v snapshots %v", folders, result.Connected.SnapshotPaths)
		}
		if len(fixture.state.Current.Organizations) != 1 || fixture.state.Current.Organizations[0].Remote != result.Remote || fixture.state.Current.Organizations[0].ConnectedAt.IsZero() {
			t.Fatalf("state %+v", fixture.state.Current)
		}
	})
	t.Run("a failed publish deletes the repository again and leaves nothing behind", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.repository.FailPublish = true
		_, err := fixture.orchestrator().CreateOrganization(request)
		if err == nil || errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "remote rejected") {
			t.Fatalf("err %v", err)
		}
		if len(fixture.host.Deleted) != 1 || fixture.host.Deleted[0] != "Danidani23/acme-agent-settings" || len(fixture.orgCloneFolders(t)) != 0 || len(fixture.state.Current.Organizations) != 0 {
			t.Fatalf("deleted %v folders %v state %+v", fixture.host.Deleted, fixture.orgCloneFolders(t), fixture.state.Current)
		}
	})
	t.Run("a refused protection is reported, not fatal", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.host.ProtectionRefusal = "Upgrade to GitHub Pro or make this repository public to enable this feature. (HTTP 403)"
		result, err := fixture.orchestrator().CreateOrganization(request)
		if err != nil || !strings.Contains(result.ProtectionRefused, "GitHub Pro") || result.Connected.Slug != "acme" {
			t.Fatalf("result %+v err %v", result, err)
		}
	})
	t.Run("a name without a letter or digit and a failed create are refused or failed before anything exists", func(t *testing.T) {
		fixture := newHarness(t)
		_, err := fixture.orchestrator().CreateOrganization(CreateOrganizationRequest{Name: "!!!"})
		if !errors.Is(err, refusal.ErrRefused) {
			t.Fatalf("err %v", err)
		}
		fixture.host.FailCreate = true
		_, err = fixture.orchestrator().CreateOrganization(request)
		if err == nil || errors.Is(err, refusal.ErrRefused) || len(fixture.host.Deleted) != 0 || len(fixture.orgCloneFolders(t)) != 0 {
			t.Fatalf("err %v deleted %v folders %v", err, fixture.host.Deleted, fixture.orgCloneFolders(t))
		}
	})
	t.Run("creating an organisation that is already connected is refused and the new repository deleted", func(t *testing.T) {
		fixture := newHarness(t)
		_, _ = fixture.orchestrator().ConnectOrganization("x")
		_, err := fixture.orchestrator().CreateOrganization(request)
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "already connected") || len(fixture.host.Deleted) != 1 || len(fixture.orgCloneFolders(t)) != 1 {
			t.Fatalf("err %v deleted %v folders %v", err, fixture.host.Deleted, fixture.orgCloneFolders(t))
		}
	})
}

// withOrganization is a fixture with an organisation created and active.
func withOrganization(t *testing.T) (*harness, CreateResult) {
	t.Helper()
	fixture := newHarness(t)
	created, err := fixture.orchestrator().CreateOrganization(CreateOrganizationRequest{Name: "Acme", Owner: "Danidani23", RepositoryName: "acme-agent-settings", Private: true, Codeowner: "Danidani23"})
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}
	return fixture, created
}

func TestCreateTeam(t *testing.T) {
	t.Run("writes the record, commits and pushes it; the remote has it", func(t *testing.T) {
		fixture, created := withOrganization(t)
		result, err := fixture.orchestrator().CreateTeam(CreateTeamRequest{Name: "Web Team", Description: "the web"})
		if err != nil || result.Slug != "web-team" || result.Organization != "acme" || result.RelativePath != "declarations/organizations/acme/teams/web-team/team.yaml" {
			t.Fatalf("result %+v err %v", result, err)
		}
		content, err := os.ReadFile(filepath.Join(created.Connected.OrgClonePath, filepath.FromSlash(result.RelativePath)))
		if err != nil || !strings.Contains(string(content), "slug: web-team") || !strings.Contains(string(content), "created_at:") {
			t.Fatalf("record %q err %v", content, err)
		}
		commits := fixture.repository.Commits
		if commits[len(commits)-1] != "team web-team (rein team create)" {
			t.Fatalf("commits %v", commits)
		}
		_, err = os.Stat(filepath.Join(fixture.repository.Published[created.Remote], filepath.FromSlash(result.RelativePath)))
		if err != nil {
			t.Fatalf("remote: %v", err)
		}
	})
	t.Run("a second team with the same slug is refused; so is a missing organisation", func(t *testing.T) {
		fixture, _ := withOrganization(t)
		_, _ = fixture.orchestrator().CreateTeam(CreateTeamRequest{Name: "Web"})
		_, err := fixture.orchestrator().CreateTeam(CreateTeamRequest{Name: "web"})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("err %v", err)
		}
		_, err = newHarness(t).orchestrator().CreateTeam(CreateTeamRequest{Name: "Web"})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "no active organisation") {
			t.Fatalf("err %v", err)
		}
		_, err = fixture.orchestrator().CreateTeam(CreateTeamRequest{Organization: "nobody", Name: "Web"})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "not connected") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("a rejected push puts the clone back and is not a refusal", func(t *testing.T) {
		fixture, created := withOrganization(t)
		fixture.repository.FailPush = true
		result, err := fixture.orchestrator().CreateTeam(CreateTeamRequest{Name: "Web"})
		if err == nil || errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "protected branch") {
			t.Fatalf("result %+v err %v", result, err)
		}
		_, err = os.Stat(filepath.Join(created.Connected.OrgClonePath, "declarations", "organizations", "acme", "teams"))
		if err == nil {
			t.Fatalf("the team record is still in the clone")
		}
	})
}

func TestCreateProject(t *testing.T) {
	t.Run("writes the record with its teams, labels and path", func(t *testing.T) {
		fixture, created := withOrganization(t)
		_, _ = fixture.orchestrator().CreateTeam(CreateTeamRequest{Name: "Web"})
		result, err := fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}, Labels: []string{"language:go", "kind:web-app"}, Path: "services/shop"})
		if err != nil || result.Slug != "shop" || result.RelativePath != "declarations/organizations/acme/projects/shop/project.yaml" {
			t.Fatalf("result %+v err %v", result, err)
		}
		content, _ := os.ReadFile(filepath.Join(created.Connected.OrgClonePath, filepath.FromSlash(result.RelativePath)))
		for _, expected := range []string{"slug: shop", "- web", "- language:go", "path: services/shop", "created_at:"} {
			if !strings.Contains(string(content), expected) {
				t.Fatalf("record lacks %q: %q", expected, content)
			}
		}
		description, err := fixture.orchestrator().DescribeOrganization("")
		if err != nil || description.Slug != "acme" || len(description.Teams) != 1 || description.Teams[0].Slug != "web" || len(description.LabelCategories) != 3 || description.LabelCategories[0].Category != "data" {
			t.Fatalf("description %+v err %v", description, err)
		}
	})
	t.Run("a missing team, an unregistered label, no team and a bad path are refused before anything is written", func(t *testing.T) {
		fixture, created := withOrganization(t)
		_, _ = fixture.orchestrator().CreateTeam(CreateTeamRequest{Name: "Web"})
		cases := map[string]CreateProjectRequest{
			"does not exist":      {Name: "Shop", Teams: []string{"nope"}},
			"not in the registry": {Name: "Shop", Teams: []string{"web"}, Labels: []string{"language:cobol"}},
			"names no category":   {Name: "Shop", Teams: []string{"web"}, Labels: []string{"colour:red"}},
			"teams is required":   {Name: "Shop"},
			"relative path":       {Name: "Shop", Teams: []string{"web"}, Path: "../outside"},
		}
		for expected, request := range cases {
			_, err := fixture.orchestrator().CreateProject(request)
			if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), expected) {
				t.Fatalf("%s: err %v", expected, err)
			}
		}
		_, err := os.Stat(filepath.Join(created.Connected.OrgClonePath, "declarations", "organizations", "acme", "projects"))
		if err == nil {
			t.Fatalf("a project record was written")
		}
	})
}

const rootCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// projectClone makes a fake repository with the fake agent's files in it:
// two tracked, one not.
func projectClone(t *testing.T) string {
	t.Helper()
	// resolved, since git reports real paths and the location is compared to it
	folder, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	err = fake.NewProjectClone(folder, []string{rootCommit}, []string{"INSTRUCTIONS.md", ".fake-agent/rules.md", "main.go"})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	for relative, content := range map[string]string{"INSTRUCTIONS.md": "be brief", ".fake-agent/rules.md": "rule", ".fake-agent/local.md": "mine", "main.go": "package main"} {
		path := filepath.Join(folder, filepath.FromSlash(relative))
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, []byte(content), 0o644)
	}
	return folder
}

// withTeam is an organisation with the team web.
func withTeam(t *testing.T) (*harness, CreateResult) {
	t.Helper()
	fixture, created := withOrganization(t)
	_, err := fixture.orchestrator().CreateTeam(CreateTeamRequest{Name: "Web"})
	if err != nil {
		t.Fatalf("team: %v", err)
	}
	return fixture, created
}

func TestInitializeProject(t *testing.T) {
	t.Run("binds to a named unbound record, moves the agent's files out, records, ignores, registers and commits", func(t *testing.T) {
		fixture, created := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		folder := projectClone(t)
		location, err := fixture.orchestrator().LocateProject(folder)
		if err != nil || len(location.Matches) != 0 || len(location.Unbound) != 1 || location.Unbound[0].Project != "shop" || location.Initialized != "" {
			t.Fatalf("location %+v err %v", location, err)
		}
		result, err := fixture.orchestrator().InitializeProject(InitializeProjectRequest{Directory: folder, Project: "shop"})
		if err != nil || !result.Bound || result.Created || result.Address != "organizations.acme.projects.shop" || result.Path != "." {
			t.Fatalf("result %+v err %v", result, err)
		}
		if len(result.MovedOut) != 1 || strings.Join(result.MovedOut[0].Paths, ",") != "INSTRUCTIONS.md,.fake-agent" || result.MovedOut[0].SnapshotPath == "" {
			t.Fatalf("moved out %+v", result.MovedOut)
		}
		for _, gone := range []string{"INSTRUCTIONS.md", ".fake-agent"} {
			_, err = os.Stat(filepath.Join(folder, gone))
			if err == nil {
				t.Fatalf("%s still there", gone)
			}
		}
		_, err = os.Stat(filepath.Join(folder, "main.go"))
		if err != nil {
			t.Fatalf("main.go gone")
		}
		ignore, _ := os.ReadFile(filepath.Join(folder, ".gitignore"))
		if !result.IgnoreChanged || !strings.Contains(string(ignore), "INSTRUCTIONS.md\n.fake-agent\n") {
			t.Fatalf("ignore %v %q", result.IgnoreChanged, ignore)
		}
		removals, _ := fixture.orchestrator().Ledger.Load()
		if len(removals.Agents["fake-agent"]) != 2 || removals.Agents["fake-agent"][0].Project != result.Address || removals.Agents["fake-agent"][0].Snapshot != result.MovedOut[0].SnapshotPath {
			t.Fatalf("removals %+v", removals)
		}
		projects, _ := fixture.orchestrator().Organizations.Projects()
		if len(projects) != 1 || projects[0].ClonePath != folder || projects[0].Fingerprint != rootCommit || projects[0].InitializedAt.IsZero() || projects[0].Organization != fixture.state.Current.Organizations[0].ID {
			t.Fatalf("projects %+v", projects)
		}
		if result.Commit == "" || result.CommitError != "" || fixture.repository.Commits[len(fixture.repository.Commits)-1] != moveOutCommitMessage {
			t.Fatalf("commit %q error %q commits %v", result.Commit, result.CommitError, fixture.repository.Commits)
		}
		record, _ := os.ReadFile(filepath.Join(fixture.repository.Published[created.Remote], "declarations", "organizations", "acme", "projects", "shop", "project.yaml"))
		if !strings.Contains(string(record), rootCommit) {
			t.Fatalf("the remote's record has no fingerprint: %q", record)
		}
		_, err = fixture.orchestrator().InitializeProject(InitializeProjectRequest{Directory: folder, Project: "shop"})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "already initialised") {
			t.Fatalf("err %v", err)
		}
		second := projectClone(t)
		location, _ = fixture.orchestrator().LocateProject(second)
		result, err = fixture.orchestrator().InitializeProject(InitializeProjectRequest{Directory: second})
		if err != nil || len(location.Matches) != 1 || result.Bound || result.Created || result.Address != "organizations.acme.projects.shop" {
			t.Fatalf("second clone: location %+v result %+v err %v", location, result, err)
		}
	})
	t.Run("creates the project on the spot with the fingerprint and path", func(t *testing.T) {
		fixture, created := withTeam(t)
		folder := projectClone(t)
		result, err := fixture.orchestrator().InitializeProject(InitializeProjectRequest{Directory: folder, Create: &CreateProjectRequest{Name: "Shop", Teams: []string{"web"}, Labels: []string{"language:go"}}, NoCommit: true})
		if err != nil || !result.Created || result.Address != "organizations.acme.projects.shop" || result.Commit != "" || len(fixture.repository.Staged) != 0 {
			t.Fatalf("result %+v err %v staged %v", result, err, fixture.repository.Staged)
		}
		record, _ := os.ReadFile(filepath.Join(fixture.repository.Published[created.Remote], "declarations", "organizations", "acme", "projects", "shop", "project.yaml"))
		if !strings.Contains(string(record), rootCommit) || !strings.Contains(string(record), "- language:go") {
			t.Fatalf("record %q", record)
		}
	})
	t.Run("refusals: no matching record without a name, an unknown record, a record bound elsewhere, no repository", func(t *testing.T) {
		fixture, _ := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Other", Teams: []string{"web"}, Fingerprint: []string{"cccccccccccccccccccccccccccccccccccccccc"}})
		folder := projectClone(t)
		cases := map[string]InitializeProjectRequest{
			"no project of acme carries": {Directory: folder},
			"does not exist":             {Directory: folder, Project: "nope"},
			"bound to another":           {Directory: folder, Project: "other"},
			"not inside a git":           {Directory: t.TempDir(), Project: "other"},
		}
		for expected, request := range cases {
			_, err := fixture.orchestrator().InitializeProject(request)
			if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), expected) {
				t.Fatalf("%s: err %v", expected, err)
			}
		}
		_, err := os.Stat(filepath.Join(folder, "INSTRUCTIONS.md"))
		if err != nil {
			t.Fatalf("a refused init moved files out")
		}
	})
	t.Run("a failure before the commit puts everything back", func(t *testing.T) {
		fixture, _ := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		folder := projectClone(t)
		fixture.state.FailSaves = 1
		_, err := fixture.orchestrator().InitializeProject(InitializeProjectRequest{Directory: folder, Project: "shop"})
		if err == nil || errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "disk full") {
			t.Fatalf("err %v", err)
		}
		for relative, content := range map[string]string{"INSTRUCTIONS.md": "be brief", ".fake-agent/rules.md": "rule", ".fake-agent/local.md": "mine"} {
			restored, err := os.ReadFile(filepath.Join(folder, filepath.FromSlash(relative)))
			if err != nil || string(restored) != content {
				t.Fatalf("%s: %q err %v", relative, restored, err)
			}
		}
		_, err = os.Stat(filepath.Join(folder, ".gitignore"))
		snapshots, _ := os.ReadDir(filepath.Join(fixture.home, "snapshots"))
		removals, _ := fixture.orchestrator().Ledger.Load()
		if err == nil || len(snapshots) != 0 || len(removals.Agents) != 0 || len(fixture.state.Current.Projects) != 0 {
			t.Fatalf("ignore err %v snapshots %v removals %+v projects %v", err, snapshots, removals, fixture.state.Current.Projects)
		}
	})
	t.Run("a failed commit is reported, the rest stands", func(t *testing.T) {
		fixture, _ := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		folder := projectClone(t)
		fixture.repository.FailCommit = true
		result, err := fixture.orchestrator().InitializeProject(InitializeProjectRequest{Directory: folder, Project: "shop"})
		if err != nil || result.Commit != "" || !strings.Contains(result.CommitError, "gpg") || len(fixture.state.Current.Projects) != 1 {
			t.Fatalf("result %+v err %v", result, err)
		}
	})
}

// claudeCodeSnapshot stores a snapshot of a Claude Code harness for the
// project address, as init would have taken it.
func claudeCodeSnapshot(t *testing.T, fixture *harness, project string) string {
	t.Helper()
	folder := t.TempDir()
	for relative, content := range map[string]string{"CLAUDE.md": "# Shop\n\nBe brief.\n", ".claude/rules/go.md": "Two-line errors.\n", ".mcp.json": `{"mcpServers":{"docs":{"type":"http","url":"https://docs.example/mcp"}}}`} {
		path := filepath.Join(folder, filepath.FromSlash(relative))
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, []byte(content), 0o644)
	}
	snapshotPath, _, err := fixture.orchestrator().Snapshots.Take("claude-code", project, folder, []string{"CLAUDE.md", ".claude", ".mcp.json"})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return snapshotPath
}

func TestDistillHarness(t *testing.T) {
	t.Run("an initialised project's latest snapshot becomes candidates with verdicts, written for propose", func(t *testing.T) {
		fixture, created := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		folder := projectClone(t)
		_, err := fixture.orchestrator().InitializeProject(InitializeProjectRequest{Directory: folder, Project: "shop"})
		if err != nil {
			t.Fatalf("init: %v", err)
		}
		snapshotPath := claudeCodeSnapshot(t, fixture, "organizations.acme.projects.shop")
		existing := filepath.Join(created.Connected.OrgClonePath, "declarations", "organizations", "acme", "rules", "go.md")
		_ = os.MkdirAll(filepath.Dir(existing), 0o755)
		_ = os.WriteFile(existing, []byte("Two-line errors.\n"), 0o644)
		result, err := fixture.orchestrator().DistillHarness(DistillRequest{Directory: folder})
		if err != nil || result.Level != "organizations.acme.projects.shop" || len(result.Candidates) != 3 || len(result.Report.Snapshots) != 2 || len(result.Report.Skipped) != 1 {
			t.Fatalf("result %+v err %v", result, err)
		}
		verdicts := map[string]string{}
		for _, summary := range result.Report.Candidates {
			verdicts[summary.Kind+"/"+summary.ID] = summary.Verdict
		}
		if verdicts["rules/go"] != "identical" || verdicts["rules/instructions"] != "new" || verdicts["mcpservers/docs"] != "new" || !strings.Contains(strings.Join(result.Report.Snapshots, ","), snapshotPath) {
			t.Fatalf("verdicts %v snapshots %v", verdicts, result.Report.Snapshots)
		}
		if !strings.HasPrefix(result.Folder, filepath.Join(fixture.home, "distill", "shop-")) {
			t.Fatalf("folder %q", result.Folder)
		}
		for _, relative := range []string{"rules/instructions.md", "rules/go.md", "mcpservers/docs.yaml", "report.yaml"} {
			_, err = os.Stat(filepath.Join(result.Folder, filepath.FromSlash(relative)))
			if err != nil {
				t.Fatalf("%s: %v", relative, err)
			}
		}
		report, _ := os.ReadFile(filepath.Join(result.Folder, "report.yaml"))
		if !strings.Contains(string(report), "level: organizations.acme.projects.shop") || !strings.Contains(string(report), "verdict: identical") {
			t.Fatalf("report %q", report)
		}
	})
	t.Run("the home becomes the user's level, a named snapshot its own, and the refusals", func(t *testing.T) {
		fixture, _ := withTeam(t)
		snapshotPath := claudeCodeSnapshot(t, fixture, "")
		result, err := fixture.orchestrator().DistillHarness(DistillRequest{Home: true})
		if err != nil || result.Level != "users.danidani23" || len(result.Candidates) != 3 {
			t.Fatalf("result %+v err %v", result, err)
		}
		result, err = fixture.orchestrator().DistillHarness(DistillRequest{SnapshotPath: snapshotPath})
		if err != nil || result.Level != "users.danidani23" {
			t.Fatalf("result %+v err %v", result, err)
		}
		_, err = fixture.orchestrator().DistillHarness(DistillRequest{Directory: projectClone(t)})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "not initialised") {
			t.Fatalf("err %v", err)
		}
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Bare", Teams: []string{"web"}})
		_, err = fixture.orchestrator().DistillHarness(DistillRequest{Project: "bare"})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "no snapshot") {
			t.Fatalf("err %v", err)
		}
	})
}

func TestProposeDeclarations(t *testing.T) {
	t.Run("a distillation goes to its level on a branch, as a pull request, and the clone ends on main", func(t *testing.T) {
		fixture, created := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		_ = claudeCodeSnapshot(t, fixture, "organizations.acme.projects.shop")
		existing := filepath.Join(created.Connected.OrgClonePath, "declarations", "organizations", "acme", "rules", "go.md")
		_ = os.MkdirAll(filepath.Dir(existing), 0o755)
		_ = os.WriteFile(existing, []byte("Two-line errors.\n"), 0o644)
		distilled, err := fixture.orchestrator().DistillHarness(DistillRequest{Project: "shop"})
		if err != nil {
			t.Fatalf("distill: %v", err)
		}
		result, err := fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: distilled.Folder})
		if err != nil || result.Level != "organizations.acme.projects.shop" || len(result.Included) != 2 || len(result.LeftOut) != 1 || !strings.HasPrefix(result.Branch, "rein/propose-shop-") || result.PullRequestURL != "https://github.com/fake/pull/1" {
			t.Fatalf("result %+v err %v", result, err)
		}
		if result.LeftOut[0].ID != "go" || result.LeftOut[0].Related[0] != "organizations.acme.rules.go" {
			t.Fatalf("left out %+v", result.LeftOut)
		}
		pullRequest := fixture.host.PullRequests[0]
		if pullRequest.Branch != result.Branch || !strings.Contains(pullRequest.Title, "Propose 2 declarations to organizations.acme.projects.shop") || !strings.Contains(pullRequest.Body, "| rules | instructions | new |") || !strings.Contains(pullRequest.Body, "Left out, identical") {
			t.Fatalf("pull request %+v", pullRequest)
		}
		published := fixture.repository.Published[created.Remote+"#"+result.Branch]
		for _, relative := range []string{"declarations/organizations/acme/projects/shop/rules/instructions.md", "declarations/organizations/acme/projects/shop/mcpservers/docs.yaml"} {
			_, err = os.Stat(filepath.Join(published, filepath.FromSlash(relative)))
			if err != nil {
				t.Fatalf("%s not on the branch: %v", relative, err)
			}
		}
		_, err = os.Stat(filepath.Join(published, "declarations", "organizations", "acme", "projects", "shop", "rules", "go.md"))
		if err == nil {
			t.Fatalf("the identical rule was proposed")
		}
		if fixture.repository.Current != "main" || strings.Join(fixture.repository.Deleted, ",") != result.Branch {
			t.Fatalf("clone on %q deleted %v", fixture.repository.Current, fixture.repository.Deleted)
		}
	})
	t.Run("a plain folder goes to the user's level with the user record, and a zip is read too", func(t *testing.T) {
		fixture, created := withTeam(t)
		folder := t.TempDir()
		_ = os.MkdirAll(filepath.Join(folder, "rules"), 0o755)
		_ = os.WriteFile(filepath.Join(folder, "rules", "brief.md"), []byte("Be brief.\n"), 0o644)
		result, err := fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: folder, Title: "My rules"})
		if err != nil || result.Level != "users.danidani23" || len(result.Included) != 1 || fixture.host.PullRequests[0].Title != "My rules" {
			t.Fatalf("result %+v err %v", result, err)
		}
		published := fixture.repository.Published[created.Remote+"#"+result.Branch]
		record, err := os.ReadFile(filepath.Join(published, "declarations", "users", "danidani23", "user.yaml"))
		if err != nil || !strings.Contains(string(record), "github: Danidani23") || !strings.Contains(string(record), "created_at:") {
			t.Fatalf("user record %q err %v", record, err)
		}
	})
	t.Run("refusals: no declarations, a bad level, a level of another organisation, the lint, nothing new", func(t *testing.T) {
		fixture, created := withTeam(t)
		empty := t.TempDir()
		_, err := fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: empty})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "holds no declarations") {
			t.Fatalf("err %v", err)
		}
		folder := t.TempDir()
		_ = os.MkdirAll(filepath.Join(folder, "rules"), 0o755)
		_ = os.WriteFile(filepath.Join(folder, "rules", "brief.md"), []byte("Be brief.\n"), 0o644)
		for expected, request := range map[string]ProposeRequest{
			"is not a level": {Source: folder, Level: "organizations.acme.rules.x"},
			"not connected":  {Source: folder, Level: "organizations.other"},
		} {
			_, err = fixture.orchestrator().ProposeDeclarations(request)
			if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), expected) {
				t.Fatalf("%s: err %v", expected, err)
			}
		}
		bad := t.TempDir()
		_ = os.MkdirAll(filepath.Join(bad, "mcpservers"), 0o755)
		_ = os.WriteFile(filepath.Join(bad, "mcpservers", "x.yaml"), []byte("transport: carrier-pigeon\n"), 0o644)
		_, err = fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: bad, Level: "organizations.acme"})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "do not pass the lint") || !strings.Contains(err.Error(), "carrier-pigeon") {
			t.Fatalf("err %v", err)
		}
		existing := filepath.Join(created.Connected.OrgClonePath, "declarations", "organizations", "acme", "rules", "brief.md")
		_ = os.MkdirAll(filepath.Dir(existing), 0o755)
		_ = os.WriteFile(existing, []byte("Be brief.\n"), 0o644)
		_, err = fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: folder, Level: "organizations.acme.teams.web"})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "nothing new") || len(fixture.repository.Branches) != 0 {
			t.Fatalf("err %v branches %v", err, fixture.repository.Branches)
		}
	})
	t.Run("a failed pull request abandons the branch locally and on the remote", func(t *testing.T) {
		fixture, created := withTeam(t)
		folder := t.TempDir()
		_ = os.MkdirAll(filepath.Join(folder, "rules"), 0o755)
		_ = os.WriteFile(filepath.Join(folder, "rules", "brief.md"), []byte("Be brief.\n"), 0o644)
		fixture.host.FailPullRequest = true
		_, err := fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: folder, Level: "organizations.acme"})
		if err == nil || errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("err %v", err)
		}
		if len(fixture.repository.RemoteDeleted) != 1 || len(fixture.repository.Deleted) != 1 || fixture.repository.Current != "main" || len(fixture.repository.Published) != 1 {
			t.Fatalf("repository %+v remote %s", fixture.repository, created.Remote)
		}
	})
}

func TestDistillWithTheJudge(t *testing.T) {
	answer := `{"version":1,"candidates":[
	 {"candidate":"rules/instructions","verdict":"new","suggestion":"keep","reason":"nothing covers it"},
	 {"candidate":"rules/go","verdict":"duplicate","related":["organizations.acme.rules.go"],"quotes":[{"address":"organizations.acme.rules.go","text":"Two-line errors."}],"suggestion":"drop","reason":"the organisation says it"},
	 {"candidate":"mcpservers/docs","verdict":"new","suggestion":"lift","level":"organization","reason":"everyone reads the docs"}]}`
	prepare := func(t *testing.T) (*harness, CreateResult) {
		t.Helper()
		fixture, created := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		_ = claudeCodeSnapshot(t, fixture, "organizations.acme.projects.shop")
		existing := filepath.Join(created.Connected.OrgClonePath, "declarations", "organizations", "acme", "rules", "go.md")
		_ = os.MkdirAll(filepath.Dir(existing), 0o755)
		_ = os.WriteFile(existing, []byte("Two-line errors.\n"), 0o644)
		return fixture, created
	}
	t.Run("the judge's answer lands in the candidates, the report and the pull request", func(t *testing.T) {
		fixture, _ := prepare(t)
		fixture.judge = &fake.Judge{Answers: []string{answer}}
		result, err := fixture.orchestrator().DistillHarness(DistillRequest{Project: "shop"})
		if err != nil || result.Report.Judge == nil || result.Report.Judge.Status != "answered" || result.Report.Judge.Backend != "fake-judge" {
			t.Fatalf("result %+v err %v", result.Report.Judge, err)
		}
		if !strings.Contains(fixture.judge.Briefs[0], "## organizations.acme.rules.go (applies)") || !strings.Contains(fixture.judge.Briefs[0], "## rules/instructions") {
			t.Fatalf("brief %q", fixture.judge.Briefs[0])
		}
		judged := map[string]*distill.Judgement{}
		for _, summary := range result.Report.Candidates {
			judged[summary.Kind+"/"+summary.ID] = summary.Judgement
		}
		if judged["rules/go"] == nil || judged["rules/go"].Verdict != "duplicate" || judged["mcpservers/docs"].Level != "organization" {
			t.Fatalf("judged %+v", judged)
		}
		report, _ := os.ReadFile(filepath.Join(result.Folder, "report.yaml"))
		if !strings.Contains(string(report), "judgement:") || !strings.Contains(string(report), "status: answered") {
			t.Fatalf("report %q", report)
		}
		proposed, err := fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: result.Folder})
		if err != nil || !strings.Contains(fixture.host.PullRequests[0].Body, "| the judge |") || !strings.Contains(fixture.host.PullRequests[0].Body, "lift to the organization level: everyone reads the docs") || !strings.Contains(fixture.host.PullRequests[0].Body, "Judge: fake-judge, answered") {
			t.Fatalf("proposed %+v err %v body %q", proposed, err, fixture.host.PullRequests[0].Body)
		}
	})
	t.Run("an unavailable judge is reported and skipped, none skips it silently, a dry run writes nothing", func(t *testing.T) {
		fixture, _ := prepare(t)
		result, err := fixture.orchestrator().DistillHarness(DistillRequest{Project: "shop"})
		if err != nil || result.Report.Judge == nil || result.Report.Judge.Status != "skipped" || !strings.Contains(result.Report.Judge.Problems[0], "no judge in this test") {
			t.Fatalf("result %+v err %v", result.Report.Judge, err)
		}
		result, err = fixture.orchestrator().DistillHarness(DistillRequest{Project: "shop", Judge: "none"})
		if err != nil || result.Report.Judge != nil || result.Brief != "" {
			t.Fatalf("result %+v err %v", result.Report, err)
		}
		before, _ := os.ReadDir(filepath.Join(fixture.home, "distill"))
		result, err = fixture.orchestrator().DistillHarness(DistillRequest{Project: "shop", DryRun: true})
		after, _ := os.ReadDir(filepath.Join(fixture.home, "distill"))
		if err != nil || result.Folder != "" || !strings.Contains(result.Brief, "# Candidates") || len(after) != len(before) {
			t.Fatalf("result folder %q brief %d err %v", result.Folder, len(result.Brief), err)
		}
	})
}
