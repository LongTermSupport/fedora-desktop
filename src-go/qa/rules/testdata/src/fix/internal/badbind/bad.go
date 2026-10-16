// Package badbind offers actions through fzf keys alone: the ccy-teams picker's Ctrl-A, Ctrl-X
// and Ctrl-O, which added a session to a team, removed it and changed its role with nothing on
// screen to choose.
package badbind

// pickerArgs is the originating instance: the keys fzf hands back so the caller acts on them.
func pickerArgs() []string {
	return []string{
		"--ansi",
		"--expect=ctrl-a,ctrl-x,ctrl-o", // want `fzf --expect outside the ui Action builder`
	}
}

// bindArgs binds keys straight to fzf actions that do something.
func bindArgs() []string {
	return []string{
		"--bind=ctrl-a:become(x)",                    // want `fzf --bind ctrl-a:become: a key bound to an action`
		"--bind=ctrl-x:execute(ccy-teams rm {})",     // want `fzf --bind ctrl-x:execute: a key bound to an action`
		"--bind", "ctrl-o:become(ccy-teams role {})", // want `fzf --bind ctrl-o:become: a key bound to an action`
		"--bind=ctrl-j:down,ctrl-r:reload(ls)", // want `fzf --bind ctrl-r:reload: a key bound to an action`
		"--bind=tab:toggle+execute(x)",         // want `fzf --bind tab:execute: a key bound to an action`
		"--expect", "ctrl-a",                   // want `fzf --expect outside the ui Action builder`
	}
}

// navigation binds keys to moving, selecting and leaving, and events to display: no action
// is reached only by a key.
func navigation() []string {
	return []string{
		"--bind=q:abort", "--bind=esc:cancel", "--bind=tab:toggle+down", "--bind=ctrl-j:down,ctrl-k:up",
		"--bind=start:last", "--bind=load:first", "--bind", "home:first",
		"--bind=pgdn:page-down,pgup:page-up,end:last",
		"--bind=change:reload(ls)",               // an event, not a key: nobody presses it
		"--bind=ctrl-a:accept,tab:toggle+accept", // any key accepting is Enter: fzf cannot say which
		"--bind=ctrl-w:change-preview-window(down),ctrl-t:toggle-track,ctrl-v:preview(cat {})",
	}
}
