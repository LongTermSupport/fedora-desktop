package winddown

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
	paneReady = strings.Join([]string{" ▐▛███▜▌   Claude Code", "", rule, "❯ ", rule, "  ⏵⏵ bypass permissions on"}, "\n")
	paneBusy  = strings.Join([]string{"✻ Working… (12s · esc to interrupt)", "", rule, "❯ ", rule}, "\n")
	paneEnded = "\nccy exited with status 1. Press Enter to close this session.\n"
)

// paneHolding is a drawn, idle box holding text.
func paneHolding(text string) string { return strings.Join([]string{rule, "❯ " + text, rule}, "\n") }

// session is one fake tmux session: what its pane shows and what it does with keys.
type session struct {
	name, dir string
	// pane is what an idle session with nothing in its box shows.
	pane string
	// box is what was typed and not yet entered; held is text a person left there.
	box string
	// busyLooks is how many more captures show the session busy.
	busyLooks int
	// dropEnters is how many Enter presses are ignored, leaving the text in the box.
	dropEnters int
	// slowClear shows each entered line once more before it leaves the box; lagging is a line
	// so shown on the next look.
	slowClear, lagging bool
	// endAfterGoal ends the session once /goal clear is entered, endAfterText once the
	// wind-down is.
	endAfterGoal, endAfterText bool
	// busyAfterGoal is how many captures show the session busy after /goal clear.
	busyAfterGoal int
	// paneAfterText is what the pane shows once the wind-down is entered, when set.
	paneAfterText string
	// entered is every line submitted; enters, escapes count those keys.
	entered          []string
	enters, escapes  int
	busyAfterEntered bool
}

// machine is teamtest's populated world with a stateful tmux and a clock.
type machine struct {
	*teamtest.World

	t        *testing.T
	w        *WindDown
	now      time.Time
	sleeps   []time.Duration
	sessions []*session
	// lxcSessions are the sessions on the LXC member's tmux server, lxcRunning whether its
	// container runs.
	lxcSessions []*session
	lxcRunning  bool
}

func newMachine(t *testing.T) *machine {
	t.Helper()
	m := &machine{World: teamtest.Populated(t), t: t, now: time.Unix(1_800_000_000, 0)}
	m.sessions = []*session{{name: "ccy-proj", dir: m.Checkout(), pane: paneReady}}
	m.fakeTmux()
	m.build()
	return m
}

// build makes the WindDown over the host as it now is, on the fake clock.
func (m *machine) build() {
	h := m.Host
	h.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	m.w = New(h, flow.New(h), board.New(h))
	m.w.Home = m.Root
	m.w.Now = func() time.Time { return m.now }
	m.w.Sleep = func(d time.Duration) {
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

// coordinator makes proj.1 alpha's coordinator, with no LXC member.
func (m *machine) coordinator() {
	list := strings.Replace(teamtest.BusList, "MEMBER\tproj.box+role.lxc\tops\tjoin\tactive\n", "", 1)
	m.On(teamtest.SudoBus+"list alpha", strings.Replace(list, "proj.1+local.podman\tdev", "proj.1+local.podman\tcoordinator", 1), 0)
}

// proj is the desktop session ccy-proj, nil once it has ended.
func (m *machine) proj() *session {
	for _, s := range m.sessions {
		if s.name == "ccy-proj" {
			return s
		}
	}
	return nil
}

func target(args []string) string {
	for i, a := range args {
		if a == "-t" && i+1 < len(args) {
			return strings.TrimSuffix(strings.TrimPrefix(args[i+1], "="), ":")
		}
	}
	return ""
}

func gone(name string) (execx.Result, error) {
	return execx.Result{ExitCode: 1, Stderr: []byte("can't find pane: " + name)}, nil
}

func (m *machine) fakeTmux() {
	m.Fake.OnFunc([]string{"tmux", "-L", "ccy"}, func(c execx.Cmd) (execx.Result, error) {
		return m.answer(m.sessions, c.Args[2:])
	})
}

// answer is one tmux command (after -L <socket>) on a server holding sessions.
func (m *machine) answer(sessions []*session, args []string) (execx.Result, error) {
	find := func() *session {
		for _, s := range sessions {
			if s.name == target(args) {
				return s
			}
		}
		return nil
	}
	switch args[0] {
	case "list-sessions":
		var b strings.Builder
		for _, s := range sessions {
			b.WriteString(s.name + " 0 " + s.dir + "\n")
		}
		return execx.Result{Stdout: []byte(b.String())}, nil
	case "capture-pane":
		s := find()
		if s == nil {
			return gone(target(args))
		}
		return execx.Result{Stdout: []byte(m.look(s) + "\n")}, nil
	case "send-keys":
		s := find()
		if s == nil {
			return gone(target(args))
		}
		m.press(s, args[len(args)-2:])
		return execx.Result{}, nil
	}
	return execx.Result{ExitCode: 1, Stderr: []byte("unknown command: " + args[0])}, nil
}

// lxcBox is the LXC member's tmux session, the one play-agent-bus.yml's service unit starts.
const lxcBox = "agent-bus-claude"

// withLxc makes alpha's LXC member reachable: the play's record names its container and user,
// sudo is ready, the container runs, and its user's tmux server holds the member's session.
func (m *machine) withLxc() *session {
	teamtest.MustWrite(m.t, filepath.Join(m.Host.AppliedDir, "lxc", "alpha.json"),
		`{"version": 1, "team": "alpha", "members": [{"container": "box", "user": "agent", "state": "present", `+
			`"status": "joined", "handle": "proj.box+role.lxc", "checkout": "/srv/proj", "why": "", "since": 1}]}`)
	m.On("sudo -n -v", "", 0)
	m.On("sudo -n lxc-config lxc.lxcpath", "/var/lib/lxc\n", 0)
	m.On("sudo -n lxc-ls -1", "box\nother\n", 0)
	m.lxcRunning = true
	m.Fake.OnFunc([]string{"sudo", "-n", "lxc-ls", "-1", "--running"}, func(execx.Cmd) (execx.Result, error) {
		if m.lxcRunning {
			return execx.Result{Stdout: []byte("box\n")}, nil
		}
		return execx.Result{}, nil
	})
	s := &session{name: lxcBox, dir: "/srv/proj", pane: paneReady}
	m.lxcSessions = []*session{s}
	m.Fake.OnFunc([]string{"sudo", "-n", "lxc-attach"}, func(c execx.Cmd) (execx.Result, error) {
		at := slices.Index(c.Args, "tmux")
		if at < 0 || c.Args[at+1] != "-L" || c.Args[at+2] != lxcBox {
			return execx.Result{ExitCode: 1, Stderr: []byte("not a tmux command on the member's server")}, nil
		}
		return m.answer(m.lxcSessions, c.Args[at+3:])
	})
	m.build()
	return s
}

// lxcTyped is every literal text typed into the LXC member's session, in order.
func (m *machine) lxcTyped() []string {
	var got []string
	for _, c := range m.Fake.Calls() {
		if c.Name == "sudo" && slices.Contains(c.Args, "lxc-attach") && slices.Contains(c.Args, "send-keys") && c.Args[len(c.Args)-2] == "-l" {
			got = append(got, c.Args[len(c.Args)-1])
		}
	}
	return got
}

// look is what the pane shows now: busy while busyLooks lasts, else the box's text, else idle.
// A line slow to clear shows once more after its Enter, and is entered then.
func (m *machine) look(s *session) string {
	switch {
	case s.lagging:
		s.lagging = false
		out := paneHolding(s.box)
		m.submit(s)
		return out
	case s.busyLooks > 0:
		s.busyLooks--
		return paneBusy
	case s.box != "":
		return paneHolding(s.box)
	}
	return s.pane
}

// press is one send-keys: the last two arguments are "-l <text>" or "<target> <key>".
func (m *machine) press(s *session, last []string) {
	switch {
	case last[0] == "-l":
		s.box += last[1]
	case last[1] == "Escape":
		s.escapes++
	case last[1] == "Enter":
		s.enters++
		if s.dropEnters > 0 {
			s.dropEnters--
			return
		}
		if s.slowClear {
			s.lagging = true
			return
		}
		m.submit(s)
	}
}

// submit enters the box's line and does what the session does with it.
func (m *machine) submit(s *session) {
	line := s.box
	s.box = ""
	s.entered = append(s.entered, line)
	if (line == goalClear && s.endAfterGoal) || (line != goalClear && s.endAfterText) {
		m.sessions = slices.DeleteFunc(m.sessions, func(x *session) bool { return x == s })
		m.lxcSessions = slices.DeleteFunc(m.lxcSessions, func(x *session) bool { return x == s })
		return
	}
	if line == goalClear {
		s.busyLooks = s.busyAfterGoal
		return
	}
	if s.busyAfterEntered {
		s.busyLooks = 1000
	}
	if s.paneAfterText != "" {
		s.pane = s.paneAfterText
	}
}

// record writes a session record of prefix for dir with the --teams list.
func (m *machine) record(name, prefix, dir, teams string) {
	teamtest.MustWrite(m.t, filepath.Join(m.Root, "home/.local/state/ccy/sessions", name),
		"ccy-session-record 1\nname="+name+"\ndir="+dir+"\nlauncher=/x/"+prefix+"\nprefix="+prefix+"\nrestore=yes\narg=--teams\narg="+teams+"\n")
}

func (m *machine) run(args ...string) term.ExitCode {
	return m.w.Run(m.t.Context(), args)
}

// typed is every literal text typed into ccy-proj, in order.
func (m *machine) typed() []string {
	const name = "ccy-proj"
	var got []string
	for _, c := range m.Fake.Calls() {
		if c.Name == "tmux" && slices.Contains(c.Args, "send-keys") && target(c.Args) == name && c.Args[len(c.Args)-2] == "-l" {
			got = append(got, c.Args[len(c.Args)-1])
		}
	}
	return got
}

func (m *machine) errHas(t *testing.T, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(m.ErrOut.String(), w) {
			t.Fatalf("stderr lacks %q:\n%s", w, m.ErrOut.String())
		}
	}
}

func (m *machine) errLacks(t *testing.T, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		if strings.Contains(m.ErrOut.String(), w) {
			t.Fatalf("stderr has %q:\n%s", w, m.ErrOut.String())
		}
	}
}

func secs(n int) string { return strconv.Itoa(n) + " s" }
