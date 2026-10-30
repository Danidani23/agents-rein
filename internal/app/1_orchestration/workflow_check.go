package orchestration

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// CheckRequest is one run of the check. AskOrganizations asks each
// organisation whether its main moved, which rein check always does and
// the app every few minutes; ReinExecutable is what an automatic sync
// writes into the session-start hook.
type CheckRequest struct {
	AskOrganizations bool
	ReinExecutable   string
}

// CheckResult is everything the check found now, and what of it was found
// for the first time, which is told.
type CheckResult struct {
	Findings []machine.Finding
	Fresh    []machine.Finding
}

// syncedFor is how long an automatic sync stays a finding: long enough for
// a status line to say that /clear loads what it wrote.
const syncedFor = time.Hour

// Check compares what sync wrote into each clone, and what rein placed in
// the agents' homes, with what is there; asks each organisation whether
// its main moved, when asked to, and GitHub whether each account's token
// still works, and refreshes the daily look at rein's releases; syncs the clones of a moved organisation
// that are set to automatic sync, under the machine lock its caller holds;
// and keeps what it found, so each finding is told once (P5.1, D50). A
// clone of a moved organisation that is not set to automatic sync is
// behind until it syncs. An organisation that cannot be asked, such as
// without a network, is left as it was.
func (orchestrator Orchestrator) Check(request CheckRequest) (CheckResult, error) {
	services := orchestrator.Services
	var found []machine.Finding
	var clones []machine.ProjectClone
	slugs := map[string]string{}
	before, err := services.Findings.Load()
	if err != nil {
		return CheckResult{}, err
	}
	var result CheckResult
	steps := []Step{
		{
			Name: "looking at the clones",
			Do: func() error {
				organizations, _, err := services.Organizations.List()
				if err != nil {
					return err
				}
				for _, organization := range organizations {
					slugs[organization.ID] = organization.Slug
				}
				clones, err = services.Organizations.ProjectClones()
				if err != nil {
					return err
				}
				for _, clone := range clones {
					page := projectPage(slugs[clone.Organization], clone.Address)
					_, statErr := os.Stat(clone.ClonePath)
					if statErr != nil {
						found = append(found, machine.Finding{Kind: machine.FindingMissing, Subject: clone.ClonePath, Detail: "the clone's folder is gone", Page: page})
						continue
					}
					files, err := orchestrator.SyncedFiles(clone.ClonePath)
					if err != nil {
						return err
					}
					for _, file := range files {
						switch file.State {
						case SyncedEditedByHand:
							found = append(found, machine.Finding{Kind: machine.FindingEdited, Subject: clone.ClonePath, Detail: file.Path, Page: page})
						case SyncedMissing:
							found = append(found, machine.Finding{Kind: machine.FindingMissing, Subject: clone.ClonePath, Detail: file.Path, Page: page})
						}
					}
				}
				return nil
			},
		},
		{
			Name: "looking at the agents' homes",
			Do: func() error {
				placed, _, err := services.Placements.List()
				if err != nil {
					return err
				}
				for _, placement := range placed {
					if !services.Placements.TargetExists(placement) {
						found = append(found, machine.Finding{Kind: machine.FindingPlacement, Subject: placement.Path, Detail: "what it points at is gone: " + placement.Target, Page: "/"})
					}
				}
				return nil
			},
		},
		{
			Name: "asking the organisations whether their main moved",
			Do: func() error {
				lastSyncOf := map[string]time.Time{}
				for _, clone := range clones {
					lastSyncOf[clone.ClonePath], _ = time.Parse(time.RFC3339, clone.LastSync)
				}
				syncedNow := map[string]bool{}
				// what an earlier run found still holds until the clone syncs, or the hour
				// passes; the last sync is kept to the second
				var carried []machine.Finding
				for _, finding := range before.Findings {
					switch {
					case finding.Kind == machine.FindingBehind && lastSyncOf[finding.Subject].Before(finding.Since.Truncate(time.Second)):
						carried = append(carried, finding)
					case finding.Kind == machine.FindingSynced && services.Clock().Sub(finding.Since) < syncedFor:
						carried = append(carried, finding)
					}
				}
				defer func() {
					for _, finding := range carried {
						if finding.Kind != machine.FindingBehind || !syncedNow[finding.Subject] {
							found = append(found, finding)
						}
					}
				}()
				if !request.AskOrganizations {
					return nil
				}
				organizations, _, err := services.Organizations.List()
				if err != nil {
					return err
				}
				for _, organization := range organizations {
					_, orgClonePath, err := services.Organizations.Lookup(organization.Slug)
					if err != nil {
						return err
					}
					moved, err := services.Clones.Moved(accessOf(organization), orgClonePath)
					if err != nil || !moved {
						continue
					}
					for _, clone := range clones {
						if clone.Organization != organization.ID {
							continue
						}
						page := projectPage(organization.Slug, clone.Address)
						if !clone.AutoSync {
							found = append(found, machine.Finding{Kind: machine.FindingBehind, Subject: clone.ClonePath, Detail: organization.Slug + " changed on GitHub since this clone's last sync", Page: page})
							continue
						}
						_, err := orchestrator.SyncClone(SyncRequest{Directory: clone.ClonePath, ReinExecutable: request.ReinExecutable})
						if err != nil {
							found = append(found, machine.Finding{Kind: machine.FindingFailed, Subject: clone.ClonePath, Detail: "the automatic sync failed: " + err.Error(), Page: page})
							continue
						}
						syncedNow[clone.ClonePath] = true
						found = append(found, machine.Finding{Kind: machine.FindingSynced, Subject: clone.ClonePath, Detail: fmt.Sprintf("synced with what %s changed, at %s", organization.Slug, services.Clock().Format("15:04")), Page: page})
					}
				}
				return nil
			},
		},
		{
			Name: "asking GitHub whether each account's token works",
			Do: func() error {
				if !request.AskOrganizations {
					for _, finding := range before.Findings {
						if finding.Kind == machine.FindingLoggedOut {
							found = append(found, finding)
						}
					}
					return nil
				}
				// a newer release of rein is a notification (D63); the look is made once a day
				_, _ = services.Updates.Check()
				accounts, err := services.Credentials.Accounts()
				if err != nil {
					return err
				}
				for _, account := range accounts {
					loggedIn, _, err := services.Credentials.Standing(account.Login)
					if err != nil || !loggedIn {
						continue
					}
					accessToken, err := services.Credentials.AccessToken(account.Login)
					if err != nil {
						continue
					}
					_, err = services.Credentials.Host.Identity(accessToken)
					if errors.Is(err, ports.ErrTokenRefused) {
						found = append(found, machine.Finding{Kind: machine.FindingLoggedOut, Subject: account.Login, Detail: "GitHub refuses its token; log in again", Page: "/accounts"})
					}
				}
				return nil
			},
		},
		{
			Name: "keeping what was found",
			Do: func() error {
				fresh, err := services.Findings.Keep(found, request.AskOrganizations)
				result = CheckResult{Findings: found, Fresh: fresh}
				return err
			},
		},
	}
	err = runSteps(orchestrator.Progress, steps)
	if err != nil {
		return CheckResult{}, err
	}
	return result, nil
}

// projectPage is the page of rein's window that shows a project.
func projectPage(organizationSlug string, projectAddress string) string {
	return "/projects?" + url.Values{"org": {organizationSlug}, "project": {projectAddress[strings.LastIndex(projectAddress, ".")+1:]}}.Encode()
}
