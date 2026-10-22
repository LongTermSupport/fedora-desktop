package board

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// held is a runner over the fake that holds a command, or an in-process seat read, until what
// it waits for has happened, with nothing held while the fake (or the reader) answers, so the
// pass under test decides what runs together. A call is known by its key: a command's line and
// the folder it runs in ("line|dir"), a seat read seatsKey of the checkout top it reads.
type held struct {
	inner execx.Runner
	// wait is what a call waits for before it is answered; ready closes when it may go, and
	// nil lets it go at once.
	wait func(key string) <-chan struct{}
	// finished is told each call's key once its answer is known.
	finished func(key string)

	mu       sync.Mutex
	running  int
	peak     int
	timedOut bool
}

// stuck is how long a held call waits before it is let go: only a pass that makes the calls
// one after another ever waits that long, and the test then fails instead of hanging.
const stuck = 20 * time.Second

func commandKey(c execx.Cmd) string { return c.Line() + "|" + c.Dir }

// seatsKey is the key of the in-process seat read of a checkout top.
func seatsKey(top string) string { return "seats|" + top }

func (h *held) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	var res execx.Result
	var err error
	h.pass(commandKey(c), func() { res, err = h.inner.Run(ctx, c) })
	return res, err
}

func (h *held) pass(key string, answer func()) {
	h.mu.Lock()
	h.running++
	h.peak = max(h.peak, h.running)
	h.mu.Unlock()
	if ready := h.wait(key); ready != nil {
		select {
		case <-ready:
		case <-time.After(stuck):
			h.mu.Lock()
			h.timedOut = true
			h.mu.Unlock()
		}
	}
	answer()
	h.mu.Lock()
	h.running--
	h.mu.Unlock()
	if h.finished != nil {
		h.finished(key)
	}
}

// overSeats sends every seat read c makes through h, as h's runner sends a command: the real
// reader answers, once h lets the read go.
func (h *held) overSeats(c *Collector) {
	c.Bus.ListSeats = func(top string, uid int) (seat.Listing, error) {
		var listing seat.Listing
		var err error
		h.pass(seatsKey(top), func() { listing, err = seat.List(top, uid) })
		return listing, err
	}
}

// overRunner is a Collector of w whose every command, and every seat read, goes through h first.
func overRunner(w *teamtest.World, h *held) *Collector {
	host := w.Host
	host.Sudo, host.Systemd, host.Tmux, host.Git, host.Checkouts, host.Declare, host.Bus = nil, nil, nil, nil, nil, nil, nil
	host.Runner = h
	host.Wire()
	host.Checkouts.ReadFile = func(string) ([]byte, error) { return nil, fs.ErrNotExist }
	c := New(host)
	h.overSeats(c)
	return c
}

// busy is a machine with several teams up, each with one person, and several checkouts with a
// seat directory, each a ccy session's: the board's slow case, one agent-bus call after another.
func busy(t *testing.T, teams []string, checkouts int) *teamtest.World {
	t.Helper()
	w := teamtest.New(t)
	var declared, status, units, tmux strings.Builder
	declared.WriteString("agent_bus_teams:\n")
	for i, team := range teams {
		fmt.Fprintf(&declared, "  - team: %s\n    port: %d\n", team, 6167+i)
		fmt.Fprintf(&status, "STATUS\t%s\tcurrent\t-\n", team)
		fmt.Fprintf(&units, "agent-bus-hs@%s.service loaded active running Homeserver %s\n", team, team)
		w.On(teamtest.SudoBus+"list "+team, "HUMAN\towner\tjoin\nMEMBER\tp0.1+local.podman\tdev\tjoin\tactive\n", 0)
		w.On(teamtest.SudoBus+"human devices "+team+" owner", "DEVICE\ta\t-\n", 0)
	}
	for i := range checkouts {
		dir := checkoutOf(w, i)
		w.AddSeat(t, dir, teams[0], "1", filepath.Base(dir)+".1+local.podman")
		fmt.Fprintf(&tmux, "ccy-p%d 0 %s\n", i, dir)
	}
	w.WriteHostVars(t, declared.String())
	w.OnDeclare("status", status.String())
	w.On("systemctl list-units", units.String(), 0)
	w.On("tmux -L ccy list-sessions", tmux.String(), 0)
	w.OnNotARepository()
	return w
}

func checkoutOf(w *teamtest.World, i int) string { return filepath.Join(w.Root, fmt.Sprintf("p%d", i)) }

// topOf is checkoutOf with its symlinks resolved, as the seat reader is handed it.
func topOf(t *testing.T, w *teamtest.World, i int) string {
	t.Helper()
	top, err := filepath.EvalSymlinks(checkoutOf(w, i))
	if err != nil {
		t.Fatal(err)
	}
	return top
}

// seatReads records the checkout top of each seat read c makes, which the real reader answers.
func seatReads(c *Collector) func() []string {
	var mu sync.Mutex
	var tops []string
	c.Bus.ListSeats = func(top string, uid int) (seat.Listing, error) {
		mu.Lock()
		tops = append(tops, top)
		mu.Unlock()
		return seat.List(top, uid)
	}
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(tops)
	}
}

// noSeatListProcess fails the test if any agent-bus seat list process ran: the seats are read
// in-process.
func noSeatListProcess(t *testing.T, w *teamtest.World) {
	t.Helper()
	if n := w.Spawned()["agent-bus seat list"]; n != 0 {
		t.Fatalf("agent-bus seat list ran %d times; the seats are read in-process", n)
	}
}

func TestCollectReadsTheSeatsOfACheckoutOnceHoweverItIsNamed(t *testing.T) {
	h := populated(t)
	alias := filepath.Join(h.Root, "proj-link")
	if err := os.Symlink(h.Checkout(), alias); err != nil {
		t.Fatal(err)
	}
	h.On("tmux -L ccy list-sessions", "ccy-proj 0 "+h.Checkout()+"\nccy-link 0 "+alias+"\n", 0)
	reads := seatReads(h.c)
	s, err := h.c.Collect(t.Context())
	if err != nil {
		t.Fatalf("%v: %s", err, h.ErrOut.String())
	}
	if tops := reads(); len(tops) != 1 {
		t.Fatalf("the seats were read %d times for one checkout under two names: %v", len(tops), tops)
	}
	noSeatListProcess(t, h.World)
	// Each name the checkout is known by still has its seats listed under it, as before.
	under := map[string]int{}
	for _, row := range s.Rows {
		if row.Kind == KindCcy && row.Dir != "-" {
			under[row.Dir]++
		}
	}
	if under[h.Checkout()] != 2 || under[alias] != 2 {
		t.Fatalf("seat rows by folder %v", under)
	}
}

func TestCollectReadsEachCheckoutsSeatsOnce(t *testing.T) {
	w := busy(t, []string{"alpha"}, 3)
	c := collectorOf(w)
	reads := seatReads(c)
	s, err := c.Collect(t.Context())
	if err != nil {
		t.Fatalf("%v: %s", err, w.ErrOut.String())
	}
	want := []string{topOf(t, w, 0), topOf(t, w, 1), topOf(t, w, 2)}
	got := reads()
	if len(got) != len(want) {
		t.Fatalf("the seats were read in %v, want once in each of %v", got, want)
	}
	for _, top := range want {
		if strings.Count(strings.Join(got, "\n")+"\n", top+"\n") != 1 {
			t.Fatalf("the seats were read in %v, want once in each of %v", got, want)
		}
	}
	noSeatListProcess(t, w)
	// Each checkout's seat, read from its own folder, is on the board.
	var dirs []string
	for _, row := range s.Rows {
		if row.Kind == KindCcy && row.Dir != "-" {
			dirs = append(dirs, row.Dir)
		}
	}
	if !slices.Equal(dirs, []string{checkoutOf(w, 0), checkoutOf(w, 1), checkoutOf(w, 2)}) {
		t.Fatalf("seat rows in %v, want one in each checkout", dirs)
	}
}

func TestCollectRunsTheAgentBusCallsTogether(t *testing.T) {
	w := busy(t, []string{"alpha", "beta"}, 2)
	// The two seat reads and the two rosters wait until all four are running at once.
	const together = 4
	var mu sync.Mutex
	entered := 0
	all := make(chan struct{})
	gate := &held{inner: w.Fake, wait: func(key string) <-chan struct{} {
		if !strings.HasPrefix(key, seatsKey("")) && !strings.HasPrefix(key, teamtest.SudoBus+"list ") {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		if entered++; entered == together {
			close(all)
		}
		return all
	}}
	c := overRunner(w, gate)
	if _, err := c.Collect(t.Context()); err != nil {
		t.Fatalf("%v: %s", err, w.ErrOut.String())
	}
	if gate.timedOut || gate.peak < together {
		t.Fatalf("at most %d calls ran at once (timed out: %v); the seat reads and rosters should run together", gate.peak, gate.timedOut)
	}
	if gate.peak > parallel {
		t.Fatalf("%d calls ran at once, more than the bound of %d", gate.peak, parallel)
	}
	noSeatListProcess(t, w)
}

func TestCollectRunsNoMoreThanItsBoundAtOnce(t *testing.T) {
	w := busy(t, []string{"alpha", "beta", "gamma", "delta"}, 6)
	gate := &held{inner: w.Fake, wait: func(string) <-chan struct{} { return nil }}
	c := overRunner(w, gate)
	c.Parallel = 2
	if _, err := c.Collect(t.Context()); err != nil {
		t.Fatalf("%v: %s", err, w.ErrOut.String())
	}
	if gate.peak > 2 {
		t.Fatalf("%d calls ran at once with a bound of 2", gate.peak)
	}
}

// failing is busy with a failure in each kind of call: the second checkout's seat, which its
// reader refuses, beta's roster and gamma's person's sessions.
func failing(t *testing.T) *teamtest.World {
	t.Helper()
	w := busy(t, []string{"alpha", "beta", "gamma"}, 2)
	w.BreakSeat(t, checkoutOf(w, 1), "alpha", "1")
	w.OnErr(teamtest.SudoBus+"list beta", "agent-bus: beta refused\n", 70)
	w.OnErr(teamtest.SudoBus+"human devices gamma owner", "agent-bus: gamma owner refused\n", 70)
	return w
}

// outOfOrder is a runner over w's fake that makes the first of each kind of call finish last:
// the first checkout's seat read after the second's, and alpha's roster after gamma's person's
// sessions, the last call the board uses.
func outOfOrder(t *testing.T, w *teamtest.World) *held {
	t.Helper()
	var mu sync.Mutex
	done := map[string]chan struct{}{}
	signal := func(key string) chan struct{} {
		mu.Lock()
		defer mu.Unlock()
		if done[key] == nil {
			done[key] = make(chan struct{})
		}
		return done[key]
	}
	tops := []string{topOf(t, w, 0), topOf(t, w, 1)}
	return &held{
		inner: w.Fake,
		wait: func(key string) <-chan struct{} {
			switch key {
			case seatsKey(tops[0]):
				return signal(seatsKey(tops[1]))
			case teamtest.SudoBus + "list alpha|":
				return signal(teamtest.SudoBus + "human devices gamma owner|")
			}
			return nil
		},
		finished: func(key string) { close(signal(key)) },
	}
}

func TestCollectReportsInTheOrderOfTheTeamsHoweverTheCallsFinish(t *testing.T) {
	// One at a time is the order the board has always reported in.
	one := failing(t)
	c := collectorOf(one)
	c.Parallel = 1
	s, err := c.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	wantErr := strings.ReplaceAll(one.ErrOut.String(), one.Root, "<root>")
	at := -1
	for _, said := range []string{
		"agent-bus: seat 1@alpha: team alpha: <root>/p1/", "agent-bus seat list could not read every seat in <root>/p1",
		"beta refused", "gamma owner refused",
	} {
		next := strings.Index(wantErr, said)
		if next <= at {
			t.Fatalf("%q is not reported after what came before it in %q", said, wantErr)
		}
		at = next
	}

	w := failing(t)
	gate := outOfOrder(t, w)
	got, err := overRunner(w, gate).Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if gate.timedOut {
		t.Fatal("the calls did not run together, so they could not finish out of order")
	}
	if said := strings.ReplaceAll(w.ErrOut.String(), w.Root, "<root>"); said != wantErr {
		t.Fatalf("stderr together:\n%s\none at a time:\n%s", said, wantErr)
	}
	together := strings.ReplaceAll(tableText(got), w.Root, "<root>")
	if alone := strings.ReplaceAll(tableText(s), one.Root, "<root>"); together != alone {
		t.Fatalf("table together:\n%s\none at a time:\n%s", together, alone)
	}
	if got.NeedSudo != s.NeedSudo || got.Incomplete != s.Incomplete || got.MembersUnknown["beta"] != "read failed (above)" {
		t.Fatalf("together: need sudo %q incomplete %q beta %q", got.NeedSudo, got.Incomplete, got.MembersUnknown["beta"])
	}
}

func tableText(s *Snapshot) string {
	table := Layout(s.Rows, 0)
	var b strings.Builder
	for _, row := range s.Rows {
		b.WriteString(table.Text(row) + "\n")
	}
	return b.String()
}

func TestCollectMembersReadsAPersonListedTwiceOnce(t *testing.T) {
	w := busy(t, []string{"alpha", "beta"}, 0)
	w.On(teamtest.SudoBus+"list beta", "HUMAN\towner\tjoin\nHUMAN\towner\tjoin\n", 0)
	c := collectorOf(w)
	s := NewSnapshot()
	c.collectMembers(t.Context(), s, "beta")
	if n := w.Spawned()["agent-bus human devices"]; n != 1 {
		t.Fatalf("a person listed twice had their sessions read %d times", n)
	}
	if s.SignedIn["beta|owner"] != "1 signed in" {
		t.Fatalf("signed in %v", s.SignedIn)
	}
}
