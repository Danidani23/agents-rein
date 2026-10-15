package execute

import (
	"errors"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	t.Run("a program that works returns its output", func(t *testing.T) {
		result, err := Run(t.TempDir(), "git", "--version")
		if err != nil || !strings.HasPrefix(result.Stdout, "git version") {
			t.Fatalf("result %+v err %v", result, err)
		}
	})
	t.Run("a program that fails is a Failure naming the command", func(t *testing.T) {
		_, err := Run(t.TempDir(), "git", "no-such-subcommand")
		var failure *Failure
		if !errors.As(err, &failure) || !strings.Contains(failure.Command, "git no-such-subcommand") || failure.Stderr == "" {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("preflight refuses a program that is not installed", func(t *testing.T) {
		err := Preflight("reins-no-such-program")
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
}
