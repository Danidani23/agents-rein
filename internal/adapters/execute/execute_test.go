package execute

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary stand in for a program a runner runs: with
// REIN_EXECUTE_HELPER set it prints REIN_EXECUTE_VALUE or waits a minute.
func TestMain(tests *testing.M) {
	switch os.Getenv("REIN_EXECUTE_HELPER") {
	case "print":
		fmt.Print(os.Getenv("REIN_EXECUTE_VALUE"))
		os.Exit(0)
	case "wait":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(tests.Run())
}

// helperFolder is a folder holding the test binary as the program
// rein-helper, which no PATH of this process names.
func helperFolder(t *testing.T) string {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatalf("executable: %v", err)
	}
	content, err := os.ReadFile(binary)
	if err != nil {
		t.Fatalf("read the test binary: %v", err)
	}
	folder := t.TempDir()
	name := "rein-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	err = os.WriteFile(filepath.Join(folder, name), content, 0o755)
	if err != nil {
		t.Fatalf("write the helper: %v", err)
	}
	return folder
}

// callerEnvironment is this process's environment with the PATH replaced
// by folder and the given variables added: a caller other than this
// process.
func callerEnvironment(folder string, variables ...string) []string {
	var environment []string
	for _, pair := range os.Environ() {
		if !strings.EqualFold(strings.SplitN(pair, "=", 2)[0], "PATH") {
			environment = append(environment, pair)
		}
	}
	environment = append(environment, "PATH="+folder)
	return append(environment, variables...)
}

func TestStartDetached(t *testing.T) {
	folder := helperFolder(t)
	log := filepath.Join(t.TempDir(), "helper.log")
	runner := NewRunner(callerEnvironment(folder, "REIN_EXECUTE_HELPER=print", "REIN_EXECUTE_VALUE=written after the start returned"))
	err := runner.StartDetached(t.TempDir(), log, "rein-helper")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		content, _ := os.ReadFile(log)
		if string(content) == "written after the start returned" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the log holds %q", content)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRunner(t *testing.T) {
	t.Run("a caller's runner gives its programs the caller's environment, found on the caller's PATH", func(t *testing.T) {
		runner := NewRunner(callerEnvironment(helperFolder(t), "REIN_EXECUTE_HELPER=print", "REIN_EXECUTE_VALUE=the caller's"))
		result, err := runner.Run(t.TempDir(), "rein-helper")
		if err != nil || result.Stdout != "the caller's" {
			t.Fatalf("result %+v err %v", result, err)
		}
	})
	t.Run("preflight looks on the caller's PATH, not on this process's", func(t *testing.T) {
		runner := NewRunner(callerEnvironment(helperFolder(t)))
		err := runner.Preflight("rein-helper")
		if err != nil {
			t.Fatalf("the caller's preflight: %v", err)
		}
		if Preflight("rein-helper") == nil {
			t.Fatalf("this process's preflight found a program only the caller's PATH holds")
		}
	})
	t.Run("interrupting one caller stops its program and leaves another caller's running", func(t *testing.T) {
		folder := helperFolder(t)
		first := NewRunner(callerEnvironment(folder, "REIN_EXECUTE_HELPER=wait"))
		second := NewRunner(callerEnvironment(folder, "REIN_EXECUTE_HELPER=wait"))
		firstDone := make(chan error, 1)
		secondDone := make(chan error, 1)
		go func() {
			_, err := first.RunWith(Options{Timeout: time.Minute}, "rein-helper")
			firstDone <- err
		}()
		go func() {
			_, err := second.RunWith(Options{Timeout: time.Minute}, "rein-helper")
			secondDone <- err
		}()
		deadline := time.Now().Add(10 * time.Second)
		for !first.Running() || !second.Running() {
			if time.Now().After(deadline) {
				t.Fatalf("the programs did not start")
			}
			time.Sleep(10 * time.Millisecond)
		}
		first.Interrupt()
		select {
		case err := <-firstDone:
			if !errors.Is(err, ErrInterrupted) {
				t.Fatalf("the first ended with %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("the first program was not stopped")
		}
		if !second.Running() || Running() {
			t.Fatalf("second running %v, this process's runner running %v", second.Running(), Running())
		}
		second.Interrupt()
		<-secondDone
	})
}

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
