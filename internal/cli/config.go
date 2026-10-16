package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/local"
	"github.com/Danidani23/agents-rein/internal/placement"
	"github.com/Danidani23/agents-rein/internal/prompt"
)

const configIntro = "rein config sets up this machine. Your identity is your GitHub account, read from gh;\n" +
	"your agent is the one rein writes settings for. Until your organisation's repository exists,\n" +
	"config also places your personal instructions and skills from a folder into the agent's\n" +
	"home. Running it again is safe."

func newConfigCommand(env *environment) *cobra.Command {
	var userLevel, agent string
	var yes bool
	command := &cobra.Command{
		Use:   "config",
		Short: "Set up this machine: your GitHub identity, your agent, and your personal settings",
		Long: "config is the first command you run on a machine. Your identity is your GitHub account,\n" +
			"read from the gh CLI, which must be installed and logged in; its lowercase form is your\n" +
			"slug, the name of your user level in every org repository. The agent rein writes settings\n" +
			"for is claude-code today. Until your organisation's repository exists, config also places\n" +
			"the instructions and skills from a folder (--user-level, remembered from the last run) into\n" +
			"the agent's home (CLAUDE_CONFIG_DIR, else ~/.claude): links on macOS and Linux, copies on\n" +
			"Windows, never replacing anything it did not place itself.\n\n" +
			"In a terminal it shows what it found and confirms before changing anything; without a\n" +
			"terminal it needs --yes. Every command that acts for you refuses until config has run;\n" +
			"lint, status and version do not. Nothing in a user level is private: everyone who can\n" +
			"read an org repository sees every level in it.",
		Example: "  rein config\n  rein config --user-level ~/toolkit --yes",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runConfig(env, userLevel, agent, yes)
		},
	}
	command.Flags().StringVar(&agent, "agent", "", "the agent rein writes settings for; today only claude-code")
	command.Flags().StringVar(&userLevel, "user-level", "", "folder holding global/AGENTS.md and skills/, placed into the agent's home until the org repository exists")
	command.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation before changing the machine")
	return command
}

// agentChoices lists the supported agent first and the planned ones after it,
// so people see where the tool is going and cannot pick them yet.
func agentChoices() []prompt.Choice {
	choices := []prompt.Choice{{Name: local.AgentClaudeCode, Description: "Claude Code"}}
	for _, planned := range []string{"codex", "cursor", "copilot", "gemini-cli", "opencode"} {
		choices = append(choices, prompt.Choice{Name: planned, Unavailable: "not yet supported"})
	}
	return choices
}

func runConfig(env *environment, userLevel string, agent string, yes bool) error {
	home, err := local.Home()
	if err != nil {
		return crashedError{cause: err}
	}
	config, _, err := local.LoadConfig(home)
	if err != nil {
		return crashedError{cause: err}
	}
	login, err := githubLogin()
	if err != nil {
		return err
	}
	session := prompt.New(env.in, env.out, env.terminal)
	if session.CanAsk() {
		session.Intro(configIntro)
	}
	fmt.Fprintf(env.out, "GitHub account  %s  (slug %s)\n", login, slugOf(login))
	if agent == "" {
		agent = config.Agent
	}
	if agent == "" {
		if session.CanAsk() && len(local.SupportedAgents) > 1 {
			agent, err = session.Choose(prompt.Select{
				Title:       "Agent",
				Description: "The agent rein writes your settings for.",
				Explanation: "Your settings are kept in rein's own format and written out in the format of the\n" +
					"agent you use. Changing the agent later means running rein config again.",
				Choices: agentChoices(),
				Default: local.AgentClaudeCode,
			})
			if err != nil {
				return err
			}
		} else {
			agent = local.AgentClaudeCode
		}
	}
	if !contains(local.SupportedAgents, agent) {
		return fmt.Errorf("agent %q is not supported; this build writes for %s", agent, strings.Join(local.SupportedAgents, ", "))
	}
	fmt.Fprintf(env.out, "agent           %s\n", agent)
	if userLevel == "" {
		userLevel = config.UserLevelSource
	}
	claudeHome, err := placement.ClaudeHome()
	if err != nil {
		return crashedError{cause: err}
	}
	var pairs []placement.Pair
	absoluteSource := ""
	if userLevel != "" {
		pairs, err = placement.Plan(userLevel, claudeHome)
		if err != nil {
			return err
		}
		absoluteSource, err = filepath.Abs(userLevel)
		if err != nil {
			return crashedError{cause: err}
		}
		fmt.Fprintf(env.out, "user level      %s\n", absoluteSource)
	}
	fmt.Fprintln(env.out)
	if len(pairs) > 0 {
		fmt.Fprintf(env.out, "This will write %s and place %d entries into %s.\n", filepath.Join(home, "config.yaml"), len(pairs), claudeHome)
	} else {
		fmt.Fprintf(env.out, "This will write %s. No user level yet: pass --user-level <folder> or run rein import to add one.\n", filepath.Join(home, "config.yaml"))
	}
	if !yes {
		if !session.CanAsk() {
			return fmt.Errorf("no terminal to confirm on; pass --yes")
		}
		confirmed, err := session.Confirm("Continue?")
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("nothing changed")
		}
	}
	config.User = slugOf(login)
	config.GitHub = login
	config.Agent = agent
	config.UserLevelSource = absoluteSource
	err = local.SaveConfig(home, config)
	if err != nil {
		return crashedError{cause: err}
	}
	state, err := local.LoadState(home)
	if err != nil {
		return crashedError{cause: err}
	}
	refused := 0
	for _, outcome := range placement.Apply(pairs, placement.ModeForSystem(), state.OwnedBy(local.OwnerConfig)) {
		if outcome.Status == placement.StatusRefused {
			refused++
			fmt.Fprintf(env.errOut, "refused  %s: %s\n", outcome.Path, outcome.Reason)
			continue
		}
		fmt.Fprintf(env.out, "%-8s %s -> %s\n", outcome.Status, outcome.Path, outcome.Target)
		state.Placements = recordPlacement(state.Placements, local.Placement{Path: outcome.Path, Target: outcome.Target, Mode: string(outcome.Mode), Owner: local.OwnerConfig})
	}
	err = local.SaveState(home, state)
	if err != nil {
		return crashedError{cause: err}
	}
	fmt.Fprintf(env.out, "configured %s for %s, agent %s\n", home, login, agent)
	if refused > 0 {
		return fmt.Errorf("%d targets refused; move them aside and run rein config again", refused)
	}
	return nil
}

// recordPlacement replaces the entry for a path, or adds it.
func recordPlacement(placements []local.Placement, placed local.Placement) []local.Placement {
	for index, existing := range placements {
		if existing.Path == placed.Path {
			placements[index] = placed
			return placements
		}
	}
	return append(placements, placed)
}

func contains(list []string, item string) bool {
	for _, candidate := range list {
		if candidate == item {
			return true
		}
	}
	return false
}
