// Package terminal asks questions in a terminal the same way in every command:
// a title, a one-line description under it, the default in brackets, a
// validator that asks again with the reason instead of failing, and "?" as
// an answer that prints the long explanation. A selector is a numbered list
// answered by number or by name. Without a terminal, a command does not ask;
// it refuses and names its flags.
package terminal

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/Danidani23/agents-rein/internal/ports"
)

// The convention every interactive command states once, as a note under
// its intro: two lines, each its own rule.
var Convention = []string{"Enter keeps the value in brackets", "? explains a question"}

// divider separates one question from the next.
const divider = "────────────────────────────────────────"

// Session asks the questions of one command. It is the Prompter port.
type Session struct {
	reader   *bufio.Reader
	out      io.Writer
	terminal bool
	asked    int
}

var _ ports.Prompter = (*Session)(nil)

// New makes a session over the command's streams. terminal says whether a
// person is typing, which decides styling and whether asking is allowed.
func New(in io.Reader, out io.Writer, terminal bool) *Session {
	return &Session{reader: bufio.NewReader(in), out: out, terminal: terminal}
}

// IsTerminal reports whether in is an interactive terminal: a character
// device that is not the null device, which is a character device too and
// the usual way to run a command with no input.
func IsTerminal(in io.Reader) bool {
	file, ok := in.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	if err == nil && os.SameFile(info, null) {
		return false
	}
	return true
}

// CanAsk reports whether the session may ask at all.
func (session *Session) CanAsk() bool {
	return session.terminal
}

// Intro prints what the command does, then the convention as a note.
func (session *Session) Intro(text string) {
	fmt.Fprintf(session.out, "%s\n\n%s\n", text, session.underline("NOTE"))
	for _, rule := range Convention {
		fmt.Fprintf(session.out, "%s\n", session.dim("- "+rule))
	}
	fmt.Fprintln(session.out)
}

// separate prints a divider before every question but the first.
func (session *Session) separate() {
	if session.asked > 0 {
		fmt.Fprintf(session.out, "%s\n", session.dim(divider))
	}
	session.asked++
}

// Ask asks a text question until it has an acceptable answer.
func (session *Session) Ask(question ports.Text) (string, error) {
	session.separate()
	for {
		session.printHeading(question.Title, question.Description)
		if question.Default != "" {
			fmt.Fprintf(session.out, "%s [%s]: ", session.bold(question.Title), question.Default)
		} else {
			fmt.Fprintf(session.out, "%s: ", session.bold(question.Title))
		}
		answer, err := session.readLine(question.Title)
		if err != nil {
			return "", err
		}
		if answer == "?" {
			session.explain(question.Explanation)
			continue
		}
		if answer == "" {
			if question.Default == "" {
				fmt.Fprintf(session.out, "%s\n\n", session.dim("An answer is required."))
				continue
			}
			answer = question.Default
		}
		if question.Validate != nil {
			reason := question.Validate(answer)
			if reason != "" {
				fmt.Fprintf(session.out, "%s\n\n", session.dim(reason))
				continue
			}
		}
		fmt.Fprintln(session.out)
		return answer, nil
	}
}

// Choose asks a selector until a valid choice is picked, by number or name.
func (session *Session) Choose(question ports.Select) (string, error) {
	defaultIndex := 0
	for index, choice := range question.Choices {
		if choice.Name == question.Default {
			defaultIndex = index + 1
		}
	}
	session.separate()
	for {
		session.printHeading(question.Title, question.Description)
		for index, choice := range question.Choices {
			line := fmt.Sprintf("  %d) %s", index+1, choice.Name)
			if choice.Description != "" {
				line += "  " + session.dim(choice.Description)
			}
			switch {
			case choice.Unavailable != "":
				line += "  " + session.dim(choice.Unavailable)
			case index+1 == defaultIndex:
				line += "  " + session.dim("(default)")
			}
			fmt.Fprintln(session.out, line)
		}
		if defaultIndex > 0 {
			fmt.Fprintf(session.out, "%s [%d]: ", session.bold("Choose"), defaultIndex)
		} else {
			fmt.Fprintf(session.out, "%s: ", session.bold("Choose"))
		}
		answer, err := session.readLine(question.Title)
		if err != nil {
			return "", err
		}
		if answer == "?" {
			session.explain(question.Explanation)
			continue
		}
		if answer == "" {
			if defaultIndex == 0 {
				fmt.Fprintf(session.out, "%s\n\n", session.dim("A choice is required."))
				continue
			}
			answer = strconv.Itoa(defaultIndex)
		}
		picked, found := pick(question.Choices, answer)
		if !found {
			fmt.Fprintf(session.out, "%s\n\n", session.dim(fmt.Sprintf("%q is not one of the choices; answer with a number or a name.", answer)))
			continue
		}
		if picked.Unavailable != "" {
			fmt.Fprintf(session.out, "%s\n\n", session.dim(fmt.Sprintf("%s: %s", picked.Name, picked.Unavailable)))
			continue
		}
		fmt.Fprintln(session.out)
		return picked.Name, nil
	}
}

// Confirm asks a yes-or-no question; Enter means yes, and ? prints the
// explanation of what a yes will do.
func (session *Session) Confirm(question string, explanation string) (bool, error) {
	session.separate()
	for {
		fmt.Fprintf(session.out, "%s [Y/n]: ", session.bold(question))
		answer, err := session.readLine(question)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		case "?":
			session.explain(explanation)
			continue
		}
		fmt.Fprintf(session.out, "%s\n", session.dim("Answer y or n; ? explains."))
	}
}

func pick(choices []ports.Choice, answer string) (ports.Choice, bool) {
	number, err := strconv.Atoi(answer)
	if err == nil {
		if number >= 1 && number <= len(choices) {
			return choices[number-1], true
		}
		return ports.Choice{}, false
	}
	for _, choice := range choices {
		if strings.EqualFold(choice.Name, answer) {
			return choice, true
		}
	}
	return ports.Choice{}, false
}

func (session *Session) printHeading(title string, description string) {
	if description != "" {
		fmt.Fprintf(session.out, "%s\n", session.dim("  "+description))
	}
}

func (session *Session) explain(explanation string) {
	if explanation == "" {
		explanation = "No further explanation for this question."
	}
	fmt.Fprintf(session.out, "\n%s\n\n", session.dim(explanation))
}

// readLine reads one answer; end of input without an answer is an error
// that names the question, so a script that runs out of answers fails
// clearly.
func (session *Session) readLine(title string) (string, error) {
	line, err := session.reader.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("%s: no answer given", title)
	}
	return strings.TrimSpace(line), nil
}

// dim styles the explanatory text, descriptions and explanations alike, as
// faint italic. Faint renders everywhere; italic depends on the terminal and
// is ignored where it is not supported. A multi-line text is styled per
// line, since some terminals drop the style at a line break.
func (session *Session) dim(text string) string {
	if !session.terminal {
		return text
	}
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = "\033[2;3m" + line + "\033[0m"
	}
	return strings.Join(lines, "\n")
}

func (session *Session) bold(text string) string {
	if !session.terminal {
		return text
	}
	return "\033[1m" + text + "\033[0m"
}

// underline styles a note heading: faint italic underlined.
func (session *Session) underline(text string) string {
	if !session.terminal {
		return text
	}
	return "\033[2;3;4m" + text + "\033[0m"
}
