// Package proposals is the service that turns candidate declarations into a
// pull request: it reads them from a distillation folder, a plain level
// folder or an export zip, writes them on a branch of the org clone, pushes
// the branch and opens the pull request. It is the only writer of
// declarations into an org repository (P2.18 c); records have their own
// service, and main is never written here.
package proposals

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
	"github.com/Danidani23/agents-rein/internal/app/2_services/exchange"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Service acts on the org clone through version control and the host.
type Service struct {
	Repository ports.Repository
	Host       ports.Host
}

// Candidates are the files of a proposal, by path relative to the level,
// with the distillation report when the source was a distillation.
type Candidates struct {
	Files  map[string]string
	Report *distill.Report
}

// Read loads the candidates from a folder or a zip. A distillation folder
// contributes its report; the report file itself is not a candidate.
func (Service) Read(source string) (Candidates, error) {
	info, err := os.Stat(source)
	if err != nil {
		return Candidates{}, err
	}
	candidates := Candidates{Files: map[string]string{}}
	if !info.IsDir() {
		_, entries, err := exchange.Read(source)
		if err != nil {
			return Candidates{}, err
		}
		for _, entry := range entries {
			candidates.Files[entry.Path] = string(entry.Content)
		}
		return candidates, nil
	}
	err = filepath.WalkDir(source, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(source, filePath)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		if relative == distill.ReportFile {
			var report distill.Report
			err = yaml.Unmarshal(content, &report)
			if err != nil {
				return fmt.Errorf("%s: %w", filePath, err)
			}
			candidates.Report = &report
			return nil
		}
		candidates.Files[filepath.ToSlash(relative)] = string(content)
		return nil
	})
	if err != nil {
		return Candidates{}, err
	}
	return candidates, nil
}

// Branch makes a fresh branch from the clone's current main.
func (service Service) Branch(orgClonePath string, branch string) error {
	return service.Repository.CreateBranch(orgClonePath, branch)
}

// Write puts files under the level's folder in the clone, replacing what is
// there at the same paths, and returns the paths written relative to the
// clone's root.
func (Service) Write(orgClonePath string, levelPath string, files map[string]string) (relativePaths []string, err error) {
	for relative, content := range files {
		relativePath := filepath.ToSlash(filepath.Join("declarations", levelPath, relative))
		target := filepath.Join(orgClonePath, filepath.FromSlash(relativePath))
		err = os.MkdirAll(filepath.Dir(target), 0o755)
		if err != nil {
			return nil, err
		}
		err = os.WriteFile(target, []byte(content), 0o644)
		if err != nil {
			return nil, err
		}
		relativePaths = append(relativePaths, relativePath)
	}
	return relativePaths, nil
}

// Publish commits the paths on the branch and pushes it.
func (service Service) Publish(access ports.RemoteAccess, orgClonePath string, branch string, relativePaths []string, message string) error {
	err := service.Repository.CommitPaths(orgClonePath, relativePaths, message)
	if err != nil {
		return err
	}
	return service.Repository.PushBranch(access, orgClonePath, branch)
}

// OpenPullRequest opens the pull request for the pushed branch.
func (service Service) OpenPullRequest(accessToken string, owner string, name string, request ports.PullRequest) (url string, err error) {
	return service.Host.CreatePullRequest(accessToken, owner, name, request)
}

// BackToMain leaves the clone on main without the branch, which lives on in
// the remote and the pull request.
func (service Service) BackToMain(orgClonePath string, branch string) error {
	err := service.Repository.Checkout(orgClonePath, "main")
	if err != nil {
		return err
	}
	return service.Repository.DeleteBranch(orgClonePath, branch)
}

// Abandon throws the branch away, locally and, when pushed, on the remote,
// and puts the clone back on main as the remote has it: the compensation.
func (service Service) Abandon(access ports.RemoteAccess, orgClonePath string, branch string, pushed bool) error {
	var failures []string
	err := service.Repository.ResetToRemote(orgClonePath)
	if err != nil {
		failures = append(failures, err.Error())
	}
	err = service.Repository.Checkout(orgClonePath, "main")
	if err != nil {
		failures = append(failures, err.Error())
	}
	err = service.Repository.DeleteBranch(orgClonePath, branch)
	if err != nil {
		failures = append(failures, err.Error())
	}
	if pushed {
		err = service.Repository.DeleteRemoteBranch(access, orgClonePath, branch)
		if err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("abandoning %s: %s", branch, strings.Join(failures, "; "))
	}
	return nil
}
