package hot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	session    = "ccy-proj"
	setRoleCmd = teamtest.SudoBus + "set-role "
	worker     = "worker"
	coord      = "coordinator"
)

// machine is the session's side of the fake: the seats the checkout has, which are held, and a
// keeper that follows the desired file whenever the seats are read.
type machine struct {
	mu        sync.Mutex
	exists    map[string]string // seat@team -> handle
	heldBy    map[string]bool   // held by another session
	keeper    bool              // the keeper follows the desired file
	alive     bool              // a seat keeper process runs in the container
	tells     bool              // the session is told its roles (the watcher's sync)
	inbound   bool
	container string
	relaunch  bool              // a relaunch request makes a new container
	roles     map[string]string // team\thandle -> role, for the checkout's seats and others
	others    map[string]int    // team -> members of other checkouts, beside roles
	onPs      func()            // run at each podman ps, when set
}

type rig struct {
	*teamtest.World

	x        *Hot
	m        *machine
	runtime  string
	storeDir string
	store    *registry.Store
	now      time.Time
}

// breakRecords makes every later record write fail: the registry's directory becomes a file.
func (r *rig) breakRecords(t *testing.T) {
	t.Helper()
	if err := os.RemoveAll(r.storeDir); err != nil {
		t.Fatal(err)
	}
	teamtest.MustWrite(t, r.storeDir, "not a directory\n")
}

// refParts is a seat@team as its seat and team.
func refParts(ref string) (string, string) {
	parts := strings.SplitN(ref, "@", 2)
	if len(parts) != 2 {
		return ref, ""
	}
	return parts[0], parts[1]
}

// teamOf is the team of a seat@team, and seatOfRef its seat.
func teamOf(ref string) string {
	parts := strings.SplitN(ref, "@", 2)
	return parts[len(parts)-1]
}

func seatOfRef(ref string) string { return strings.SplitN(ref, "@", 2)[0] }

func newRig(t *testing.T, seats ...membership.Seat) *rig {
	t.Helper()
	w := teamtest.New(t)
	r := &rig{World: w, runtime: t.TempDir(), storeDir: t.TempDir(), now: time.UnixMilli(1760000000000)}
	r.store = registry.New(r.storeDir)
	if err := os.MkdirAll(w.Checkout(), 0o700); err != nil {
		t.Fatal(err)
	}
	r.m = &machine{
		exists: map[string]string{"3@alpha": "proj.3+local.podman", "1@beta": "proj.1+local.podman"},
		heldBy: map[string]bool{}, keeper: true, alive: true, tells: true, container: "abc123", relaunch: true,
		roles: map[string]string{}, others: map[string]int{},
	}
	w.Env["XDG_RUNTIME_DIR"] = r.runtime
	w.Host.Registry = func() (*registry.Store, error) { return r.store, nil }
	args := []string{"--no-ssh"}
	if len(seats) > 0 {
		args = append(args, "--teams", membership.Canonical(seats))
	}
	if err := r.store.Write(registry.Record{Name: session, Dir: w.Checkout(), Launcher: "/x/ccy", Prefix: "ccy", Restore: true, Args: args}); err != nil {
		t.Fatal(err)
	}
	if seats == nil {
		seats = []membership.Seat{}
	}
	if err := membership.Write(r.dir(), membership.Desired{
		V: 1, Session: session, Kind: "ccy", Checkout: w.Checkout(), Generation: 1, WrittenMS: 1,
		Seats: seats, Farewell: []membership.Farewell{},
	}); err != nil {
		t.Fatal(err)
	}
	r.m.inbound = len(seats) > 0
	r.writeReport(t)
	r.x = New(w.Host, flow.New(w.Host))
	r.x.Now = func() time.Time { return r.now }
	r.x.Sleep = func(d time.Duration) { r.now = r.now.Add(d) }
	r.script(t)
	return r
}

func (r *rig) dir() string { return membership.Dir(r.runtime, session) }

// role is a member's role in the fake roster, worker unless set.
func (m *machine) role(team, handle string) string {
	if role, ok := m.roles[team+"\t"+handle]; ok {
		return role
	}
	return worker
}

type toldRole struct {
	Team   string `json:"team"`
	Handle string `json:"handle"`
	Role   string `json:"role"`
}

// writeReport is the keeper's report for what the desired file says now.
func (r *rig) writeReport(t *testing.T) {
	t.Helper()
	d, err := membership.Read(r.dir())
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	told := struct {
		Generation int        `json:"generation"`
		Roles      []toldRole `json:"roles"`
	}{Generation: d.Generation, Roles: []toldRole{}}
	rep := membership.Report{V: 1, Generation: d.Generation, Inbound: &r.m.inbound, Seats: []membership.ReportSeat{}, Refused: []membership.ReportRefused{}}
	for _, s := range d.Seats {
		ref := s.Seat + "@" + s.Team
		if r.m.keeper && !r.m.heldBy[ref] {
			rep.Seats = append(rep.Seats, membership.ReportSeat{Team: s.Team, Seat: s.Seat, Handle: r.m.exists[ref], State: "held"})
			if r.m.tells {
				told.Roles = append(told.Roles, toldRole{Team: s.Team, Handle: r.m.exists[ref], Role: r.m.role(s.Team, r.m.exists[ref])})
			}
		} else {
			rep.Refused = append(rep.Refused, membership.ReportRefused{Team: s.Team, Seat: s.Seat, Why: "no-bundle", Holder: "-"})
		}
	}
	raw, err := json.Marshal(told)
	if err != nil {
		t.Fatal(err)
	}
	rep.Told = raw
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	teamtest.MustWrite(t, membership.ReportPath(r.Checkout(), session), string(data))
}

func (r *rig) podmanPs(t *testing.T) execx.Result {
	t.Helper()
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	if r.m.onPs != nil {
		r.m.onPs()
	}
	request := filepath.Join(r.dir(), membership.RelaunchFile)
	if _, err := os.Lstat(request); err == nil && r.m.relaunch {
		if err := os.Remove(request); err != nil {
			t.Error(err)
		}
		r.m.container, r.m.inbound = "def456", true
		r.writeReport(t)
	}
	if r.m.container == "" {
		return execx.Result{}
	}
	return execx.Result{Stdout: []byte(r.m.container + "\trunning\n")}
}

func (r *rig) seatList(t *testing.T) execx.Result {
	t.Helper()
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	r.writeReport(t)
	d, err := membership.Read(r.dir())
	if err != nil && !os.IsNotExist(err) {
		t.Error(err)
	}
	refs := make([]string, 0, len(r.m.exists))
	for ref := range r.m.exists {
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	var b strings.Builder
	for _, ref := range refs {
		seat, team := refParts(ref)
		state := "free"
		mine := r.m.keeper && slices.Contains(d.Seats, membership.Seat{Team: team, Seat: seat}) && r.m.container != ""
		if r.m.heldBy[ref] || mine {
			state = "held"
		}
		b.WriteString("SEAT\t" + team + "\t" + seat + "\t" + state + "\t-\t" + r.m.exists[ref] + "\n")
	}
	return execx.Result{Stdout: []byte(b.String())}
}

// roster is "agent-bus list <team>": the checkout's seats of the team, then the members others
// added, each with its role.
func (r *rig) roster(team string) execx.Result {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	var b strings.Builder
	b.WriteString("HUMAN\towner\tjoin\n")
	var handles []string
	for ref, handle := range r.m.exists {
		if teamOf(ref) == team {
			handles = append(handles, handle)
		}
	}
	for key := range r.m.roles {
		parts := strings.SplitN(key, "\t", 2)
		if parts[0] == team && !slices.Contains(handles, parts[1]) {
			handles = append(handles, parts[1])
		}
	}
	slices.Sort(handles)
	for i := range r.m.others[team] {
		handles = append(handles, "other"+strconv.Itoa(i)+".1+local.podman")
	}
	for _, h := range handles {
		b.WriteString("MEMBER\t" + h + "\t" + r.m.role(team, h) + "\tjoin\tactive\n")
	}
	return execx.Result{Stdout: []byte(b.String())}
}

func (r *rig) script(t *testing.T) {
	t.Helper()
	r.On("systemctl is-active agent-bus-hs@alpha.service", "active\n", 0)
	r.On("systemctl is-active agent-bus-hs@beta.service", "active\n", 0)
	r.On("tmux -L ccy list-sessions", session+" 0 "+r.Checkout()+"\n", 0)
	r.On("podman exec abc123 pingbus reach --team", "", 0)
	r.On("/usr/local/share/agent-bus/kit/pingbus seat retire", "", 0)
	r.On("/usr/local/share/agent-bus/kit/pingbus seat farewell", "", 0)
	r.On("sudo -n -v", "", 0)
	r.Fake.OnFunc(strings.Fields(teamtest.SudoBus+"list"), func(c execx.Cmd) (execx.Result, error) { return r.roster(c.Args[3]), nil })
	r.Fake.OnFunc(strings.Fields(setRoleCmd), func(c execx.Cmd) (execx.Result, error) {
		r.m.mu.Lock()
		defer r.m.mu.Unlock()
		r.m.roles[c.Args[3]+"\t"+c.Args[4]] = strings.TrimPrefix(c.Args[5], "--role=")
		r.writeReport(t)
		return execx.Result{}, nil
	})
	r.Fake.OnFunc([]string{"podman", "ps"}, func(execx.Cmd) (execx.Result, error) { return r.podmanPs(t), nil })
	r.Fake.OnFunc([]string{"podman", "top"}, func(execx.Cmd) (execx.Result, error) {
		r.m.mu.Lock()
		defer r.m.mu.Unlock()
		out := "COMMAND\n/bin/bash /usr/local/bin/entrypoint\n"
		if r.m.alive {
			out += "python3 -P /usr/local/bin/pingbus seat keep --launch --parent 7\n"
		}
		return execx.Result{Stdout: []byte(out)}, nil
	})
	r.Fake.OnFunc([]string{"podman", "exec", "-d"}, func(execx.Cmd) (execx.Result, error) {
		r.m.mu.Lock()
		defer r.m.mu.Unlock()
		r.m.alive, r.m.keeper = true, true
		return execx.Result{}, nil
	})
	r.Fake.OnFunc([]string{"agent-bus", "seat", "list"}, func(execx.Cmd) (execx.Result, error) { return r.seatList(t), nil })
	r.Fake.OnFunc([]string{"agent-bus", "seat", "take"}, func(c execx.Cmd) (execx.Result, error) {
		r.m.mu.Lock()
		defer r.m.mu.Unlock()
		r.m.exists[c.Args[2]] = "proj." + seatOfRef(c.Args[2]) + "+local.podman"
		return execx.Result{}, nil
	})
	r.On("agent-bus seat choose alpha --type=podman", "CHOICE\t3\tfree\tproj.3+local.podman\tw\nCHOICE\t4\tnew\tproj.4+local.podman\tw\nDEFAULT\t3\n", 0)
}

func (r *rig) run(t *testing.T, args ...string) term.ExitCode {
	t.Helper()
	r.ErrOut.Reset()
	r.Out.Reset()
	return r.x.Run(t.Context(), args)
}

func (r *rig) desired(t *testing.T) membership.Desired {
	t.Helper()
	d, err := membership.Read(r.dir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (r *rig) recordArgs(t *testing.T) []string {
	t.Helper()
	rec, err := r.store.Get(session)
	if err != nil {
		t.Fatal(err)
	}
	return rec.Args
}

// setRoles is every set-role run, in order, as "<team> <handle> <flag>".
func (r *rig) setRoles() []string {
	var calls []string
	for _, c := range r.Fake.Calls() {
		if line := c.Line(); strings.HasPrefix(line, setRoleCmd) {
			calls = append(calls, strings.TrimPrefix(line, setRoleCmd))
		}
	}
	return calls
}

func wantLines(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w+"\n") {
			t.Errorf("stderr lacks %q:\n%s", w, got)
		}
	}
}

// onlySkips fails unless every step line is "-" (the checks of the session and the team aside).
func onlySkips(t *testing.T, got string) {
	t.Helper()
	for line := range strings.Lines(got) {
		if (strings.HasPrefix(line, "✓") && !strings.Contains(line, "can be hot-controlled") && !strings.Contains(line, ": homeserver up, ")) || strings.HasPrefix(line, "✗") {
			t.Fatalf("a rerun does nothing, every step '-':\n%s", got)
		}
	}
}

func TestRefusals(t *testing.T) {
	r := newRig(t)
	r.On("systemctl is-active agent-bus-hs@alpha.service", "inactive\n", 3)
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitFailure ||
		!strings.Contains(r.ErrOut.String(), "✗ team alpha: homeserver for alpha is down: systemctl status agent-bus-hs@alpha") {
		t.Fatalf("down: exit %d:\n%s", code, r.ErrOut.String())
	}
	r.On("systemctl is-active agent-bus-hs@alpha.service", "active\n", 0)
	for _, args := range [][]string{
		{"add", session, "alpha/3"},
		{"add", session, "Alpha", "--yes"},
		{"frob"},
		{"add", session, "alpha", "--coordinator=demote", "--yes"},
		{"remove", session, "alpha", "--role=worker", "--yes"},
		{"remove", session, "alpha", "--coordinator=sideways", "--yes"},
		{"move", session, "alpha", "alpha/3", "--yes"},
		{"move", session, "alpha", "--yes"},
		{"role", session, "alpha", "boss", "--yes"},
		{"repair", session, "alpha", "--yes"},
		{"list", "extra"},
		{"add", session, "alpha", "--role=boss", "--yes"},
	} {
		if code := r.run(t, args...); code != term.ExitUsage {
			t.Fatalf("%v: exit %d:\n%s", args, code, r.ErrOut.String())
		}
	}
	r.m.container = ""
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != exitConfig {
		t.Fatalf("not running: exit %d:\n%s", code, r.ErrOut.String())
	}
	r.m.container = "abc123"
	if err := os.Remove(filepath.Join(r.dir(), membership.File)); err != nil {
		t.Fatal(err)
	}
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != exitConfig ||
		!strings.Contains(r.ErrOut.String(), "relaunch") {
		t.Fatalf("no membership: exit %d:\n%s", code, r.ErrOut.String())
	}
}

func TestAChangeInProgressRefusesAnotherOnTheSameSession(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
	lockPath, err := changePath(r.runtime, session)
	if err != nil {
		t.Fatal(err)
	}
	held, err := takeLock(lockPath, "ccy-teams session add ccy-proj alpha (pid 41)")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", session, "alpha", "--yes"},
		{"remove", session, "beta", "--yes"},
		{"move", session, "beta", "alpha", "--yes"},
		{"role", session, "beta", coord, "--yes"},
		{"repair", session, "--yes"},
	} {
		if code := r.run(t, args...); code != exitBusy {
			t.Fatalf("%v: exit %d:\n%s", args, code, r.ErrOut.String())
		}
		wantLines(t, r.ErrOut.String(), "✗ session "+session+": another change to it is in progress: ccy-teams session add ccy-proj alpha (pid 41)")
	}
	if r.desired(t).Generation != 1 || len(r.setRoles()) != 0 {
		t.Fatal("nothing is changed while another change runs")
	}
	if releaseErr := held.release(); releaseErr != nil {
		t.Fatal(releaseErr)
	}
	if code := r.run(t, "add", session, "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("free again: exit %d:\n%s", code, r.ErrOut.String())
	}
	if _, statErr := os.Lstat(filepath.Join(r.dir(), ".change.lock")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a change lock was made in the mounted membership directory: %v", statErr)
	}
	data, err := os.ReadFile(filepath.Clean(lockPath))
	if err != nil || !strings.HasPrefix(string(data), "ccy-teams session add ccy-proj alpha (pid ") {
		t.Fatalf("the change names itself in the lock: %q %v", data, err)
	}
}
