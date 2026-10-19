package ledger

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestRemovals(t *testing.T) {
	removedAt := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	service := Service{Home: t.TempDir(), Clock: func() time.Time { return removedAt }}
	err := service.RecordRemovals("claude-code", []Removal{
		{Path: "/p/CLAUDE.md", Scope: ScopeProject, Project: "organizations.acme.projects.shop", Snapshot: "/s/1.json"},
		{Path: "/p/.claude", Scope: ScopeProject, Project: "organizations.acme.projects.shop", Snapshot: "/s/1.json"},
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	err = service.RecordRemovals("claude-code", []Removal{{Path: "/home/.claude/CLAUDE.md", Scope: ScopeHome, Snapshot: "/s/2.json"}})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	content, _ := os.ReadFile(service.Path())
	if !strings.HasPrefix(string(content), "# Written by rein") || !strings.Contains(string(content), "version: 1") || !strings.Contains(string(content), "removed_at: 2026-10-19T10:00:00Z") {
		t.Fatalf("content %q", content)
	}
	all, err := service.Load()
	if err != nil || len(all.Agents["claude-code"]) != 3 || all.Agents["claude-code"][0].Path != "/p/CLAUDE.md" || !all.Agents["claude-code"][0].RemovedAt.Equal(removedAt) {
		t.Fatalf("all %+v err %v", all, err)
	}
	err = service.ForgetSnapshot("/s/1.json")
	all, _ = service.Load()
	if err != nil || len(all.Agents["claude-code"]) != 1 || all.Agents["claude-code"][0].Snapshot != "/s/2.json" {
		t.Fatalf("all %+v err %v", all, err)
	}
	err = service.ForgetSnapshot("/s/2.json")
	all, _ = service.Load()
	if err != nil || len(all.Agents) != 0 {
		t.Fatalf("all %+v err %v", all, err)
	}
	entries, _ := os.ReadDir(service.Home)
	if len(entries) != 1 {
		t.Fatalf("temporary files left: %v", entries)
	}
}
