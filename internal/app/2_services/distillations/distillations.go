// Package distillations is the service that owns the folders under
// <home>/distill/: each holds the candidates of one distillation in the
// exchange layout of a level, rules/, skills/, mcpservers/, permissions/,
// hooks/, with the report beside them, ready for propose.
package distillations

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
)

// Service writes and removes distillations. Clock names them (D20); nil
// means the wall clock.
type Service struct {
	Home  string
	Clock func() time.Time
}

// ReportFile is the report's name inside a distillation.
const ReportFile = "report.yaml"

// Folder is where the distillations live.
func (service Service) Folder() string {
	return filepath.Join(service.Home, "distill")
}

func (service Service) now() time.Time {
	if service.Clock == nil {
		return time.Now().UTC()
	}
	return service.Clock()
}

// Write stores the candidates and the report under a fresh folder named
// after the level's last segment and the time, and returns it.
func (service Service) Write(name string, candidates []distill.Candidate, report distill.Report) (folder string, err error) {
	err = os.MkdirAll(service.Folder(), 0o700)
	if err != nil {
		return "", err
	}
	stamp := service.now().UTC().Format("20060102-150405")
	for attempt := 0; ; attempt++ {
		folder = filepath.Join(service.Folder(), fmt.Sprintf("%s-%s", name, stamp))
		if attempt > 0 {
			folder += fmt.Sprintf("-%d", attempt)
		}
		err = os.Mkdir(folder, 0o700)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		break
	}
	for _, candidate := range candidates {
		for relative, content := range candidate.Files {
			target := filepath.Join(folder, filepath.FromSlash(relative))
			err = os.MkdirAll(filepath.Dir(target), 0o700)
			if err != nil {
				return "", err
			}
			err = os.WriteFile(target, []byte(content), 0o600)
			if err != nil {
				return "", err
			}
		}
	}
	encoded, err := yaml.Marshal(report)
	if err != nil {
		return "", err
	}
	header := "# Written by rein distill: what the harness became, and how each candidate compares with the organisation.\n"
	err = os.WriteFile(filepath.Join(folder, ReportFile), append([]byte(header), encoded...), 0o600)
	if err != nil {
		return "", err
	}
	return folder, nil
}

// Remove deletes a distillation: the compensation of a failed workflow.
func (Service) Remove(folder string) error {
	if folder == "" {
		return nil
	}
	return os.RemoveAll(folder)
}
