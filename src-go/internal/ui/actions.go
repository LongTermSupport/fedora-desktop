package ui

import (
	"errors"
	"fmt"
	"strings"
)

// Action is a visible, named choice (CLAUDE/InteractiveScripts.md rule 14): Label is the words it
// is listed under, Key an optional shortcut (an fzf key name: "ctrl-e", "tab", "?") that chooses
// it from the list as well and is printed beside the Label, and ID is what Picked.Action carries
// back. A key exists in a picker only as the Key of an Action, so no key is the only way to its
// action; the project rule project_hidden_bindings holds that (CLAUDE/GoProjectRules.md).
type Action struct {
	ID, Label, Key string
}

// Line is the action as it is listed: its words, with its key beside them.
func (a Action) Line() string {
	if a.Key == "" {
		return a.Label
	}
	return a.Label + "   (" + KeyName(a.Key) + ")"
}

// Menu is what Enter on a row offers: the row's actions, listed under Title and Header, with a
// last row that goes back to the list. A key of one of them chooses it from the list too.
type Menu struct {
	Title, Header string
	Actions       []Action
}

// menuBack is the last row of every row menu.
const menuBack = "Back to the list"

// KeyName is a key as a person reads it: "ctrl-e" is "Ctrl-E", "tab" is "Tab", "?" is "?".
func KeyName(key string) string {
	parts := strings.Split(key, "-")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "-")
}

// ErrBadAction is an action a picker cannot show: no words, no ID, a key it already uses for
// something else, or a key that is navigation.
var ErrBadAction = errors.New("picker: an action that cannot be shown")

// ErrBack is leaving a row's menu: nothing was chosen, and the list comes back.
var ErrBack = errors.New("picker: back to the list")

// NotOfferedError is a key pressed where the screen and the row offer nothing for it.
type NotOfferedError struct{ Key string }

func (e *NotOfferedError) Error() string {
	return KeyName(e.Key) + " does nothing here; Enter lists what can be done, each with its key"
}

// navigationKeys move, choose or leave in every picker, so no action may take them.
var navigationKeys = map[string]bool{
	"enter": true, "esc": true, "q": true, "ctrl-c": true, "ctrl-m": true, "up": true, "down": true,
}

// checkActions refuses actions that cannot be shown, and keys that would choose two things.
func (s PickSpec) checkActions() error {
	if len(s.Menus) != 0 && len(s.Menus) != len(s.Rows) {
		return fmt.Errorf("%w: %d menus for %d rows", ErrBadAction, len(s.Menus), len(s.Rows))
	}
	if n := len(s.RowHeaders); n != 0 && len(s.Actions) != 0 && n != len(s.Rows) && n != len(s.Rows)+len(s.Actions) {
		return fmt.Errorf("%w: %d row headers for %d rows and %d actions", ErrBadAction, n, len(s.Rows), len(s.Actions))
	}
	screen := map[string]bool{}
	if err := checkGroup(s.Actions, screen); err != nil {
		return err
	}
	for _, m := range s.Menus {
		taken := map[string]bool{}
		for k := range screen {
			taken[k] = true
		}
		if err := checkGroup(m.Actions, taken); err != nil {
			return err
		}
	}
	return nil
}

// checkGroup checks one list of actions shown together, adding their keys to taken.
func checkGroup(acts []Action, taken map[string]bool) error {
	for _, a := range acts {
		switch {
		case a.Label == "":
			return fmt.Errorf("%w: %q has no words to be listed under", ErrBadAction, a.ID)
		case a.ID == "":
			return fmt.Errorf("%w: %q has no ID", ErrBadAction, a.Label)
		case a.Key == "":
			continue
		case navigationKeys[a.Key]:
			return fmt.Errorf("%w: %s moves, chooses or leaves; it cannot be %q", ErrBadAction, KeyName(a.Key), a.Label)
		case taken[a.Key]:
			return fmt.Errorf("%w: %s would choose two things, one of them %q", ErrBadAction, KeyName(a.Key), a.Label)
		}
		taken[a.Key] = true
	}
	return nil
}

// keys is every key the list takes, in the order they are declared: the screen's, then the
// rows' menus'.
func (s PickSpec) keys() []string {
	var keys []string
	seen := map[string]bool{}
	add := func(acts []Action) {
		for _, a := range acts {
			if a.Key != "" && !seen[a.Key] {
				seen[a.Key] = true
				keys = append(keys, a.Key)
			}
		}
	}
	add(s.Actions)
	for _, m := range s.Menus {
		add(m.Actions)
	}
	return keys
}

// expectArg is fzf's --expect for keys: the one place it is built, from Action keys only
// (project_hidden_bindings allows it nowhere else).
func expectArg(keys []string) string {
	return "--expect=" + strings.Join(keys, ",")
}

// byKey is the action of acts whose key is key.
func byKey(acts []Action, key string) (Action, bool) {
	for _, a := range acts {
		if a.Key == key {
			return a, true
		}
	}
	return Action{}, false
}
