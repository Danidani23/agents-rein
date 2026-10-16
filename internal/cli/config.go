package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/address"
	"github.com/Danidani23/agents-rein/internal/local"
	"github.com/Danidani23/agents-rein/internal/placement"
	"github.com/Danidani23/agents-rein/internal/prompt"
)

const configIntro = "rein config sets up this machine: who you are, which agent you use, and where your\n" +
	"personal instructions and skills come from. It writes ~/.rein/config.yaml and places your\n" +
	"instructions and skills into the agent's home folder. Running it again is safe and is how you\n" +
	"change a value."

func newConfigCommand(env *environment) *cobra.Command {
	var user, userLevel, agent string
	var yes bool
	command := &cobra.Command{
		Use:   "config",
		Short: "Set up this machine: who you are, your agent, and where your settings come from",
		Long: "config is the first command you run on a machine. It records three things in\n" +
			"~/.rein/config.yaml: your user slug, the name of your user level in every org repository;\n" +
			"the agent rein writes settings for, claude-code today; and, until sync exists, the folder\n" +
			"your global instructions and skills come from. It then places those instructions and skills\n" +
			"into the agent's home folder (CLAUDE_CONFIG_DIR, else ~/.claude): links on macOS and Linux,\n" +
			"copies on Windows, each recorded so rein reset can remove it. It refuses to replace anything\n" +
			"it did not place itself.\n\n" +
			"In a terminal it asks for each value it was not given as a flag and confirms before it\n" +
			"changes anything. Without a terminal it needs every flag. Every command that acts for you\n" +
			"refuses until config has run; lint, status and version do not.",
		Example: "  rein config\n  rein config --user danielmolnar --user-level ~/toolkit --yes",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runConfig(env, user, userLevel, agent, yes)
		},
	}
	command.Flags().StringVar(&user, "user", "", "your user slug: lowercase letters, digits and single hyphens")
	command.Flags().StringVar(&agent, "agent", "", "the agent rein writes settings for; today only claude-code")
	command.Flags().StringVar(&userLevel, "user-level", "", "folder holding global/AGENTS.md and skills/, placed into the agent's home")
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

func validateSlug(answer string) string {
	if !address.IsSlug(answer) {
		return fmt.Sprintf("%q may only contain lowercase letters, digits and single hyphens, with none at the ends.", answer)
	}
	return ""
}

func runConfig(env *environment, user string, userLevel string, agent string, yes bool) error {
	home, err := local.Home()
	if err != nil {
		return crashedError{cause: err}
	}
	config, _, err := local.LoadConfig(home)
	if err != nil {
		return crashedError{cause: err}
	}
	if agent == "" {
		agent = config.Agent
	}
	session := prompt.New(env.in, env.out, env.terminal)
	needsAsking := user == "" || userLevel == ""
	if needsAsking && !session.CanAsk() {
		return fmt.Errorf("no terminal to ask on; pass --user and --user-level")
	}
	if agent == "" && !session.CanAsk() {
		agent = local.AgentClaudeCode
		fmt.Fprintf(env.out, "agent: %s, the only agent this build writes for\n", agent)
	}
	if needsAsking {
		session.Intro(configIntro)
	}
	if user == "" {
		user, err = session.Ask(prompt.Text{
			Title:       "User slug",
			Description: "Your name inside Agents Rein: the folder of your user level in every org repository, and the author of your proposals.",
			Explanation: "The slug names the folder declarations/users/<slug>/ in each org repository you join,\n" +
				"and is recorded as the author of every proposal you make. Lowercase letters, digits and\n" +
				"single hyphens, so it can be a folder name and an address segment. It is not your GitHub\n" +
				"login, though matching it is sensible. It never leaves this machine except as that folder\n" +
				"name in org repositories you push to.",
			Default:  config.User,
			Validate: validateSlug,
		})
		if err != nil {
			return err
		}
	}
	if agent == "" {
		agent, err = session.Choose(prompt.Select{
			Title:       "Agent",
			Description: "The agent rein writes your settings for.",
			Explanation: "Your settings are kept in rein's own format and written out in the format of the\n" +
				"agent you use. Claude Code is the first agent; the others are planned and listed so you\n" +
				"can see where the tool is going. Changing the agent later means running rein config again.",
			Choices: agentChoices(),
			Default: local.AgentClaudeCode,
		})
		if err != nil {
			return err
		}
	}
	if userLevel == "" {
		userLevel, err = session.Ask(prompt.Text{
			Title:       "User-level source",
			Description: "A folder holding global/AGENTS.md and skills/<id>/. Temporary: once sync exists, your user level lives in the org repository.",
			Explanation: "Until rein sync delivers your user level per project, config places it from a folder on\n" +
				"this machine: global/AGENTS.md becomes the agent's global instructions and each\n" +
				"skills/<id>/ a skill. The pilot's folder is the toolkit in the agents-rein repository.",
			Default: config.UserLevelSource,
			Validate: func(answer string) string {
				_, err := placement.Plan(answer, "")
				if err != nil {
					return err.Error() + "."
				}
				return ""
			},
		})
		if err != nil {
			return err
		}
	}
	reason := validateSlug(user)
	if reason != "" {
		return fmt.Errorf("user slug %s", strings.TrimSuffix(reason, "."))
	}
	if !contains(local.SupportedAgents, agent) {
		return fmt.Errorf("agent %q is not supported; this build writes for %s", agent, strings.Join(local.SupportedAgents, ", "))
	}
	claudeHome, err := placement.ClaudeHome()
	if err != nil {
		return crashedError{cause: err}
	}
	pairs, err := placement.Plan(userLevel, claudeHome)
	if err != nil {
		return err
	}
	absoluteSource, err := filepath.Abs(userLevel)
	if err != nil {
		return crashedError{cause: err}
	}
	if !yes {
		fmt.Fprintf(env.out, "This will write %s and place %d entries into %s.\n", filepath.Join(home, "config.yaml"), len(pairs), claudeHome)
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
	config.User = user
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
	fmt.Fprintf(env.out, "configured %s for user %s, agent %s\n", home, user, agent)
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
