// Package prompt asks questions in a terminal the same way in every command:
// a title, a one-line description under it, the default in brackets, a
// validator that asks again with the reason instead of failing, and "?" as
// an answer that prints the long explanation. A selector is a numbered list
// answered by number or by name. Without a terminal, a command does not ask;
// it refuses and names its flags.
package prompt

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// The convention every interactive command states once.
const Convention = "Enter keeps the value in brackets; ? explains a question."

// Session asks the questions of one command.
type Session struct {
	reader   *bufio.Reader
	out      io.Writer
	terminal bool
}

// New makes a session over the command's streams. terminal says whether a
// person is typing, which decides styling and whether asking is allowed.
func New(in io.Reader, out io.Writer, terminal bool) *Session {
	return &Session{reader: bufio.NewReader(in), out: out, terminal: terminal}
}

// IsTerminal reports whether in is an interactive terminal.
func IsTerminal(in io.Reader) bool {
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

// CanAsk reports whether the session may ask at all.
func (session *Session) CanAsk() bool {
	return session.terminal
}

// Intro prints what the command does, then the convention.
func (session *Session) Intro(text string) {
	fmt.Fprintf(session.out, "%s\n%s\n\n", text, session.dim(Convention))
}

// Text is a free-text question.
type Text struct {
	Title       string
	Description string
	Explanation string
	Default     string
	// Validate returns the reason an answer is refused, or "".
	Validate func(answer string) string
}

// Ask asks a text question until it has an acceptable answer.
func (session *Session) Ask(question Text) (string, error) {
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

// Choice is one option of a selector.
type Choice struct {
	Name        string
	Description string
	// Unavailable is the reason the choice cannot be picked, or "". Such a
	// choice is listed so people see what is planned, and refused.
	Unavailable string
}

// Select is a question answered by picking one choice.
type Select struct {
	Title       string
	Description string
	Explanation string
	Choices     []Choice
	Default     string
}

// Choose asks a selector until a valid choice is picked, by number or name.
func (session *Session) Choose(question Select) (string, error) {
	defaultIndex := 0
	for index, choice := range question.Choices {
		if choice.Name == question.Default {
			defaultIndex = index + 1
		}
	}
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

// Confirm asks a yes-or-no question; Enter means yes.
func (session *Session) Confirm(question string) (bool, error) {
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
		}
		fmt.Fprintf(session.out, "%s\n", session.dim("Answer y or n."))
	}
}

func pick(choices []Choice, answer string) (Choice, bool) {
	number, err := strconv.Atoi(answer)
	if err == nil {
		if number >= 1 && number <= len(choices) {
			return choices[number-1], true
		}
		return Choice{}, false
	}
	for _, choice := range choices {
		if strings.EqualFold(choice.Name, answer) {
			return choice, true
		}
	}
	return Choice{}, false
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
