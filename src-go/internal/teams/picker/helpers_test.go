package picker

import (
	"context"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/create"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/hot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/login"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/lxcadd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/restart"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seats"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/settings"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/shutdown"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// rig is the fake machine, on a terminal with scripted fzf and gum, and the Picker over the
// real flows it hands work to.
type rig struct {
	*teamtest.World
	*teamtest.Console

	b *board.Collector
	o *ops.Ops
	p *Picker
	// ses stands in for `ccy-teams session`: the sessions it lists and the commands it was run with.
	ses *fakeSessions
}

// fakeSessions is hot control as the picker sees it: canned sessions, and every command line
// it was asked to run, each answered with code.
type fakeSessions struct {
	list []hot.Session
	err  error
	runs [][]string
	code term.ExitCode
	// listed is how many times the sessions were asked for.
	listed int
}

func (f *fakeSessions) Sessions(context.Context) ([]hot.Session, error) {
	f.listed++
	return f.list, f.err
}

func (f *fakeSessions) Run(_ context.Context, args []string) term.ExitCode {
	f.runs = append(f.runs, args)
	return f.code
}

func newRig(t *testing.T) rig {
	t.Helper()
	w := teamtest.Populated(t)
	c := teamtest.Interactive(t, w)
	h := w.Host
	kit := flow.New(h)
	b := board.New(h)
	o := ops.New(h, b.Statuses, func() { b.ForgetDevices("") })
	l := login.New(h, kit, b, o)
	s := seats.New(h, kit, o)
	w.On(h.Play, "", 0)
	w.OnDeclare("element-teams", "")
	p := New(h, kit, b, o, create.New(h, o, l), l, s, lxcadd.New(h, kit, o))
	p.Shutdown = shutdown.New(h, kit, b)
	p.Restart = restart.New(h, kit, b)
	p.Settings = settings.New(h, kit, o.ApplyTeams, o.Locked, s.ChangeRole)
	ses := &fakeSessions{}
	p.Session = ses
	return rig{World: w, Console: c, b: b, o: o, p: p, ses: ses}
}

// run1 is one collection of the machine, and the picker state the key handlers act on.
func (h rig) run1(t *testing.T) *run {
	t.Helper()
	s, err := h.b.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return &run{p: h.p, snap: s}
}

// row is the row of member in the pass.
func (h rig) row(t *testing.T, snap *board.Snapshot, member string) board.Row {
	t.Helper()
	for _, row := range snap.Rows {
		if row.Member == member {
			return row
		}
	}
	t.Fatalf("no row for %s", member)
	return board.Row{}
}

// text is the row of member as the list shows it, laid out with the rest of the pass.
func (h rig) text(t *testing.T, snap *board.Snapshot, member string) string {
	t.Helper()
	return board.Layout(snap.Rows, 0).Text(h.row(t, snap, member))
}

// inProject makes the working directory a work tree whose top is the checkout.
func (h rig) inProject() {
	h.On("git rev-parse --is-inside-work-tree", "true\n", 0)
	h.On("git rev-parse --show-toplevel", h.Checkout()+"\n", 0)
}

func (h rig) run() term.ExitCode { return h.p.run(context.Background()) }

// plays is how many times the apply play was started.
func (h rig) plays() int {
	n := 0
	for _, c := range h.Fake.Calls() {
		if c.Name == h.Host.Play {
			n++
		}
	}
	return n
}

// outdatedUntilApplied makes alpha out of date (in the way detail says) until the apply play has run.
func (h rig) outdatedUntilApplied(detail string) {
	applied := false
	h.Fake.OnFunc(strings.Fields(h.Host.Play), func(execx.Cmd) (execx.Result, error) {
		applied = true
		return execx.Result{}, nil
	})
	h.Fake.OnFunc(strings.Fields(h.DeclarePrefix("overview")), func(execx.Cmd) (execx.Result, error) {
		state := "STATUS\talpha\toutdated\t" + detail + "\n"
		if applied {
			state = "STATUS\talpha\tcurrent\t-\n"
		}
		return execx.Result{Stdout: []byte(teamtest.DeclaredAlpha + state)}, nil
	})
}
