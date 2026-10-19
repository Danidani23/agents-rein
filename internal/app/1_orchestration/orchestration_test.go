package orchestration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
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
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return &harness{
		home:       t.TempDir(),
		host:       &fake.Host{Login: "Danidani23"},
		repository: &fake.Repository{Record: validRecord},
		state:      fake.NewState(),
		agent:      &fake.Agent{Present: true, HomePath: t.TempDir()},
	}
}

func (fixture *harness) orchestrator() Orchestrator {
	return New(NewServices(fixture.home, fixture.host, fixture.repository, fixture.state, []ports.Agent{fixture.agent}))
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
		orchestrator := New(NewServices(fixture.home, &fake.Host{}, fixture.repository, fixture.state, []ports.Agent{fixture.agent}))
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
