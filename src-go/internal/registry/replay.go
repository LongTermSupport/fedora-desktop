package registry

import "strings"

// flagNoRestore is the registry's opt-out: consumed before the launcher runs.
const flagNoRestore = "--no-restore"

// ReplayArgs is what a ccy launch with argv args records as its arguments: args with
// --no-restore removed before any `--` (ccy_registry_launch_args), then the one-shot set removed
// (ccy_registry_replay_args). A value-taking flag consumes the next word whatever it is, `--`
// ends ccy's options and every word after it is claude's, kept as it is, and a word that is not a
// flag is a first message, stale on a replay, unless it follows a flag ccy does not know, when it
// is that flag's value.
func ReplayArgs(args []string) []string {
	out := []string{}
	afterDD, dropNext, keepNext, valueSlot := false, false, false, false
	for _, w := range args {
		switch {
		case !afterDD && w == flagNoRestore:
			// The launcher never sees it, so it consumes no value slot either.
			continue
		case afterDD:
			out = append(out, w)
			continue
		case w == "--":
			afterDD = true
			out = append(out, w)
			continue
		case dropNext:
			dropNext = false
			continue
		case keepNext:
			keepNext = false
			out = append(out, w)
			continue
		}
		class := FlagClass(w)
		dropNext = class == ClassDropValue
		keepNext = class == ClassKeepValue
		isFlag := strings.HasPrefix(w, "-")
		if class == ClassKeep || keepNext || (class == ClassUnknown && (isFlag || valueSlot)) {
			out = append(out, w)
		}
		valueSlot = class == ClassUnknown && isFlag
	}
	return out
}
