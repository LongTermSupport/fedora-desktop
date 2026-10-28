package sshkey

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// markerVar tells the run ssh-agent started that it is inside its own agent: its value is the
// pid of the run that replaced itself with ssh-agent, which the run inside keeps (E14).
const markerVar = "SSH_WITH_KEY_AGENT"

// remoteInit is the remote command of a forwarded interactive login (E18): git on the far side
// uses the forwarded agent and ignores that machine's ssh configuration, then a login shell
// starts. $SHELL is for the remote shell to expand.
const remoteInit = `export GIT_SSH_COMMAND="ssh -F /dev/null"; exec "$SHELL" -l`

// maxAddFailures is how many ssh-add failures end the run (E16).
const maxAddFailures = 3

// The forwarding menu's lines besides the keys (E15).
const (
	allKey  = "a"
	doneKey = "d"
)

// startAgent replaces the run with ssh-agent running this program again, with the chosen key
// first, inside a fresh agent that ends when the run does (D2, E14).
func (r *run) startAgent(inv invocation) term.ExitCode {
	self, err := r.env.Self()
	if err != nil {
		r.failf("could not find this program to run it inside the agent: %v.", err)
		return term.ExitFailure
	}
	args := append([]string{"--", self, r.tool.KeyFlag, inv.key}, inv.passed...)
	marker := markerVar + "=" + strconv.Itoa(r.env.Getpid())
	return r.replace(execx.Cmd{Name: "ssh-agent", Args: args, Env: []string{marker}})
}

// inAgent is the run inside the agent (E14-E19): the agent checked, the keys added, the program
// in the foreground, then the agent ended.
func (r *run) inAgent(ctx context.Context, inv invocation) term.ExitCode {
	if !r.ownAgent() {
		r.failf("the agent in SSH_AUTH_SOCK is not one this run started; nothing was run.")
		return term.ExitFailure
	}
	return r.endAgent(ctx, r.forward(ctx, inv))
}

// ownAgent reports whether the agent in the environment is the one ssh-agent started for this
// run: the marker is this pid, and the agent's parent is this process (E14).
func (r *run) ownAgent() bool {
	pid := r.env.Getpid()
	if r.env.Getenv(markerVar) != strconv.Itoa(pid) || r.env.Getenv("SSH_AUTH_SOCK") == "" {
		return false
	}
	agent, err := strconv.Atoi(r.env.Getenv("SSH_AGENT_PID"))
	if err != nil || agent <= 0 {
		return false
	}
	parent, err := r.env.ParentOf(agent)
	return err == nil && parent == pid
}

// endAgent ends the agent at once (E18, E19); code is the run's status so far.
func (r *run) endAgent(ctx context.Context, code term.ExitCode) term.ExitCode {
	kill := execx.Cmd{Name: "ssh-agent", Args: []string{"-k"}, Unset: []string{markerVar}}
	if _, err := r.env.Runner.Run(context.WithoutCancel(ctx), kill); err != nil {
		r.failf("could not end the forwarding agent (pid %s): %v.", r.env.Getenv("SSH_AGENT_PID"), err)
		if code == term.ExitOK {
			return term.ExitFailure
		}
	}
	return code
}

// forward adds the keys the person chooses, then runs the program in the foreground.
func (r *run) forward(ctx context.Context, inv invocation) term.ExitCode {
	if !r.keyUsable(inv.key) {
		return term.ExitFailure
	}
	keys, err := privateKeys(r.sshDir())
	if err != nil {
		r.failf("could not list the keys in %s: %v.", r.show(r.sshDir()), err)
		return term.ExitFailure
	}
	if len(keys) == 0 {
		r.failf("agent forwarding (-A) was asked, but %s has no private key to forward; nothing was run.", r.show(r.sshDir()))
		return term.ExitFailure
	}
	lines, closeTTY, ok := r.terminal()
	if !ok {
		r.failf("agent forwarding (-A) needs a terminal to choose the keys on; nothing was run.")
		return term.ExitUsage
	}
	code := r.addKeys(ctx, lines, keys)
	if err := closeTTY(); err != nil && code == term.ExitOK {
		r.failf("could not close the terminal: %v.", err)
		return term.ExitFailure
	}
	if code != term.ExitOK {
		return code
	}
	return r.foreground(inv)
}

// filling is the forwarding agent being filled: the keys offered, which are added, their names
// in the order added, and how many adds failed (E15, E16).
type filling struct {
	keys     []string
	added    []bool
	names    []string
	failures int
}

// addKeys is E15 and E16: one menu at a time, each chosen key added at once.
func (r *run) addKeys(ctx context.Context, lines *ui.Lines, keys []string) term.ExitCode {
	f := &filling{keys: keys, added: make([]bool, len(keys))}
	for {
		left := f.left()
		if len(left) == 0 {
			return term.ExitOK
		}
		answer, err := lines.Choose(ctx, r.forwardHeading(f.names), "Key number", r.forwardChoices(keys, left, len(f.names) > 0))
		if err != nil {
			return r.menuFailed(ctx, err, "no keys chosen")
		}
		var todo []int
		switch answer {
		case cancelKey:
			return r.cancelled(ctx)
		case doneKey:
			return term.ExitOK
		case allKey:
			todo = left
		default:
			n, err := strconv.Atoi(answer)
			if err != nil {
				r.failf("the menu answered %q: %v.", answer, err)
				return term.ExitFailure
			}
			todo = []int{n - 1}
		}
		for _, i := range todo {
			if code, stop := r.addKey(ctx, f, i); stop {
				return code
			}
		}
	}
}

// addKey runs ssh-add for the key at i in the foreground, where it asks its passphrase on the
// terminal itself; stop ends the run with code.
func (r *run) addKey(ctx context.Context, f *filling, i int) (term.ExitCode, bool) {
	name := filepath.Base(f.keys[i])
	exit, err := r.env.Foregrounder.Foreground(execx.Cmd{Name: "ssh-add", Args: []string{f.keys[i]}, Unset: []string{markerVar}})
	switch {
	case err != nil:
		r.startFailed("ssh-add", err)
		return term.ExitFailure, true
	case interrupted(exit):
		return r.cancelled(ctx), true
	case exit.Code != 0:
		r.say("ssh-add could not add " + name + " (exit " + strconv.Itoa(exit.Code) + ").")
		f.failures++
		if f.failures >= maxAddFailures {
			r.failf("three keys could not be added; nothing was run.")
			return term.ExitFailure, true
		}
		return term.ExitOK, false
	}
	f.added[i] = true
	f.names = append(f.names, name)
	return term.ExitOK, false
}

// left are the keys not yet added, by their place in the list.
func (f *filling) left() []int {
	var left []int
	for i, done := range f.added {
		if !done {
			left = append(left, i)
		}
	}
	return left
}

func (r *run) forwardHeading(names []string) ui.Heading {
	about := "added: none"
	if len(names) > 0 {
		about = "added: " + strings.Join(names, ", ")
	}
	return ui.Here(r.tool.Name, "Keys to forward").About(about)
}

// forwardChoices are the keys not yet added, by their place in the full list, then All (when
// more than one is left), Done (once one is added) and Cancel (E15).
func (r *run) forwardChoices(keys []string, left []int, anyAdded bool) []ui.Choice {
	choices := make([]ui.Choice, 0, len(left)+3)
	for _, i := range left {
		choices = append(choices, ui.Choice{Key: strconv.Itoa(i + 1), Label: filepath.Base(keys[i])})
	}
	if len(left) > 1 {
		choices = append(choices, ui.Choice{Key: allKey, Label: "All the keys not yet added"})
	}
	if anyAdded {
		choices = append(choices, ui.Choice{Key: doneKey, Label: "Done: forward the keys added"})
	}
	return append(choices, ui.Choice{Key: cancelKey, Label: "Cancel"})
}

// interrupted reports whether a foreground child was ended by the terminal's Ctrl-C.
func interrupted(exit execx.Exit) bool {
	return exit.Signal == syscall.SIGINT || slices.Contains(exit.Received, os.Signal(syscall.SIGINT))
}

// foreground runs the program with the agent forwarded (E18); its status is the run's.
func (r *run) foreground(inv invocation) term.ExitCode {
	args := append(r.ownOptions(inv.key, false), inv.passed...)
	if r.tool.kind == kindSSH && !inv.command && !inv.noCommand {
		args = append(append([]string{"-t"}, args...), remoteInit)
	}
	c := r.tool.command(args)
	c.Unset = []string{markerVar}
	exit, err := r.env.Foregrounder.Foreground(c)
	if err != nil {
		r.startFailed(r.tool.Program, err)
		return term.ExitFailure
	}
	return term.ExitCode(exit.Code)
}
