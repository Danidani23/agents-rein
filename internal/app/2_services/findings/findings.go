// Package findings is the service that keeps what the check found (P5.1,
// D50): one document of rein's store, check.yaml, holding each finding with
// the time it was first found, so each is told once, and when the check
// last ran and last asked the organisations whether their main moved.
package findings

import (
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// document is the finding's name in rein's store.
const document = "check.yaml"

// keyOf tells one finding from another; the time does not count.
func keyOf(finding machine.Finding) string {
	return finding.Kind + "\x00" + finding.Subject + "\x00" + finding.Detail
}

// Record is what the check knows: when it ran, when it last asked the
// organisations, and what it found.
type Record struct {
	Version              int               `yaml:"version"`
	CheckedAt            time.Time         `yaml:"checked_at,omitempty"`
	OrganizationsAskedAt time.Time         `yaml:"organizations_asked_at,omitempty"`
	Findings             []machine.Finding `yaml:"findings"`
}

// Service owns check.yaml in Documents. Clock stamps the record; nil means
// the wall clock.
type Service struct {
	Documents ports.Documents
	Clock     func() time.Time
}

func (service Service) now() time.Time {
	if service.Clock == nil {
		return time.Now()
	}
	return service.Clock()
}

// Load reads what the check last found; before the first check it is
// empty.
func (service Service) Load() (Record, error) {
	record := Record{Version: 1}
	content, found, err := service.Documents.Read(document)
	if err != nil || !found {
		return record, err
	}
	err = yaml.Unmarshal(content, &record)
	return record, err
}

// Keep replaces the findings with found, each once, and returns the ones
// not found before, which are told; a finding found again keeps the time
// it was first found. organizationsAsked stamps the ask.
func (service Service) Keep(found []machine.Finding, organizationsAsked bool) (fresh []machine.Finding, err error) {
	before, err := service.Load()
	if err != nil {
		return nil, err
	}
	known := map[string]machine.Finding{}
	for _, finding := range before.Findings {
		known[keyOf(finding)] = finding
	}
	now := service.now()
	record := Record{Version: 1, CheckedAt: now, OrganizationsAskedAt: before.OrganizationsAskedAt}
	if organizationsAsked {
		record.OrganizationsAskedAt = now
	}
	kept := map[string]bool{}
	for _, finding := range found {
		if kept[keyOf(finding)] {
			continue
		}
		kept[keyOf(finding)] = true
		earlier, told := known[keyOf(finding)]
		switch {
		case told:
			finding.Since = earlier.Since
		case finding.Since.IsZero():
			finding.Since = now
		}
		if !told {
			fresh = append(fresh, finding)
		}
		record.Findings = append(record.Findings, finding)
	}
	content, err := yaml.Marshal(record)
	if err != nil {
		return nil, err
	}
	return fresh, service.Documents.Write(document, content)
}
