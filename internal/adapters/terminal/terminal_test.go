package terminal

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/ports"
)

func session(input string) (*Session, *bytes.Buffer) {
	var out bytes.Buffer
	return New(strings.NewReader(input), &out, false), &out
}

func slugOnly(answer string) string {
	if strings.ToLower(answer) != answer {
		return "lowercase only"
	}
	return ""
}

func TestAsk(t *testing.T) {
	t.Run("an answer is returned and the description is shown", func(t *testing.T) {
		s, out := session("dani\n")
		answer, err := s.Ask(ports.Text{Title: "User slug", Description: "Your name inside the tool."})
		if err != nil || answer != "dani" || !strings.Contains(out.String(), "Your name inside the tool.") {
			t.Fatalf("answer %q err %v out %q", answer, err, out.String())
		}
	})
	t.Run("Enter keeps the default", func(t *testing.T) {
		s, out := session("\n")
		answer, err := s.Ask(ports.Text{Title: "User slug", Default: "dani"})
		if err != nil || answer != "dani" || !strings.Contains(out.String(), "[dani]") {
			t.Fatalf("answer %q err %v out %q", answer, err, out.String())
		}
	})
	t.Run("Enter without a default asks again", func(t *testing.T) {
		s, out := session("\ndani\n")
		answer, err := s.Ask(ports.Text{Title: "User slug"})
		if err != nil || answer != "dani" || !strings.Contains(out.String(), "required") {
			t.Fatalf("answer %q err %v out %q", answer, err, out.String())
		}
	})
	t.Run("? prints the explanation and asks again", func(t *testing.T) {
		s, out := session("?\ndani\n")
		answer, err := s.Ask(ports.Text{Title: "User slug", Explanation: "The long story."})
		if err != nil || answer != "dani" || !strings.Contains(out.String(), "The long story.") {
			t.Fatalf("answer %q err %v out %q", answer, err, out.String())
		}
	})
	t.Run("an invalid answer is refused with the reason and asked again", func(t *testing.T) {
		s, out := session("Dani\ndani\n")
		answer, err := s.Ask(ports.Text{Title: "User slug", Validate: slugOnly})
		if err != nil || answer != "dani" || !strings.Contains(out.String(), "lowercase only") {
			t.Fatalf("answer %q err %v out %q", answer, err, out.String())
		}
	})
	t.Run("running out of input names the question", func(t *testing.T) {
		s, _ := session("")
		_, err := s.Ask(ports.Text{Title: "User slug"})
		if err == nil || !strings.Contains(err.Error(), "User slug") {
			t.Fatalf("err %v", err)
		}
	})
}

func agents() []ports.Choice {
	return []ports.Choice{
		{Name: "claude-code", Description: "Claude Code"},
		{Name: "codex", Description: "Codex", Unavailable: "not yet supported"},
	}
}

func TestChoose(t *testing.T) {
	t.Run("a number picks a choice", func(t *testing.T) {
		s, out := session("1\n")
		picked, err := s.Choose(ports.Select{Title: "Agent", Choices: agents(), Default: "claude-code"})
		if err != nil || picked != "claude-code" || !strings.Contains(out.String(), "1) claude-code") || !strings.Contains(out.String(), "(default)") {
			t.Fatalf("picked %q err %v out %q", picked, err, out.String())
		}
	})
	t.Run("a name picks a choice, case-insensitively", func(t *testing.T) {
		s, _ := session("Claude-Code\n")
		picked, err := s.Choose(ports.Select{Title: "Agent", Choices: agents()})
		if err != nil || picked != "claude-code" {
			t.Fatalf("picked %q err %v", picked, err)
		}
	})
	t.Run("Enter picks the default", func(t *testing.T) {
		s, _ := session("\n")
		picked, err := s.Choose(ports.Select{Title: "Agent", Choices: agents(), Default: "claude-code"})
		if err != nil || picked != "claude-code" {
			t.Fatalf("picked %q err %v", picked, err)
		}
	})
	t.Run("an unavailable choice is listed with its reason and refused", func(t *testing.T) {
		s, out := session("2\n1\n")
		picked, err := s.Choose(ports.Select{Title: "Agent", Choices: agents()})
		if err != nil || picked != "claude-code" || !strings.Contains(out.String(), "codex: not yet supported") {
			t.Fatalf("picked %q err %v out %q", picked, err, out.String())
		}
	})
	t.Run("an answer outside the list is refused and asked again", func(t *testing.T) {
		s, out := session("7\nnope\n1\n")
		picked, err := s.Choose(ports.Select{Title: "Agent", Choices: agents()})
		if err != nil || picked != "claude-code" || strings.Count(out.String(), "not one of the choices") != 2 {
			t.Fatalf("picked %q err %v out %q", picked, err, out.String())
		}
	})
	t.Run("? explains and asks again", func(t *testing.T) {
		s, out := session("?\n1\n")
		_, err := s.Choose(ports.Select{Title: "Agent", Choices: agents(), Explanation: "Which agent reads your settings."})
		if err != nil || !strings.Contains(out.String(), "Which agent reads your settings.") {
			t.Fatalf("err %v out %q", err, out.String())
		}
	})
}

func TestConfirm(t *testing.T) {
	cases := map[string]bool{"\n": true, "y\n": true, "Yes\n": true, "n\n": false, "no\n": false, "maybe\nn\n": false}
	for input, expected := range cases {
		s, _ := session(input)
		got, err := s.Confirm("Continue?", "")
		if err != nil || got != expected {
			t.Fatalf("input %q: got %v err %v", input, got, err)
		}
	}
	t.Run("? explains what a yes will do and asks again", func(t *testing.T) {
		s, out := session("?\ny\n")
		got, err := s.Confirm("Continue?", "It deletes the links.")
		if err != nil || !got || !strings.Contains(out.String(), "It deletes the links.") {
			t.Fatalf("got %v err %v out %q", got, err, out.String())
		}
	})
}

func TestStyling(t *testing.T) {
	t.Run("no escape codes without a terminal", func(t *testing.T) {
		s, out := session("x\n")
		s.Intro("Hello.")
		_, _ = s.Ask(ports.Text{Title: "Q", Description: "d"})
		if strings.Contains(out.String(), "\033[") {
			t.Fatalf("escape codes in non-terminal output: %q", out.String())
		}
		for _, rule := range Convention {
			if !strings.Contains(out.String(), "- "+rule) {
				t.Fatalf("intro missing %q: %q", rule, out.String())
			}
		}
		if !strings.Contains(out.String(), "NOTE") {
			t.Fatalf("intro missing the note heading: %q", out.String())
		}
	})
	t.Run("escape codes with a terminal, and the explanation is styled like a description", func(t *testing.T) {
		var out bytes.Buffer
		s := New(strings.NewReader("?\nx\n"), &out, true)
		_, _ = s.Ask(ports.Text{Title: "Q", Description: "d", Explanation: "first line\nsecond line"})
		text := out.String()
		if !strings.Contains(text, "\033[1m") {
			t.Fatalf("no bold in terminal output: %q", text)
		}
		if !strings.Contains(text, "\033[2;3m  d\033[0m") || !strings.Contains(text, "\033[2;3mfirst line\033[0m\n\033[2;3msecond line\033[0m") {
			t.Fatalf("description or explanation not faint italic per line: %q", text)
		}
	})
}

func TestDivider(t *testing.T) {
	t.Run("a divider separates questions, none before the first", func(t *testing.T) {
		s, out := session("a\n1\n\n")
		_, _ = s.Ask(ports.Text{Title: "First"})
		_, _ = s.Choose(ports.Select{Title: "Second", Choices: []ports.Choice{{Name: "one"}}})
		_, _ = s.Confirm("Third?", "")
		text := out.String()
		if strings.Count(text, divider) != 2 || strings.Index(text, divider) < strings.Index(text, "First") {
			t.Fatalf("dividers wrong: %q", text)
		}
	})
}

func TestIsTerminal(t *testing.T) {
	t.Run("the null device is not a terminal", func(t *testing.T) {
		null, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer null.Close()
		if IsTerminal(null) {
			t.Fatalf("the null device counted as a terminal")
		}
	})
	t.Run("a reader that is not a file is not a terminal", func(t *testing.T) {
		if IsTerminal(strings.NewReader("")) {
			t.Fatalf("a strings.Reader counted as a terminal")
		}
	})
}

func TestChooseMany(t *testing.T) {
	teams := ports.Select{Title: "Teams", Choices: []ports.Choice{{Name: "web", Description: "Web"}, {Name: "data", Description: "Data"}, {Name: "ops", Description: "Ops"}}}
	t.Run("numbers and names, separated by spaces or commas, each once", func(t *testing.T) {
		prompt, out := session("1, data web\n")
		picked, err := prompt.ChooseMany(teams)
		if err != nil || strings.Join(picked, "+") != "web+data" || !strings.Contains(out.String(), "Choose one or more") {
			t.Fatalf("picked %v err %v out %q", picked, err, out.String())
		}
	})
	t.Run("Enter takes the defaults, and asks again when there are none", func(t *testing.T) {
		withDefault := teams
		withDefault.Default = "web,ops"
		prompt, out := session("\n")
		picked, err := prompt.ChooseMany(withDefault)
		if err != nil || strings.Join(picked, "+") != "web+ops" || !strings.Contains(out.String(), "[web, ops]") {
			t.Fatalf("picked %v err %v out %q", picked, err, out.String())
		}
		prompt, out = session("\n2\n")
		picked, err = prompt.ChooseMany(teams)
		if err != nil || strings.Join(picked, "+") != "data" || !strings.Contains(out.String(), "At least one choice is required") {
			t.Fatalf("picked %v err %v out %q", picked, err, out.String())
		}
	})
	t.Run("an optional question takes none, and a wrong token asks again", func(t *testing.T) {
		optional := teams
		optional.Optional = true
		prompt, out := session("\n")
		picked, err := prompt.ChooseMany(optional)
		if err != nil || len(picked) != 0 || !strings.Contains(out.String(), "Choose any, or none") {
			t.Fatalf("picked %v err %v out %q", picked, err, out.String())
		}
		prompt, out = session("9\nops\n")
		picked, err = prompt.ChooseMany(teams)
		if err != nil || strings.Join(picked, "+") != "ops" || !strings.Contains(out.String(), "not one of the choices") {
			t.Fatalf("picked %v err %v out %q", picked, err, out.String())
		}
	})
}

func TestConfirmOrEdit(t *testing.T) {
	for input, expected := range map[string]ports.Decision{"\n": ports.DecisionYes, "n\n": ports.DecisionNo, "e\n": ports.DecisionEdit, "x\ny\n": ports.DecisionYes} {
		prompt, out := session(input)
		decision, err := prompt.ConfirmOrEdit("Continue?", "why")
		if err != nil || decision != expected || !strings.Contains(out.String(), "[Y/n/e]") {
			t.Fatalf("%q: decision %q err %v out %q", input, decision, err, out.String())
		}
	}
	prompt, out := session("?\n\n")
	_, _ = prompt.ConfirmOrEdit("Continue?", "why")
	if !strings.Contains(out.String(), "Answer e to go through the questions again") {
		t.Fatalf("out %q", out.String())
	}
	painted := Style{Enabled: true}
	plain := Style{}
	if painted.Green("ok") != "\033[32mok\033[0m" || plain.Red("x") != "x" {
		t.Fatalf("styles %q %q", painted.Green("ok"), plain.Red("x"))
	}
}

func TestAskOptional(t *testing.T) {
	prompt, out := session("\n")
	answer, err := prompt.Ask(ports.Text{Title: "Description", Description: "Enter for none.", Optional: true})
	if err != nil || answer != "" || strings.Contains(out.String(), "An answer is required") {
		t.Fatalf("answer %q err %v out %q", answer, err, out.String())
	}
	prompt, out = session("\nsomething\n")
	answer, err = prompt.Ask(ports.Text{Title: "Name"})
	if err != nil || answer != "something" || !strings.Contains(out.String(), "An answer is required") {
		t.Fatalf("answer %q err %v out %q", answer, err, out.String())
	}
}
