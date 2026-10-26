package fzfbind

import "strings"

// noScreen are the fzf options that make it draw no screen and read no key: it prints
// something (its version, its help, a shell's integration script, its manual) or filters its
// input non-interactively. An Esc cannot be lost where no key is read.
var noScreen = map[string]bool{
	"--version": true, "--help": true, "-h": true, "--bash": true, "--zsh": true, "--fish": true,
	"--man": true, "--filter": true, "-f": true,
}

// DrawsNoScreen is whether the fzf option word makes fzf read no key: one of noScreen, or
// --filter=QUERY.
func DrawsNoScreen(word string) bool {
	return noScreen[word] || strings.HasPrefix(word, "--filter=")
}

// EscJoined is the keys fzf reads an Esc and the key after it as, when both arrive in one read
// or inside fzf's 100 ms wait after an Esc (tmux at its default escape-time hands a pane both in
// one write): that key with Alt. Esc then Down is alt-down, Esc then Enter is alt-enter
// (ctrl-alt-m in fzf 0.38), Esc then Ctrl-R is ctrl-alt-r. The arrows, Enter, Space,
// Backspace, every letter, every Ctrl+letter and every digit. A screen that binds none of them
// drops the Esc: the person stays on a screen they meant to leave, and their next key acts on
// it. The rule project_esc_joined requires every one bound to abort.
func EscJoined() []string {
	keys := []string{"alt-up", "alt-down", "alt-left", "alt-right", "alt-enter", "alt-space", "alt-bs"}
	for c := 'a'; c <= 'z'; c++ {
		keys = append(keys, "alt-"+string(c), "ctrl-alt-"+string(c))
	}
	for c := '0'; c <= '9'; c++ {
		keys = append(keys, "alt-"+string(c))
	}
	return keys
}

// leave is the one action that leaves fzf whatever the query holds (cancel clears a query
// first).
const leave = "abort"

// NotLeaving is the keys of EscJoined that none of specs (the text of fzf --bind options) binds
// to abort alone. A spec that cannot be read binds nothing.
func NotLeaving(specs []string) []string {
	leaves := map[string]bool{}
	for _, spec := range specs {
		bindings, _, err := Read(spec)
		if err != nil {
			continue
		}
		for _, b := range bindings {
			leaves[b.Trigger] = len(b.Actions) == 1 && b.Actions[0] == leave
		}
	}
	var missing []string
	for _, k := range EscJoined() {
		if !leaves[k] {
			missing = append(missing, k)
		}
	}
	return missing
}
