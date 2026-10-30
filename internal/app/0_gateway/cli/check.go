package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
)

func newCheckCommand(environment *commandEnvironment) *cobra.Command {
	var line bool
	command := &cobra.Command{
		Use:   "check",
		Short: "Look for what changed since the last sync: hand edits, the organisation, the agent's home",
		Long: "check compares the files sync wrote into each initialised clone, and the links rein made in\n" +
			"your agent's home, with what is there; asks each organisation's repository whether its main\n" +
			"moved since the clone was pulled; syncs the clones you set to automatic sync with rein\n" +
			"auto-sync; and keeps what it found, so each finding is told once. rein's app runs it every\n" +
			"minute and notifies what is new. --line prints one line about the clone this folder is in,\n" +
			"from what the check last found, for Claude Code's status line; rein status-line on has sync\n" +
			"write that status line into the clone.",
		Example: "  rein check\n  rein check --line",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			if line {
				return printCheckLine(environment)
			}
			return runCheck(environment)
		},
	}
	command.Flags().BoolVar(&line, "line", false, "print one line about this folder's clone from the last check, for a status line")
	return command
}

func runCheck(environment *commandEnvironment) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return err
	}
	defer release()
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	result, err := orchestrator.Check(orchestration.CheckRequest{AskOrganizations: true, ReinExecutable: executable})
	if err != nil {
		return err
	}
	if len(result.Findings) == 0 {
		fmt.Fprintf(environment.out, "nothing found: the clones hold what sync wrote, the agent's home what rein placed, and no organisation moved\n")
		return nil
	}
	fresh := map[string]bool{}
	for _, finding := range result.Fresh {
		fresh[finding.Kind+finding.Subject+finding.Detail] = true
	}
	style := environment.style()
	for _, finding := range result.Findings {
		marker := "   "
		if fresh[finding.Kind+finding.Subject+finding.Detail] {
			marker = style.Yellow("new")
		}
		fmt.Fprintf(environment.out, "%s  %-9s %s: %s\n", marker, finding.Kind, finding.Subject, finding.Detail)
	}
	return nil
}

// printCheckLine prints one line about the clone the caller's folder is in,
// from what the check last found, or nothing; it changes nothing and waits
// for nothing, as a status line asks often.
func printCheckLine(environment *commandEnvironment) error {
	orchestrator, err := environment.orchestrate()
	if err != nil {
		return err
	}
	line, err := orchestrator.CheckLine(environment.folder)
	if err != nil || line == "" {
		return err
	}
	fmt.Fprintf(environment.out, "%s\n", line)
	return nil
}

func newAutoSyncCommand(environment *commandEnvironment) *cobra.Command {
	return newCloneSettingCommand(environment, machine.SettingAutoSync,
		"Have rein's app sync a clone when its organisation changes, or stop it",
		"auto-sync on has the check, which rein's app runs every minute, sync this clone when its\n"+
			"organisation's main moved on GitHub, under the machine lock as any sync, and tell you once\n"+
			"it did; a new session reads what it wrote, and /clear loads it into an open one. off, the\n"+
			"default, leaves the clone behind until you sync, and the check says so. Without on or off it\n"+
			"shows the setting of every clone.",
		"the check syncs it when its organisation changes", "the check says when its organisation changes; rein sync writes it")
}

func newStatusLineCommand(environment *commandEnvironment) *cobra.Command {
	return newCloneSettingCommand(environment, machine.SettingStatusLine,
		"Have sync write rein's line into Claude Code's status line in a clone, or stop it",
		"status-line on has the next rein sync write a status line into the clone's\n"+
			".claude/settings.local.json that runs rein check --line, which says what the check found\n"+
			"for the clone: that it is behind its organisation, that rein synced it and /clear loads it,\n"+
			"or that files differ from what sync wrote. A status line of your own there stays, and sync\n"+
			"says so; your own status line script can call rein check --line instead. off, the default,\n"+
			"has the next sync take rein's status line out. Without on or off it shows the setting of\n"+
			"every clone.",
		"the next rein sync writes the status line", "the next rein sync takes rein's status line out")
}

// newCloneSettingCommand is a command that turns a clone's choice on or
// off, for the clone at --path or every clone, or shows it for every clone.
func newCloneSettingCommand(environment *commandEnvironment, setting machine.CloneSetting, short string, long string, saidOn string, saidOff string) *cobra.Command {
	var folder string
	var all bool
	command := &cobra.Command{
		Use:     string(setting) + " [on|off]",
		Short:   short,
		Long:    long,
		Example: "  rein " + string(setting) + "\n  rein " + string(setting) + " on\n  rein " + string(setting) + " off --all",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			if len(arguments) == 0 {
				orchestrator, err := environment.orchestrate()
				if err != nil {
					return err
				}
				clones, err := orchestrator.Organizations.ProjectClones()
				if err != nil {
					return err
				}
				rows := [][]string{{"CLONE", strings.ToUpper(string(setting))}}
				for _, clone := range clones {
					on := clone.AutoSync
					if setting == machine.SettingStatusLine {
						on = clone.StatusLine
					}
					rows = append(rows, []string{clone.ClonePath, map[bool]string{true: "on", false: "off"}[on]})
				}
				printTable(environment, rows)
				return nil
			}
			if arguments[0] != "on" && arguments[0] != "off" {
				return refusal.New(fmt.Sprintf("%s takes on or off, not %s", setting, arguments[0]))
			}
			on := arguments[0] == "on"
			orchestrator, release, err := environment.orchestrateLocked()
			if err != nil {
				return err
			}
			defer release()
			clones, err := orchestrator.Organizations.ProjectClones()
			if err != nil {
				return err
			}
			var targets []string
			if all {
				for _, clone := range clones {
					targets = append(targets, clone.ClonePath)
				}
			} else {
				target, err := environment.absolute(folder)
				if err != nil {
					return err
				}
				targets = []string{target}
			}
			said := saidOff
			if on {
				said = saidOn
			}
			for _, target := range targets {
				found, err := orchestrator.Organizations.SetCloneSetting(target, setting, on)
				if err != nil {
					return err
				}
				if !found {
					return refusal.New(target + " is not a project clone rein initialised on this machine; rein init puts it under rein")
				}
				fmt.Fprintf(environment.out, "%s %s for %s: %s\n", setting, arguments[0], target, said)
			}
			return nil
		},
	}
	command.Flags().StringVar(&folder, "path", ".", "the clone's folder")
	command.Flags().BoolVar(&all, "all", false, "every initialised clone on this machine")
	return command
}
