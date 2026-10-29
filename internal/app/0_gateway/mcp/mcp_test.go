package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// connect runs the server over an in-memory pipe, the way the agent runs it
// over stdio, against the real orchestrator over the fake adapters and a
// scratch home, and returns the client's session.
func connect(t *testing.T, loggedIn bool) *mcp.ClientSession {
	t.Helper()
	return connectIn(t, loggedIn, "")
}

// connectIn is connect for an agent whose session runs in folder.
func connectIn(t *testing.T, loggedIn bool, folder string) *mcp.ClientSession {
	t.Helper()
	home := t.TempDir()
	state := fake.NewState()
	if loggedIn {
		state.Current.Logins = []string{"Danidani23"}
	}
	keychain := &fake.Keychain{Tokens: map[string]ports.Token{"Danidani23": {Login: "Danidani23", AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}}}
	agents := []ports.Agent{&fake.Agent{Present: true, HomePath: t.TempDir()}}
	services := orchestration.NewServices(home, &fake.Host{Login: "Danidani23"}, &fake.Repository{}, state, fake.NewDocuments(), agents, &fake.Judge{Unavailable: "no judge"}, &fake.Lock{}, keychain, &fake.Releases{Latest_: ports.Release{Version: "v0.1.0"}}, "v0.1.0", filepath.Join(home, "bin", "rein"))
	server := NewServer(orchestration.New(services), "v0.1.0", folder)
	clientEnd, serverEnd := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverEnd, nil)
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(context.Background(), clientEnd, nil)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func call(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	answer, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var text string
	for _, content := range answer.Content {
		if block, ok := content.(*mcp.TextContent); ok {
			text += block.Text
		}
	}
	return answer, text
}

func TestTheToolsAreTheReadOnlyQueries(t *testing.T) {
	session := connect(t, true)
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s is not marked read-only", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
	}
	expected := append([]string(nil), Tools...)
	sort.Strings(names)
	sort.Strings(expected)
	if strings.Join(names, ",") != strings.Join(expected, ",") {
		t.Fatalf("tools %v, expected %v", names, expected)
	}
}

func TestStatusAnswersWithTheReportAsStructuredContentAndText(t *testing.T) {
	session := connect(t, true)
	answer, text := call(t, session, "status", map[string]any{})
	if answer.IsError {
		t.Fatalf("status refused: %s", text)
	}
	structured, _ := json.Marshal(answer.StructuredContent)
	if !strings.Contains(string(structured), "Danidani23") || !strings.Contains(text, "Danidani23") {
		t.Fatalf("status structured %s text %s", structured, text)
	}
}

func TestAToolBehindTheLoginGateRefusesWithoutOne(t *testing.T) {
	session := connect(t, false)
	answer, text := call(t, session, "resolve", map[string]any{"project": "shop"})
	if !answer.IsError || !strings.Contains(text, "not logged in") {
		t.Fatalf("resolve without a login: error %v text %q", answer.IsError, text)
	}
}

func TestAMissingArgumentIsRejectedBeforeTheQueryRuns(t *testing.T) {
	session := connect(t, true)
	answer, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "resolve", Arguments: map[string]any{}})
	if err == nil && !answer.IsError {
		t.Fatalf("resolve without a project ran: %+v", answer)
	}
}

func TestLintReadsAFolderOnThisMachine(t *testing.T) {
	t.Run("a folder given whole", func(t *testing.T) {
		session := connect(t, true)
		folder := t.TempDir()
		_ = os.MkdirAll(filepath.Join(folder, "rules"), 0o755)
		_ = os.WriteFile(filepath.Join(folder, "rules", "go.md"), []byte("Two-line errors.\n"), 0o644)
		answer, text := call(t, session, "lint", map[string]any{"path": folder})
		if answer.IsError || answer.StructuredContent == nil {
			t.Fatalf("lint: error %v text %q", answer.IsError, text)
		}
	})
	t.Run("a relative path starts from the folder the agent's session runs in", func(t *testing.T) {
		sessionFolder := t.TempDir()
		_ = os.MkdirAll(filepath.Join(sessionFolder, "declarations", "rules"), 0o755)
		_ = os.WriteFile(filepath.Join(sessionFolder, "declarations", "rules", "go.md"), []byte("Two-line errors.\n"), 0o644)
		session := connectIn(t, true, sessionFolder)
		answer, text := call(t, session, "lint", map[string]any{"path": "declarations"})
		if answer.IsError || answer.StructuredContent == nil {
			t.Fatalf("lint: error %v text %q", answer.IsError, text)
		}
	})
}
