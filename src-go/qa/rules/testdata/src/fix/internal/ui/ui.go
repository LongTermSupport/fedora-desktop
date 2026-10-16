// Package ui is the owner of fzf: its one --expect builder takes the keys from Actions, each
// declared with the words it is listed under.
package ui

import "strings"

// Action is a visible, named choice and its optional shortcut key.
type Action struct {
	ID, Label, Key string
}

// expectArg is the one function allowed to build --expect: its keys come from Actions.
func expectArg(acts []Action) string {
	keys := make([]string, 0, len(acts))
	for _, a := range acts {
		keys = append(keys, a.Key)
	}
	return "--expect=" + strings.Join(keys, ",")
}

// pickArgs is the house style: q leaves (the Exit row is visible), the cursor starts last,
// and the header follows the cursor. None of these is an action reachable only by a key.
func pickArgs(acts []Action, header string) []string {
	return []string{
		"--bind=q:abort", "--bind=start:last", expectArg(acts),
		"--bind=focus:transform-header^" + header + "^",
	}
}

// rawKeys is the shape the defence replaced: keys handed straight to fzf, with no words.
func rawKeys(keys []string) []string {
	return []string{"--expect=" + strings.Join(keys, ",")} // want `fzf --expect outside the ui Action builder`
}

// Actions declare their keys beside their words; a key with no words is a hidden binding.
var (
	named   = Action{ID: "add", Label: "Add to a team", Key: "ctrl-a"}
	noKey   = Action{ID: "attach", Label: "Attach"}
	unnamed = Action{ID: "remove", Key: "ctrl-x"}            // want `ui.Action with a Key and no Label`
	blank   = Action{ID: "role", Label: "", Key: "ctrl-o"}   // want `ui.Action with a Key and no Label`
	pos     = Action{"move", "", "ctrl-m"}                   // want `ui.Action with a Key and no Label`
	all     = []Action{named, noKey, unnamed, blank, pos, {ID: "x", Key: "ctrl-y"}} // want `ui.Action with a Key and no Label`
)
