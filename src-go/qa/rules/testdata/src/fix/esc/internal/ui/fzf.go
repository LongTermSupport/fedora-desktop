// Package ui starts fzf: each start must hand it the bindings that make an Esc joined to the
// next key leave, as Esc does.
package ui

import "fix/internal/execx"

// escJoined binds every key fzf reads an Esc and the next key as to abort.
const escJoined = "--bind=" +
	"alt-up:abort,alt-down:abort,alt-left:abort,alt-right:abort,alt-enter:abort,alt-space:abort,alt-bs:abort," +
	"alt-a:abort,alt-b:abort,alt-c:abort,alt-d:abort,alt-e:abort,alt-f:abort,alt-g:abort,alt-h:abort,alt-i:abort,alt-j:abort,alt-k:abort,alt-l:abort,alt-m:abort," +
	"alt-n:abort,alt-o:abort,alt-p:abort,alt-q:abort,alt-r:abort,alt-s:abort,alt-t:abort,alt-u:abort,alt-v:abort,alt-w:abort,alt-x:abort,alt-y:abort,alt-z:abort," +
	"ctrl-alt-a:abort,ctrl-alt-b:abort,ctrl-alt-c:abort,ctrl-alt-d:abort,ctrl-alt-e:abort,ctrl-alt-f:abort,ctrl-alt-g:abort,ctrl-alt-h:abort,ctrl-alt-i:abort,ctrl-alt-j:abort,ctrl-alt-k:abort,ctrl-alt-l:abort,ctrl-alt-m:abort," +
	"ctrl-alt-n:abort,ctrl-alt-o:abort,ctrl-alt-p:abort,ctrl-alt-q:abort,ctrl-alt-r:abort,ctrl-alt-s:abort,ctrl-alt-t:abort,ctrl-alt-u:abort,ctrl-alt-v:abort,ctrl-alt-w:abort,ctrl-alt-x:abort,ctrl-alt-y:abort,ctrl-alt-z:abort," +
	"alt-0:abort,alt-1:abort,alt-2:abort,alt-3:abort,alt-4:abort,alt-5:abort,alt-6:abort,alt-7:abort,alt-8:abort,alt-9:abort"

// The same bindings split over two --bind options.
const (
	escArrows = "--bind=alt-up:abort,alt-down:abort,alt-left:abort,alt-right:abort,alt-enter:abort,alt-space:abort,alt-bs:abort"
	escRest   = "--bind=" +
		"alt-a:abort,alt-b:abort,alt-c:abort,alt-d:abort,alt-e:abort,alt-f:abort,alt-g:abort,alt-h:abort,alt-i:abort,alt-j:abort,alt-k:abort,alt-l:abort,alt-m:abort," +
		"alt-n:abort,alt-o:abort,alt-p:abort,alt-q:abort,alt-r:abort,alt-s:abort,alt-t:abort,alt-u:abort,alt-v:abort,alt-w:abort,alt-x:abort,alt-y:abort,alt-z:abort," +
		"ctrl-alt-a:abort,ctrl-alt-b:abort,ctrl-alt-c:abort,ctrl-alt-d:abort,ctrl-alt-e:abort,ctrl-alt-f:abort,ctrl-alt-g:abort,ctrl-alt-h:abort,ctrl-alt-i:abort,ctrl-alt-j:abort,ctrl-alt-k:abort,ctrl-alt-l:abort,ctrl-alt-m:abort," +
		"ctrl-alt-n:abort,ctrl-alt-o:abort,ctrl-alt-p:abort,ctrl-alt-q:abort,ctrl-alt-r:abort,ctrl-alt-s:abort,ctrl-alt-t:abort,ctrl-alt-u:abort,ctrl-alt-v:abort,ctrl-alt-w:abort,ctrl-alt-x:abort,ctrl-alt-y:abort,ctrl-alt-z:abort," +
		"alt-0:abort,alt-1:abort,alt-2:abort,alt-3:abort,alt-4:abort,alt-5:abort,alt-6:abort,alt-7:abort,alt-8:abort,alt-9:abort"
)

// arrowsOnly leaves on the joined arrows and nothing else.
const arrowsOnly = escArrows

// fzfProgram is fzf by a constant name.
const fzfProgram = "fzf"

// Leaves hands fzf every joined key bound to abort.
func Leaves(args []string) execx.Cmd {
	return execx.Cmd{Name: "fzf", Args: append(args, escJoined)}
}

// LeavesSplit hands fzf the same bindings in two options.
func LeavesSplit(args []string) execx.Cmd {
	return execx.Cmd{Name: "fzf", Args: append([]string{escArrows, escRest}, args...)}
}

// LeavesPositional is the same literal without field names.
func LeavesPositional(args []string) execx.Cmd {
	return execx.Cmd{"fzf", append(args, escJoined)}
}

// Drops hands fzf nothing for a joined Esc: Esc then Down is alt-down, and the Esc is lost.
func Drops(args []string) execx.Cmd {
	return execx.Cmd{Name: "fzf", Args: args} // want `fzf started without the Esc-joined bindings`
}

// DropsSome leaves on the arrows only: Esc then Enter is still lost.
func DropsSome(args []string) execx.Cmd {
	return execx.Cmd{Name: fzfProgram, Args: append(args, arrowsOnly)} // want `fzf started without the Esc-joined bindings: alt-a`
}

// DropsByPath names fzf by its path.
func DropsByPath(args []string) execx.Cmd {
	return execx.Cmd{Name: "/usr/bin/fzf", Args: args} // want `fzf started without the Esc-joined bindings`
}

// DropsNoArgs passes no arguments at all.
func DropsNoArgs() execx.Cmd {
	return execx.Cmd{Name: "fzf"} // want `fzf started without the Esc-joined bindings`
}

// DropsElsewhere names the bindings beside the command, not in its arguments.
func DropsElsewhere(args []string) (execx.Cmd, string) {
	return execx.Cmd{Name: "fzf", Args: args}, escJoined // want `fzf started without the Esc-joined bindings`
}

// ByField builds the command field by field, where the rule cannot see its arguments.
func ByField(args []string) execx.Cmd {
	var c execx.Cmd
	c.Name = "fzf" // want `fzf command built field by field`
	c.Args = append(args, escJoined)
	return c
}

// Version draws no screen and reads no key: not judged.
func Version() execx.Cmd {
	return execx.Cmd{Name: "fzf", Args: []string{"--version"}}
}

// Filters filters its input without a screen: not judged.
func Filters(query string) execx.Cmd {
	return execx.Cmd{Name: "fzf", Args: []string{"--read0", "--filter", query}}
}

// NotFzf is another program: not judged.
func NotFzf(args []string) execx.Cmd {
	c := execx.Cmd{Name: "gum", Args: args}
	c.Name = "gum"
	return c
}
