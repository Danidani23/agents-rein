// Package wording is the sentences rein's CLI and its pages both show
// (D60): what a command will do, what it did and what comes next, built
// from the orchestration's plain data, so the terminal and the page say
// the same thing. Each gateway lays them out its own way: the CLI prints a
// line each, a warning on the error stream; the page lists them, a warning
// marked.
package wording

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
)

// Line is one sentence of what a command did; Warning marks what needs the
// developer's eye.
type Line struct {
	Text    string
	Warning bool
}

// Outcome is what a command did: its headline, its lines, and the next
// step.
type Outcome struct {
	Headline string
	Lines    []Line
	Next     string
}

// ResetProject is what reset project says before it asks: what it will do
// to the clone at folder, and the explanation behind its question.
func ResetProject(folder string) (summary string, explanation string) {
	summary = fmt.Sprintf("This will put back the agent's files rein init moved out of %s, take the rein block out of .gitignore, forget the clone on this machine, and commit the put-back.", folder)
	explanation = "The files come back from their snapshots in rein's store exactly as they were. The commit holds only the files that were tracked and .gitignore; pushing it is yours. The snapshots stay until rein reset machine."
	return summary, explanation
}

// Restored is what reset project did.
func Restored(result orchestration.RestoreResult) Outcome {
	done := Outcome{Headline: fmt.Sprintf("restored %s, formerly %s", result.Folder, result.Address), Next: "the clone is as before rein init; rein init puts it under rein again, rein org destroy and rein reset machine take the rest off this machine"}
	if len(result.Restored) > 0 {
		done.Lines = append(done.Lines, Line{Text: "put back: " + strings.Join(result.Restored, ", ")})
	} else {
		done.Lines = append(done.Lines, Line{Text: "nothing to put back: init had moved nothing out"})
	}
	if len(result.InsertsRemoved) > 0 {
		done.Lines = append(done.Lines, Line{Text: "removed sync's files: " + strings.Join(result.InsertsRemoved, ", ")})
	}
	if len(result.InsertsKept) > 0 {
		done.Lines = append(done.Lines, Line{Text: "kept, edited by hand since sync wrote them: " + strings.Join(result.InsertsKept, ", "), Warning: true})
	}
	if result.IgnoreRemoved {
		done.Lines = append(done.Lines, Line{Text: ".gitignore: rein block removed"})
	}
	switch {
	case result.Commit != "":
		done.Lines = append(done.Lines, Line{Text: "committed " + result.Commit[:min(7, len(result.Commit))] + ": restore agent settings from rein (rein reset project)"})
	case result.CommitError != "":
		done.Lines = append(done.Lines, Line{Text: "the put-back is staged but not committed: " + result.CommitError, Warning: true})
	default:
		done.Lines = append(done.Lines, Line{Text: "the put-back is staged, not committed"})
	}
	return done
}

// SyncNext is what comes after a sync.
const SyncNext = "a new session in the clone reads the harness, and /clear loads it in an open one; a change to it is proposed with rein distill and rein propose, never edited in the clone"

// SyncedNotes are what a sync of one clone says beside the files it
// wrote: an edit kept in a snapshot, files not written by rein, the
// compiler's notes, the variables its tool servers lack, and an agent that
// has not trusted the folder yet.
func SyncedNotes(synced orchestration.CloneSync) []Line {
	var lines []Line
	snapshot := filepath.Base(synced.SnapshotPath)
	if synced.SnapshotPath != "" && len(synced.Modified) > 0 {
		lines = append(lines, Line{Text: fmt.Sprintf("your edits to %s were kept in the snapshot %s before being overwritten", strings.Join(synced.Modified, ", "), snapshot)})
	}
	if len(synced.Foreign) > 0 {
		lines = append(lines, Line{Text: "not written by rein, so in no other clone and not in the organisation's repository: " + strings.Join(synced.Foreign, ", "), Warning: true})
		if synced.SnapshotPath != "" {
			lines = append(lines, Line{Text: fmt.Sprintf("the files not from rein are in the snapshot %s; rein distill --snapshot %s turns them into a proposal", snapshot, snapshot)})
		} else {
			lines = append(lines, Line{Text: "a sync with force keeps the files not from rein in a snapshot for rein distill"})
		}
	}
	for _, note := range synced.Notes {
		lines = append(lines, Line{Text: "note: " + note})
	}
	if len(synced.UnsetVariables) > 0 {
		lines = append(lines, Line{Text: fmt.Sprintf("the tool servers of %s need %s in your shell, not set", synced.Address, strings.Join(synced.UnsetVariables, ", ")), Warning: true})
	}
	for _, untrusted := range synced.Untrusted {
		lines = append(lines, Line{Text: fmt.Sprintf("%s has not trusted this folder yet, so rein's tool server stays pending; start it there once and accept the trust dialog: cd %s && %s", untrusted.Agent, synced.Folder, untrusted.Command), Warning: true})
	}
	return lines
}

// LoggedIn is what a login ends with.
func LoggedIn(login string) string {
	return "logged in as " + login + "; the token is in your keychain"
}

// LogOut is what a logout says before it asks, and LoggedOut what it ends
// with.
func LogOut(login string) string {
	return "This will forget " + login + " on this machine and delete its token from your keychain; organisations connected with it need another login to act."
}

func LoggedOut(login string) string {
	return "logged out " + login + "; organisations connected with it need another login to act"
}

// Update is what rein update says of its plan: where this rein stands, and
// either how it is updated, the confirmation's summary and explanation
// when rein replaces itself, or why it does not.
func Update(plan orchestration.UpdatePlan) (lines []string, summary string, explanation string, refused string) {
	switch {
	case !plan.Newer && plan.Latest != plan.Current:
		return []string{fmt.Sprintf("rein %s is the latest release; this build, %s, is newer than it", plan.Latest, plan.Current)}, "", "", ""
	case !plan.Newer:
		return []string{fmt.Sprintf("rein %s is the latest release, and this is it", plan.Latest)}, "", "", ""
	}
	lines = []string{fmt.Sprintf("rein %s is available; this is %s", plan.Latest, plan.Current)}
	switch {
	case plan.Command != "":
		lines = append(lines, fmt.Sprintf("this rein was installed by %s, which updates it: %s", plan.InstalledBy, plan.Command))
	case plan.Advice != "":
		lines = append(lines, fmt.Sprintf("this rein at %s came from %s; %s", plan.Executable, plan.InstalledBy, plan.Advice))
	case !plan.Self:
		refused = fmt.Sprintf("this rein at %s was not installed by a package manager or the install script, so rein does not replace it; install the new release the way this one was installed", plan.Executable)
	case !plan.Writable:
		refused = fmt.Sprintf("%s cannot be replaced: its folder is not writable", plan.Executable)
	default:
		summary = fmt.Sprintf("This will download rein %s for this machine, verify it against the release's checksum file, and put it in place of %s.", plan.Latest, plan.Executable)
		explanation = "The archive comes from the GitHub release of Danidani23/agents-rein. A build that fails the checksum is not installed. The running command finishes; the next rein is the new one."
	}
	return lines, summary, explanation, refused
}

// Updated is what an update ends with.
func Updated(replaced string, version string) string {
	return fmt.Sprintf("updated %s to rein %s", replaced, version)
}

// ResetMachine is what reset machine says it will do, one line each.
func ResetMachine(plan orchestration.ResetPlan) []string {
	summary := []string{"This will:", fmt.Sprintf("- put back %d initialised projects and commit the put-back in each:", len(plan.ProjectClones))}
	for _, project := range plan.ProjectClones {
		summary = append(summary, fmt.Sprintf("    %s  (%s)", project.ClonePath, project.Address))
	}
	summary = append(summary, fmt.Sprintf("- remove %d placements rein made in your agent's home; your own files are untouched", len(plan.Placements)))
	for _, placed := range plan.Placements {
		summary = append(summary, "    "+placed.Path)
	}
	if len(plan.Imports) > 0 {
		summary = append(summary, fmt.Sprintf("- remove %d imports under %s", len(plan.Imports), plan.ImportsFolder))
	}
	summary = append(summary, fmt.Sprintf("- forget %d organisations and delete their clones under %s; the remotes stay", len(plan.Organizations), plan.OrgClonesFolder))
	summary = append(summary, fmt.Sprintf("- remove the proposals under %s, and erase rein's store %s, with the ledger and the snapshots, and its key; your logins stay", plan.ProposalsFolder, plan.Store))
	return summary
}

// ResetMachineExplanation is what stands behind reset machine's question.
const ResetMachineExplanation = "reset machine is reset project for everything, then forgetting. Every initialised project is put back as rein reset project does: the files init moved out return from their snapshots, the rein block leaves .gitignore, and the put-back is committed. Every link or copy rein made in an agent's home is removed, dangling ones included; one that points somewhere rein did not record is left alone and reported. Then the organisations are forgotten and their clones deleted, the proposals removed, and rein's store, with the ledger and the snapshots, is erased with its key; your logins stay, their tokens in the keychain. The remotes on GitHub are never touched. A project whose restore fails stops the reset before anything is wiped, so no snapshot is ever lost."

// MachineReset is what reset machine did after the projects it put back,
// which Restored words: the placements, the clones and what was wiped.
func MachineReset(result orchestration.ResetResult) Outcome {
	done := Outcome{Headline: fmt.Sprintf("reset done; %d placements kept because they were not rein's", result.Kept), Next: "the machine is as before rein; rein login and rein org connect bring it back"}
	for _, unplaced := range result.Placements {
		switch {
		case unplaced.Removed:
			done.Lines = append(done.Lines, Line{Text: "removed  " + unplaced.Path})
		case unplaced.Reason != "":
			done.Lines = append(done.Lines, Line{Text: "skipped  " + unplaced.Path + ": " + unplaced.Reason, Warning: true})
		default:
			done.Lines = append(done.Lines, Line{Text: "gone     " + unplaced.Path})
		}
	}
	for _, organizationID := range result.RemovedOrgClones {
		done.Lines = append(done.Lines, Line{Text: "removed  clone " + organizationID})
	}
	done.Lines = append(done.Lines, Line{Text: "removed  " + strings.Join(result.Wiped, ", ")})
	return done
}

// ProjectReset is how reset machine says a project it did not put back.
func ProjectReset(project orchestration.ProjectReset) Line {
	if project.Failed {
		return Line{Text: "failed   " + project.Folder + ": " + project.Skipped, Warning: true}
	}
	return Line{Text: "skipped  " + project.Folder + ": " + project.Skipped, Warning: true}
}

// KeyRemove is what removing a provider's key says before it asks, and
// KeyRemoved what it ends with.
func KeyRemove(name string) string {
	return "This will remove " + name + " from your keychain; a distiller that names it then reads it from your environment, or rein key set keeps it again."
}

func KeyRemoved(name string) string {
	return "removed " + name + " from your keychain"
}
