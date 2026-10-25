package orchestration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/proposal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/resolve"
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
	releases   *fake.Releases
	lock       *fake.Lock
	keychain   *fake.Keychain
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	fixture := &harness{
		home:       t.TempDir(),
		host:       &fake.Host{Login: "Danidani23"},
		repository: &fake.Repository{Record: validRecord},
		state:      fake.NewState(),
		agent:      &fake.Agent{Present: true, HomePath: t.TempDir()},
		judge:      &fake.Judge{Unavailable: "no judge in this test"},
		releases:   &fake.Releases{Latest_: ports.Release{Version: "v0.1.0"}},
		lock:       &fake.Lock{},
		keychain:   &fake.Keychain{Tokens: map[string]ports.Token{"Danidani23": {Login: "Danidani23", AccessToken: "token-of-Danidani23", ExpiresAt: time.Now().Add(time.Hour)}}},
	}
	fixture.state.Current.Logins = []string{"Danidani23"}
	return fixture
}

func (fixture *harness) orchestrator() Orchestrator {
	return New(NewServices(fixture.home, fixture.host, fixture.repository, fixture.state, []ports.Agent{fixture.agent}, fixture.judge, fixture.lock, fixture.keychain, fixture.releases, "v0.1.0", filepath.Join(fixture.home, "bin", "rein")))
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
		result, err := fixture.orchestrator().ConnectOrganization("git@example:acme.git", "")
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
		result, err := fixture.orchestrator().ConnectOrganization("x", "")
		if err != nil || len(result.SnapshotPaths) != 1 || !strings.HasPrefix(result.SnapshotPaths[0], filepath.Join(fixture.home, "snapshots")) {
			t.Fatalf("result %+v err %v", result, err)
		}
	})
	t.Run("a second connect of the same organisation is refused and leaves the first clone alone", func(t *testing.T) {
		fixture := newHarness(t)
		first, err := fixture.orchestrator().ConnectOrganization("x", "")
		if err != nil || first.OrgClonePath == "" {
			t.Fatalf("first connect: %+v err %v", first, err)
		}
		marker := filepath.Join(first.OrgClonePath, "marker")
		_ = os.WriteFile(marker, []byte("x"), 0o644)
		_, err = fixture.orchestrator().ConnectOrganization("x", "")
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
		_, err := fixture.orchestrator().ConnectOrganization("x", "")
		if !errors.Is(err, refusal.ErrRefused) || len(fixture.orgCloneFolders(t)) != 0 {
			t.Fatalf("err %v clones %v", err, fixture.orgCloneFolders(t))
		}
	})
	t.Run("a failed clone is not a refusal", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.repository = &fake.Repository{Fail: true}
		_, err := fixture.orchestrator().ConnectOrganization("x", "")
		if err == nil || errors.Is(err, refusal.ErrRefused) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("without a login connecting refuses", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Logins = nil
		_, err := fixture.orchestrator().ConnectOrganization("x", "")
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "rein login") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("a failure after cloning compensates: the clone is removed and nothing is registered", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.FailSaves = 1
		_, err := fixture.orchestrator().ConnectOrganization("x", "")
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
		_, _ = fixture.orchestrator().ConnectOrganization("x", "")
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
	t.Run("a machine without a login is reported, not refused", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Logins = nil
		report, err := fixture.orchestrator().Status()
		if err != nil || len(report.Logins) != 0 {
			t.Fatalf("report %+v err %v", report, err)
		}
		fixture.state.Current.Logins = []string{"Danidani23"}
		report, _ = fixture.orchestrator().Status()
		if strings.Join(report.Logins, ",") != "Danidani23" {
			t.Fatalf("logins %v", report.Logins)
		}
	})
	t.Run("placements are checked for their targets and agents are listed", func(t *testing.T) {
		fixture := newHarness(t)
		present := filepath.Join(t.TempDir(), "present")
		_ = os.WriteFile(present, []byte("x"), 0o644)
		fixture.state.Current.Placements = []machine.Placement{{Path: "/a", Target: present}, {Path: "/b", Target: "/nowhere/gone"}}
		fixture.state.Current.ProjectClones = []machine.ProjectClone{{Organization: "id-1"}}
		report, err := fixture.orchestrator().Status()
		if err != nil || report.Missing != 1 || len(report.Placements) != 2 || !report.Placements[0].TargetExists || report.Placements[1].TargetExists {
			t.Fatalf("report %+v err %v", report, err)
		}
		if len(report.Agents) != 1 || report.Agents[0] != "fake-agent" || len(report.ProjectClones) != 1 {
			t.Fatalf("agents %v projects %v", report.Agents, report.ProjectClones)
		}
		fixture.agent.Present = false
		report, _ = fixture.orchestrator().Status()
		if len(report.Agents) != 0 {
			t.Fatalf("agents %v", report.Agents)
		}
	})
}

func TestReset(t *testing.T) {
	t.Run("the plan lists what the state holds and where the ledger lives", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Placements = []machine.Placement{{Path: "/a"}}
		fixture.state.Current.Organizations = []machine.OrganizationRef{{Slug: "acme"}}
		plan, err := fixture.orchestrator().PlanReset()
		if err != nil || len(plan.Placements) != 1 || len(plan.Organizations) != 1 {
			t.Fatalf("plan %+v err %v", plan, err)
		}
		if plan.OrgClonesFolder != filepath.Join(fixture.home, "orgs") || plan.ImportsFolder != filepath.Join(fixture.home, "imports") || plan.RemovalsFile != filepath.Join(fixture.home, "removals.yaml") || plan.SnapshotsFolder != filepath.Join(fixture.home, "snapshots") || plan.ProposalsFolder != filepath.Join(fixture.home, "proposals") {
			t.Fatalf("plan %+v", plan)
		}
	})
	t.Run("reset unplaces through the agent, keeps what was skipped, forgets the machine and wipes rein's folders", func(t *testing.T) {
		fixture := newHarness(t)
		orgClonePath := filepath.Join(fixture.home, "orgs", "id-1")
		_ = os.MkdirAll(orgClonePath, 0o755)
		for _, folder := range []string{"imports", "snapshots", "proposals"} {
			_ = os.MkdirAll(filepath.Join(fixture.home, folder), 0o755)
		}
		_ = os.WriteFile(filepath.Join(fixture.home, "removals.yaml"), []byte("version: 1\n"), 0o644)
		fixture.state.Current.Placements = []machine.Placement{{Path: "/a"}, {Path: "/foreign"}}
		fixture.state.Current.Organizations = []machine.OrganizationRef{{ID: "id-1", Slug: "acme"}}
		fixture.state.Current.OrgClonePaths = map[string]string{"id-1": orgClonePath}
		fixture.state.Current.Active = "acme"
		fixture.state.Current.Imports = []machine.Import{{}}
		plan, _ := fixture.orchestrator().PlanReset()
		result, err := fixture.orchestrator().ResetMachine(plan)
		if err != nil || result.Kept != 1 || len(result.RemovedOrgClones) != 1 || len(fixture.agent.Unplaced) != 2 || len(result.Wiped) != 5 {
			t.Fatalf("result %+v err %v unplaced %v", result, err, fixture.agent.Unplaced)
		}
		state := fixture.state.Current
		if len(state.Placements) != 1 || state.Placements[0].Path != "/foreign" || state.Active != "" || len(state.Organizations) != 0 || len(state.ProjectClones) != 0 || len(state.Imports) != 0 {
			t.Fatalf("state %+v", state)
		}
		for _, gone := range []string{orgClonePath, filepath.Join(fixture.home, "imports"), filepath.Join(fixture.home, "snapshots"), filepath.Join(fixture.home, "proposals"), filepath.Join(fixture.home, "removals.yaml")} {
			_, err = os.Stat(gone)
			if err == nil {
				t.Fatalf("%s still there", gone)
			}
		}
	})
	t.Run("reset puts an initialised project back first, skips a gone folder, and stops when a restore fails", func(t *testing.T) {
		fixture, _ := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		folder := projectClone(t)
		_, err := fixture.orchestrator().InitializeProject(InitializeProjectRequest{Directory: folder, Project: "shop"})
		if err != nil {
			t.Fatalf("init: %v", err)
		}
		fixture.state.Current.ProjectClones = append(fixture.state.Current.ProjectClones, machine.ProjectClone{Organization: "x", Address: "organizations.acme.projects.gone", ClonePath: filepath.Join(t.TempDir(), "gone")})
		plan, _ := fixture.orchestrator().PlanReset()
		result, err := fixture.orchestrator().ResetMachine(plan)
		if err != nil || len(result.ProjectClones) != 2 || result.ProjectClones[0].Restore.Commit == "" || result.ProjectClones[1].Skipped == "" || result.Stopped {
			t.Fatalf("result %+v err %v", result, err)
		}
		content, err := os.ReadFile(filepath.Join(folder, "INSTRUCTIONS.md"))
		if err != nil || string(content) != "be brief" {
			t.Fatalf("instructions %q err %v", content, err)
		}
		_, err = os.Stat(filepath.Join(fixture.home, "removals.yaml"))
		if err == nil || len(fixture.state.Current.ProjectClones) != 0 || len(fixture.state.Current.Organizations) != 0 {
			t.Fatalf("ledger err %v state %+v", err, fixture.state.Current)
		}
		// a restore that fails stops the reset before the wipe
		fixture, _ = withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		folder = projectClone(t)
		_, _ = fixture.orchestrator().InitializeProject(InitializeProjectRequest{Directory: folder, Project: "shop"})
		_ = os.WriteFile(filepath.Join(folder, "INSTRUCTIONS.md"), []byte("new work"), 0o644)
		plan, _ = fixture.orchestrator().PlanReset()
		result, err = fixture.orchestrator().ResetMachine(plan)
		if !errors.Is(err, refusal.ErrRefused) || !result.Stopped || !result.ProjectClones[0].Failed || len(fixture.state.Current.Organizations) != 1 {
			t.Fatalf("result %+v err %v", result, err)
		}
		_, err = os.Stat(filepath.Join(fixture.home, "removals.yaml"))
		if err != nil {
			t.Fatalf("the ledger was wiped although a restore failed")
		}
	})
}

func TestRestoreProject(t *testing.T) {
	t.Run("puts the files back, removes the block, marks the ledger, unregisters and commits the tracked ones", func(t *testing.T) {
		fixture, _ := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		folder := projectClone(t)
		_, err := fixture.orchestrator().InitializeProject(InitializeProjectRequest{Directory: folder, Project: "shop"})
		if err != nil {
			t.Fatalf("init: %v", err)
		}
		result, err := fixture.orchestrator().RestoreProject(RestoreRequest{Directory: folder})
		if err != nil || result.Address != "organizations.acme.projects.shop" || strings.Join(result.Restored, ",") != ".fake-agent/local.md,.fake-agent/rules.md,INSTRUCTIONS.md" || !result.IgnoreRemoved || !result.Unregistered || result.Commit == "" {
			t.Fatalf("result %+v err %v", result, err)
		}
		for relative, content := range map[string]string{"INSTRUCTIONS.md": "be brief", ".fake-agent/rules.md": "rule", ".fake-agent/local.md": "mine"} {
			restored, err := os.ReadFile(filepath.Join(folder, filepath.FromSlash(relative)))
			if err != nil || string(restored) != content {
				t.Fatalf("%s: %q err %v", relative, restored, err)
			}
		}
		_, err = os.Stat(filepath.Join(folder, ".gitignore"))
		if err == nil {
			t.Fatalf(".gitignore still holds the block")
		}
		staged := strings.Join(fixture.repository.Staged, ",")
		commits := fixture.repository.Commits
		if commits[len(commits)-1] != restoreCommitMessage || staged != "" {
			t.Fatalf("commits %v staged %q", commits, staged)
		}
		removals, _ := fixture.orchestrator().Ledger.Load()
		for _, removal := range removals.Agents["fake-agent"] {
			if removal.RestoredAt.IsZero() {
				t.Fatalf("not marked restored: %+v", removal)
			}
		}
		pending, _ := fixture.orchestrator().Ledger.Pending("organizations.acme.projects.shop")
		if len(pending) != 0 || len(fixture.state.Current.ProjectClones) != 0 {
			t.Fatalf("pending %v projects %v", pending, fixture.state.Current.ProjectClones)
		}
		_, err = fixture.orchestrator().RestoreProject(RestoreRequest{Directory: folder})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "not initialised") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("a file in the way stops the put-back unless forced, and a failure after it undoes everything", func(t *testing.T) {
		fixture, _ := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		folder := projectClone(t)
		_, _ = fixture.orchestrator().InitializeProject(InitializeProjectRequest{Directory: folder, Project: "shop"})
		_ = os.WriteFile(filepath.Join(folder, "INSTRUCTIONS.md"), []byte("new work"), 0o644)
		_, err := fixture.orchestrator().RestoreProject(RestoreRequest{Directory: folder})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "already holds INSTRUCTIONS.md") {
			t.Fatalf("err %v", err)
		}
		fixture.state.FailSaves = 1
		_, err = fixture.orchestrator().RestoreProject(RestoreRequest{Directory: folder, Force: true})
		if err == nil || errors.Is(err, refusal.ErrRefused) {
			t.Fatalf("err %v", err)
		}
		content, _ := os.ReadFile(filepath.Join(folder, "INSTRUCTIONS.md"))
		pending, _ := fixture.orchestrator().Ledger.Pending("organizations.acme.projects.shop")
		if string(content) != "" || len(pending) != 2 || !fixture.orchestrator().WorkingCopy.HasIgnoreBlock(folder) || len(fixture.state.Current.ProjectClones) != 1 {
			t.Fatalf("content %q pending %d projects %d", content, len(pending), len(fixture.state.Current.ProjectClones))
		}
		result, err := fixture.orchestrator().RestoreProject(RestoreRequest{Directory: folder, Force: true, NoCommit: true})
		if err != nil || result.Commit != "" || len(result.Restored) != 3 {
			t.Fatalf("result %+v err %v", result, err)
		}
	})
}

func TestCreateOrganization(t *testing.T) {
	request := CreateOrganizationRequest{Name: "Acme", Description: "Agent settings of Acme", Owner: "Danidani23", RepositoryName: "acme-agent-settings", Private: true}
	t.Run("creates, publishes, protects and connects; the scaffold folder is gone afterwards", func(t *testing.T) {
		fixture := newHarness(t)
		_ = os.WriteFile(filepath.Join(fixture.agent.HomePath, "INSTRUCTIONS.md"), []byte("be brief"), 0o644)
		result, err := fixture.orchestrator().CreateOrganization(request)
		if err != nil || result.Remote != "https://github.com/Danidani23/acme-agent-settings.git" || result.ProtectionRefused != "" || result.Connected.Slug != "acme" || !result.Connected.BecameActive || result.Login != "Danidani23" {
			t.Fatalf("result %+v err %v", result, err)
		}
		connected := fixture.state.Current.Organizations[0]
		if connected.Login != "Danidani23" || connected.Owner != "Danidani23" || connected.Repository != "acme-agent-settings" {
			t.Fatalf("organisation %+v", connected)
		}
		if len(fixture.host.Created) != 1 || !fixture.host.Created[0].Private || fixture.host.Created[0].Description != "Agent settings of Acme" || len(fixture.host.ProtectedBranches) != 1 {
			t.Fatalf("host %+v", fixture.host)
		}
		for _, relative := range []string{"CODEOWNERS", "README.md", filepath.Join("declarations", "organization.yaml"), filepath.Join("declarations", "labels", "language.yaml")} {
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
	t.Run("a failed publish leaves the empty repository, which a rerun adopts; nothing is deleted (D26)", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.repository.FailPublish = true
		_, err := fixture.orchestrator().CreateOrganization(request)
		if err == nil || errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "remote rejected") {
			t.Fatalf("err %v", err)
		}
		if !fixture.host.Repositories["Danidani23/acme-agent-settings"].Exists || len(fixture.orgCloneFolders(t)) != 0 || len(fixture.state.Current.Organizations) != 0 {
			t.Fatalf("repositories %v folders %v state %+v", fixture.host.Repositories, fixture.orgCloneFolders(t), fixture.state.Current)
		}
		fixture.repository.FailPublish = false
		result, err := fixture.orchestrator().CreateOrganization(request)
		if err != nil || !result.Adopted || len(fixture.host.Created) != 1 || result.Connected.Slug != "acme" {
			t.Fatalf("rerun %+v err %v created %d", result, err, len(fixture.host.Created))
		}
		fixture.host.Repositories["Danidani23/other"] = ports.RepositoryInfo{Exists: true, Empty: false}
		_, err = fixture.orchestrator().CreateOrganization(CreateOrganizationRequest{Name: "Other", Owner: "Danidani23", RepositoryName: "other", Private: true})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "holds commits") {
			t.Fatalf("err %v", err)
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
	t.Run("a name without a letter or digit, a failed create and a missing login are refused or failed before anything exists", func(t *testing.T) {
		fixture := newHarness(t)
		_, err := fixture.orchestrator().CreateOrganization(CreateOrganizationRequest{Name: "!!!"})
		if !errors.Is(err, refusal.ErrRefused) {
			t.Fatalf("err %v", err)
		}
		fixture.host.FailCreate = true
		_, err = fixture.orchestrator().CreateOrganization(request)
		if err == nil || errors.Is(err, refusal.ErrRefused) || len(fixture.orgCloneFolders(t)) != 0 {
			t.Fatalf("err %v folders %v", err, fixture.orgCloneFolders(t))
		}
		fixture.host.FailCreate = false
		_, err = fixture.orchestrator().CreateOrganization(CreateOrganizationRequest{Name: "Acme", Login: "nobody", RepositoryName: "x"})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "nobody is not logged in") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("creating an organisation that is already connected is refused; the new repository stays for a rerun", func(t *testing.T) {
		fixture := newHarness(t)
		_, _ = fixture.orchestrator().ConnectOrganization("x", "")
		_, err := fixture.orchestrator().CreateOrganization(request)
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "already connected") || !fixture.host.Repositories["Danidani23/acme-agent-settings"].Exists || len(fixture.orgCloneFolders(t)) != 1 {
			t.Fatalf("err %v repositories %v folders %v", err, fixture.host.Repositories, fixture.orgCloneFolders(t))
		}
	})
}

// withOrganization is a fixture with an organisation created and active.
func withOrganization(t *testing.T) (*harness, CreateResult) {
	t.Helper()
	fixture := newHarness(t)
	created, err := fixture.orchestrator().CreateOrganization(CreateOrganizationRequest{Name: "Acme", Owner: "Danidani23", RepositoryName: "acme-agent-settings", Private: true})
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}
	return fixture, created
}

func TestCreateTeam(t *testing.T) {
	t.Run("writes the record, commits and pushes it; the remote has it", func(t *testing.T) {
		fixture, created := withOrganization(t)
		result, err := fixture.orchestrator().CreateTeam(CreateTeamRequest{Name: "Web Team", Description: "the web"})
		if err != nil || result.Slug != "web-team" || result.Organization != "acme" || result.RelativePath != "declarations/teams/web-team/team.yaml" {
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
		_, err = os.Stat(filepath.Join(created.Connected.OrgClonePath, "declarations", "teams"))
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
		if err != nil || result.Slug != "shop" || result.RelativePath != "declarations/projects/shop/project.yaml" {
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
		_, err := os.Stat(filepath.Join(created.Connected.OrgClonePath, "declarations", "projects"))
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
		projects, _ := fixture.orchestrator().Organizations.ProjectClones()
		if len(projects) != 1 || projects[0].ClonePath != folder || projects[0].Fingerprint != rootCommit || projects[0].InitializedAt.IsZero() || projects[0].Organization != fixture.state.Current.Organizations[0].ID {
			t.Fatalf("projects %+v", projects)
		}
		if result.Commit == "" || result.CommitError != "" || fixture.repository.Commits[len(fixture.repository.Commits)-1] != moveOutCommitMessage {
			t.Fatalf("commit %q error %q commits %v", result.Commit, result.CommitError, fixture.repository.Commits)
		}
		record, _ := os.ReadFile(filepath.Join(fixture.repository.Published[created.Remote], "declarations", "projects", "shop", "project.yaml"))
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
		record, _ := os.ReadFile(filepath.Join(fixture.repository.Published[created.Remote], "declarations", "projects", "shop", "project.yaml"))
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
		if err == nil || len(snapshots) != 0 || len(removals.Agents) != 0 || len(fixture.state.Current.ProjectClones) != 0 {
			t.Fatalf("ignore err %v snapshots %v removals %+v projects %v", err, snapshots, removals, fixture.state.Current.ProjectClones)
		}
	})
	t.Run("a failed commit is reported, the rest stands", func(t *testing.T) {
		fixture, _ := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		folder := projectClone(t)
		fixture.repository.FailCommit = true
		result, err := fixture.orchestrator().InitializeProject(InitializeProjectRequest{Directory: folder, Project: "shop"})
		if err != nil || result.Commit != "" || !strings.Contains(result.CommitError, "gpg") || len(fixture.state.Current.ProjectClones) != 1 {
			t.Fatalf("result %+v err %v", result, err)
		}
	})
}

// claudeCodeSnapshot stores a snapshot of a Claude Code harness for the
// project address, as init would have taken it.
func claudeCodeSnapshot(t *testing.T, fixture *harness, project string, folder string) string {
	t.Helper()
	if folder == "" {
		folder = t.TempDir()
	}
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
	t.Run("an initialised project's latest snapshot becomes one pending proposal in the clone, with verdicts, and the rein folder is ignored", func(t *testing.T) {
		fixture, created := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		folder := projectClone(t)
		_, err := fixture.orchestrator().InitializeProject(InitializeProjectRequest{Directory: folder, Project: "shop"})
		if err != nil {
			t.Fatalf("init: %v", err)
		}
		snapshotPath := claudeCodeSnapshot(t, fixture, "organizations.acme.projects.shop", folder)
		existing := filepath.Join(created.Connected.OrgClonePath, "declarations", "rules", "go.md")
		_ = os.MkdirAll(filepath.Dir(existing), 0o755)
		_ = os.WriteFile(existing, []byte("Two-line errors.\n"), 0o644)
		before, _ := fixture.orchestrator().Status()
		if len(before.Clones) != 1 || before.Clones[0].Snapshot != "snapshot not distilled yet; rein distill" {
			t.Fatalf("status before distill %+v", before.Clones)
		}
		result, err := fixture.orchestrator().DistillHarness(DistillRequest{Judge: "none", Directory: folder})
		if err != nil || result.Level != "organizations.acme.projects.shop" || len(result.Proposal.Candidates) != 3 || len(result.Proposal.Skipped) != 1 || result.Proposal.Snapshot.Path != snapshotPath || len(result.Proposal.ID) != 12 || result.Proposal.Status != proposal.StatusPending {
			t.Fatalf("result %+v err %v", result, err)
		}
		verdicts := map[string]string{}
		for _, candidate := range result.Proposal.Candidates {
			verdicts[candidate.Kind+"/"+candidate.ID] = candidate.Compared.Verdict
			if !candidate.Include || candidate.Level != "organizations.acme.projects.shop" || candidate.Judge != nil {
				t.Fatalf("unjudged candidate %+v", candidate)
			}
		}
		if verdicts["rules/go"] != "identical" || verdicts["rules/instructions"] != "new" || verdicts["mcpservers/docs"] != "new" {
			t.Fatalf("verdicts %v", verdicts)
		}
		if result.File != filepath.Join(folder, ".rein", "proposals", "proposal-"+result.Proposal.ID) {
			t.Fatalf("file %q", result.File)
		}
		content, _ := os.ReadFile(filepath.Join(result.File, "proposal.yaml"))
		instructions, _ := os.ReadFile(filepath.Join(result.File, "rule_instructions.yaml"))
		goRule, _ := os.ReadFile(filepath.Join(result.File, "rule_go.yaml"))
		if !strings.Contains(string(content), "distilled_from: organizations.acme.projects.shop") || !strings.Contains(string(goRule), "verdict: identical") || !strings.Contains(string(instructions), "Be brief.") {
			t.Fatalf("proposal %q instructions %q go %q", content, instructions, goRule)
		}
		ignore, _ := os.ReadFile(filepath.Join(folder, ".gitignore"))
		if !strings.Contains(string(ignore), "\n.rein/\n") {
			t.Fatalf("gitignore %q", ignore)
		}
		_, err = fixture.orchestrator().DistillHarness(DistillRequest{Judge: "none", Directory: folder})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "is pending at "+result.File) {
			t.Fatalf("second distill: %v", err)
		}
		again, err := fixture.orchestrator().DistillHarness(DistillRequest{Judge: "none", Directory: folder, Force: true})
		if err != nil || again.File != result.File {
			t.Fatalf("forced: %+v err %v", again, err)
		}
		pending, err := fixture.orchestrator().PendingProposals(folder)
		if err != nil || len(pending) != 1 || pending[0].Path != result.File || pending[0].Candidates != 3 {
			t.Fatalf("pending %+v err %v", pending, err)
		}
		report, err := fixture.orchestrator().Status()
		if err != nil || len(report.Pending) != 1 || report.Pending[0].Path != result.File || report.Pending[0].Level != "organizations.acme.projects.shop" || report.Clones[0].Snapshot != "snapshot distilled, proposal pending at "+result.File {
			t.Fatalf("status pending %+v clones %+v err %v", report.Pending, report.Clones, err)
		}
	})
	t.Run("the home becomes the user's level under the home's pending folder, a named snapshot its own, and the refusals", func(t *testing.T) {
		fixture, _ := withTeam(t)
		snapshotPath := claudeCodeSnapshot(t, fixture, "", "")
		result, err := fixture.orchestrator().DistillHarness(DistillRequest{Judge: "none", Home: true})
		if err != nil || result.Level != "organizations.acme.users.danidani23" || len(result.Proposal.Candidates) != 3 || filepath.Dir(result.File) != filepath.Join(fixture.home, "proposals", "pending") {
			t.Fatalf("result %+v err %v", result, err)
		}
		result, err = fixture.orchestrator().DistillHarness(DistillRequest{Judge: "none", SnapshotPath: snapshotPath, Force: true})
		if err != nil || result.Level != "organizations.acme.users.danidani23" {
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
	t.Run("a proposal file goes where it says, as a pull request, is marked proposed and moved to the history, and the clone ends on main", func(t *testing.T) {
		fixture, created := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		_ = claudeCodeSnapshot(t, fixture, "organizations.acme.projects.shop", "")
		existing := filepath.Join(created.Connected.OrgClonePath, "declarations", "rules", "go.md")
		_ = os.MkdirAll(filepath.Dir(existing), 0o755)
		_ = os.WriteFile(existing, []byte("Two-line errors.\n"), 0o644)
		distilled, err := fixture.orchestrator().DistillHarness(DistillRequest{Judge: "none", Project: "shop"})
		if err != nil {
			t.Fatalf("distill: %v", err)
		}
		result, err := fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: distilled.File})
		if err != nil || result.Organization != "acme" || len(result.Included) != 2 || len(result.LeftOut) != 1 || !strings.HasPrefix(result.Branch, "rein/propose-shop-") || result.PullRequestURL != "https://github.com/Danidani23/acme-agent-settings/pull/1" {
			t.Fatalf("result %+v err %v", result, err)
		}
		if result.LeftOut[0].ID != "go" || result.LeftOut[0].Related[0] != "organizations.acme.rules.go" || result.Included[0].Level != "organizations.acme.projects.shop" {
			t.Fatalf("left out %+v included %+v", result.LeftOut, result.Included)
		}
		pullRequest := fixture.host.PullRequests[0]
		if pullRequest.Branch != result.Branch || !strings.Contains(pullRequest.Title, "Propose 2 declarations to organizations.acme.projects.shop") || !strings.Contains(pullRequest.Body, "| rules | instructions | organizations.acme.projects.shop |  | new |") || !strings.Contains(pullRequest.Body, "Left out, identical") {
			t.Fatalf("pull request %+v", pullRequest)
		}
		published := fixture.repository.Published[created.Remote+"#"+result.Branch]
		for _, relative := range []string{"declarations/projects/shop/rules/instructions.md", "declarations/projects/shop/mcpservers/docs.yaml"} {
			_, err = os.Stat(filepath.Join(published, filepath.FromSlash(relative)))
			if err != nil {
				t.Fatalf("%s not on the branch: %v", relative, err)
			}
		}
		_, err = os.Stat(filepath.Join(published, "declarations", "projects", "shop", "rules", "go.md"))
		if err == nil {
			t.Fatalf("the identical rule was proposed")
		}
		if fixture.repository.Current != "main" || strings.Join(fixture.repository.Deleted, ",") != result.Branch {
			t.Fatalf("clone on %q deleted %v", fixture.repository.Current, fixture.repository.Deleted)
		}
		_, err = os.Stat(distilled.File)
		if err == nil || !strings.HasPrefix(result.HistoryFile, filepath.Join(fixture.home, "proposals", "acme")) {
			t.Fatalf("pending file still there, or history %q", result.HistoryFile)
		}
		history, err := fixture.orchestrator().ProposalFiles.History("acme")
		if err != nil || len(history) != 1 || history[0].Proposal.Status != proposal.StatusProposed || history[0].Proposal.PullRequest != result.PullRequestURL || history[0].Proposal.Branch != result.Branch || history[0].Proposal.ProposedAt.IsZero() {
			t.Fatalf("history %+v err %v", history, err)
		}
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Bound", Teams: []string{"web"}})
		clone := projectClone(t)
		_, _ = fixture.orchestrator().InitializeProject(InitializeProjectRequest{Directory: clone, Project: "bound"})
		_ = claudeCodeSnapshot(t, fixture, "organizations.acme.projects.bound", clone)
		boundProposal, _ := fixture.orchestrator().DistillHarness(DistillRequest{Judge: "none", Directory: clone})
		proposed, err := fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: boundProposal.File})
		if err != nil {
			t.Fatalf("propose bound: %v", err)
		}
		after, _ := fixture.orchestrator().Status()
		for _, status := range after.Clones {
			if status.ClonePath == clone && status.Snapshot != "snapshot distilled and proposed, "+proposed.PullRequestURL {
				t.Fatalf("status after propose %+v", status)
			}
		}
		_, err = fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: result.HistoryFile})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "was proposed on") {
			t.Fatalf("proposing the history: %v", err)
		}
	})
	t.Run("the judge's lifts and drops are written into the proposal, and the file is what propose follows after an edit", func(t *testing.T) {
		fixture, created := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		_ = claudeCodeSnapshot(t, fixture, "organizations.acme.projects.shop", "")
		existing := filepath.Join(created.Connected.OrgClonePath, "declarations", "mcpservers", "docs.yaml")
		_ = os.MkdirAll(filepath.Dir(existing), 0o755)
		_ = os.WriteFile(existing, []byte("description: the docs server\ntransport: http\nurl: https://docs.example/other\n"), 0o644)
		fixture.judge = &fake.Judge{Answers: []string{`{"version":2,"candidates":[` +
			`{"candidate":"rules/instructions","verdict":"new","suggestion":"lift","level":"organization","applyIf":["language:go","not a label"],"reason":"general"},` +
			`{"candidate":"rules/go","verdict":"new","suggestion":"lift","level":"team","reason":"the team's style"},` +
			`{"candidate":"mcpservers/docs","verdict":"duplicate","related":["organizations.acme.mcpservers.docs"],"suggestion":"drop","reason":"already declared"}]}`}}
		distilled, err := fixture.orchestrator().DistillHarness(DistillRequest{Project: "shop"})
		if err != nil {
			t.Fatalf("distill: %v", err)
		}
		if !strings.HasPrefix(fixture.judge.Briefs[0], "rein distill, the distiller's brief for organizations.acme.projects.shop, ") || !strings.Contains(fixture.judge.Briefs[0], "applyIf labels come from the organisation's registry and nowhere else: data:internal") {
			t.Fatalf("brief: %q", fixture.judge.Briefs[0][:120])
		}
		byKey := map[string]proposal.Candidate{}
		for _, candidate := range distilled.Proposal.Candidates {
			byKey[candidate.Kind+"/"+candidate.ID] = candidate
		}
		instructions, goRule, docs := byKey["rules/instructions"], byKey["rules/go"], byKey["mcpservers/docs"]
		if !instructions.Include || instructions.Level != "organizations.acme" || strings.Join(instructions.ApplyIf, ",") != "language:go" || instructions.Judge.Level != "organizations.acme" || instructions.Judge.Suggestion != "lift" {
			t.Fatalf("instructions %+v", instructions)
		}
		if !goRule.Include || goRule.Level != "organizations.acme.teams.web" || docs.Include || docs.Judge.Suggestion != "drop" || distilled.Proposal.Judge.Session != "fake-session" {
			t.Fatalf("go %+v docs %+v judge %+v", goRule, docs, distilled.Proposal.Judge)
		}
		plan, err := fixture.orchestrator().PlanProposal(ProposeRequest{Source: distilled.File})
		if err != nil || len(plan.Included) != 2 || len(plan.Excluded) != 1 || len(plan.LeftOut) != 0 || len(fixture.repository.Branches) != 0 {
			t.Fatalf("plan %+v err %v branches %v", plan, err, fixture.repository.Branches)
		}
		if plan.Included[0].Level != "organizations.acme" || plan.Included[0].ID != "instructions" || plan.Included[1].Level != "organizations.acme.teams.web" || plan.Excluded[0].ID != "docs" || !strings.Contains(plan.Excluded[0].Judge, "drop: already declared") {
			t.Fatalf("placed %+v excluded %+v", plan.Included, plan.Excluded)
		}
		// the person edits proposal.yaml: the team rule moves to the project, the docs server is included after all
		edited, _ := proposal.Load(distilled.File)
		for index := range edited.Candidates {
			switch edited.Candidates[index].ID {
			case "go":
				edited.Candidates[index].Level = "organizations.acme.projects.shop"
			case "docs":
				edited.Candidates[index].Include = true
			}
		}
		_ = edited.WriteFolder(distilled.File)
		result, err := fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: distilled.File})
		if err != nil || len(result.Included) != 3 || !strings.Contains(fixture.host.PullRequests[0].Title, "Propose 3 declarations to 2 levels of acme") {
			t.Fatalf("result %+v err %v", result, err)
		}
		published := fixture.repository.Published[created.Remote+"#"+result.Branch]
		instructionsFile, err := os.ReadFile(filepath.Join(published, "declarations", "rules", "instructions.md"))
		if err != nil || !strings.HasPrefix(string(instructionsFile), "---\napplyIf:\n    - language:go\n---\n") || !strings.Contains(string(instructionsFile), "Be brief.") {
			t.Fatalf("instructions %q err %v", instructionsFile, err)
		}
		for _, present := range []string{"projects/shop/rules/go.md", "projects/shop/mcpservers/docs.yaml"} {
			_, err = os.Stat(filepath.Join(published, "declarations", filepath.FromSlash(present)))
			if err != nil {
				t.Fatalf("%s is not on the branch: %v", present, err)
			}
		}
		_, err = os.Stat(filepath.Join(published, "declarations", "teams", "web", "rules", "go.md"))
		if err == nil {
			t.Fatalf("the edited level was ignored")
		}
		body := fixture.host.PullRequests[0].Body
		if !strings.Contains(body, "| rules | instructions | organizations.acme | language:go | new | lift: general |") || !strings.Contains(body, "Distiller: fake-judge, answered, session fake-session") {
			t.Fatalf("body %q", body)
		}
	})
	t.Run("a lift to the team level with several teams stays at the project with a note", func(t *testing.T) {
		fixture, _ := withTeam(t)
		_, _ = fixture.orchestrator().CreateTeam(CreateTeamRequest{Name: "Ops"})
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web", "ops"}})
		_ = claudeCodeSnapshot(t, fixture, "organizations.acme.projects.shop", "")
		fixture.judge = &fake.Judge{Answers: []string{`{"version":2,"candidates":[{"candidate":"rules/go","verdict":"new","suggestion":"lift","level":"team","reason":"style"},{"candidate":"rules/instructions","verdict":"new","suggestion":"keep","reason":"fine"},{"candidate":"mcpservers/docs","verdict":"new","suggestion":"keep","reason":"fine"}]}`}}
		distilled, err := fixture.orchestrator().DistillHarness(DistillRequest{Project: "shop"})
		if err != nil {
			t.Fatalf("distill: %v", err)
		}
		for _, candidate := range distilled.Proposal.Candidates {
			if candidate.ID == "go" && (candidate.Level != "organizations.acme.projects.shop" || len(candidate.Notes) != 1 || !strings.Contains(candidate.Notes[0], "belongs to the teams web, ops; set level to one of them")) {
				t.Fatalf("go %+v", candidate)
			}
		}
	})
	t.Run("a plain folder goes to the user's level with the user record, and a zip is read too", func(t *testing.T) {
		fixture, created := withTeam(t)
		folder := t.TempDir()
		_ = os.MkdirAll(filepath.Join(folder, "rules"), 0o755)
		_ = os.WriteFile(filepath.Join(folder, "rules", "brief.md"), []byte("Be brief.\n"), 0o644)
		result, err := fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: folder, Title: "My rules"})
		if err != nil || len(result.Included) != 1 || result.Included[0].Level != "organizations.acme.users.danidani23" || result.HistoryFile != "" || fixture.host.PullRequests[0].Title != "My rules" {
			t.Fatalf("result %+v err %v", result, err)
		}
		published := fixture.repository.Published[created.Remote+"#"+result.Branch]
		record, err := os.ReadFile(filepath.Join(published, "declarations", "users", "danidani23", "user.yaml"))
		if err != nil || !strings.Contains(string(record), "github: Danidani23") || !strings.Contains(string(record), "created_at:") {
			t.Fatalf("user record %q err %v", record, err)
		}
	})
	t.Run("refusals: no declarations, a bad level, a level of another organisation, a bad proposal, the lint, nothing new, no GitHub remote", func(t *testing.T) {
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
		broken := proposal.Proposal{Version: proposal.Version, Status: proposal.StatusPending, Organization: "acme", DistilledFrom: "organizations.acme", Candidates: []proposal.Candidate{{Kind: "rules", ID: "x", Include: true, Level: "nowhere", ApplyIf: []string{"stack:aws"}, Files: map[string]string{"x.md": "x"}}}}
		brokenPath := filepath.Join(t.TempDir(), "proposal-abc")
		_ = broken.WriteFolder(brokenPath)
		_, err = fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: brokenPath})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "cannot go as it is") || !strings.Contains(err.Error(), `level "nowhere" is not one`) || !strings.Contains(err.Error(), `label "stack:aws" names no category`) {
			t.Fatalf("broken: %v", err)
		}
		_, err = fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: brokenPath, Level: "organizations.acme"})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "instead of --level") {
			t.Fatalf("level with a file: %v", err)
		}
		bad := t.TempDir()
		_ = os.MkdirAll(filepath.Join(bad, "mcpservers"), 0o755)
		_ = os.WriteFile(filepath.Join(bad, "mcpservers", "x.yaml"), []byte("transport: carrier-pigeon\n"), 0o644)
		_, err = fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: bad, Level: "organizations.acme"})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "do not pass the lint") || !strings.Contains(err.Error(), "carrier-pigeon") {
			t.Fatalf("err %v", err)
		}
		existing := filepath.Join(created.Connected.OrgClonePath, "declarations", "rules", "brief.md")
		_ = os.MkdirAll(filepath.Dir(existing), 0o755)
		_ = os.WriteFile(existing, []byte("Be brief.\n"), 0o644)
		_, err = fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: folder, Level: "organizations.acme.teams.web"})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "nothing to propose") || len(fixture.repository.Branches) != 0 {
			t.Fatalf("err %v branches %v", err, fixture.repository.Branches)
		}
		local := newHarness(t)
		local.repository = &fake.Repository{Record: validRecord}
		_, _ = local.orchestrator().ConnectOrganization("/a/local/path", "")
		_, err = local.orchestrator().ProposeDeclarations(ProposeRequest{Source: folder, Level: "organizations.acme"})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "not on GitHub") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("a failed pull request abandons the branch locally and on the remote, and the proposal stays pending", func(t *testing.T) {
		fixture, created := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		_ = claudeCodeSnapshot(t, fixture, "organizations.acme.projects.shop", "")
		distilled, err := fixture.orchestrator().DistillHarness(DistillRequest{Judge: "none", Project: "shop"})
		if err != nil {
			t.Fatalf("distill: %v", err)
		}
		fixture.host.FailPullRequest = true
		_, err = fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: distilled.File})
		if err == nil || errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("err %v", err)
		}
		if len(fixture.repository.RemoteDeleted) != 1 || len(fixture.repository.Deleted) != 1 || fixture.repository.Current != "main" || len(fixture.repository.Published) != 1 {
			t.Fatalf("repository %+v remote %s", fixture.repository, created.Remote)
		}
		still, err := proposal.Load(distilled.File)
		if err != nil || still.Status != proposal.StatusPending {
			t.Fatalf("pending file: %+v err %v", still, err)
		}
	})
}

func TestDistillWithTheJudge(t *testing.T) {
	answer := `{"version":2,"candidates":[
	 {"candidate":"rules/instructions","verdict":"new","suggestion":"keep","reason":"nothing covers it"},
	 {"candidate":"rules/go","verdict":"duplicate","related":["organizations.acme.rules.go"],"quotes":[{"address":"organizations.acme.rules.go","text":"Two-line errors."}],"suggestion":"drop","reason":"the organisation says it"},
	 {"candidate":"mcpservers/docs","verdict":"new","suggestion":"lift","level":"organization","reason":"everyone reads the docs"}]}`
	prepare := func(t *testing.T) (*harness, CreateResult) {
		t.Helper()
		fixture, created := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		_ = claudeCodeSnapshot(t, fixture, "organizations.acme.projects.shop", "")
		existing := filepath.Join(created.Connected.OrgClonePath, "declarations", "rules", "go.md")
		_ = os.MkdirAll(filepath.Dir(existing), 0o755)
		_ = os.WriteFile(existing, []byte("Two-line errors.\n"), 0o644)
		return fixture, created
	}
	conflicting := `{"version":2,"candidates":[{"candidate":"mcpservers/docs","verdict":"new","suggestion":"keep","reason":"fine"},{"candidate":"rules/go","verdict":"duplicate","related":["organizations.acme.rules.go"],"suggestion":"drop","reason":"said"}],"units":[
	 {"id":"loose-errors","kind":"rules","from":[{"candidate":"rules/instructions","lines":"1, 3"}],"shape":"always","verdict":"contradicts","related":["organizations.acme.rules.go"],"quotes":[{"address":"organizations.acme.rules.go","text":"Two-line errors."}],"suggestion":"keep","reason":"the project wants it otherwise"}]}`
	resolve := func(t *testing.T, proposalPath string, resolution string) {
		t.Helper()
		edited, err := proposal.Load(proposalPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		for index := range edited.Candidates {
			if edited.Candidates[index].Conflict != nil {
				edited.Candidates[index].Conflict.Resolution = resolution
			}
		}
		_ = edited.WriteFolder(proposalPath)
	}
	t.Run("a distiller that fails writes nothing, so the next distill starts fresh", func(t *testing.T) {
		fixture, _ := prepare(t)
		fixture.judge = &fake.Judge{Answers: []string{"nonsense", "still nonsense"}}
		result, err := fixture.orchestrator().DistillHarness(DistillRequest{Project: "shop"})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "the distiller fake-judge failed, so nothing was written") || !strings.Contains(err.Error(), "holds no JSON object") || result.File != "" {
			t.Fatalf("result %+v err %v", result, err)
		}
		pending, _ := fixture.orchestrator().ProposalFiles.ListPending(fixture.orchestrator().ProposalFiles.HomePendingFolder())
		if len(pending) != 0 {
			t.Fatalf("a failed distillation left %d proposals", len(pending))
		}
	})
	t.Run("a conflict blocks propose until it is resolved, and an override names what it replaces", func(t *testing.T) {
		fixture, created := prepare(t)
		fixture.judge = &fake.Judge{Answers: []string{conflicting}}
		distilled, err := fixture.orchestrator().DistillHarness(DistillRequest{Project: "shop"})
		if err != nil {
			t.Fatalf("distill: %v", err)
		}
		var unit proposal.Candidate
		for _, candidate := range distilled.Proposal.Candidates {
			if candidate.ID == "loose-errors" {
				unit = candidate
			}
		}
		if unit.Include || unit.Conflict == nil || strings.Join(unit.Conflict.With, ",") != "organizations.acme.rules.go" || unit.Conflict.Resolution != "" {
			t.Fatalf("unit %+v conflict %+v", unit, unit.Conflict)
		}
		_, err = fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: distilled.File})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "rules/loose-errors conflicts with organizations.acme.rules.go and is not resolved") {
			t.Fatalf("unresolved: %v", err)
		}
		resolve(t, distilled.File, "override")
		result, err := fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: distilled.File})
		if err != nil {
			t.Fatalf("override: %v", err)
		}
		published := fixture.repository.Published[created.Remote+"#"+result.Branch]
		content, _ := os.ReadFile(filepath.Join(published, "declarations", "projects", "shop", "rules", "loose-errors.md"))
		if string(content) != "This replaces `organizations.acme.rules.go` where the two disagree.\n\n# Shop\n\nBe brief.\n" {
			t.Fatalf("override content %q", content)
		}
		if !strings.Contains(fixture.host.PullRequests[0].Body, "conflict with organizations.acme.rules.go, resolved override: the project wants it otherwise") {
			t.Fatalf("body %q", fixture.host.PullRequests[0].Body)
		}
	})
	t.Run("an override of a required declaration is refused", func(t *testing.T) {
		fixture, created := prepare(t)
		required := filepath.Join(created.Connected.OrgClonePath, "declarations", "rules", "go.md")
		_ = os.WriteFile(required, []byte("---\nenforcement: required\n---\nTwo-line errors.\n"), 0o644)
		fixture.judge = &fake.Judge{Answers: []string{conflicting}}
		distilled, err := fixture.orchestrator().DistillHarness(DistillRequest{Project: "shop"})
		if err != nil {
			t.Fatalf("distill: %v", err)
		}
		resolve(t, distilled.File, "override")
		_, err = fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: distilled.File})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "rules/loose-errors cannot override organizations.acme.rules.go, which is required") {
			t.Fatalf("required: %v", err)
		}
	})
	t.Run("units the distiller cut out replace their candidates and are compared like them", func(t *testing.T) {
		fixture, _ := prepare(t)
		units := `{"version":2,"candidates":[{"candidate":"mcpservers/docs","verdict":"new","suggestion":"keep","reason":"fine"}],"units":[
		 {"id":"shop-title","kind":"rules","from":[{"candidate":"rules/instructions","lines":"1"}],"shape":"always","verdict":"new","suggestion":"drop","reason":"a title"},
		 {"id":"brevity","kind":"rules","from":[{"candidate":"rules/instructions","lines":"3"}],"shape":"always","verdict":"new","suggestion":"keep","reason":"the project's own"},
		 {"id":"errors","kind":"rules","from":[{"candidate":"rules/go","lines":"1"}],"shape":"always","verdict":"duplicate","related":["organizations.acme.rules.go"],"suggestion":"drop","reason":"the organisation says it"}]}`
		fixture.judge = &fake.Judge{Answers: []string{units}}
		result, err := fixture.orchestrator().DistillHarness(DistillRequest{Project: "shop"})
		if err != nil || result.Proposal.Judge.Status != "answered" {
			t.Fatalf("judge %+v err %v", result.Proposal.Judge, err)
		}
		placed := map[string]proposal.Candidate{}
		for _, candidate := range result.Proposal.Candidates {
			placed[candidate.Kind+"/"+candidate.ID] = candidate
		}
		if _, found := placed["rules/instructions"]; found || placed["rules/brevity"].Files["brevity.md"] != "Be brief.\n" || placed["rules/brevity"].Judge == nil || !placed["rules/brevity"].Include || placed["rules/shop-title"].Include {
			t.Fatalf("placed %+v", placed)
		}
		if placed["rules/errors"].Compared.Verdict != "identical" {
			t.Fatalf("the unit was not compared: %+v", placed["rules/errors"].Compared)
		}
	})
	t.Run("the judge's answer lands in the proposal and the pull request", func(t *testing.T) {
		fixture, _ := prepare(t)
		fixture.judge = &fake.Judge{Answers: []string{answer}}
		result, err := fixture.orchestrator().DistillHarness(DistillRequest{Project: "shop"})
		if err != nil || result.Proposal.Judge.Status != "answered" || result.Proposal.Judge.Backend != "fake-judge" {
			t.Fatalf("result %+v err %v", result.Proposal.Judge, err)
		}
		if !strings.Contains(fixture.judge.Briefs[0], "## organizations.acme.rules.go (applies)") || !strings.Contains(fixture.judge.Briefs[0], "## rules/instructions") {
			t.Fatalf("brief %q", fixture.judge.Briefs[0])
		}
		judged := map[string]proposal.Candidate{}
		for _, candidate := range result.Proposal.Candidates {
			judged[candidate.Kind+"/"+candidate.ID] = candidate
		}
		if judged["rules/go"].Judge == nil || judged["rules/go"].Judge.Verdict != "duplicate" || judged["rules/go"].Include || judged["mcpservers/docs"].Level != "organizations.acme" || len(judged["rules/go"].Judge.Quotes) != 1 {
			t.Fatalf("judged %+v", judged)
		}
		content, _ := os.ReadFile(filepath.Join(result.File, "proposal.yaml"))
		if !strings.Contains(string(content), "status: answered") || !strings.Contains(string(content), "session: fake-session") {
			t.Fatalf("proposal %q", content)
		}
		proposed, err := fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: result.File})
		if err != nil || !strings.Contains(fixture.host.PullRequests[0].Body, "| the distiller |") || !strings.Contains(fixture.host.PullRequests[0].Body, "| mcpservers | docs | organizations.acme |  | new | lift: everyone reads the docs |") || !strings.Contains(fixture.host.PullRequests[0].Body, "Distiller: fake-judge, answered") || !strings.Contains(fixture.host.PullRequests[0].Body, "- rules/go: drop: the organisation says it") {
			t.Fatalf("proposed %+v err %v body %q", proposed, err, fixture.host.PullRequests[0].Body)
		}
	})
	t.Run("an unavailable judge refuses and writes nothing, none skips it, a dry run writes nothing", func(t *testing.T) {
		fixture, _ := prepare(t)
		result, err := fixture.orchestrator().DistillHarness(DistillRequest{Project: "shop"})
		if err == nil || !strings.Contains(err.Error(), "the distiller fake-judge is not available: no judge in this test") || !strings.Contains(err.Error(), "--distiller none") || result.File != "" {
			t.Fatalf("result %+v err %v", result.Proposal.Judge, err)
		}
		// nothing is pending after the refusal, so no --force is needed
		result, err = fixture.orchestrator().DistillHarness(DistillRequest{Project: "shop", Judge: "none"})
		if err != nil || result.Proposal.Judge.Status != "skipped" || result.Proposal.Judge.Problems[0] != "not asked" || result.Brief != "" {
			t.Fatalf("result %+v err %v", result.Proposal.Judge, err)
		}
		before, _ := os.ReadDir(filepath.Join(fixture.home, "proposals", "pending"))
		result, err = fixture.orchestrator().DistillHarness(DistillRequest{Project: "shop", DryRun: true})
		after, _ := os.ReadDir(filepath.Join(fixture.home, "proposals", "pending"))
		if err != nil || result.File != "" || !strings.Contains(result.Brief, "# Candidates") || len(after) != len(before) || len(result.Proposal.Candidates) != 3 {
			t.Fatalf("result file %q brief %d err %v", result.File, len(result.Brief), err)
		}
	})
}

func TestResolveProject(t *testing.T) {
	fixture, created := withTeam(t)
	_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}, Labels: []string{"language:go"}})
	write := func(relative string, content string) {
		target := filepath.Join(created.Connected.OrgClonePath, "declarations", filepath.FromSlash(relative))
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		_ = os.WriteFile(target, []byte(content), 0o644)
	}
	write("rules/style.md", "---\nenforcement: required\n---\nStyle.\n")
	write("rules/testing.md", "Org tests.\n")
	write("rules/py.md", "---\napplyIf: [language:python]\n---\nPython.\n")
	write("teams/web/rules/testing.md", "Web tests.\n")
	write("projects/shop/rules/testing.md", "Shop tests.\n")
	write("users/danidani23/rules/mine.md", "Mine.\n")
	resolution, err := fixture.orchestrator().ResolveProject("", "shop")
	if err != nil || resolution.Address != "organizations.acme.projects.shop" || resolution.User != "danidani23" || len(resolution.Result.Problems) != 0 {
		t.Fatalf("resolution %+v err %v", resolution, err)
	}
	if addresses := addressesOf(resolution.Result.Chosen); addresses != "organizations.acme.rules.style,organizations.acme.projects.shop.rules.testing,organizations.acme.users.danidani23.rules.mine" {
		t.Fatalf("chosen %s", addresses)
	}
	if strings.Join(resolution.Result.Chosen[1].Replaced, ",") != "organizations.acme.rules.testing,organizations.acme.teams.web.rules.testing" {
		t.Fatalf("replaced %v", resolution.Result.Chosen[1].Replaced)
	}
	_, err = fixture.orchestrator().ResolveProject("", "nope")
	if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err %v", err)
	}
}

func addressesOf(chosen []resolve.Chosen) string {
	var list []string
	for _, item := range chosen {
		list = append(list, item.Address)
	}
	return strings.Join(list, ",")
}

// claudeProjectClone is a fake repository with Claude Code's files, for a
// fake agent that reads the Claude Code format.
func claudeProjectClone(t *testing.T) string {
	t.Helper()
	folder, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	err = fake.NewProjectClone(folder, []string{rootCommit}, []string{"CLAUDE.md", "main.go"})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	_ = os.WriteFile(filepath.Join(folder, "CLAUDE.md"), []byte("old instructions"), 0o644)
	_ = os.WriteFile(filepath.Join(folder, "main.go"), []byte("package main"), 0o644)
	return folder
}

// withSyncableProject is an organisation with declarations, a project and
// an initialised clone of it, the agent reading the Claude Code format.
func withSyncableProject(t *testing.T) (*harness, CreateResult, string) {
	t.Helper()
	fixture, created := withTeam(t)
	fixture.agent.HarnessFormat = "claude-code"
	_, err := fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}, Labels: []string{"language:go"}})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	write := func(relative string, content string) {
		target := filepath.Join(created.Connected.OrgClonePath, "declarations", filepath.FromSlash(relative))
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		_ = os.WriteFile(target, []byte(content), 0o644)
	}
	write("rules/style.md", "---\nenforcement: required\n---\nHouse style.\n")
	write("rules/go.md", "---\npaths: [\"**/*.go\"]\napplyIf: [language:go]\n---\nTwo-line errors.\n")
	write("teams/web/skills/commit/SKILL.md", "---\nname: commit\ndescription: Draft commits.\n---\n")
	write("mcpservers/github.yaml", "transport: stdio\ncommand: npx\nenv:\n  GITHUB_PERSONAL_ACCESS_TOKEN: ${REIN_TEST_GITHUB_TOKEN}\nvariables:\n  REIN_TEST_GITHUB_TOKEN: a token\n")
	folder := claudeProjectClone(t)
	_, err = fixture.orchestrator().InitializeProject(InitializeProjectRequest{Directory: folder, Project: "shop"})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	return fixture, created, folder
}

func TestSyncClone(t *testing.T) {
	t.Run("writes the harness, records the inserts with hashes, and a second sync changes nothing", func(t *testing.T) {
		fixture, created, folder := withSyncableProject(t)
		result, err := fixture.orchestrator().SyncClone(SyncRequest{Directory: folder, ReinExecutable: "/bin/rein"})
		if err != nil || len(result.Clones) != 1 {
			t.Fatalf("result %+v err %v", result, err)
		}
		synced := result.Clones[0]
		if !synced.Created || synced.Agent != "fake-agent" || len(synced.Written) != 7 || len(synced.Unchanged) != 0 || strings.Join(synced.UnsetVariables, ",") != "REIN_TEST_GITHUB_TOKEN" || !strings.Contains(synced.AlwaysOn, "House style.") {
			t.Fatalf("synced %+v", synced)
		}
		for _, relative := range []string{"CLAUDE.md", ".claude/rein/organization.md", ".claude/rules/go.md", ".claude/skills/commit/SKILL.md", ".claude/settings.local.json", ".mcp.json"} {
			_, err = os.Stat(filepath.Join(folder, filepath.FromSlash(relative)))
			if err != nil {
				t.Fatalf("%s: %v", relative, err)
			}
		}
		claude, _ := os.ReadFile(filepath.Join(folder, "CLAUDE.md"))
		if !strings.Contains(string(claude), "@.claude/rein/organization.md") || strings.Contains(string(claude), "old instructions") {
			t.Fatalf("CLAUDE.md %q", claude)
		}
		inserts, _ := fixture.orchestrator().Ledger.InsertsOf(created.Connected.OrgClonePath[:0]+fixture.state.Current.Organizations[0].ID, folder)
		if len(inserts) != 7 || inserts[0].SHA256 == "" {
			t.Fatalf("inserts %+v", inserts)
		}
		clones, _ := fixture.orchestrator().Organizations.ProjectClones()
		if clones[0].LastSync == "" {
			t.Fatalf("last sync not stamped: %+v", clones[0])
		}
		result, err = fixture.orchestrator().SyncClone(SyncRequest{Directory: folder, ReinExecutable: "/bin/rein"})
		if err != nil || result.Clones[0].Created || len(result.Clones[0].Written) != 0 || len(result.Clones[0].Unchanged) != 7 {
			t.Fatalf("second sync %+v err %v", result.Clones[0], err)
		}
		report, _ := fixture.orchestrator().Status()
		if len(report.Clones) != 1 || report.Clones[0].Inserts != 7 || report.Clones[0].Modified != 0 {
			t.Fatalf("status %+v", report.Clones)
		}
	})
	t.Run("names a rule pointing at a document the clone lacks", func(t *testing.T) {
		fixture, created, folder := withSyncableProject(t)
		pointer := filepath.Join(created.Connected.OrgClonePath, "declarations", "rules", "overview.md")
		_ = os.WriteFile(pointer, []byte("---\ndocument: docs/overview.md\n---\nRead docs/overview.md before changing the shop.\n"), 0o644)
		result, err := fixture.orchestrator().SyncClone(SyncRequest{Directory: folder})
		if err != nil || !strings.Contains(strings.Join(result.Clones[0].Notes, "\n"), "organizations.acme.rules.overview points at docs/overview.md, which this clone lacks") {
			t.Fatalf("result %+v err %v", result.Clones, err)
		}
		_ = os.MkdirAll(filepath.Join(folder, "docs"), 0o755)
		_ = os.WriteFile(filepath.Join(folder, "docs", "overview.md"), []byte("# Overview\n"), 0o644)
		result, _ = fixture.orchestrator().SyncClone(SyncRequest{Directory: folder})
		if strings.Contains(strings.Join(result.Clones[0].Notes, "\n"), "points at") {
			t.Fatalf("the document is there: %v", result.Clones[0].Notes)
		}
	})
	t.Run("names a file under its folders that it did not write, leaves it, and snapshots it with force", func(t *testing.T) {
		fixture, _, folder := withSyncableProject(t)
		_, _ = fixture.orchestrator().SyncClone(SyncRequest{Directory: folder})
		handMade := filepath.Join(folder, ".claude", "skills", "mine", "SKILL.md")
		_ = os.MkdirAll(filepath.Dir(handMade), 0o755)
		_ = os.WriteFile(handMade, []byte("---\nname: mine\n---\n"), 0o644)
		result, err := fixture.orchestrator().SyncClone(SyncRequest{Directory: folder})
		if err != nil || strings.Join(result.Clones[0].Foreign, ",") != ".claude/skills/mine/SKILL.md" || result.Clones[0].SnapshotPath != "" {
			t.Fatalf("result %+v err %v", result.Clones, err)
		}
		_, err = os.Stat(handMade)
		if err != nil {
			t.Fatalf("the hand-made file was removed: %v", err)
		}
		result, err = fixture.orchestrator().SyncClone(SyncRequest{Directory: folder, Force: true})
		if err != nil || result.Clones[0].SnapshotPath == "" {
			t.Fatalf("forced %+v err %v", result.Clones, err)
		}
		snapshot, _ := os.ReadFile(result.Clones[0].SnapshotPath)
		if !strings.Contains(string(snapshot), "name: mine") {
			t.Fatalf("the snapshot lacks the hand-made skill: %s", snapshot)
		}
	})
	t.Run("a hand edit stops the sync, --force snapshots it, and a stale file goes", func(t *testing.T) {
		fixture, created, folder := withSyncableProject(t)
		_, _ = fixture.orchestrator().SyncClone(SyncRequest{Directory: folder})
		_ = os.WriteFile(filepath.Join(folder, "CLAUDE.md"), []byte("my own words"), 0o644)
		_, err := fixture.orchestrator().SyncClone(SyncRequest{Directory: folder})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "edited by hand since the last sync: CLAUDE.md") {
			t.Fatalf("err %v", err)
		}
		report, _ := fixture.orchestrator().Status()
		if report.Clones[0].Modified != 1 {
			t.Fatalf("status %+v", report.Clones)
		}
		_ = os.Remove(filepath.Join(created.Connected.OrgClonePath, "declarations", "rules", "go.md"))
		result, err := fixture.orchestrator().SyncClone(SyncRequest{Directory: folder, Force: true})
		if err != nil || result.Clones[0].SnapshotPath == "" || strings.Join(result.Clones[0].Modified, ",") != "CLAUDE.md" || strings.Join(result.Clones[0].Removed, ",") != ".claude/rules/go.md" {
			t.Fatalf("forced %+v err %v", result.Clones[0], err)
		}
		snapshot, _ := fixture.orchestrator().Snapshots.Read(result.Clones[0].SnapshotPath)
		if snapshot.Files["CLAUDE.md"] != "my own words" {
			t.Fatalf("snapshot %+v", snapshot.Files)
		}
		_, err = os.Stat(filepath.Join(folder, ".claude", "rules"))
		if err == nil {
			t.Fatalf("the stale rules folder stayed")
		}
	})
	t.Run("a resolution problem refuses before writing; --all reports a gone folder and goes on", func(t *testing.T) {
		fixture, created, folder := withSyncableProject(t)
		bad := filepath.Join(created.Connected.OrgClonePath, "declarations", "teams", "web", "rules", "style.md")
		_ = os.MkdirAll(filepath.Dir(bad), 0o755)
		_ = os.WriteFile(bad, []byte("Ours.\n"), 0o644)
		_, err := fixture.orchestrator().SyncClone(SyncRequest{Directory: folder})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "does not resolve") || !strings.Contains(err.Error(), "redeclares") {
			t.Fatalf("err %v", err)
		}
		_, err = os.Stat(filepath.Join(folder, ".claude", "settings.local.json"))
		if err == nil {
			t.Fatalf("files were written despite the problem")
		}
		_ = os.Remove(bad)
		fixture.state.Current.ProjectClones = append(fixture.state.Current.ProjectClones, machine.ProjectClone{Organization: fixture.state.Current.Organizations[0].ID, Address: "organizations.acme.projects.gone", ClonePath: filepath.Join(t.TempDir(), "gone")})
		result, err := fixture.orchestrator().SyncClone(SyncRequest{All: true})
		if !errors.Is(err, refusal.ErrRefused) || len(result.Clones) != 2 || result.Clones[0].Skipped != "" || !strings.Contains(result.Clones[1].Skipped, "is gone") {
			t.Fatalf("result %+v err %v", result, err)
		}
		_, err = fixture.orchestrator().SyncClone(SyncRequest{Directory: claudeProjectClone(t)})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "not initialised") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("restore removes what sync wrote, keeps a hand-edited file, and puts the original back", func(t *testing.T) {
		fixture, _, folder := withSyncableProject(t)
		_, _ = fixture.orchestrator().SyncClone(SyncRequest{Directory: folder})
		_ = os.WriteFile(filepath.Join(folder, ".mcp.json"), []byte("{}"), 0o644)
		result, err := fixture.orchestrator().RestoreProject(RestoreRequest{Directory: folder})
		if err != nil || len(result.InsertsRemoved) != 6 || strings.Join(result.InsertsKept, ",") != ".mcp.json" || strings.Join(result.Restored, ",") != "CLAUDE.md" {
			t.Fatalf("result %+v err %v", result, err)
		}
		claude, _ := os.ReadFile(filepath.Join(folder, "CLAUDE.md"))
		_, reinErr := os.Stat(filepath.Join(folder, ".claude", "rein"))
		_, mcpErr := os.Stat(filepath.Join(folder, ".mcp.json"))
		if string(claude) != "old instructions" || reinErr == nil || mcpErr != nil {
			t.Fatalf("CLAUDE.md %q rein %v mcp %v", claude, reinErr, mcpErr)
		}
		plan, _ := fixture.orchestrator().PlanReset()
		_, err = fixture.orchestrator().ResetMachine(plan)
		_, insertsErr := os.Stat(filepath.Join(fixture.home, "inserts"))
		if err != nil || insertsErr == nil {
			t.Fatalf("reset err %v inserts %v", err, insertsErr)
		}
	})
}

func TestEditProject(t *testing.T) {
	fixture, created := withTeam(t)
	_, _ = fixture.orchestrator().CreateTeam(CreateTeamRequest{Name: "Ops"})
	_, err := fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Description: "a shop", Teams: []string{"web"}, Labels: []string{"kind:service"}, Fingerprint: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	result, err := fixture.orchestrator().EditProject(EditProjectRequest{Slug: "shop", Description: "the shop", Teams: []string{"web", "ops"}, Labels: []string{"kind:service", "language:go"}, Path: "services/shop"})
	if err != nil || result.Slug != "shop" || result.Name != "Shop" || strings.Join(result.Changed, ",") != "description,teams,labels,path" {
		t.Fatalf("result %+v err %v", result, err)
	}
	content, _ := os.ReadFile(filepath.Join(created.Connected.OrgClonePath, "declarations", "projects", "shop", "project.yaml"))
	record, problems := declaration.ParseProject("shop", content)
	if len(problems) != 0 || record.Description != "the shop" || strings.Join(record.Teams, ",") != "web,ops" || strings.Join(record.Labels, ",") != "kind:service,language:go" || record.Path != "services/shop" || strings.Join(record.Fingerprint, ",") != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || record.UpdatedAt.IsZero() || record.CreatedAt.IsZero() || record.ID == "" {
		t.Fatalf("record %+v problems %v", record, problems)
	}
	if !strings.Contains(fixture.repository.Commits[len(fixture.repository.Commits)-1], "project shop: description, teams, labels, path (rein project edit)") {
		t.Fatalf("commits %v", fixture.repository.Commits)
	}
	for expected, request := range map[string]EditProjectRequest{
		"does not exist in acme; projects are shop":     {Slug: "nope", Teams: []string{"web"}},
		`team "nowhere" does not exist`:                 {Slug: "shop", Teams: []string{"nowhere"}},
		`label "kind:spaceship" is not in the registry`: {Slug: "shop", Teams: []string{"web"}, Labels: []string{"kind:spaceship"}},
		"teams is required":                             {Slug: "shop"},
	} {
		_, err = fixture.orchestrator().EditProject(request)
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), expected) {
			t.Fatalf("%s: err %v", expected, err)
		}
	}
}

func TestEditLabelRegistry(t *testing.T) {
	fixture, created := withTeam(t)
	_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}, Labels: []string{"kind:service"}})
	registry := func(category string) declaration.LabelCategory {
		content, _ := os.ReadFile(filepath.Join(created.Connected.OrgClonePath, "declarations", "labels", category+".yaml"))
		parsed, _ := declaration.ParseLabelCategory(category, content)
		return parsed
	}
	result, err := fixture.orchestrator().EditLabelRegistry(LabelRegistryRequest{Operation: LabelAdd, Category: "kind", Value: "worker", Description: "a background worker"})
	if err != nil || result.CreatedCategory || result.Required != true || registry("kind").Labels["worker"] != "a background worker" {
		t.Fatalf("add: result %+v err %v registry %+v", result, err, registry("kind"))
	}
	yes, no := true, false
	result, err = fixture.orchestrator().EditLabelRegistry(LabelRegistryRequest{Operation: LabelAdd, Category: "stack", Value: "aws", Description: "runs on AWS", Required: &yes, Multiple: &yes})
	if err != nil || !result.CreatedCategory || !result.Required || !result.Multiple || result.RelativePath != "declarations/labels/stack.yaml" || !registry("stack").Required || !registry("stack").Multiple || registry("stack").Labels["aws"] != "runs on AWS" {
		t.Fatalf("create: result %+v err %v", result, err)
	}
	result, err = fixture.orchestrator().EditLabelRegistry(LabelRegistryRequest{Operation: LabelSet, Category: "stack", Required: &no})
	if err != nil || result.Required || !result.Multiple || registry("stack").Required || !registry("stack").Multiple {
		t.Fatalf("optional keeps multiple: result %+v err %v", result, err)
	}
	result, err = fixture.orchestrator().EditLabelRegistry(LabelRegistryRequest{Operation: LabelSet, Category: "language", Required: &yes, Multiple: &no})
	if err != nil || !result.Required || result.Multiple || !registry("language").Required || registry("language").Multiple {
		t.Fatalf("set both: result %+v err %v", result, err)
	}
	_, _ = fixture.orchestrator().EditLabelRegistry(LabelRegistryRequest{Operation: LabelSet, Category: "language", Multiple: &yes})
	_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Poly", Teams: []string{"web"}, Labels: []string{"kind:service", "language:go", "language:rust"}})
	_, err = fixture.orchestrator().EditLabelRegistry(LabelRegistryRequest{Operation: LabelSet, Category: "language", Multiple: &no})
	if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "the project poly carries several values of language; change it first") {
		t.Fatalf("single with a carrier: err %v", err)
	}
	_, err = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Two", Teams: []string{"web"}, Labels: []string{"kind:service", "data:internal", "data:public"}})
	if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "data takes one value; given internal, public") {
		t.Fatalf("two values of data: err %v", err)
	}
	_, err = fixture.orchestrator().EditLabelRegistry(LabelRegistryRequest{Operation: LabelRemove, Category: "kind", Value: "worker"})
	if err != nil || registry("kind").Labels["worker"] != "" {
		t.Fatalf("remove: err %v registry %+v", err, registry("kind"))
	}
	for expected, request := range map[string]LabelRegistryRequest{
		"is already in the registry":             {Operation: LabelAdd, Category: "kind", Value: "service"},
		"is carried by the projects poly, shop":  {Operation: LabelRemove, Category: "kind", Value: "service"},
		"is the last value of its category":      {Operation: LabelRemove, Category: "stack", Value: "aws"},
		`category "nope" is not in the registry`: {Operation: LabelSet, Category: "nope", Required: &yes},
		"nothing to set":                         {Operation: LabelSet, Category: "kind"},
		`category "Not Slug" is not a slug`:      {Operation: LabelAdd, Category: "Not Slug", Value: "x"},
		`label value "X" is not a slug`:          {Operation: LabelAdd, Category: "kind", Value: "X"},
		"is not a registry operation":            {Operation: "explode", Category: "kind"},
	} {
		_, err = fixture.orchestrator().EditLabelRegistry(request)
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), expected) {
			t.Fatalf("%s: err %v", expected, err)
		}
	}
	if len(fixture.repository.Branches) != 0 {
		t.Fatalf("branches %v", fixture.repository.Branches)
	}
}

func TestDeclarationEditAndRemove(t *testing.T) {
	seed := func(t *testing.T) (*harness, CreateResult, string) {
		t.Helper()
		fixture, created := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}, Labels: []string{"kind:service"}})
		root := filepath.Join(created.Connected.OrgClonePath, "declarations")
		write := func(relative string, content string) {
			target := filepath.Join(root, filepath.FromSlash(relative))
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			_ = os.WriteFile(target, []byte(content), 0o644)
		}
		write("users/danidani23/user.yaml", "slug: danidani23\ngithub: Danidani23\nname: Dani\n")
		write("users/danidani23/skills/commit/SKILL.md", "---\nname: commit\ndescription: Draft commits.\n---\n\n# Commit\n")
		write("users/danidani23/skills/commit/references/format.md", "subject, body\n")
		write("rules/style.md", "---\napplyIf:\n  - language:go\n---\n\nHouse style.\n")
		write("rules/brief.md", "Be brief.\n")
		write("exceptions/shop-style.yaml", "declaration: organizations.acme.rules.style\ngrantee: organizations.acme.projects.shop\nreason: legacy\n")
		// main holds the seeded files, as a merge would have left it
		_ = fixture.repository.Push(ports.RemoteAccess{}, created.Connected.OrgClonePath)
		return fixture, created, root
	}
	t.Run("a move carries the files with the selectors set, removes the old folder, and opens the pull request", func(t *testing.T) {
		fixture, created, root := seed(t)
		described, err := fixture.orchestrator().DescribeDeclaration("", "organizations.acme.users.danidani23.skills.commit")
		if err != nil || described.Level != "organizations.acme.users.danidani23" || described.Kind != "skills" || strings.Join(described.Files, ",") != "skills/commit/SKILL.md,skills/commit/references/format.md" || len(described.Selectors.ApplyIf) != 0 {
			t.Fatalf("described %+v err %v", described, err)
		}
		listed, err := fixture.orchestrator().ListDeclarations("", "")
		if err != nil || len(listed) != 3 || listed[0].Address != "organizations.acme.rules.brief" || listed[1].Address != "organizations.acme.rules.style" || strings.Join(listed[1].Selectors.ApplyIf, ",") != "language:go" || listed[2].Address != "organizations.acme.users.danidani23.skills.commit" {
			t.Fatalf("listed %+v err %v", listed, err)
		}
		under, _ := fixture.orchestrator().ListDeclarations("", "organizations.acme")
		if len(under) != 2 {
			t.Fatalf("under %+v", under)
		}
		result, err := fixture.orchestrator().EditDeclaration(EditDeclarationRequest{Address: "organizations.acme.users.danidani23.skills.commit", Level: "organizations.acme", Selectors: declaration.Selectors{ApplyIf: []string{"language:go"}}})
		if err != nil || result.NewAddress != "organizations.acme.skills.commit" || strings.Join(result.Changed, ",") != "level,applyIf" || !strings.HasPrefix(result.Branch, "rein/edit-commit-") || result.PullRequestURL != "https://github.com/Danidani23/acme-agent-settings/pull/1" {
			t.Fatalf("result %+v err %v", result, err)
		}
		pullRequest := fixture.host.PullRequests[0]
		if pullRequest.Title != "organizations.acme.users.danidani23.skills.commit: moved to organizations.acme, applyIf language:go" || !strings.Contains(pullRequest.Body, "- declarations/users/danidani23/skills/commit/SKILL.md") {
			t.Fatalf("pull request %+v", pullRequest)
		}
		published := fixture.repository.Published[created.Remote+"#"+result.Branch]
		moved, err := os.ReadFile(filepath.Join(published, "declarations", "skills", "commit", "SKILL.md"))
		if err != nil || !strings.Contains(string(moved), "rein-apply-if: language:go") || !strings.Contains(string(moved), "# Commit") {
			t.Fatalf("moved %q err %v", moved, err)
		}
		_, err = os.Stat(filepath.Join(published, "declarations", "skills", "commit", "references", "format.md"))
		if err != nil {
			t.Fatalf("the reference did not move: %v", err)
		}
		_, err = os.Stat(filepath.Join(published, "declarations", "users", "danidani23", "skills"))
		if err == nil {
			t.Fatalf("the old folder stayed")
		}
		if fixture.repository.Current != "main" {
			t.Fatalf("clone on %q", fixture.repository.Current)
		}
		_, err = os.Stat(filepath.Join(root, "users", "danidani23", "skills", "commit", "SKILL.md"))
		if err != nil {
			t.Fatalf("the clone did not go back to main: %v", err)
		}
	})
	t.Run("a selector change stays at the level; a move to a user level adds the user record", func(t *testing.T) {
		fixture, created, _ := seed(t)
		result, err := fixture.orchestrator().EditDeclaration(EditDeclarationRequest{Address: "organizations.acme.rules.style", Level: "organizations.acme", Selectors: declaration.Selectors{DoNotApplyIf: []string{"data:regulated"}, Enforcement: "required"}})
		if err != nil || strings.Join(result.Changed, ",") != "applyIf,doNotApplyIf,enforcement" || result.NewAddress != "organizations.acme.rules.style" {
			t.Fatalf("result %+v err %v", result, err)
		}
		published := fixture.repository.Published[created.Remote+"#"+result.Branch]
		style, _ := os.ReadFile(filepath.Join(published, "declarations", "rules", "style.md"))
		if strings.Contains(string(style), "applyIf") || !strings.Contains(string(style), "doNotApplyIf:\n    - data:regulated\n") || !strings.Contains(string(style), "enforcement: required\n") || !strings.Contains(string(style), "House style.") {
			t.Fatalf("style %q", style)
		}
		fixture.host.PullRequests = nil
		_ = os.RemoveAll(filepath.Join(created.Connected.OrgClonePath, "declarations", "users"))
		result, err = fixture.orchestrator().EditDeclaration(EditDeclarationRequest{Address: "organizations.acme.rules.brief", Level: "organizations.acme.users.danidani23"})
		if err != nil || result.NewAddress != "organizations.acme.users.danidani23.rules.brief" {
			t.Fatalf("to the user: result %+v err %v", result, err)
		}
		published = fixture.repository.Published[created.Remote+"#"+result.Branch]
		for _, relative := range []string{"users/danidani23/user.yaml", "users/danidani23/rules/brief.md"} {
			_, err = os.Stat(filepath.Join(published, "declarations", filepath.FromSlash(relative)))
			if err != nil {
				t.Fatalf("%s missing: %v", relative, err)
			}
		}
	})
	t.Run("refusals: nothing changed, no such declaration, a bad address, a bad level, a level outside, a missing team, a bad label, a taken address", func(t *testing.T) {
		fixture, created, root := seed(t)
		taken := filepath.Join(root, "users", "danidani23", "rules", "style.md")
		_ = os.MkdirAll(filepath.Dir(taken), 0o755)
		_ = os.WriteFile(taken, []byte("mine\n"), 0o644)
		_ = fixture.repository.Push(ports.RemoteAccess{}, created.Connected.OrgClonePath)
		for expected, request := range map[string]EditDeclarationRequest{
			"nothing changed on organizations.acme.rules.style":                               {Address: "organizations.acme.rules.style", Level: "organizations.acme", Selectors: declaration.Selectors{ApplyIf: []string{"language:go"}}},
			"no declaration at organizations.acme.rules.nope":                                 {Address: "organizations.acme.rules.nope", Level: "organizations.acme"},
			"is not a declaration's address":                                                  {Address: "organizations.acme", Level: "organizations.acme"},
			`"organizations.acme.rules.x" is not a level`:                                     {Address: "organizations.acme.rules.style", Level: "organizations.acme.rules.x"},
			"the level organizations.other is not in acme":                                    {Address: "organizations.acme.rules.style", Level: "organizations.other"},
			`team "ops" does not exist in acme`:                                               {Address: "organizations.acme.rules.style", Level: "organizations.acme.teams.ops"},
			`label "stack:aws" names no category`:                                             {Address: "organizations.acme.rules.style", Level: "organizations.acme", Selectors: declaration.Selectors{ApplyIf: []string{"stack:aws"}}},
			"applyIf data takes one value":                                                    {Address: "organizations.acme.rules.style", Level: "organizations.acme", Selectors: declaration.Selectors{ApplyIf: []string{"data:public", "data:internal"}}},
			`enforcement "maybe" is neither`:                                                  {Address: "organizations.acme.rules.style", Level: "organizations.acme", Selectors: declaration.Selectors{Enforcement: "maybe"}},
			"organizations.acme.users.danidani23.rules.style already exists; remove it first": {Address: "organizations.acme.rules.style", Level: "organizations.acme.users.danidani23", Selectors: declaration.Selectors{ApplyIf: []string{"language:go"}}},
		} {
			_, err := fixture.orchestrator().EditDeclaration(request)
			if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), expected) {
				t.Fatalf("%s: err %v", expected, err)
			}
		}
		if len(fixture.repository.Branches) != 0 {
			t.Fatalf("branches %v", fixture.repository.Branches)
		}
	})
	t.Run("a removal deletes the files on a branch, unless a skip list or an exception names the declaration", func(t *testing.T) {
		fixture, created, root := seed(t)
		_, err := fixture.orchestrator().RemoveDeclaration(RemoveDeclarationRequest{Address: "organizations.acme.rules.style"})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "is named by the exception organizations.acme.exceptions.shop-style; take those out first") {
			t.Fatalf("named by an exception: %v", err)
		}
		project := filepath.Join(root, "projects", "shop", "project.yaml")
		content, _ := os.ReadFile(project)
		_ = os.WriteFile(project, []byte(strings.Replace(string(content), "skip: []", "skip:\n    - organizations.acme.rules.brief", 1)), 0o644)
		_ = fixture.repository.Push(ports.RemoteAccess{}, created.Connected.OrgClonePath)
		_, err = fixture.orchestrator().RemoveDeclaration(RemoveDeclarationRequest{Address: "organizations.acme.rules.brief"})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "is named by the skip list of project shop") {
			t.Fatalf("named by a skip list: %v", err)
		}
		result, err := fixture.orchestrator().RemoveDeclaration(RemoveDeclarationRequest{Address: "organizations.acme.users.danidani23.skills.commit"})
		if err != nil || strings.Join(result.Removed, ",") != "declarations/users/danidani23/skills/commit/SKILL.md,declarations/users/danidani23/skills/commit/references/format.md" || !strings.HasPrefix(result.Branch, "rein/remove-commit-") || fixture.host.PullRequests[0].Title != "organizations.acme.users.danidani23.skills.commit: removed" {
			t.Fatalf("result %+v err %v", result, err)
		}
		published := fixture.repository.Published[created.Remote+"#"+result.Branch]
		_, err = os.Stat(filepath.Join(published, "declarations", "users", "danidani23", "skills"))
		if err == nil {
			t.Fatalf("the files stayed on the branch")
		}
		_, err = fixture.orchestrator().RemoveDeclaration(RemoveDeclarationRequest{Address: "organizations.acme.hooks.nope"})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "no declaration at") {
			t.Fatalf("missing: %v", err)
		}
	})
}

func TestEditTeamAndRefresh(t *testing.T) {
	fixture, created := withTeam(t)
	_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}, Labels: []string{"kind:service"}})
	result, err := fixture.orchestrator().EditTeam(EditTeamRequest{Slug: "web", Description: "the web shop"})
	if err != nil || result.Slug != "web" || result.Name != "Web" || result.RelativePath != "declarations/teams/web/team.yaml" {
		t.Fatalf("result %+v err %v", result, err)
	}
	content, _ := os.ReadFile(filepath.Join(created.Connected.OrgClonePath, "declarations", "teams", "web", "team.yaml"))
	team, problems := declaration.ParseTeam("web", content)
	if len(problems) != 0 || team.Description != "the web shop" || team.ID == "" || team.CreatedAt.IsZero() {
		t.Fatalf("record %+v problems %v", team, problems)
	}
	if !strings.Contains(fixture.repository.Commits[len(fixture.repository.Commits)-1], "team web: description (rein team edit)") {
		t.Fatalf("commits %v", fixture.repository.Commits)
	}
	for expected, request := range map[string]EditTeamRequest{
		"nothing changed on team web":                       {Slug: "web", Description: "the web shop"},
		`team "nope" does not exist in acme; teams are web`: {Slug: "nope", Description: "x"},
	} {
		_, err = fixture.orchestrator().EditTeam(request)
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), expected) {
			t.Fatalf("%s: err %v", expected, err)
		}
	}
	refreshed, err := fixture.orchestrator().RefreshOrganization("")
	if err != nil || len(refreshed.Teams) != 1 || refreshed.Teams[0].Description != "the web shop" || strings.Join(refreshed.Teams[0].Projects, ",") != "shop" || refreshed.UserLevel != "organizations.acme.users.danidani23" {
		t.Fatalf("refreshed %+v err %v", refreshed, err)
	}
}

func TestProposeDocuments(t *testing.T) {
	prepare := func(t *testing.T) (*harness, CreateResult, string, DistillResult) {
		t.Helper()
		fixture, created := withTeam(t)
		_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
		folder := projectClone(t)
		_, err := fixture.orchestrator().InitializeProject(InitializeProjectRequest{Directory: folder, Project: "shop"})
		if err != nil {
			t.Fatalf("init: %v", err)
		}
		_ = claudeCodeSnapshot(t, fixture, "organizations.acme.projects.shop", folder)
		_ = os.WriteFile(filepath.Join(folder, "README.md"), []byte("# Shop\n\nA web shop in Go.\n"), 0o644)
		fixture.judge = &fake.Judge{Answers: []string{`{"version":2,"candidates":[{"candidate":"mcpservers/docs","verdict":"new","suggestion":"keep","reason":"fine"},{"candidate":"rules/go","verdict":"new","suggestion":"keep","reason":"fine"}],"units":[
		 {"id":"overview","kind":"documents","from":[{"candidate":"rules/instructions","lines":"1, 3"}],"shape":"document","path":"docs/overview.md","pointer":"Read docs/overview.md before changing the shop.","verdict":"new","suggestion":"keep","reason":"knowledge about the code"}]}`}}
		distilled, err := fixture.orchestrator().DistillHarness(DistillRequest{Directory: folder})
		if err != nil {
			t.Fatalf("distill: %v", err)
		}
		return fixture, created, folder, distilled
	}
	t.Run("a document goes into the clone, its pointer into the pull request", func(t *testing.T) {
		fixture, created, folder, distilled := prepare(t)
		// the distiller saw what the project is: its README and its files' names
		if !strings.Contains(fixture.judge.Briefs[0], "# About the project") || !strings.Contains(fixture.judge.Briefs[0], "A web shop in Go.") || !strings.Contains(fixture.judge.Briefs[0], "\n- main.go\n") {
			t.Fatalf("brief %q", fixture.judge.Briefs[0])
		}
		// the distiller saw where the repository keeps its documents
		if !strings.Contains(fixture.judge.Briefs[0], "# Documents in the repository\n\n") || !strings.Contains(fixture.judge.Briefs[0], "\n- INSTRUCTIONS.md\n") {
			t.Fatalf("brief %q", fixture.judge.Briefs[0])
		}
		placed := map[string]proposal.Candidate{}
		for _, candidate := range distilled.Proposal.Candidates {
			placed[candidate.Kind+"/"+candidate.ID] = candidate
		}
		if placed["documents/overview"].Path != "docs/overview.md" || placed["rules/overview"].ID == "" {
			t.Fatalf("placed %+v", placed)
		}
		result, err := fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: distilled.File})
		if err != nil || strings.Join(result.Documents, ",") != "docs/overview.md" {
			t.Fatalf("result %+v err %v", result, err)
		}
		written, _ := os.ReadFile(filepath.Join(folder, "docs", "overview.md"))
		published := fixture.repository.Published[created.Remote+"#"+result.Branch]
		pointer, _ := os.ReadFile(filepath.Join(published, "declarations", "projects", "shop", "rules", "overview.md"))
		_, documentInOrg := os.Stat(filepath.Join(published, "declarations", "projects", "shop", "documents"))
		if string(written) != "# Shop\n\nBe brief.\n" || !strings.Contains(string(pointer), "document: docs/overview.md") || documentInOrg == nil {
			t.Fatalf("written %q pointer %q in org %v", written, pointer, documentInOrg)
		}
		if !strings.Contains(fixture.host.PullRequests[0].Body, "Documents for the project's repository, written into its clone and left for the developer to commit:\n- docs/overview.md") {
			t.Fatalf("body %q", fixture.host.PullRequests[0].Body)
		}
	})
	t.Run("a document never goes over a file the clone has", func(t *testing.T) {
		fixture, _, folder, distilled := prepare(t)
		_ = os.MkdirAll(filepath.Join(folder, "docs"), 0o755)
		_ = os.WriteFile(filepath.Join(folder, "docs", "overview.md"), []byte("mine"), 0o644)
		_, err := fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: distilled.File})
		if !errors.Is(err, refusal.ErrRefused) || !strings.Contains(err.Error(), "docs/overview.md exists in the clone; change the document's path in proposal.yaml, or merge the text by hand") || len(fixture.host.PullRequests) != 0 {
			t.Fatalf("over a file: %v", err)
		}
	})
}

func TestProposeLabels(t *testing.T) {
	fixture, created := withTeam(t)
	_, _ = fixture.orchestrator().CreateProject(CreateProjectRequest{Name: "Shop", Teams: []string{"web"}})
	_ = claudeCodeSnapshot(t, fixture, "organizations.acme.projects.shop", "")
	fixture.judge = &fake.Judge{Answers: []string{`{"version":2,"candidates":[
	 {"candidate":"rules/instructions","verdict":"new","suggestion":"lift","level":"organization","applyIf":["framework:astro"],"reason":"every Astro site"},
	 {"candidate":"rules/go","verdict":"new","suggestion":"keep","reason":"fine"},
	 {"candidate":"mcpservers/docs","verdict":"new","suggestion":"keep","reason":"fine"}],
	"labels":[
	 {"action":"assign","label":"language:typescript","reason":"the pages are TypeScript"},
	 {"action":"add","label":"framework:astro","meaning":"Sites built with Astro","reason":"the lifted instructions need it"},
	 {"action":"redundant","label":"kind:web-app","reason":"the same as kind:static-site here"}]}`}}
	distilled, err := fixture.orchestrator().DistillHarness(DistillRequest{Project: "shop"})
	if err != nil {
		t.Fatalf("distill: %v", err)
	}
	// the distiller saw what the project carries
	if !strings.Contains(fixture.judge.Briefs[0], "The project carries no labels yet.") {
		t.Fatalf("brief %q", fixture.judge.Briefs[0])
	}
	placed := map[string]proposal.Candidate{}
	for _, candidate := range distilled.Proposal.Candidates {
		placed[candidate.Kind+"/"+candidate.ID] = candidate
	}
	if placed["labels/framework-astro"].Level != "organizations.acme" || !placed["labels/framework-astro"].Include || placed["labels/language-typescript"].Level != "organizations.acme.projects.shop" || placed["labels/kind-web-app"].Include {
		t.Fatalf("placed %+v", placed)
	}
	result, err := fixture.orchestrator().ProposeDeclarations(ProposeRequest{Source: distilled.File})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	// the registry and the project's record change on the same branch as the declarations
	published := fixture.repository.Published[created.Remote+"#"+result.Branch]
	registry, _ := os.ReadFile(filepath.Join(published, "declarations", "labels", "framework.yaml"))
	project, _ := os.ReadFile(filepath.Join(published, "declarations", "projects", "shop", "project.yaml"))
	instructions, _ := os.ReadFile(filepath.Join(published, "declarations", "rules", "instructions.md"))
	if !strings.Contains(string(registry), "astro: Sites built with Astro") || !strings.Contains(string(project), "- language:typescript") || !strings.Contains(string(instructions), "framework:astro") {
		t.Fatalf("registry %q project %q instructions %q", registry, project, instructions)
	}
	body := fixture.host.PullRequests[0].Body
	for _, expected := range []string{"Label changes:", "- add framework:astro: Sites built with Astro", "- assign language:typescript to organizations.acme.projects.shop", "labels/kind-web-app: redundant: the same as kind:static-site here"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in body %q", expected, body)
		}
	}
}
