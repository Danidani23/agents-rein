package orchestration

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
	"github.com/Danidani23/agents-rein/internal/app/2_services/compile"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/ledger"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/resolve"
	"github.com/Danidani23/agents-rein/internal/app/2_services/semver"
)

// SyncRequest names what to sync: the clone around Directory, or every
// initialised clone with All.
type SyncRequest struct {
	Directory string
	All       bool
	// Force overwrites files a hand edit changed, after snapshotting them.
	Force bool
	// ReinExecutable is the path the session-start hook calls.
	ReinExecutable string
}

// CloneSync is what sync did to one clone.
// UntrustedAgent is an agent that has not asked the developer to trust the
// clone's folder yet, and the command that makes it ask.
type UntrustedAgent struct {
	Agent   string
	Command string
}

type CloneSync struct {
	Folder    string
	Address   string
	Agent     string
	Created   bool
	Written   []string
	Unchanged []string
	Removed   []string
	// Modified are files a hand edit changed since the last sync; with
	// Force they were snapshotted and overwritten, else they stopped the sync.
	Modified []string
	// Foreign are files in rein's folders that rein did not write: left
	// where they are, and snapshotted with Force so distill can propose them.
	Foreign      []string
	SnapshotPath string
	// Untrusted are the agents that have not been told to trust the folder
	// yet, with the command that asks; their approval of rein's tool server
	// waits for it.
	Untrusted []UntrustedAgent
	// UnsetVariables are the REIN_ variables the tool servers need and the
	// shell does not set (D22).
	UnsetVariables []string
	Notes          []string
	// Skipped says why the clone was not synced, when it was not.
	Skipped string
	// AlwaysOn is the text of the level files, printed into a session that
	// started before the files existed (O1).
	AlwaysOn string
}

// SyncResult is what sync did.
type SyncResult struct {
	Clones []CloneSync
}

// SyncClone resolves each clone's project, compiles the harness for its
// agents and writes it into the clone, recording every file with its hash
// in the inserts ledger (D12). A hand-edited file stops the sync unless
// Force, which snapshots it first. A resolution problem refuses before
// anything is written. With All, every initialised clone is synced and a
// clone that cannot be is reported, not fatal.
func (orchestrator Orchestrator) SyncClone(request SyncRequest) (SyncResult, error) {
	services := orchestrator.Services
	registered, err := services.Organizations.ProjectClones()
	if err != nil {
		return SyncResult{}, err
	}
	var targets []machine.ProjectClone
	if request.All {
		targets = registered
	} else {
		location, err := services.WorkingCopy.Locate(request.Directory)
		if err != nil {
			return SyncResult{}, refusal.New(fmt.Sprintf("%s is not inside a git repository with a commit: %v", request.Directory, err))
		}
		for _, clone := range registered {
			if clone.ClonePath == location.Folder {
				targets = append(targets, clone)
			}
		}
		if len(targets) == 0 {
			return SyncResult{}, refusal.New(fmt.Sprintf("%s is not initialised; run rein init first", location.Folder))
		}
	}
	var result SyncResult
	var firstErr error
	for _, clone := range targets {
		synced, err := syncOne(orchestrator, clone, request)
		if err != nil {
			if !request.All {
				return SyncResult{}, err
			}
			synced.Folder = clone.ClonePath
			synced.Address = clone.Address
			synced.Skipped = err.Error()
			if firstErr == nil {
				firstErr = err
			}
		}
		result.Clones = append(result.Clones, synced)
	}
	if firstErr != nil {
		return result, refusal.New("some clones were not synced; see above")
	}
	return result, nil
}

// syncOne syncs one clone for every agent rein has a compiler for.
func syncOne(orchestrator Orchestrator, clone machine.ProjectClone, request SyncRequest) (CloneSync, error) {
	services := orchestrator.Services
	synced := CloneSync{Folder: clone.ClonePath, Address: clone.Address}
	_, err := os.Stat(clone.ClonePath)
	if err != nil {
		return synced, refusal.New(fmt.Sprintf("the folder %s is gone; rein restore cannot help, rein reset drops it", clone.ClonePath))
	}
	organization, orgClonePath, err := organizationByID(services, clone.Organization)
	if err != nil {
		return synced, err
	}
	err = services.Records.Pull(accessOf(organization), orgClonePath)
	if err != nil {
		synced.Notes = append(synced.Notes, "the org repository could not be pulled; synced from the clone as it was: "+err.Error())
	}
	tree, err := services.Records.Tree(orgClonePath)
	if err != nil {
		return synced, err
	}
	if minimum := tree.Organization.MinimumReinVersion; minimum != "" && semver.IsRelease(services.ReinVersion) && semver.Compare(services.ReinVersion, minimum) < 0 {
		return synced, refusal.New(fmt.Sprintf("this rein is %s and %s requires at least %s; run rein update", services.ReinVersion, organization.Slug, minimum))
	}
	projectSlug := path.Base(strings.ReplaceAll(clone.Address, ".", "/"))
	context, found := resolve.ContextOf(tree, projectSlug, userSlugOf(organization), services.Clock())
	if !found {
		return synced, refusal.New(fmt.Sprintf("the project record %s is gone from the org repository; rein restore puts the clone back", clone.Address))
	}
	declarations, err := resolve.LoadDeclarations(orgClonePath)
	if err != nil {
		return synced, err
	}
	resolution := resolve.Resolve(declarations, context)
	if len(resolution.Problems) > 0 {
		var messages []string
		for _, problem := range resolution.Problems {
			messages = append(messages, problem.Address+": "+problem.Message)
		}
		return synced, refusal.New(fmt.Sprintf("%s does not resolve; nothing was written:\n  %s", clone.Address, strings.Join(messages, "\n  ")))
	}
	// a rule pointing at a document needs the document in this clone (D34)
	for _, chosen := range resolution.Chosen {
		if chosen.Kind != address.Rules {
			continue
		}
		for _, content := range chosen.Files {
			rule, _, _ := declaration.ParseRuleDeclaration("", []byte(content))
			if rule.Document == "" {
				continue
			}
			_, err := os.Stat(filepath.Join(clone.ClonePath, filepath.FromSlash(rule.Document)))
			if err != nil {
				synced.Notes = append(synced.Notes, fmt.Sprintf("%s points at %s, which this clone lacks; the agent is told to read a file that is not there", chosen.Address, rule.Document))
			}
		}
	}
	for _, agent := range services.Snapshots.Agents {
		if agent.Format() != "claude-code" {
			synced.Notes = append(synced.Notes, "no compiler for "+agent.Name()+" yet")
			continue
		}
		synced.Agent = agent.Name()
		input := compile.Input{OrganizationSlug: organization.Slug, ProjectName: tree.Projects[projectSlug].Name, ProjectSlug: projectSlug, Teams: context.Teams, UserSlug: context.UserSlug, ReinExecutable: request.ReinExecutable, ExistingSettings: services.WorkingCopy.ReadFile(clone.ClonePath, compile.SettingsFile)}
		output, err := compile.ClaudeCode(input, resolution)
		if err != nil {
			return synced, err
		}
		synced.Notes = append(synced.Notes, output.Notes...)
		if trusted, err := agent.TrustsFolder(clone.ClonePath); err == nil && !trusted {
			synced.Untrusted = append(synced.Untrusted, UntrustedAgent{Agent: agent.Name(), Command: agent.TrustCommand()})
		}
		synced.UnsetVariables = services.WorkingCopy.UnsetVariables(output.Variables)
		err = writeHarness(orchestrator, clone, organization, output, request.Force, &synced)
		if err != nil {
			return synced, err
		}
	}
	err = services.Organizations.MarkSynced(clone.ClonePath)
	if err != nil {
		return synced, err
	}
	return synced, nil
}

// writeHarness puts the compiled files into the clone: it verifies the
// previous inserts, removes the stale ones, writes the new ones, and
// records them with their hashes. A failure puts the previous files and the
// previous record back.
func writeHarness(orchestrator Orchestrator, clone machine.ProjectClone, organization machine.OrganizationRef, output compile.Output, force bool, synced *CloneSync) error {
	services := orchestrator.Services
	previousInserts, err := services.Ledger.InsertsOf(organization.ID, clone.ClonePath)
	if err != nil {
		return err
	}
	synced.Created = len(previousInserts) == 0
	recorded := map[string]string{}
	for _, insert := range previousInserts {
		if insert.Path != compile.SettingsFile {
			recorded[insert.Path] = insert.SHA256
		}
	}
	verification := services.WorkingCopy.Verify(clone.ClonePath, recorded)
	synced.Modified = verification.Modified
	if len(verification.Modified) > 0 && !force {
		return refusal.New(fmt.Sprintf("%s was edited by hand since the last sync: %s; rein distill --snapshot after rein sync --force keeps the edit as a proposal, or move it to CLAUDE.local.md, which rein never touches", clone.ClonePath, strings.Join(verification.Modified, ", ")))
	}
	files := map[string]string{}
	var inserts []ledger.Insert
	newPaths := map[string]bool{}
	for _, file := range output.Files {
		files[file.Path] = file.Content
		newPaths[file.Path] = true
		inserts = append(inserts, ledger.Insert{Path: file.Path, Address: file.Address, SHA256: ledger.Hash([]byte(file.Content))})
	}
	inserted := map[string]bool{}
	for _, insert := range previousInserts {
		inserted[insert.Path] = true
	}
	for _, relative := range services.WorkingCopy.FilesUnder(clone.ClonePath, compile.OwnedFolders) {
		if !inserted[relative] && !newPaths[relative] {
			synced.Foreign = append(synced.Foreign, relative)
		}
	}
	toSnapshot := append([]string{}, verification.Modified...)
	if force {
		toSnapshot = append(toSnapshot, synced.Foreign...)
	}
	if len(toSnapshot) > 0 {
		snapshotPath, _, err := services.Snapshots.Take(synced.Agent, clone.Address, clone.ClonePath, toSnapshot)
		if err != nil {
			return err
		}
		synced.SnapshotPath = snapshotPath
	}
	var stale []string
	for _, insert := range previousInserts {
		if !newPaths[insert.Path] {
			stale = append(stale, insert.Path)
		}
	}
	sort.Strings(stale)
	var previousContents map[string]string
	var alwaysOn strings.Builder
	err = runSteps(services.Progress, []Step{
		{
			Name: "removing stale files",
			Do: func() error {
				if len(stale) == 0 {
					return nil
				}
				err := services.WorkingCopy.Remove(clone.ClonePath, stale)
				if err != nil {
					return err
				}
				services.WorkingCopy.RemoveEmptyFolders(clone.ClonePath, stale)
				synced.Removed = stale
				return nil
			},
		},
		{
			Name: "writing the harness",
			Do: func() error {
				var err error
				previousContents, err = services.WorkingCopy.WriteFiles(clone.ClonePath, files)
				return err
			},
			Undo: func() error {
				var created []string
				for relative := range files {
					if _, existed := previousContents[relative]; !existed {
						created = append(created, relative)
					}
				}
				err := services.WorkingCopy.Remove(clone.ClonePath, created)
				if err != nil {
					return err
				}
				_, err = services.WorkingCopy.WriteFiles(clone.ClonePath, previousContents)
				return err
			},
		},
		{
			Name: "recording the inserts",
			Do:   func() error { return services.Ledger.RecordInserts(organization.ID, clone.ClonePath, inserts) },
		},
	})
	if err != nil {
		return err
	}
	for _, file := range output.Files {
		if previous, existed := previousContents[file.Path]; existed && previous == file.Content {
			synced.Unchanged = append(synced.Unchanged, file.Path)
		} else {
			synced.Written = append(synced.Written, file.Path)
		}
		if strings.HasPrefix(file.Path, compile.ReinFolder+"/") {
			alwaysOn.WriteString(file.Content)
			alwaysOn.WriteString("\n")
		}
	}
	synced.AlwaysOn = alwaysOn.String()
	return nil
}

// organizationByID finds a connected organisation by its id.
func organizationByID(services Services, organizationID string) (machine.OrganizationRef, string, error) {
	connected, _, err := services.Organizations.List()
	if err != nil {
		return machine.OrganizationRef{}, "", err
	}
	for _, organization := range connected {
		if organization.ID == organizationID {
			return services.Organizations.Lookup(organization.Slug)
		}
	}
	return machine.OrganizationRef{}, "", refusal.New(fmt.Sprintf("the organisation %s of this clone is no longer connected; rein org connect it again, or rein restore the clone", organizationID))
}
