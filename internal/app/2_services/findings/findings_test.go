package findings

import (
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
)

func TestKeep(t *testing.T) {
	t.Run("a finding is told once, keeps when it was first found, and goes when it no longer holds", func(t *testing.T) {
		clock := time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)
		service := Service{Documents: fake.NewDocuments(), Clock: func() time.Time { return clock }}
		edited := machine.Finding{Kind: machine.FindingEdited, Subject: "/work/shop", Detail: "CLAUDE.md"}
		fresh, err := service.Keep([]machine.Finding{edited}, true)
		if err != nil || len(fresh) != 1 || !fresh[0].Since.Equal(clock) {
			t.Fatalf("first: fresh %+v err %v", fresh, err)
		}
		first := clock
		clock = clock.Add(time.Minute)
		behind := machine.Finding{Kind: machine.FindingBehind, Subject: "/work/web", Detail: "acme changed"}
		fresh, err = service.Keep([]machine.Finding{edited, behind}, false)
		if err != nil || len(fresh) != 1 || fresh[0].Kind != machine.FindingBehind {
			t.Fatalf("second: fresh %+v err %v", fresh, err)
		}
		record, err := service.Load()
		if err != nil || len(record.Findings) != 2 || !record.Findings[0].Since.Equal(first) || !record.CheckedAt.Equal(clock) || !record.OrganizationsAskedAt.Equal(first) {
			t.Fatalf("record %+v err %v", record, err)
		}
		_, _ = service.Keep([]machine.Finding{behind, behind}, false)
		record, _ = service.Load()
		if len(record.Findings) != 1 {
			t.Fatalf("a finding found twice is kept twice: %+v", record.Findings)
		}
		_, _ = service.Keep(nil, false)
		record, _ = service.Load()
		if len(record.Findings) != 0 {
			t.Fatalf("a finding that no longer holds stayed: %+v", record.Findings)
		}
		fresh, _ = service.Keep([]machine.Finding{edited}, false)
		if len(fresh) != 1 {
			t.Fatalf("a finding that came back is told again: %+v", fresh)
		}
	})
	t.Run("before the first check the record is empty", func(t *testing.T) {
		record, err := Service{Documents: fake.NewDocuments()}.Load()
		if err != nil || record.Version != 1 || len(record.Findings) != 0 || !record.CheckedAt.IsZero() {
			t.Fatalf("record %+v err %v", record, err)
		}
	})
}
