package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// ErrInputEnded is the input ending before an answer (Ctrl-D, or no more piped input): the
// person left the question, so it is a cancel, and nothing was changed.
var ErrInputEnded = fmt.Errorf("%w: the input ended", ErrCancelled)

// DefaultTries is how many answers a question takes before it gives up
// (CLAUDE/InteractiveScripts.md rule 2).
const DefaultTries = 3

// GaveUpError is a question that got no valid answer in its tries.
type GaveUpError struct{ Tries int }

func (e *GaveUpError) Error() string { return fmt.Sprintf("no valid answer after %d tries", e.Tries) }

// Choice is one line of a numbered menu: the key typed to choose it, printed beside its label.
type Choice struct {
	Key, Label string
}

// Lines asks plain line questions: a prompt on Out, one line read from In. They are the
// launcher's questions (Plan 00191 decision D5) where gum would be a second dependency and the
// answers can be piped. Each validates strictly and asks again on a bad answer, at most Tries
// times (DefaultTries when zero); the end of input cancels. Every question draws its heading
// first and refuses to ask without one (ErrNoHeading).
type Lines struct {
	In  io.Reader
	Out io.Writer
	// Colour draws the heading's crumbs in colour; Width is the width it wraps to (no limit
	// when zero).
	Colour bool
	Width  int
	Tries  int

	reader *bufio.Reader
}

// YesNo is a [y/N] (or [Y/n]) question: y, yes, n or no in any of their usual cases, or Enter
// for the default.
func (l *Lines) YesNo(h Heading, question string, defaultYes bool) (bool, error) {
	hint := "[y/N]"
	if defaultYes {
		hint = "[Y/n]"
	}
	var answer bool
	err := l.ask(h, "", question+" "+hint+" ", func(reply string) (string, bool) {
		switch reply {
		case "y", "Y", "yes", "Yes", "YES":
			answer = true
		case "n", "N", "no", "No", "NO":
			answer = false
		case "":
			answer = defaultYes
		default:
			return "Answer y or n.", false
		}
		return "", true
	})
	return answer, err
}

// Ask is a line of text that check accepts (any line when check is nil); check's error is
// what the person is shown before the question is asked again.
func (l *Lines) Ask(h Heading, prompt string, check func(string) error) (string, error) {
	var answer string
	err := l.ask(h, "", prompt+": ", func(reply string) (string, bool) {
		if check != nil {
			if err := check(reply); err != nil {
				return err.Error(), false
			}
		}
		answer = reply
		return "", true
	})
	return answer, err
}

// Choose is a numbered menu: each choice on its own line with its key, the key typed back. The
// keys must be distinct and every line labelled.
func (l *Lines) Choose(h Heading, prompt string, choices []Choice) (string, error) {
	if err := checkMenu(choices); err != nil {
		return "", err
	}
	var menu strings.Builder
	keys := make([]string, len(choices))
	for i, c := range choices {
		fmt.Fprintf(&menu, "  %s) %s\n", c.Key, c.Label)
		keys[i] = c.Key
	}
	var answer string
	err := l.ask(h, menu.String(), prompt+": ", func(reply string) (string, bool) {
		if !slices.Contains(keys, reply) {
			return "Choose one of: " + strings.Join(keys, ", ") + ".", false
		}
		answer = reply
		return "", true
	})
	return answer, err
}

func checkMenu(choices []Choice) error {
	if len(choices) == 0 {
		return errors.New("ui: a menu with no choices")
	}
	seen := map[string]bool{}
	for _, c := range choices {
		if c.Key == "" || strings.TrimSpace(c.Label) == "" {
			return fmt.Errorf("ui: a menu line needs a key and a label: %+v", c)
		}
		if seen[c.Key] {
			return fmt.Errorf("ui: the menu key %q is on two lines", c.Key)
		}
		seen[c.Key] = true
	}
	return nil
}

// ask draws the heading and the body once, then asks the prompt until judge accepts a reply,
// showing judge's complaint after each one it refuses.
func (l *Lines) ask(h Heading, body, prompt string, judge func(reply string) (complaint string, ok bool)) error {
	heading, err := h.Draw(l.Colour, l.Width)
	if err != nil {
		return err
	}
	if err := l.write(heading + "\n" + body); err != nil {
		return err
	}
	tries := l.Tries
	if tries <= 0 {
		tries = DefaultTries
	}
	for range tries {
		if err := l.write(prompt); err != nil {
			return err
		}
		reply, err := l.readLine()
		if err != nil {
			return err
		}
		complaint, ok := judge(reply)
		if ok {
			return nil
		}
		if err := l.write("   " + complaint + "\n"); err != nil {
			return err
		}
	}
	return &GaveUpError{Tries: tries}
}

// readLine is one line, trimmed of surrounding spaces. A line the input ends inside is not an
// answer: the end of input cancels.
func (l *Lines) readLine() (string, error) {
	if l.reader == nil {
		l.reader = bufio.NewReader(l.In)
	}
	line, err := l.reader.ReadString('\n')
	switch {
	case errors.Is(err, io.EOF):
		if writeErr := l.write("\n"); writeErr != nil {
			return "", writeErr
		}
		return "", ErrInputEnded
	case err != nil:
		return "", fmt.Errorf("read the answer: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func (l *Lines) write(s string) error {
	if _, err := io.WriteString(l.Out, s); err != nil {
		return fmt.Errorf("write the question: %w", err)
	}
	return nil
}
