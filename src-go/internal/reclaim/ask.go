package reclaim

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// maxTries bounds a value question's re-asks and the menu's invalid choices
// (InteractiveScripts.md rule 2).
const maxTries = 3

// readLine is one line typed at the terminal, trimmed; end of input, even after part of a
// line, is io.EOF.
func (r *run) readLine() (string, error) {
	line, err := r.tty.ReadString('\n')
	if errors.Is(err, io.EOF) {
		return "", io.EOF
	}
	if err != nil {
		return "", fmt.Errorf("read the terminal: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// prompt is a question on stderr, its text bold, then its hint.
func (r *run) prompt(question, hint string) {
	r.errOut.Print(r.env.Stderr, r.paint(styleBold, question)+" "+hint+" ")
}

// confirm is E15: yes only for y or yes in any case; anything else, and end of input, is no.
func (r *run) confirm(question string) bool {
	r.prompt(question, "[y/N]")
	reply, err := r.readLine()
	if err != nil {
		r.say("")
		if !errors.Is(err, io.EOF) {
			r.failf("%v", err)
		}
		return false
	}
	switch strings.ToLower(reply) {
	case "y", "yes":
		return true
	}
	return false
}

// askValue is E16: the answer, or the default on an empty line, asked again while check
// names what is wrong with it, three tries in all; false when the action is to end.
func (r *run) askValue(question, def string, check func(string) string) (string, bool) {
	for range maxTries {
		r.prompt(question, "["+def+"]")
		reply, err := r.readLine()
		if err != nil {
			r.say("")
			if !errors.Is(err, io.EOF) {
				r.failf("%v", err)
			}
			r.warn("  Cancelled.")
			return "", false
		}
		if reply == "" {
			reply = def
		}
		wrong := check(reply)
		if wrong == "" {
			return reply, true
		}
		r.errLine("  " + wrong)
	}
	r.warn(fmt.Sprintf("  Giving up on this action after %d tries.", maxTries))
	return "", false
}
