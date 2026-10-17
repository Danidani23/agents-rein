package orchestration

import (
	"errors"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	t.Run("steps run in order and nothing is undone when all succeed", func(t *testing.T) {
		var trace []string
		step := func(name string) Step {
			return Step{
				Name: name,
				Do:   func() error { trace = append(trace, "do "+name); return nil },
				Undo: func() error { trace = append(trace, "undo "+name); return nil },
			}
		}
		err := run([]Step{step("a"), step("b"), step("c")})
		if err != nil || strings.Join(trace, ",") != "do a,do b,do c" {
			t.Fatalf("err %v trace %v", err, trace)
		}
	})
	t.Run("a failing step undoes the completed ones in reverse, skipping steps without an undo", func(t *testing.T) {
		var trace []string
		failure := errors.New("c failed")
		steps := []Step{
			{Name: "a", Do: func() error { trace = append(trace, "do a"); return nil }, Undo: func() error { trace = append(trace, "undo a"); return nil }},
			{Name: "b", Do: func() error { trace = append(trace, "do b"); return nil }},
			{Name: "c", Do: func() error { trace = append(trace, "do c"); return failure }, Undo: func() error { trace = append(trace, "undo c"); return nil }},
			{Name: "d", Do: func() error { trace = append(trace, "do d"); return nil }},
		}
		err := run(steps)
		if !errors.Is(err, failure) || strings.Join(trace, ",") != "do a,do b,do c,undo a" {
			t.Fatalf("err %v trace %v", err, trace)
		}
	})
	t.Run("a refusal passes through unchanged so the gateway exits 1", func(t *testing.T) {
		err := run([]Step{{Name: "a", Do: func() error { return Refusal{Reason: "no"} }}})
		if !errors.Is(err, ErrRefused) || err.Error() != "no" {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("a failing undo is reported with the cause and still wraps it", func(t *testing.T) {
		failure := errors.New("b failed")
		steps := []Step{
			{Name: "a", Do: func() error { return nil }, Undo: func() error { return errors.New("cannot undo a") }},
			{Name: "b", Do: func() error { return failure }},
		}
		err := run(steps)
		var compensation *CompensationError
		if !errors.As(err, &compensation) || !errors.Is(err, failure) || !strings.Contains(err.Error(), "a: cannot undo a") {
			t.Fatalf("err %v", err)
		}
	})
}
