package wording

import (
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
	"github.com/Danidani23/agents-rein/internal/app/2_services/proposal"
)

func TestWording(t *testing.T) {
	t.Run("a reset that could not commit says so as a warning, with the next step", func(t *testing.T) {
		done := Restored(orchestration.RestoreResult{Folder: "/work/shop", Address: "organizations.acme.projects.shop", Restored: []string{"CLAUDE.md"}, CommitError: "nothing staged"})
		last := done.Lines[len(done.Lines)-1]
		if done.Headline != "restored /work/shop, formerly organizations.acme.projects.shop" || !last.Warning || last.Text != "the put-back is staged but not committed: nothing staged" || done.Next == "" {
			t.Fatalf("outcome %+v", done)
		}
	})
	t.Run("files not from rein are a warning, with the way to a proposal from their snapshot", func(t *testing.T) {
		lines := SyncedNotes(orchestration.CloneSync{Foreign: []string{".claude/skills/mine/SKILL.md"}, SnapshotPath: "/home/.rein/snapshots/shop-claude-code-20261029-100000.json"})
		if len(lines) != 2 || !lines[0].Warning || !strings.Contains(lines[1].Text, "rein distill --snapshot shop-claude-code-20261029-100000.json") {
			t.Fatalf("lines %+v", lines)
		}
	})
}

func TestJudgementLine(t *testing.T) {
	candidate := proposal.Candidate{Level: "organizations.acme.projects.shop", Judge: &proposal.Judgement{Verdict: "contradicts", Suggestion: "keep", Reason: "the project's own", Quotes: []distill.Quote{{Address: "organizations.acme.skills.adr", Text: "one"}, {Address: "organizations.acme.skills.adr", Text: "two"}, {Address: "organizations.acme.rules.global", Text: "three"}}}}
	line := judgementLine(candidate)
	if line != "contradicts with organizations.acme.skills.adr with organizations.acme.rules.global; keep at organizations.acme.projects.shop: the project's own" {
		t.Fatalf("line %q", line)
	}
}
