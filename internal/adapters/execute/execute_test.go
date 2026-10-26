package execute

import (
	"errors"
	"strings"
	"testing"
)

func TestRunWithLines(t *testing.T) {
	var lines []string
	result, err := RunWith(Options{Directory: t.TempDir(), Lines: func(line string) { lines = append(lines, line) }}, "git", "--version")
	if err != nil || len(lines) != 1 || !strings.HasPrefix(lines[0], "git version") || !strings.HasPrefix(result.Stdout, "git version") {
		t.Fatalf("lines %q result %+v err %v", lines, result, err)
	}
	// a line written in pieces comes whole, and the last one without a newline too
	lines = nil
	writer := &lineWriter{each: func(line string) { lines = append(lines, line) }}
	_, _ = writer.Write([]byte("one\ntw"))
	_, _ = writer.Write([]byte("o\nthree"))
	writer.flush()
	if strings.Join(lines, "|") != "one|two|three" {
		t.Fatalf("lines %q", lines)
	}
}

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
		err := Preflight("rein-no-such-program")
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
}
