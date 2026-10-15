package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/address"
	"github.com/Danidani23/agents-rein/internal/local"
	"github.com/Danidani23/agents-rein/internal/placement"
)

func newConfigCommand(env *environment) *cobra.Command {
	var user, userLevel, agent string
	command := &cobra.Command{
		Use:   "config",
		Short: "Set up this machine: who you are and the source of your global instructions",
		Long: "config records your user slug and the folder your global instructions and skills come\n" +
			"from, then places them into the Claude home (CLAUDE_CONFIG_DIR, else ~/.claude): links on\n" +
			"macOS and Linux, copies on Windows. It refuses to replace anything it did not place itself.\n" +
			"Without --user and --user-level it asks, when a terminal is attached. Every command that\n" +
			"acts for a user refuses until config has run. Running it again is safe.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runConfig(env, user, userLevel, agent)
		},
	}
	command.Flags().StringVar(&user, "user", "", "your user slug: lowercase letters, digits and single hyphens")
	command.Flags().StringVar(&agent, "agent", "", "the agent rein writes settings for; today only claude-code")
	command.Flags().StringVar(&userLevel, "user-level", "", "folder holding global/AGENTS.md and skills/, placed into the Claude home")
	return command
}

func runConfig(env *environment, user string, userLevel string, agent string) error {
	home, err := local.Home()
	if err != nil {
		return crashedError{cause: err}
	}
	config, _, err := local.LoadConfig(home)
	if err != nil {
		return crashedError{cause: err}
	}
	if user == "" || userLevel == "" {
		if !isTerminal(env.in) {
			return fmt.Errorf("no terminal to ask on; pass --user and --user-level")
		}
		asker := &prompter{reader: bufio.NewReader(env.in), out: env.out}
		if user == "" {
			user, err = asker.ask("User slug", config.User)
			if err != nil {
				return err
			}
		}
		if userLevel == "" {
			userLevel, err = asker.ask("User-level source folder (holds global/AGENTS.md and skills/)", config.UserLevelSource)
			if err != nil {
				return err
			}
		}
	}
	if !address.IsSlug(user) {
		return fmt.Errorf("user slug %q may only contain lowercase letters, digits and single hyphens", user)
	}
	if agent == "" {
		agent = config.Agent
	}
	if agent == "" {
		agent = local.AgentClaudeCode
		fmt.Fprintf(env.out, "agent: %s, the only agent this build writes for\n", agent)
	}
	if !slices.Contains(local.SupportedAgents, agent) {
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
	fmt.Fprintf(env.out, "configured %s for user %s\n", home, user)
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

// isTerminal reports whether the input is an interactive terminal, so a
// command may ask instead of refusing.
func isTerminal(in io.Reader) bool {
	file, ok := in.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

type prompter struct {
	reader *bufio.Reader
	out    io.Writer
}

// ask prints a label with a suggestion and reads one line; an empty line
// takes the suggestion, and is refused when there is none.
func (asker *prompter) ask(label string, suggestion string) (string, error) {
	if suggestion != "" {
		fmt.Fprintf(asker.out, "%s [%s]: ", label, suggestion)
	} else {
		fmt.Fprintf(asker.out, "%s: ", label)
	}
	line, err := asker.reader.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("%s: no answer", label)
	}
	answer := strings.TrimSpace(line)
	if answer == "" {
		if suggestion == "" {
			return "", fmt.Errorf("%s is required", label)
		}
		return suggestion, nil
	}
	return answer, nil
}
