package placements

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
)

func TestPlacements(t *testing.T) {
	home := t.TempDir()
	state := fake.NewState()
	agent := &fake.Agent{Present: true, HomePath: t.TempDir()}
	service := Service{Home: home, State: state, Agents: []ports.Agent{agent}}
	if service.ImportsFolder() != filepath.Join(home, "imports") || strings.Join(service.InstalledAgents(), ",") != "fake-agent" {
		t.Fatalf("folder %s agents %v", service.ImportsFolder(), service.InstalledAgents())
	}
	agent.Present = false
	if len(service.InstalledAgents()) != 0 {
		t.Fatalf("an absent agent is listed")
	}
	target := filepath.Join(t.TempDir(), "CLAUDE.md")
	_ = os.WriteFile(target, []byte("x"), 0o644)
	placed := machine.Placement{Path: filepath.Join(agent.HomePath, "CLAUDE.md"), Target: target, Mode: "link", Owner: "import"}
	state.Current.Placements = []machine.Placement{placed, {Path: "/elsewhere", Target: filepath.Join(t.TempDir(), "gone")}}
	state.Current.Imports = []machine.Import{{}}
	_ = os.MkdirAll(service.ImportsFolder(), 0o755)
	listed, imports, err := service.List()
	if err != nil || len(listed) != 2 || len(imports) != 1 {
		t.Fatalf("listed %v imports %v err %v", listed, imports, err)
	}
	if !service.TargetExists(listed[0]) || service.TargetExists(listed[1]) {
		t.Fatalf("target checks are wrong")
	}
	removed, reason, err := service.Unplace(placed)
	if err != nil || reason != "" || strings.Join(agent.Unplaced, ",") != placed.Path {
		t.Fatalf("unplace: removed %v reason %q err %v unplaced %v", removed, reason, err, agent.Unplaced)
	}
	removed, reason, err = Service{State: state}.Unplace(placed)
	if err != nil || removed || !strings.Contains(reason, "no agent adapter") {
		t.Fatalf("unplace without an agent: removed %v reason %q err %v", removed, reason, err)
	}
	err = service.Keep([]machine.Placement{listed[1]})
	_, importsErr := os.Stat(service.ImportsFolder())
	if err != nil || len(state.Current.Placements) != 1 || state.Current.Placements[0].Path != "/elsewhere" || len(state.Current.Imports) != 0 || importsErr == nil {
		t.Fatalf("keep: err %v state %+v imports folder %v", err, state.Current, importsErr)
	}
}
