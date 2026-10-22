package restart

import (
	"fmt"
	"hash/crc32"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/transcript"
)

// session is one fake session of the machine: its seat in alpha, whether tmux runs it, its
// container (ccy), and what it does when asked to restart or started.
type session struct {
	name, prefix, seat string
	live               bool
	container          string
	// supervised sessions restart in place when relaunch.json appears; others never do.
	supervised bool
	// holdsOnStart is whether a started session takes its seat.
	holdsOnStart bool
}

// machine is teamtest's populated world, with ccy-proj on seat 1 of alpha running in a
// container, a stateful tmux, podman and seat list, and a fake clock.
type machine struct {
	*teamtest.World

	t        *testing.T
	r        *Restart
	now      time.Time
	sessions []*session
	runtime  string
	started  []string
}

func newMachine(t *testing.T) *machine {
	t.Helper()
	m := &machine{World: teamtest.Populated(t), t: t, now: time.Unix(1_800_000_000, 0)}
	m.runtime = filepath.Join(m.Root, "run")
	m.Env["XDG_RUNTIME_DIR"] = m.runtime
	m.sessions = []*session{{name: "ccy-proj", prefix: "ccy", seat: "1", live: true, container: "c1", supervised: true, holdsOnStart: true}}
	m.desire(m.sessions[0])
	m.converse("ccy-proj", "ccy")
	m.fakeTmux()
	m.fakePodman()
	m.Host.Tmux.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	m.build()
	return m
}

// build makes the Restart over the host as it now is, on the fake clock.
func (m *machine) build() {
	h := m.Host
	m.r = New(h, flow.New(h), board.New(h))
	m.r.Home = m.Root
	m.r.Now = func() time.Time { return m.now }
	m.r.Sleep = func(d time.Duration) { m.now = m.now.Add(d) }
}

// interactive puts the machine on a terminal with scripted fzf answers.
func (m *machine) interactive(fzf ...string) *teamtest.Console {
	c := teamtest.Interactive(m.t, m.World)
	c.Script.Fzf = fzf
	m.Host.Tmux.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	m.build()
	return c
}

// noLxc makes alpha's roster hold only seat 1 and the parked seat 2.
func (m *machine) noLxc() {
	m.On(teamtest.SudoBus+"list alpha", strings.Replace(teamtest.BusList, "MEMBER\tproj.box+role.lxc\tops\tjoin\tactive\n", "", 1), 0)
}

// withDev adds a second member on seat dev, run by s, recorded with --teams dev@alpha.
func (m *machine) withDev(s *session) {
	m.On(teamtest.SudoBus+"list alpha", teamtest.BusList+"MEMBER\tproj.dev+local.podman\tdev\tjoin\tactive\n", 0)
	m.sessions = append(m.sessions, s)
	m.record(s.name, s.prefix, "dev@alpha")
	if s.live && s.prefix == "ccy" {
		m.desire(s)
	}
	m.HoldSeat(m.t, m.Checkout(), "alpha", s.seat, s.live)
}

// record writes a session record of prefix in the checkout with the --teams list.
func (m *machine) record(name, prefix, teams string) {
	extra := ""
	if prefix == "cc" {
		extra = "arg=--bus\n"
	}
	teamtest.MustWrite(m.t, filepath.Join(m.Root, "home/.local/state/ccy/sessions", name),
		"ccy-session-record 1\nname="+name+"\ndir="+m.Checkout()+"\nlauncher=/x/"+prefix+"\nprefix="+prefix+
			"\nrestore=yes\narg=--teams\narg="+teams+"\n"+extra)
}

// desire writes the desired membership a running ccy session's launch wrote.
func (m *machine) desire(s *session) {
	d := membership.Desired{
		V: 1, Session: s.name, Kind: s.prefix, Checkout: m.Checkout(), Generation: 3, WrittenMS: 1,
		Seats: []membership.Seat{{Team: "alpha", Seat: s.seat}}, Farewell: []membership.Farewell{},
	}
	if err := membership.Write(m.dir(s.name), d); err != nil {
		m.t.Fatal(err)
	}
}

func (m *machine) dir(name string) string { return membership.DirOf(m.runtime, "ccy", name) }

// conversationOf is the conversation a test session is in: one per name.
func conversationOf(name string) string {
	return fmt.Sprintf("%08x-0000-4000-8000-000000000000", crc32.ChecksumIEEE([]byte(name)))
}

// converse leaves what the session's SessionStart hook leaves: its conversation and transcript.
func (m *machine) converse(name, prefix string) {
	id := conversationOf(name)
	teamtest.MustWrite(m.t, filepath.Join(membership.SessionDir(m.Checkout(), name), conversation.File),
		`{"v":1,"session":"`+name+`","session_id":"`+id+`","source":"startup","written_ms":1}`)
	if prefix == "cc" {
		teamtest.MustWrite(m.t, filepath.Join(m.Checkout(), ".git", "HEAD"), "ref: refs/heads/main\n")
	}
	dir, err := transcript.Dir(prefix, m.Checkout(), conversation.ClaudeHome(m.Host.Getenv))
	if err != nil {
		m.t.Fatal(err)
	}
	teamtest.MustWrite(m.t, filepath.Join(dir, id+".jsonl"), "{}\n")
}

func (m *machine) find(name string) *session {
	for _, s := range m.sessions {
		if s.name == name {
			return s
		}
	}
	return nil
}

func (m *machine) fakeTmux() {
	m.Fake.OnFunc([]string{"tmux", "-L", "ccy", "list-sessions"}, func(execx.Cmd) (execx.Result, error) {
		var b strings.Builder
		for _, s := range m.sessions {
			if s.live {
				b.WriteString(s.name + " 0 " + m.Checkout() + "\n")
			}
		}
		return execx.Result{Stdout: []byte(b.String())}, nil
	})
	m.Fake.OnFunc([]string{"systemd-escape"}, func(c execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: []byte(c.Args[len(c.Args)-1] + "\n")}, nil
	})
	m.Fake.OnFunc([]string{"systemd-run"}, func(c execx.Cmd) (execx.Result, error) {
		at := slices.Index(c.Args, "-s")
		s := m.find(c.Args[at+1])
		s.live = true
		m.started = append(m.started, strings.Join(c.Args[slices.Index(c.Args, "ccy-tmux")+1:slices.Index(c.Args, ";")], " "))
		if s.holdsOnStart {
			m.HoldSeat(m.t, m.Checkout(), "alpha", s.seat, true)
		}
		return execx.Result{}, nil
	})
}

// fakePodman lists each running ccy session's container; a supervised one asked to restart
// comes back in a new container.
func (m *machine) fakePodman() {
	m.Fake.OnFunc([]string{"podman", "ps"}, func(execx.Cmd) (execx.Result, error) {
		var b strings.Builder
		for _, s := range m.sessions {
			if !s.live || s.prefix != "ccy" {
				continue
			}
			if pending, err := membership.RelaunchPending(m.dir(s.name)); err == nil && pending && s.supervised {
				s.container += "+"
			}
			b.WriteString(s.container + "\trunning\t" + s.name + "\n")
		}
		return execx.Result{Stdout: []byte(b.String())}, nil
	})
}

// stop makes s a stopped session: its record kept, no tmux session, its seat free.
func (m *machine) stop(s *session) {
	s.live = false
	m.HoldSeat(m.t, m.Checkout(), "alpha", s.seat, false)
}

func (m *machine) run(args ...string) term.ExitCode {
	return m.r.Run(m.t.Context(), args)
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
