package board

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// held is a runner over the fake that holds a command until what it waits for has happened,
// with nothing held while the fake answers, so the pass under test decides what runs together.
type held struct {
	inner execx.Runner
	// wait is what a command waits for before it is answered, by the start of its line and
	// the folder it runs in ("" for any); ready closes when it may go.
	wait func(c execx.Cmd) <-chan struct{}
	// finished is told each command once its answer is known.
	finished func(c execx.Cmd)

	mu       sync.Mutex
	running  int
	peak     int
	timedOut bool
}

// stuck is how long a held command waits before it is let go: only a pass that runs the
// commands one after another ever waits that long, and the test then fails instead of hanging.
const stuck = 20 * time.Second

func (h *held) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	h.mu.Lock()
	h.running++
	h.peak = max(h.peak, h.running)
	h.mu.Unlock()
	if ready := h.wait(c); ready != nil {
		select {
		case <-ready:
		case <-time.After(stuck):
			h.mu.Lock()
			h.timedOut = true
			h.mu.Unlock()
		}
	}
	res, err := h.inner.Run(ctx, c)
	h.mu.Lock()
	h.running--
	h.mu.Unlock()
	if h.finished != nil {
		h.finished(c)
	}
	return res, err
}

// overRunner is a Collector of w whose every command goes through r first.
func overRunner(w *teamtest.World, r execx.Runner) *Collector {
	host := w.Host
	host.Sudo, host.Systemd, host.Tmux, host.Git, host.Checkouts, host.Declare, host.Bus = nil, nil, nil, nil, nil, nil, nil
	host.Runner = r
	host.Wire()
	host.Checkouts.ReadFile = func(string) ([]byte, error) { return nil, fs.ErrNotExist }
	return New(host)
}

// busy is a machine with several teams up, each with one person, and several checkouts with a
// seat directory, each a ccy session's: the board's slow case, one agent-bus call after another.
func busy(t *testing.T, teams []string, checkouts int) *teamtest.World {
	t.Helper()
	w := teamtest.New(t)
	var declared, status, units, tmux strings.Builder
	for i, team := range teams {
		fmt.Fprintf(&declared, "TEAM\t%s\t%d\tpresent\t%s.agent-bus.internal\t-\t-\n", team, 6167+i, team)
		fmt.Fprintf(&status, "STATUS\t%s\tcurrent\t-\n", team)
		fmt.Fprintf(&units, "agent-bus-hs@%s.service loaded active running Homeserver %s\n", team, team)
		w.On(teamtest.SudoBus+"list "+team, "HUMAN\towner\tjoin\nMEMBER\tp0.1+local.podman\tdev\tjoin\tactive\n", 0)
		w.On(teamtest.SudoBus+"human devices "+team+" owner", "DEVICE\ta\t-\n", 0)
	}
	for i := range checkouts {
		dir := checkoutOf(w, i)
		teamtest.MustWrite(t, filepath.Join(dir, ".claude/ccy/pingbus/seats/x"), "")
		fmt.Fprintf(&tmux, "ccy-p%d 0 %s\n", i, dir)
	}
	w.OnDeclare("declared", declared.String())
	w.OnDeclare("status", status.String())
	w.On("systemctl list-units", units.String(), 0)
	w.On("tmux -L ccy list-sessions", tmux.String(), 0)
	w.OnNotARepository()
	w.Fake.OnFunc([]string{"agent-bus", "seat", "list"}, func(c execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: []byte("SEAT\t" + teams[0] + "\t1\tfree\t-\t" + filepath.Base(c.Dir) + ".1+local.podman\n")}, nil
	})
	return w
}

func checkoutOf(w *teamtest.World, i int) string { return filepath.Join(w.Root, fmt.Sprintf("p%d", i)) }

func seatLists(w *teamtest.World) []string {
	var dirs []string
	for _, c := range w.Fake.Calls() {
		if c.Line() == "agent-bus seat list" {
			dirs = append(dirs, c.Dir)
		}
	}
	return dirs
}

func TestCollectReadsTheSeatsOfACheckoutOnceHoweverItIsNamed(t *testing.T) {
	h := populated(t)
	alias := filepath.Join(h.Root, "proj-link")
	if err := os.Symlink(h.Checkout(), alias); err != nil {
		t.Fatal(err)
	}
	h.On("tmux -L ccy list-sessions", "ccy-proj 0 "+h.Checkout()+"\nccy-link 0 "+alias+"\n", 0)
	s, err := h.c.Collect(t.Context())
	if err != nil {
		t.Fatalf("%v: %s", err, h.ErrOut.String())
	}
	if dirs := seatLists(h.World); len(dirs) != 1 {
		t.Fatalf("agent-bus seat list ran %d times for one checkout under two names: %v", len(dirs), dirs)
	}
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
	if _, err := collectorOf(w).Collect(t.Context()); err != nil {
		t.Fatalf("%v: %s", err, w.ErrOut.String())
	}
	want := []string{checkoutOf(w, 0), checkoutOf(w, 1), checkoutOf(w, 2)}
	got := seatLists(w)
	if len(got) != len(want) {
		t.Fatalf("seat list ran in %v, want once in each of %v", got, want)
	}
	for _, dir := range want {
		if strings.Count(strings.Join(got, "\n")+"\n", dir+"\n") != 1 {
			t.Fatalf("seat list ran in %v, want once in each of %v", got, want)
		}
	}
}

func TestCollectRunsTheAgentBusCallsTogether(t *testing.T) {
	w := busy(t, []string{"alpha", "beta"}, 2)
	// The two seat lists and the two rosters wait until all four are running at once.
	const together = 4
	var mu sync.Mutex
	entered := 0
	all := make(chan struct{})
	gate := &held{inner: w.Fake, wait: func(c execx.Cmd) <-chan struct{} {
		line := c.Line()
		if line != "agent-bus seat list" && !strings.HasPrefix(line, teamtest.SudoBus+"list ") {
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
		t.Fatalf("at most %d commands ran at once (timed out: %v); the seat lists and rosters should run together", gate.peak, gate.timedOut)
	}
	if gate.peak > parallel {
		t.Fatalf("%d commands ran at once, more than the bound of %d", gate.peak, parallel)
	}
}

func TestCollectRunsNoMoreThanItsBoundAtOnce(t *testing.T) {
	w := busy(t, []string{"alpha", "beta", "gamma", "delta"}, 6)
	gate := &held{inner: w.Fake, wait: func(execx.Cmd) <-chan struct{} { return nil }}
	c := overRunner(w, gate)
	c.Parallel = 2
	if _, err := c.Collect(t.Context()); err != nil {
		t.Fatalf("%v: %s", err, w.ErrOut.String())
	}
	if gate.peak > 2 {
		t.Fatalf("%d commands ran at once with a bound of 2", gate.peak)
	}
}

// failing is busy with a failure in each kind of call: the second checkout's seat list, beta's
// roster and gamma's person's sessions.
func failing(t *testing.T) *teamtest.World {
	t.Helper()
	w := busy(t, []string{"alpha", "beta", "gamma"}, 2)
	second := checkoutOf(w, 1)
	w.Fake.OnFunc([]string{"agent-bus", "seat", "list"}, func(c execx.Cmd) (execx.Result, error) {
		if c.Dir == second {
			return execx.Result{Stderr: []byte("agent-bus: seats in p1 unreachable\n"), ExitCode: 69}, nil
		}
		return execx.Result{Stdout: []byte("SEAT\talpha\t1\tfree\t-\tp0.1+local.podman\n")}, nil
	})
	w.OnErr(teamtest.SudoBus+"list beta", "agent-bus: beta refused\n", 70)
	w.OnErr(teamtest.SudoBus+"human devices gamma owner", "agent-bus: gamma owner refused\n", 70)
	return w
}

// outOfOrder is a runner over w's fake that makes the first of each kind of call finish last:
// the first checkout's seat list after the second's, and alpha's roster after gamma's person's
// sessions, the last call the board uses.
func outOfOrder(w *teamtest.World) *held {
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
	keyOf := func(c execx.Cmd) string { return c.Line() + "|" + c.Dir }
	return &held{
		inner: w.Fake,
		wait: func(c execx.Cmd) <-chan struct{} {
			switch {
			case c.Line() == "agent-bus seat list" && c.Dir == checkoutOf(w, 0):
				return signal("agent-bus seat list|" + checkoutOf(w, 1))
			case c.Line() == teamtest.SudoBus+"list alpha":
				return signal(teamtest.SudoBus + "human devices gamma owner|")
			}
			return nil
		},
		finished: func(c execx.Cmd) { close(signal(keyOf(c))) },
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
	for _, said := range []string{"seats in p1 unreachable", "agent-bus seat list could not read every seat in", "beta refused", "gamma owner refused"} {
		next := strings.Index(wantErr, said)
		if next <= at {
			t.Fatalf("%q is not reported after what came before it in %q", said, wantErr)
		}
		at = next
	}

	w := failing(t)
	gate := outOfOrder(w)
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
