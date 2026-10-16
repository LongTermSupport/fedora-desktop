package badbind

import (
	"fmt"
	"slices"
	"strings"
)

// keysFlag is the --expect flag in a constant, judged where it is spelt.
const keysFlag = "--expect" // want `fzf --expect outside the ui Action builder`

// bindConst is a whole binding in a constant.
const bindConst = "--bind=ctrl-o:become(role)" // want `fzf --bind ctrl-o:become: a key bound to an action`

// inVariable builds the binding in a variable and appends it.
func inVariable(args []string) []string {
	b := "--bind=ctrl-a:accept" // want `fzf --bind ctrl-a:accept: a key bound to an action`
	return append(args, b, keysFlag, "ctrl-x", bindConst)
}

// splitWords passes --bind and its binding as separate words, in append and in a literal.
func splitWords(args []string, spec string) []string {
	args = append(args, "--bind", "ctrl-x:execute(rm {})") // want `fzf --bind ctrl-x:execute: a key bound to an action`
	args = append(args, "--bind", spec)                     // want `fzf --bind with a binding built at run time`
	args = append(args, "--bind")                           // want `fzf --bind with no binding beside it`
	return append(args, "--bind", "ctrl-k:up")
}

// concat hides the binding in slices.Concat and in an append of a literal slice.
func concat(base []string) []string {
	more := slices.Concat(base, []string{"--expect=ctrl-t"}) // want `fzf --expect outside the ui Action builder`
	return append(more, []string{"--bind", "alt-x:execute-silent(x)"}...) // want `fzf --bind alt-x:execute-silent: a key bound to an action`
}

// runTime builds the binding at run time, which the rule cannot judge.
func runTime(key, spec, header string) []string {
	return []string{
		fmt.Sprintf("--bind=%s:accept", key),                         // want `fzf --bind %s:accept: a key bound to an action`
		"--bind=" + spec,                                             // want `fzf --bind with a binding built at run time`
		"--exp" + "ect=ctrl-a",                                       // want `fzf --expect outside the ui Action builder`
		"--bind=focus:transform-header^" + header + "^,ctrl-a:accept", // want `fzf --bind with a binding after a run-time argument`
		"--bind=ctrl-a:accept" + spec,                                // want `fzf --bind ctrl-a:accept: a key bound to an action`
		"--bind=focus:transform-header^" + header + ";; esac^",
	}
}

// readsArgv inspects argv something else built: comparing and searching are not building.
func readsArgv(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "--expect") || a == "--bind" {
			return true
		}
		switch a {
		case "--expect=ctrl-a":
			return true
		}
	}
	if rest, ok := strings.CutPrefix(args[0], "--expect="); ok {
		return rest != ""
	}
	return slices.Contains(args, "--expect")
}

// trimmedIntoArgv returns what a reader function makes of its first argument, which becomes argv.
func trimmedIntoArgv() []string {
	before, after, found := strings.Cut("--bind=ctrl-k:execute(rm {})|x", "|") // want `fzf --bind ctrl-k:execute`
	rest, ok := strings.CutPrefix("x--bind=ctrl-j:accept", "x")                 // want `fzf --bind ctrl-j:accept`
	if !found || !ok {
		return nil
	}
	return []string{strings.TrimPrefix("x--expect=ctrl-z", "x"), before, after, rest} // want `fzf --expect outside the ui Action builder`
}
