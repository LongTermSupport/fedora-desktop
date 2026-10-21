// Package teamtest is the fake machine the ccy-teams packages are tested against: a Host over
// an execx.Fake and buffers, with helpers to script what the tools answer and to count what was
// started. Tests of the table, the picker and the app all build on it, so the machine they
// describe is the same one.
package teamtest

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/hostcheckout"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/playlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/config"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
)

// What the scripted tools answer for team alpha.
const (
	BusList = "HUMAN\towner\tjoin\n" +
		"HUMAN\tguest\tinvite\n" +
		"HUMAN\tghost\t-\n" +
		"MEMBER\tproj.1+local.podman\tdev\tjoin\tactive\n" +
		"MEMBER\tproj.2+local.podman\tdev\tjoin\tparked\n" +
		"MEMBER\tproj.box+role.lxc\tops\tjoin\tactive\n"
	DeclaredAlpha = "TEAM\talpha\t6167\tpresent\talpha.agent-bus.internal\thttp://192.0.2.10:6167\t-\n"
	UnitAlpha     = "agent-bus-hs@alpha.service loaded active running Homeserver alpha\n"
	// LxcPath is where root's LXC keeps its containers (hostcheckout's default); LxcContainer is
	// one of them.
	LxcPath      = hostcheckout.DefaultLxcPath
	LxcContainer = "box"
	// SudoBus is the start of every agent-bus call made as root.
	SudoBus = "sudo -n /usr/local/bin/agent-bus "
)

// World is a Host on a fake machine. Fake answers the commands, Out and ErrOut are the
// streams, Env is the environment, Root the checkout.
type World struct {
	Host   *host.Host
	Out    *bytes.Buffer
	ErrOut *bytes.Buffer
	Fake   *execx.Fake
	Env    map[string]string
	Root   string

	overview map[string]string
	counted  int

	seatMu    sync.Mutex
	seatLocks map[string]*os.File
}

// New is a Host over a scratch root with an installed ccy lib and the declare helper present,
// so the flows get past their start-up checks. Nothing is scripted yet.
func New(t *testing.T) *World {
	t.Helper()
	root := t.TempDir()
	lib := filepath.Join(root, "lib")
	MustWrite(t, filepath.Join(lib, "tmux-session.bash"), "")
	MustWrite(t, filepath.Join(root, "helpers", "agent_bus", "declare.py"), "")
	w := &World{
		Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}, Fake: &execx.Fake{}, Root: root,
		overview: map[string]string{},
	}
	// The runtime directory the play lock is taken in: the user's own, which only it can write.
	run := filepath.Join(root, "run")
	if err := os.Mkdir(run, 0o700); err != nil {
		t.Fatal(err)
	}
	w.Env = map[string]string{"CCY_LIB": lib, "HOME": filepath.Join(root, "home"), "XDG_RUNTIME_DIR": run}
	getenv := func(k string) string { return w.Env[k] }
	w.Host = &host.Host{
		Config: config.Load(root, getenv),
		Runner: w.Fake, Stdout: w.Out, Stderr: w.ErrOut, Getenv: getenv, Stdin: &bytes.Buffer{},
		InContainer: func() bool { return false },
	}
	w.Host.AppliedDir = filepath.Join(root, "applied")
	w.Host.Wire()
	// A run that made sudo ready ends by dropping the credential.
	w.On("sudo -k", "", 0)
	// Root's LXC configuration, read before a checkout is offered for a session: absent, so
	// LXC keeps its containers in the default LxcPath.
	w.Host.Checkouts.ReadFile = func(string) ([]byte, error) { return nil, fs.ErrNotExist }
	return w
}

// InLxcCheckout makes the working directory a checkout inside LXC container LxcContainer's root
// filesystem, seen from the host; it returns that checkout's top.
func (w *World) InLxcCheckout() string {
	top := LxcPath + "/" + LxcContainer + "/rootfs/srv/proj"
	w.On("git rev-parse --is-inside-work-tree", "true\n", 0)
	w.On("git rev-parse --show-toplevel", top+"\n", 0)
	return top
}

// Populated is New with team alpha declared, built and up, one checkout with a held seat, and a
// sudoers rule that lets the roster be read.
func Populated(t *testing.T) *World {
	t.Helper()
	w := New(t)
	w.AddSeat(t, w.Checkout(), "alpha", "1", "proj.1+local.podman")
	w.AddSeat(t, w.Checkout(), "alpha", "dev", "proj.dev+local.podman")
	w.HoldSeat(t, w.Checkout(), "alpha", "1", true)
	w.OnDeclare("declared", DeclaredAlpha)
	w.OnDeclare("status", "STATUS\talpha\tcurrent\t-\n")
	w.On("systemctl list-units", UnitAlpha, 0)
	w.On("systemctl is-active agent-bus-hs@alpha.service", "active\n", 0)
	w.On("tmux -L ccy list-sessions", "ccy-proj 0 "+w.Checkout()+"\n", 0)
	w.OnNotARepository()
	w.On(SudoBus+"list alpha", BusList, 0)
	w.On(SudoBus+"human devices alpha owner", "", 0)
	w.On(SudoBus+"human devices alpha guest", "DEVICE\ta\t-\nDEVICE\tb\t1\n", 0)
	w.On(SudoBus+"human devices alpha ghost", "DEVICE\ta\t-\n", 0)
	MustWrite(t, filepath.Join(w.Root, "home/.local/state/ccy/sessions/ccy-proj"),
		"ccy-session-record 1\nname=ccy-proj\ndir="+w.Checkout()+"\nlauncher=/x/ccy\nprefix=ccy\nrestore=yes\narg=--teams\narg=1@alpha\n")
	// The root health check's record (helpers/agent_bus/health.py): no problem, current until 2100.
	MustWrite(t, filepath.Join(w.Host.AppliedDir, "health", "alpha.json"),
		`{"checked": 1791000000, "expires": 4102444800, "problems": []}`)
	return w
}

// Checkout is the project checkout Populated sets up.
func (w *World) Checkout() string { return filepath.Join(w.Root, "proj") }

// On scripts every command whose argv starts with prefix (space-separated).
func (w *World) On(prefix, stdout string, code int) {
	w.Fake.On(strings.Fields(prefix), execx.Result{Stdout: []byte(stdout), ExitCode: code})
}

// OnErr is On with a stderr text, for commands that fail with a reason.
func (w *World) OnErr(prefix, stderr string, code int) {
	w.Fake.On(strings.Fields(prefix), execx.Result{Stderr: []byte(stderr), ExitCode: code})
}

// NotARepository is git's own refusal for a folder in no repository, the one refusal
// gitx.ProjectHere reads as "not a checkout".
const NotARepository = "fatal: not a git repository (or any of the parent directories): .git"

// OnNotARepository scripts git to say the folder is in no repository.
func (w *World) OnNotARepository() {
	w.OnErr("git rev-parse --is-inside-work-tree", NotARepository, 128)
}

// DeclarePrefix is the argv start of one declare.py subcommand.
func (w *World) DeclarePrefix(sub string) string {
	return "env PYTHONPATH=" + w.Host.RootDir + " /usr/bin/python3 -P -m helpers.agent_bus.declare " + sub
}

// OnDeclare scripts one declare.py subcommand. The list is read with "overview", which prints
// what "declared" prints and then what "status" prints, so scripting either of those scripts
// its half of the overview.
func (w *World) OnDeclare(sub, stdout string) {
	if sub != "declared" && sub != "status" {
		w.On(w.DeclarePrefix(sub), stdout, 0)
		return
	}
	w.overview[sub] = stdout
	w.Fake.OnFunc(strings.Fields(w.DeclarePrefix("overview")), func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: []byte(w.overview["declared"] + w.overview["status"])}, nil
	})
}

// Spawned counts the processes the fake runner was asked to start since the last call, by
// program and subcommand.
func (w *World) Spawned() map[string]int {
	counts := map[string]int{}
	calls := w.Fake.Calls()
	for _, c := range calls[w.counted:] {
		key := c.Name
		switch {
		case c.Name == "env" && len(c.Args) > 5 && strings.HasPrefix(c.Args[0], "PYTHONPATH="):
			key = "declare.py " + c.Args[5]
		case c.Name == "sudo" && len(c.Args) > 3 && c.Args[2] == "human":
			key = "agent-bus human " + c.Args[3]
		case c.Name == "sudo" && len(c.Args) > 2:
			key = "agent-bus " + c.Args[2]
		case c.Name == "agent-bus" && len(c.Args) > 1:
			key = "agent-bus (user) " + c.Args[0] + " " + c.Args[1]
		}
		counts[key]++
	}
	w.counted = len(calls)
	return counts
}

// HoldPlayNote is the holder line HoldPlayLock writes: another play run, a meta-deploy unit.
const HoldPlayNote = "pid=999 what=run.bash playbooks/imports/play-basic-configs.yml since=2026-10-16T11:59:20Z\n"

// HoldPlayLock takes the play lock in the world's runtime directory on a descriptor of its own,
// as another play run does, until the test ends or the returned func lets it go.
func (w *World) HoldPlayLock(t *testing.T) func() {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(w.Env["XDG_RUNTIME_DIR"], playlock.Name), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte(HoldPlayNote), 0); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			if err := f.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(release)
	return release
}

// SeatDir is the folder of seat of team in checkout.
func SeatDir(checkout, team, seat string) string {
	return filepath.Join(checkout, ".claude/ccy/pingbus/seats", team, seat)
}

// AddSeat makes seat of team in checkout as a seat take leaves it, read by the in-process seat
// reader: a member.json naming handle on the team's server. It is free until HoldSeat holds it.
func (w *World) AddSeat(t *testing.T, checkout, team, seat, handle string) {
	t.Helper()
	server := team + ".agent-bus.internal"
	MustWrite(t, filepath.Join(SeatDir(checkout, team, seat), "member.json"), `{"protocol": 1, "team": "`+team+
		`", "user_id": "@`+handle+`:`+server+`", "server_name": "`+server+
		`", "base_url": "http://192.0.2.10:6167", "plain_http_hosts": ["192.0.2.10"], "token_file": "token", "admin": "@admin:`+server+
		`", "room": "!`+strings.Repeat("A", 43)+`"}`)
}

// RemoveSeat lets go of seat's lock and removes its folder, as a seat retire does.
func (w *World) RemoveSeat(t *testing.T, checkout, team, seat string) {
	t.Helper()
	w.HoldSeat(t, checkout, team, seat, false)
	if err := os.RemoveAll(SeatDir(checkout, team, seat)); err != nil {
		t.Fatal(err)
	}
}

// BreakSeat makes seat's member.json one the reader refuses, so the seat is left out and named.
func (w *World) BreakSeat(t *testing.T, checkout, team, seat string) {
	t.Helper()
	MustWrite(t, filepath.Join(SeatDir(checkout, team, seat), "member.json"), "{}")
}

// HoldSeat takes seat's seat.lock on a descriptor of its own, as the session sitting in it
// does, until the test ends; held false lets it go. Asking for the state it is in does nothing.
func (w *World) HoldSeat(t *testing.T, checkout, team, seat string, held bool) {
	t.Helper()
	path := filepath.Join(SeatDir(checkout, team, seat), "seat.lock")
	w.seatMu.Lock()
	defer w.seatMu.Unlock()
	if w.seatLocks == nil {
		w.seatLocks = map[string]*os.File{}
	}
	f, holding := w.seatLocks[path]
	switch {
	case held == holding:
		return
	case !held:
		delete(w.seatLocks, path)
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	w.seatLocks[path] = f
	t.Cleanup(func() { w.HoldSeat(t, checkout, team, seat, false) })
}

// PlayLockHeld is whether anyone holds the play lock in the world's runtime directory now.
func (w *World) PlayLockHeld(t *testing.T) bool {
	t.Helper()
	probe := playlock.New(func(k string) string { return w.Env[k] })
	lock, err := probe.Take(context.Background(), "probe", 0, nil)
	if errors.As(err, new(*playlock.TimeoutError)) {
		return true
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	return false
}

// MustWrite writes a file, making its directories.
func MustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
