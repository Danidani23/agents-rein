package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/exchange"
	"github.com/Danidani23/agents-rein/internal/lint"
	"github.com/Danidani23/agents-rein/internal/local"
	"github.com/Danidani23/agents-rein/internal/placement"
	"github.com/Danidani23/agents-rein/internal/version"
)

func newExportCommand(env *environment) *cobra.Command {
	var out string
	command := &cobra.Command{
		Use:   "export <folder>",
		Short: "Write a folder of agent settings as a zip in the rein exchange format",
		Long: "export reads a folder holding one level, in the declarations layout (rules/<id>.md and\n" +
			"skills/<id>/) or the toolkit layout (global/AGENTS.md and skills/<id>/), and writes it as\n" +
			"a zip with a manifest that hashes every file. The layout is rein's own, not any agent's:\n" +
			"someone with rein runs rein import on it, whatever agent they use. Once your organisation's\n" +
			"repository exists, export without a folder will export your user level from it.",
		Example: "  rein export ./toolkit\n  rein export ./toolkit --out settings.zip",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			return runExport(env, arguments[0], out)
		},
	}
	command.Flags().StringVar(&out, "out", "", "where to write the zip; default rein-export-<user>-<date>.zip in the current directory")
	return command
}

func runExport(env *environment, folder string, out string) error {
	_, who, err := requireIdentity()
	if err != nil {
		return err
	}
	entries, err := exchange.FromFolder(folder)
	if err != nil {
		return err
	}
	absoluteFolder, err := filepath.Abs(folder)
	if err != nil {
		return crashedError{cause: err}
	}
	now := time.Now().UTC()
	if out == "" {
		out = fmt.Sprintf("rein-export-%s-%s.zip", who.Slug, now.Format("20060102"))
	}
	manifest := exchange.Manifest{
		Kind:        "user",
		User:        who.Slug,
		Source:      absoluteFolder,
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
		Use:   "import <zip or folder>",
		Short: "Place a level of agent settings into your agent's home",
		Long: "import takes a rein export zip, or a folder in the declarations layout (rules/<id>.md and\n" +
			"skills/<id>/) or the toolkit layout (global/AGENTS.md and skills/<id>/). It copies the\n" +
			"level under ~/.rein/imports/<hash>/, lints it, and places it for every supported agent\n" +
			"found on this machine: for Claude Code, rules into ~/.claude/rules/ and skills into\n" +
			"~/.claude/skills/, as links on macOS and Linux and copies on Windows, each recorded so\n" +
			"rein reset can remove it. It never replaces a file it did not place itself. Importing the\n" +
			"same folder again after an edit refreshes it. Until your organisation's repository\n" +
			"exists, this is how your own instructions and skills reach the agent.",
		Example: "  rein import ./toolkit\n  rein import settings.zip",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			return runImport(env, arguments[0])
		},
	}
}

func runImport(env *environment, source string) error {
	home, _, err := requireIdentity()
	if err != nil {
		return err
	}
	agents := installedAgents()
	if len(agents) == 0 {
		return fmt.Errorf("no supported agent found on this machine; this build writes for %s, whose command is claude", local.AgentClaudeCode)
	}
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("%s: %v", source, err)
	}
	var manifest exchange.Manifest
	var entries []exchange.Entry
	// origin identifies where a level came from: the folder on this machine,
	// or the exporter and their folder for a zip. It decides what a later
	// import may replace.
	var origin string
	if info.IsDir() {
		entries, err = exchange.FromFolder(source)
		if err != nil {
			return err
		}
		manifest.Source, _ = filepath.Abs(source)
		origin = manifest.Source
	} else {
		manifest, entries, err = exchange.Read(source)
		if err != nil {
			return err
		}
		origin = manifest.User + "@" + manifest.Source
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
		return fmt.Errorf("the level has %d lint errors; nothing was placed", report.Errors())
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
	for _, outcome := range placement.Apply(pairs, placement.ModeForSystem(), state.OwnedBy(local.OwnerImport, origin)) {
		if outcome.Status == placement.StatusRefused {
			refused++
			fmt.Fprintf(env.errOut, "refused  %s: %s\n", outcome.Path, outcome.Reason)
			continue
		}
		fmt.Fprintf(env.out, "%-8s %s -> %s\n", outcome.Status, outcome.Path, outcome.Target)
		state.Placements = recordPlacement(state.Placements, local.Placement{Path: outcome.Path, Target: outcome.Target, Mode: string(outcome.Mode), Owner: local.OwnerImport, Source: origin})
	}
	state.Imports = recordImport(state.Imports, local.Import{Hash: hash, Path: directory, FromUser: manifest.User, Source: manifest.Source, Imported: time.Now().UTC().Format(time.RFC3339)})
	err = local.SaveState(home, state)
	if err != nil {
		return crashedError{cause: err}
	}
	rules, skills := exchange.Count(entries)
	fmt.Fprintf(env.out, "imported %s: %d rules, %d skills, for %s\n", hash[:12], rules, skills, agents[0])
	if refused > 0 {
		return fmt.Errorf("%d targets refused; move them aside and run rein import again", refused)
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

func recordImport(imports []local.Import, imported local.Import) []local.Import {
	for index, existing := range imports {
		if existing.Hash == imported.Hash {
			imports[index] = imported
			return imports
		}
	}
	return append(imports, imported)
}
