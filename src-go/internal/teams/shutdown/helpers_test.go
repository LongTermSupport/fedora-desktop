package shutdown

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

var rule = strings.Repeat("─", 60)

// The panes a fake session can show.
var (
	paneReady   = strings.Join([]string{" ▐▛███▜▌   Claude Code", "", rule, "❯ ", rule, "  ⏵⏵ bypass permissions on"}, "\n")
	paneBusy    = strings.Join([]string{"✻ Working… (12s · esc to interrupt)", "", rule, "❯ ", rule}, "\n")
	paneTyped   = strings.Join([]string{rule, "❯ half a thought", rule}, "\n")
	paneStart   = "Building image...\n"
	paneCompose = "CCY Session Ended — Compose Services Running\nStop compose services? [Y/n]: "
	paneEnded   = "\nccy exited with status 1. Press Enter to close this session.\n"
	paneSSHKey  = "Select SSH key"
)

// session is one fake tmux session: what its pane shows and what it does with keys.
type session struct {
	name, dir string
	attached  int
	pane      string
	// typed is what was typed and not yet entered.
	typed string
	// afterExit is what /exit and Enter do: "" ends the session, anything else becomes the pane
	// and the session stays; either way its seats are freed.
	afterExit string
	// ignoreExit leaves the pane as it is after /exit, as a session that does not end.
	ignoreExit bool
	// keepSeat leaves the seats held when the session ends.
	keepSeat bool
	// seats are the seat names in the team that the session's process holds.
	seats []string
	// enters counts Enter presses, escapes Escape presses.
	enters, escapes int
}

// machine is teamtest's populated world with a stateful tmux, seat list and clock.
type machine struct {
	*teamtest.World

	t        *testing.T
	s        *Shutdown
	now      time.Time
	sleeps   []time.Duration
	sessions []*session
	// held is the seats of alpha in the checkout that a live process holds.
	held map[string]bool
}

func newMachine(t *testing.T) *machine {
	t.Helper()
	m := &machine{World: teamtest.Populated(t), t: t, now: time.Unix(1_800_000_000, 0), held: map[string]bool{"1": true}}
	m.sessions = []*session{{name: "ccy-proj", dir: m.Checkout(), pane: paneReady, seats: []string{"1"}}}
	m.fakeTmux()
	m.fakeSeatList()
	m.build()
	return m
}

// build makes the Shutdown over the host as it now is, on the fake clock, with the checkout
// under the home folder as the preview shows it.
func (m *machine) build() {
	h := m.Host
	m.s = New(h, flow.New(h), board.New(h))
	m.s.Home = m.Root
	m.s.Now = func() time.Time { return m.now }
	m.s.Sleep = func(d time.Duration) {
		m.sleeps = append(m.sleeps, d)
		m.now = m.now.Add(d)
	}
}

// interactive puts the machine on a terminal with scripted fzf answers.
func (m *machine) interactive(fzf ...string) *teamtest.Console {
	c := teamtest.Interactive(m.t, m.World)
	c.Script.Fzf = fzf
	m.build()
	return c
}

// noLxc makes alpha's roster hold only the ccy members.
func (m *machine) noLxc() {
	m.On(teamtest.SudoBus+"list alpha", strings.Replace(teamtest.BusList, "MEMBER\tproj.box+role.lxc\tops\tjoin\tactive\n", "", 1), 0)
}

func (m *machine) find(name string) *session {
	for _, s := range m.sessions {
		if s.name == name {
			return s
		}
	}
	return nil
}

func (m *machine) end(s *session) {
	m.sessions = slices.DeleteFunc(m.sessions, func(x *session) bool { return x == s })
	if !s.keepSeat {
		for _, seat := range s.seats {
			m.held[seat] = false
		}
	}
}

func target(args []string) string {
	for i, a := range args {
		if a == "-t" && i+1 < len(args) {
			return strings.TrimSuffix(strings.TrimPrefix(args[i+1], "="), ":")
		}
	}
	return ""
}

func (m *machine) fakeTmux() {
	m.Fake.OnFunc([]string{"tmux", "-L", "ccy", "list-sessions"}, func(execx.Cmd) (execx.Result, error) {
		var b strings.Builder
		for _, s := range m.sessions {
			b.WriteString(s.name + " " + strconv.Itoa(s.attached) + " " + s.dir + "\n")
		}
		return execx.Result{Stdout: []byte(b.String())}, nil
	})
	m.Fake.OnFunc([]string{"tmux", "-L", "ccy", "capture-pane"}, func(c execx.Cmd) (execx.Result, error) {
		s := m.find(target(c.Args))
		if s == nil {
			return execx.Result{ExitCode: 1, Stderr: []byte("can't find pane: " + target(c.Args))}, nil
		}
		return execx.Result{Stdout: []byte(s.pane + "\n")}, nil
	})
	m.Fake.OnFunc([]string{"tmux", "-L", "ccy", "send-keys"}, func(c execx.Cmd) (execx.Result, error) {
		s := m.find(target(c.Args))
		if s == nil {
			return execx.Result{ExitCode: 1, Stderr: []byte("can't find pane: " + target(c.Args))}, nil
		}
		m.press(s, c.Args[len(c.Args)-2:])
		return execx.Result{}, nil
	})
	m.Fake.OnFunc([]string{"tmux", "-L", "ccy", "kill-session"}, func(c execx.Cmd) (execx.Result, error) {
		s := m.find(target(c.Args))
		if s == nil {
			return execx.Result{ExitCode: 1, Stderr: []byte("can't find session: " + target(c.Args))}, nil
		}
		m.end(s)
		return execx.Result{}, nil
	})
}

// press is one send-keys: the last two arguments are "-l <text>" or "<target> <key>".
func (m *machine) press(s *session, last []string) {
	switch {
	case last[0] == "-l":
		s.typed += last[1]
	case last[1] == "Escape":
		s.escapes++
		if s.pane == paneBusy {
			s.pane = paneReady
		}
	case last[1] == "Enter":
		s.enters++
		if s.typed != "/exit" {
			return
		}
		if s.ignoreExit {
			if s.afterExit != "" {
				s.pane = s.afterExit
			}
			return
		}
		s.typed = ""
		if s.afterExit == "" {
			m.end(s)
			return
		}
		// claude has ended, and the container with it: its seats are free, though tmux still
		// runs the launcher.
		s.pane = s.afterExit
		if !s.keepSeat {
			for _, seat := range s.seats {
				m.held[seat] = false
			}
		}
	}
}

func (m *machine) fakeSeatList() {
	m.Fake.OnFunc([]string{"agent-bus", "seat", "list"}, func(execx.Cmd) (execx.Result, error) {
		var b strings.Builder
		for _, seat := range []string{"1", "dev"} {
			state, self := "free", "-"
			if m.held[seat] {
				state, self = "held", "self"
			}
			b.WriteString("SEAT\talpha\t" + seat + "\t" + state + "\t" + self + "\tproj." + seat + "+local.podman\n")
		}
		return execx.Result{Stdout: []byte(b.String())}, nil
	})
}

// record writes a session record of prefix for dir with the --teams list.
func (m *machine) record(name, prefix, dir, teams string) {
	teamtest.MustWrite(m.t, filepath.Join(m.Root, "home/.local/state/ccy/sessions", name),
		"ccy-session-record 1\nname="+name+"\ndir="+dir+"\nlauncher=/x/"+prefix+"\nprefix="+prefix+"\nrestore=yes\narg=--teams\narg="+teams+"\n")
}

func (m *machine) run(args ...string) term.ExitCode {
	return m.s.Run(m.t.Context(), args)
}

// typedInto is every literal text typed into a session, in order.
func (m *machine) typedInto(name string) []string {
	var got []string
	for _, c := range m.Fake.Calls() {
		if c.Name == "tmux" && slices.Contains(c.Args, "send-keys") && target(c.Args) == name && c.Args[len(c.Args)-2] == "-l" {
			got = append(got, c.Args[len(c.Args)-1])
		}
	}
	return got
}

// killed is whether ccy-proj was killed.
func (m *machine) killed() bool {
	return m.Fake.Called("tmux -L ccy kill-session -t =ccy-proj")
}

func (m *machine) errHas(t *testing.T, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(m.ErrOut.String(), w) {
			t.Fatalf("stderr lacks %q:\n%s", w, m.ErrOut.String())
		}
	}
}
