// Package fzfbind reads fzf --bind specifications and judges each binding: a key bound to
// anything but navigation is an action reachable only by that key, which the project rule
// project_hidden_bindings (Go) and the gate qa-hidden-bindings (shell) forbid. Both read
// bindings here, so the two cannot disagree on what navigation is.
//
// Navigation, decided for this project: moving the cursor or the page, selecting and
// deselecting rows (Tab's toggle in a multi picker), editing the query, changing only how
// the same rows are shown (sort order, preview and its window, prompt, header and border
// label, wrapping, tracking), and leaving (Esc, q, abort), which the pickers also offer as a
// visible Exit row. Any key accepting the highlighted row chooses the visible row: without
// --expect (which only ui builds) fzf cannot tell the caller which key it was, so it is
// Enter. fzf events (start, load, focus, change, ...) are not keys: nobody presses them. Every
// other action on a key (become, execute, reload, print, transform-*, an action fzf adds
// later) does something the screen does not show.
package fzfbind

import (
	"errors"
	"fmt"
	"strings"
)

// Binding is one trigger of a --bind specification and the actions chained to it.
type Binding struct {
	Trigger string
	Actions []string
}

// events are fzf's triggers that are not keys.
var events = map[string]bool{
	"start": true, "load": true, "resize": true, "result": true, "change": true, "focus": true,
	"multi": true, "one": true, "zero": true, "backward-eof": true, "jump": true,
	"jump-cancel": true, "click-header": true, "click-footer": true,
}

// navigation is the actions a key may be bound to: see the package doc.
var navigation = setOf(
	// movement
	"up", "down", "first", "last", "top", "page-up", "page-down", "half-page-up",
	"half-page-down", "offset-up", "offset-down", "offset-middle", "pos", "jump",
	// selection
	"toggle", "toggle-up", "toggle-down", "toggle-in", "toggle-out", "toggle-all",
	"select", "deselect", "select-all", "deselect-all", "clear-selection",
	// leaving
	"abort", "cancel", "close",
	// query editing
	"backward-char", "forward-char", "backward-word", "forward-word",
	"backward-delete-char", "backward-delete-char/eof", "backward-kill-word",
	"beginning-of-line", "end-of-line", "delete-char", "delete-char/eof", "kill-line",
	"kill-word", "unix-line-discard", "unix-word-rubout", "yank", "clear-query", "put",
	"next-history", "prev-history",
	// how the same rows are shown
	"toggle-sort", "toggle-preview", "toggle-preview-wrap", "toggle-wrap", "preview-up",
	"preview-down", "preview-page-up", "preview-page-down", "preview-half-page-up",
	"preview-half-page-down", "preview-top", "preview-bottom", "refresh-preview",
	"show-preview", "hide-preview", "clear-screen", "ignore", "toggle-header",
	"change-preview-window", "change-preview", "preview", "change-prompt", "change-header",
	"change-border-label", "track", "untrack", "toggle-track", "toggle-search",
	// choosing the highlighted row: without --expect (which only ui builds) fzf cannot tell the
	// caller which key accepted, so any key that accepts is Enter on the visible row
	"accept", "accept-non-empty",
)

func setOf(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// closers maps each delimiter fzf accepts around an action's argument to its closing one.
var closers = map[byte]byte{
	'(': ')', '[': ']', '{': '}', '<': '>', '~': '~', '!': '!', '@': '@', '#': '#',
	'$': '$', '%': '%', '^': '^', '&': '&', '*': '*', ';': ';', '/': '/', '|': '|',
}

// errUnreadable is a specification this package cannot read as bindings.
var errUnreadable = errors.New("not an fzf binding")

// Read parses spec into its bindings. open is the delimiter of the last action's argument
// when spec does not close it ("" when it does): the text that follows, built at run time, is
// that argument.
func Read(spec string) ([]Binding, string, error) {
	var out []Binding
	rest := spec
	for {
		trigger, after, found := cutTrigger(rest)
		if !found {
			return nil, "", fmt.Errorf("%w: %q has no trigger:action", errUnreadable, spec)
		}
		b := Binding{Trigger: trigger}
		rest = after
		for {
			name, tail := cutAction(rest)
			if name == "" {
				return nil, "", fmt.Errorf("%w: %q has no action after %q", errUnreadable, spec, trigger)
			}
			b.Actions = append(b.Actions, name)
			arg := skipArgument(tail)
			if arg.open != "" || arg.whole || arg.rest == "" {
				return append(out, b), arg.open, nil
			}
			sep := arg.rest[0]
			rest = arg.rest[1:]
			if sep == ',' {
				break
			}
		}
		out = append(out, b)
		if rest == "" {
			return nil, "", fmt.Errorf("%w: %q ends with a comma", errUnreadable, spec)
		}
	}
}

// cutTrigger reads "trigger:" from the front of s. A trigger is never empty, so a ':' or ','
// that starts s is the key itself.
func cutTrigger(s string) (string, string, bool) {
	if len(s) < 2 {
		return "", "", false
	}
	i := strings.IndexByte(s[1:], ':')
	if i < 0 {
		return "", "", false
	}
	return s[:i+1], s[i+2:], true
}

// cutAction reads an action name, [a-z][a-z0-9-]* with an optional "/eof", from the front of
// s, and returns it and what follows.
func cutAction(s string) (string, string) {
	i := 0
	for i < len(s) && (s[i] >= 'a' && s[i] <= 'z' || i > 0 && (s[i] >= '0' && s[i] <= '9' || s[i] == '-')) {
		i++
	}
	if i == 0 {
		return "", s
	}
	if strings.HasPrefix(s[i:], "/eof") {
		i += len("/eof")
	}
	return s[:i], s[i:]
}

// argument is what follows an action: the rest after a closed argument, the delimiter of one
// left open, or whole for the "action:argument" form, whose argument is the rest of the
// specification.
type argument struct {
	rest  string
	open  string
	whole bool
}

// skipArgument steps over an action's argument at the front of s.
func skipArgument(s string) argument {
	if s == "" {
		return argument{}
	}
	closer, delimited := closers[s[0]]
	switch {
	case s[0] == ',' || s[0] == '+':
		return argument{rest: s}
	case !delimited:
		// ':' and anything else that is not a delimiter: the rest is the argument.
		return argument{whole: true}
	}
	end := strings.IndexByte(s[1:], closer)
	if end < 0 {
		return argument{open: s[:1]}
	}
	return argument{rest: s[end+2:]}
}

// ClosesThenBinds is whether text, read inside an argument opened with delimiter, closes it
// and goes on to another binding (",") or action ("+"): the way to hide a binding after an
// argument built at run time.
func ClosesThenBinds(delimiter, text string) bool {
	if delimiter == "" {
		return false
	}
	closer := string(closers[delimiter[0]])
	for i := range len(text) {
		if strings.HasPrefix(text[i:], closer) && i+1 < len(text) && (text[i+1] == ',' || text[i+1] == '+') {
			return true
		}
	}
	return false
}

// HiddenAction is the first action of b that a key does without the screen showing it, or ""
// when b is an event or every action is navigation.
func HiddenAction(b Binding) string {
	if events[b.Trigger] {
		return ""
	}
	for _, a := range b.Actions {
		if navigation[a] {
			continue
		}
		return a
	}
	return ""
}
