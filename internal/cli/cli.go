// Package cli wires the reins commands. Every byte of output goes through the
// writers Run receives; nothing in this module writes to the process streams
// directly except main, and a test enforces it, so a whole run can be captured.
package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Danidani23/ai-assisted-dev-tool/internal/lint"
)

// Exit codes: 0 worked, 1 refused (a precondition or a check failed), 2
// crashed (an error the command did not expect).
const (
	ExitWorked  = 0
	ExitRefused = 1
	ExitCrashed = 2
)

// crashedError marks an error the command did not expect, such as an
// unreadable directory; every other error is a refusal.
type crashedError struct {
	cause error
}

func (crashed crashedError) Error() string {
	return crashed.cause.Error()
}

// Run executes reins with arguments and returns the exit code.
func Run(arguments []string, out io.Writer, errOut io.Writer) int {
	root := newRootCommand(out, errOut)
	root.SetArgs(arguments)
	err := root.Execute()
	if err == nil {
		return ExitWorked
	}
	fmt.Fprintf(errOut, "reins: %v\n", err)
	var crashed crashedError
	if errors.As(err, &crashed) {
		return ExitCrashed
	}
	return ExitRefused
}

func newRootCommand(out io.Writer, errOut io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "reins",
		Short: "Compile and lint the agent harness of an organisation",
		Long: "reins turns the declarations of an org repository into the files each agent tool\n" +
			"reads, and checks them against the specifications. It is a compiler and a linter,\n" +
			"never a service.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(newLintCommand(out))
	return root
}

func newLintCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "lint <path>",
		Short: "Check the skills, rules and settings under a path",
		Long: "lint walks the path and checks every SKILL.md against the Agent Skills specification,\n" +
			"every rule file's frontmatter, every settings file's permission rules, and the skills\n" +
			"that share a name. Exit 1 when it finds an error, 0 when only warnings or nothing.",
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			report, err := lint.Run(arguments[0])
			if err != nil {
				return crashedError{cause: err}
			}
			for _, finding := range report.Findings {
				location := finding.Path
				if finding.Address != "" {
					location += " (" + finding.Address + ")"
				}
				fmt.Fprintf(out, "%s: %s: %s\n", location, finding.Severity, finding.Message)
			}
			fmt.Fprintf(out, "%d errors, %d warnings\n", report.Errors(), report.Warnings())
			if report.Errors() > 0 {
				return fmt.Errorf("lint found %d errors", report.Errors())
			}
			return nil
		},
	}
}
