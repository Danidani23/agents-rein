package cli

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
)

func newStatusCommand(environment *commandEnvironment) *cobra.Command {
	var directory string
	command := &cobra.Command{
		Use:   "status",
		Short: "Show the project you are in, then what rein has set up on this machine",
		Long: "status starts with the initialised project the folder is in: its address and team, its\n" +
			"labels, the agent sync writes for, its settings counted by kind, how many characters load\n" +
			"into every session and into the skill listing, what sync wrote, and its proposals. Then\n" +
			"the machine: the GitHub accounts logged in, which are your identities, the supported\n" +
			"agents found, the files rein placed into the agent's home; the connected organisations\n" +
			"with the active one marked; the other project clones; the imports. It changes nothing and\n" +
			"runs without a login: a machine without one is reported, not refused.",
		Example: "  rein status\n  rein status --path services/shop",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, err := environment.orchestrate()
			if err != nil {
				return err
			}
			folder, err := environment.absolute(directory)
			if err != nil {
				return err
			}
			report, err := orchestrator.Status()
			if err != nil {
				return err
			}
			here, found, err := orchestrator.ProjectHere(folder)
			if err != nil {
				return err
			}
			printStatus(environment, report, here, found)
			printNudge(environment, orchestrator)
			return nil
		},
	}
	command.Flags().StringVar(&directory, "path", ".", "the folder whose project comes first")
	return command
}

// printStatus shows the project the folder is in first, then the machine,
// the organisations and the other projects (D41).
func printStatus(environment *commandEnvironment, report orchestration.StatusReport, here orchestration.ProjectHere, found bool) {
	out := environment.out
	if found {
		printProjectHere(environment, here)
	} else {
		fmt.Fprintf(out, "this folder is not a rein project; rein init puts it under rein\n")
	}
	logins := "none; run rein login"
	if len(report.Logins) > 0 {
		logins = strings.Join(report.Logins, ", ")
	}
	agents := "none found; this build writes for claude-code"
	if len(report.Agents) > 0 {
		agents = strings.Join(report.Agents, ", ")
	}
	fmt.Fprintf(out, "\nmachine\n  login        %s · agents %s · home %s · %s in your agent's home\n", logins, agents, shortPath(report.Home), counted(len(report.Placements), "file", "files"))
	for _, placed := range report.Placements {
		if !placed.TargetExists {
			fmt.Fprintf(out, "  missing      %s -> %s\n", placed.Path, placed.Target)
		}
	}
	if report.Missing > 0 {
		fmt.Fprintf(out, "               rein reset machine removes the dangling links\n")
	}
	fmt.Fprintf(out, "\norganisations\n")
	if len(report.Organizations) == 0 {
		fmt.Fprintf(out, "  none; rein org create or rein org connect\n")
	}
	for _, ref := range report.Organizations {
		marker := " "
		if ref.Slug == report.Active {
			marker = "*"
		}
		fmt.Fprintf(out, "  %s %-10s  %s\n", marker, ref.Slug, strings.TrimSuffix(strings.TrimPrefix(ref.Remote, "https://"), ".git"))
	}
	var others []orchestration.CloneStatus
	for _, clone := range report.Clones {
		if !found || clone.Address != here.Address {
			others = append(others, clone)
		}
	}
	if len(others) > 0 {
		heading := "projects"
		if found {
			heading = "other projects"
		}
		fmt.Fprintf(out, "\n%s\n", heading)
		for _, clone := range others {
			fmt.Fprintf(out, "  %-24s %s   %s · %s\n", slugOf(clone.Address), shortPath(clone.ClonePath), describeSync(clone, false), describeFate(clone.Snapshot))
		}
	}
	var homePending []orchestration.PendingProposal
	for _, pending := range report.Pending {
		if !strings.Contains(pending.Path, string(filepath.Separator)+".rein"+string(filepath.Separator)+"proposals"+string(filepath.Separator)+"proposal-") {
			homePending = append(homePending, pending)
		}
	}
	if len(homePending) > 0 {
		fmt.Fprintf(out, "\nproposals pending for your agent's home\n")
		for _, pending := range homePending {
			fmt.Fprintf(out, "  %s  %d candidates for %s, distilled %s\n", pending.Path, pending.Candidates, pending.Level, pending.DistilledAt.Local().Format("2006-01-02 15:04"))
		}
	}
	if len(report.Imports) > 0 {
		fmt.Fprintf(out, "\nimports  %d\n", len(report.Imports))
		for _, imported := range report.Imports {
			origin := imported.Source
			if imported.FromUser != "" {
				origin = "from " + imported.FromUser
			}
			fmt.Fprintf(out, "  %s  %s  %s\n", imported.Hash[:12], origin, imported.Path)
		}
	}
}

// printProjectHere is the first block: the project the folder is in.
func printProjectHere(environment *commandEnvironment, here orchestration.ProjectHere) {
	out := environment.out
	fmt.Fprintf(out, "this project   %s\n", environment.style().Bold(slugOf(here.Address)))
	where := here.Address
	switch len(here.Teams) {
	case 0:
	case 1:
		where += " · team " + here.Teams[0]
	default:
		where += " · teams " + strings.Join(here.Teams, ", ")
	}
	fmt.Fprintf(out, "  address      %s\n", where)
	labels := "none"
	if len(here.Labels) > 0 {
		labels = strings.Join(here.Labels, ", ")
	}
	fmt.Fprintf(out, "  labels       %s\n", labels)
	agents := "none rein writes for yet"
	if len(here.Agents) > 0 {
		agents = strings.Join(here.Agents, ", ")
	}
	fmt.Fprintf(out, "  agent        %s\n", agents)
	if here.Problem != "" {
		fmt.Fprintf(out, "  settings     not known: %s\n", here.Problem)
	} else {
		fmt.Fprintf(out, "  settings     %s\n", describeSettings(here))
		fmt.Fprintf(out, "  load         %s characters in every session · %s in the skill listing\n", withThousands(here.EverySession), withThousands(here.SkillListing))
	}
	fmt.Fprintf(out, "  sync         %s\n", describeSync(here.CloneStatus, true))
	proposals := "none pending"
	if len(here.Pending) > 0 {
		proposals = fmt.Sprintf("%d pending: %s", len(here.Pending), here.Pending[0].Path)
	}
	if fate := describeFate(here.Snapshot); fate != "" && len(here.Pending) == 0 {
		proposals += "; " + fate
	}
	fmt.Fprintf(out, "  proposals    %s\n", proposals)
}

// describeSettings counts what a project resolves by kind, the rules split
// into always on and by path.
func describeSettings(here orchestration.ProjectHere) string {
	var parts []string
	if count := here.Settings["rules"]; count > 0 {
		parts = append(parts, fmt.Sprintf("%s (%d always on, %d by path)", counted(count, "rule", "rules"), here.AlwaysOn, here.ByPath))
	}
	for _, kind := range []struct{ name, one, many string }{
		{"skills", "skill", "skills"},
		{"permissions", "permission set", "permission sets"},
		{"hooks", "hook", "hooks"},
		{"mcpservers", "tool server", "tool servers"},
	} {
		if count := here.Settings[kind.name]; count > 0 {
			parts = append(parts, counted(count, kind.one, kind.many))
		}
	}
	if len(parts) == 0 {
		return "none yet"
	}
	return strings.Join(parts, " · ")
}

// describeSync says what sync wrote into a clone and how it drifted; long
// adds when it last ran.
func describeSync(clone orchestration.CloneStatus, long bool) string {
	if clone.Inserts == 0 {
		return "never synced; rein sync"
	}
	text := counted(clone.Inserts, "file", "files")
	if long && clone.LastSync != "" {
		if synced, err := time.Parse(time.RFC3339, clone.LastSync); err == nil {
			text += ", last " + synced.Local().Format("2006-01-02 15:04")
		}
	}
	if clone.Modified > 0 {
		text += fmt.Sprintf(", %d edited by hand", clone.Modified)
	} else if long {
		text += ", none edited by hand"
	}
	if clone.Missing > 0 {
		text += fmt.Sprintf(", %d missing", clone.Missing)
	} else if long {
		text += ", none missing"
	}
	return text
}

// describeFate shortens what became of a project's latest snapshot.
func describeFate(snapshot string) string {
	const proposed = "snapshot distilled and proposed, "
	switch {
	case snapshot == "":
		return ""
	case strings.HasPrefix(snapshot, proposed):
		pullRequest := strings.TrimPrefix(snapshot, proposed)
		if number := path.Base(pullRequest); strings.Contains(pullRequest, "/pull/") {
			return "the last went as pull request #" + number
		}
		return "the last went as " + pullRequest
	case strings.HasPrefix(snapshot, "snapshot not distilled"):
		return "the snapshot is not distilled yet; rein distill"
	default:
		return snapshot
	}
}

// slugOf is the last part of an address.
func slugOf(declarationAddress string) string {
	return declarationAddress[strings.LastIndex(declarationAddress, ".")+1:]
}

// shortPath writes the home folder as ~.
func shortPath(folder string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !strings.HasPrefix(folder, home+string(filepath.Separator)) {
		return folder
	}
	return "~" + strings.TrimPrefix(folder, home)
}

// counted says a count with its noun.
func counted(count int, one string, many string) string {
	if count == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", count, many)
}

// withThousands writes a count with commas between its thousands.
func withThousands(count int) string {
	digits := fmt.Sprint(count)
	for index := len(digits) - 3; index > 0; index -= 3 {
		digits = digits[:index] + "," + digits[index:]
	}
	return digits
}
