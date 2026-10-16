package hot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

const session = "ccy-proj"

// machine is the session's side of the fake: the seats the checkout has, which are held, and a
// keeper that follows the desired file whenever the seats are read.
type machine struct {
	mu        sync.Mutex
	exists    map[string]string // seat@team -> handle
	heldBy    map[string]bool   // held by another session
	keeper    bool              // the keeper follows the desired file
	inbound   bool
	container string
	relaunch  bool // a relaunch request makes a new container
}

type rig struct {
	*teamtest.World

	x       *Hot
	m       *machine
	runtime string
	store   *registry.Store
	now     time.Time
}

// refParts is a seat@team as its seat and team.
func refParts(ref string) (string, string) {
	parts := strings.SplitN(ref, "@", 2)
	if len(parts) != 2 {
		return ref, ""
	}
	return parts[0], parts[1]
}

func newRig(t *testing.T, seats ...membership.Seat) *rig {
	t.Helper()
	w := teamtest.New(t)
	r := &rig{World: w, runtime: t.TempDir(), store: registry.New(t.TempDir()), now: time.UnixMilli(1760000000000)}
	r.m = &machine{
		exists: map[string]string{"3@alpha": "proj.3+local.podman", "1@beta": "proj.1+local.podman"},
		heldBy: map[string]bool{}, keeper: true, container: "abc123", relaunch: true,
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

// writeReport is the keeper's report for what the desired file says now.
func (r *rig) writeReport(t *testing.T) {
	t.Helper()
	d, err := membership.Read(r.dir())
	if err != nil {
		t.Fatal(err)
	}
	rep := membership.Report{V: 1, Generation: d.Generation, Inbound: &r.m.inbound, Seats: []membership.ReportSeat{}, Refused: []membership.ReportRefused{}}
	for _, s := range d.Seats {
		ref := s.Seat + "@" + s.Team
		if r.m.keeper && !r.m.heldBy[ref] {
			rep.Seats = append(rep.Seats, membership.ReportSeat{Team: s.Team, Seat: s.Seat, Handle: r.m.exists[ref], State: "held"})
		} else {
			rep.Refused = append(rep.Refused, membership.ReportRefused{Team: s.Team, Seat: s.Seat, Why: "no-bundle", Holder: "-"})
		}
	}
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
	if err != nil {
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

func (r *rig) script(t *testing.T) {
	t.Helper()
	r.On("systemctl is-active agent-bus-hs@alpha.service", "active\n", 0)
	r.On("tmux -L ccy list-sessions", session+" 0 "+r.Checkout()+"\n", 0)
	r.On("podman exec abc123 pingbus reach --team alpha", "", 0)
	r.On("/usr/local/share/agent-bus/kit/pingbus seat retire", "", 0)
	r.On("/usr/local/share/agent-bus/kit/pingbus seat farewell", "", 0)
	r.On(teamtest.SudoBus+"list alpha", "MEMBER\tproj.3+local.podman\tworker\tjoin\tactive\n", 0)
	r.Fake.OnFunc([]string{"podman", "ps"}, func(execx.Cmd) (execx.Result, error) { return r.podmanPs(t), nil })
	r.Fake.OnFunc([]string{"agent-bus", "seat", "list"}, func(execx.Cmd) (execx.Result, error) { return r.seatList(t), nil })
	r.Fake.OnFunc([]string{"agent-bus", "seat", "take"}, func(c execx.Cmd) (execx.Result, error) {
		r.m.mu.Lock()
		defer r.m.mu.Unlock()
		seat, _, found := strings.Cut(c.Args[2], "@")
		if !found {
			t.Errorf("seat take of %q", c.Args[2])
		}
		r.m.exists[c.Args[2]] = "proj." + seat + "+local.podman"
		return execx.Result{}, nil
	})
	r.On("agent-bus seat choose alpha --type=podman", "CHOICE\t3\tfree\tproj.3+local.podman\tw\nCHOICE\t4\tnew\tproj.4+local.podman\tw\nDEFAULT\t3\n", 0)
}

func (r *rig) run(t *testing.T, args ...string) term.ExitCode {
	t.Helper()
	r.ErrOut.Reset()
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

func wantLines(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w+"\n") {
			t.Errorf("stderr lacks %q:\n%s", w, got)
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
	if code := r.run(t, "add", session, "alpha/3"); code != term.ExitUsage {
		t.Fatalf("no terminal, no --yes: exit %d", code)
	}
	if code := r.run(t, "add", session, "Alpha", "--yes"); code != term.ExitUsage {
		t.Fatalf("a bad team: exit %d", code)
	}
	if code := r.run(t, "frob"); code != term.ExitUsage {
		t.Fatalf("unknown: exit %d", code)
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
