package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Danidani23/reins/internal/exchange"
	"github.com/Danidani23/reins/internal/lint"
	"github.com/Danidani23/reins/internal/local"
	"github.com/Danidani23/reins/internal/placement"
	"github.com/Danidani23/reins/internal/version"
)

func newExportCommand(env *environment) *cobra.Command {
	var out string
	command := &cobra.Command{
		Use:   "export",
		Short: "Write your agent settings as a zip in the reins exchange format",
		Long: "export writes one level in the declarations layout, rules/<id>.md and skills/<id>/, with a\n" +
			"manifest that hashes every file. Today it exports your user level; inside a registered\n" +
			"project it will export everything the agent sees there. Someone with reins runs\n" +
			"reins import on the zip; the layout is reins's own, not any agent's.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return runExport(env, out)
		},
	}
	command.Flags().StringVar(&out, "out", "", "where to write the zip; default reins-export-<user>-<date>.zip in the current directory")
	return command
}

func runExport(env *environment, out string) error {
	_, config, err := requireConfig()
	if err != nil {
		return err
	}
	if config.UserLevelSource == "" {
		return fmt.Errorf("no user level to export; run reins config with --user-level")
	}
	entries, err := exchange.FromUserLevelSource(config.UserLevelSource)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if out == "" {
		out = fmt.Sprintf("reins-export-%s-%s.zip", config.User, now.Format("20060102"))
	}
	manifest := exchange.Manifest{
		Kind:        "user",
		User:        config.User,
		Source:      config.UserLevelSource,
		Created:     now.Format(time.RFC3339),
		ToolVersion: version.String(),
	}
	err = exchange.Write(out, manifest, entries)
	if err != nil {
		return crashedError{cause: err}
	}
	absoluteOut, err := filepath.Abs(out)
	if err != nil {
		absoluteOut = out
	}
	rules, skills := exchange.Count(entries)
	fmt.Fprintf(env.out, "wrote %s: %d rules, %d skills, hash %s\n", absoluteOut, rules, skills, exchange.Hash(entries)[:12])
	return nil
}

func newImportCommand(env *environment) *cobra.Command {
	return &cobra.Command{
		Use:   "import <zip>",
		Short: "Place an exported level into your agent's settings",
		Long: "import checks the manifest and every hash, unpacks the level under the reins home, lints\n" +
			"it, and places it for the agent named in your config: for Claude Code, rules into\n" +
			"~/.claude/rules/ and skills into ~/.claude/skills/, as links on macOS and Linux and copies\n" +
			"on Windows, every placement recorded so reins reset can remove it. It never replaces a\n" +
			"file it did not place itself.",
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			return runImport(env, arguments[0])
		},
	}
}

func runImport(env *environment, zipPath string) error {
	home, config, err := requireConfig()
	if err != nil {
		return err
	}
	if config.Agent != local.AgentClaudeCode {
		return fmt.Errorf("agent %q is not supported by this build; it writes for %s", config.Agent, local.AgentClaudeCode)
	}
	manifest, entries, err := exchange.Read(zipPath)
	if err != nil {
		return err
	}
	hash := exchange.Hash(entries)
	directory := filepath.Join(home, "imports", hash[:12])
	err = os.RemoveAll(directory)
	if err != nil {
		return crashedError{cause: err}
	}
	err = exchange.Unpack(directory, entries)
	if err != nil {
		return crashedError{cause: err}
	}
	report, err := lint.Run(directory)
	if err != nil {
		return crashedError{cause: err}
	}
	for _, finding := range report.Findings {
		fmt.Fprintf(env.errOut, "%s: %s: %s\n", finding.Path, finding.Severity, finding.Message)
	}
	if report.Errors() > 0 {
		os.RemoveAll(directory)
		return fmt.Errorf("the export has %d lint errors; nothing was placed", report.Errors())
	}
	claudeHome, err := placement.ClaudeHome()
	if err != nil {
		return crashedError{cause: err}
	}
	pairs, err := placement.PlanLevel(directory, claudeHome)
	if err != nil {
		return err
	}
	state, err := local.LoadState(home)
	if err != nil {
		return crashedError{cause: err}
	}
	refused := 0
	for _, outcome := range placement.Apply(pairs, placement.ModeForSystem(), state.OwnedBy(local.OwnerImport)) {
		if outcome.Status == placement.StatusRefused {
			refused++
			fmt.Fprintf(env.errOut, "refused  %s: %s\n", outcome.Path, outcome.Reason)
			continue
		}
		fmt.Fprintf(env.out, "%-8s %s -> %s\n", outcome.Status, outcome.Path, outcome.Target)
		state.Placements = recordPlacement(state.Placements, local.Placement{Path: outcome.Path, Target: outcome.Target, Mode: string(outcome.Mode), Owner: local.OwnerImport})
	}
	state.Imports = recordImport(state.Imports, local.Import{Hash: hash, Path: directory, FromUser: manifest.User, Imported: time.Now().UTC().Format(time.RFC3339)})
	err = local.SaveState(home, state)
	if err != nil {
		return crashedError{cause: err}
	}
	rules, skills := exchange.Count(entries)
	fmt.Fprintf(env.out, "imported %s from %s: %d rules, %d skills, for %s\n", hash[:12], manifest.User, rules, skills, config.Agent)
	if refused > 0 {
		return fmt.Errorf("%d targets refused; move them aside and run reins import again", refused)
	}
	return nil
}

func recordImport(imports []local.Import, imported local.Import) []local.Import {
	for index, existing := range imports {
		if existing.Hash == imported.Hash {
			imports[index] = imported
			return imports
		}
	}
	return append(imports, imported)
}
