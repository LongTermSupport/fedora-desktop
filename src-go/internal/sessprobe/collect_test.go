package sessprobe_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

type fixedPanes struct {
	panes []tmuxccy.Pane
	err   error
}

func (f fixedPanes) Panes(context.Context) ([]tmuxccy.Pane, error) { return f.panes, f.err }

const (
	psArgs      = "ps -ww -eo pid=,ppid=,args="
	podmanPS    = "podman ps --filter label=ccy=true --format " + sessprobe.DetailFormat
	dockerPS    = "docker ps --filter label=ccy=true --format " + sessprobe.DetailFormat
	podmanInspt = "podman inspect --format " + sessprobe.PIDFormat + " a_yolo"
)

func ok(stdout string) execx.Result { return execx.Result{Stdout: []byte(stdout)} }

// onExact registers a command whose argv has words with spaces (the --format templates).
func onExact(f *execx.Fake, argv []string, r execx.Result) { f.On(argv, r) }

func psTable() string {
	return `1 0 systemd
100 1 bash -c trampoline
110 100 podman run --rm --name a_yolo --network net1 img claude
650 1 conmon
700 650 /entrypoint.sh
701 700 claude
400 1 bash -c trampoline
410 400 claude`
}

// rig is a Probe over a fake runner and a /proc under a temp dir whose second sample shows CPU used.
type rig struct {
	probe *sessprobe.Probe
	fake  *execx.Fake
	slept []time.Duration
	clock time.Time
}

func newRig(t *testing.T, panes []tmuxccy.Pane) *rig {
	t.Helper()
	root := t.TempDir()
	for _, pid := range []int{100, 110, 700, 701, 400, 410} {
		writeStat(t, root, pid, itoa(pid)+" (x) S 1 1 1 0 -1 0 0 0 0 0 0 0 0 0\n")
	}
	g := &rig{fake: &execx.Fake{}, clock: time.Unix(1_700_000_000, 0)}
	g.probe = &sessprobe.Probe{
		Tmux:     fixedPanes{panes: panes},
		Runner:   g.fake,
		ProcRoot: root,
		Now: func() time.Time {
			defer func() { g.clock = g.clock.Add(g.stepEvery()) }()
			return g.clock
		},
		Sleep: func(d time.Duration) { g.slept = append(g.slept, d) },
	}
	g.fake.On(strings.Fields(psArgs), ok(psTable()))
	return g
}

// stepEvery makes the clock advance one second between the two samples, whatever Sleep did.
func (g *rig) stepEvery() time.Duration { return time.Second }

// busy rewrites two stat files so the second sample shows 50 and 10 ticks of CPU.
func (g *rig) busy(t *testing.T) {
	t.Helper()
	g.probe.Sleep = func(d time.Duration) {
		g.slept = append(g.slept, d)
		writeStat(t, g.probe.ProcRoot, 701, "701 (x) S 1 1 1 0 -1 0 0 0 0 0 30 20 0 0\n")
		writeStat(t, g.probe.ProcRoot, 410, "410 (x) S 1 1 1 0 -1 0 0 0 0 0 5 5 0 0\n")
	}
}

func TestDetailRows(t *testing.T) {
	g := newRig(t, []tmuxccy.Pane{{Session: "ccy-a", PID: 100}, {Session: "cc-b", PID: 400}})
	onExact(g.fake, []string{"podman", "ps", "--filter", "label=ccy=true", "--format", sessprobe.DetailFormat}, ok("a_yolo|[net1]|work|key_0\nother_yolo|[x]||\n"))
	onExact(g.fake, []string{"podman", "inspect", "--format", sessprobe.PIDFormat, "a_yolo"}, ok("a_yolo 700\n"))
	g.busy(t)
	got, err := g.probe.Detail(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []sessprobe.Detail{
		{Session: "ccy-a", Network: "net1", Token: "work", Keys: "key_0", CPU: "50%"},
		{Session: "cc-b", Network: "no container", Token: "-", Keys: "-", CPU: "10%"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v", got)
	}
	if len(g.slept) != 1 || g.slept[0] != time.Second {
		t.Fatalf("slept %v", g.slept)
	}
}

func TestDetailNoSessionsNeedsNoProbesAndNoSleep(t *testing.T) {
	g := newRig(t, nil)
	got, err := g.probe.Detail(context.Background())
	if err != nil || len(got) != 0 || len(g.slept) != 0 || len(g.fake.Calls()) != 0 {
		t.Fatalf("got %+v, %v, slept %v, calls %v", got, err, g.slept, g.fake.Calls())
	}
}

func TestDetailWithNoContainersStillSamplesCPU(t *testing.T) {
	g := newRig(t, []tmuxccy.Pane{{Session: "cc-b", PID: 400}})
	g.busy(t)
	got, err := g.probe.Detail(context.Background())
	if err != nil || len(got) != 1 || got[0].CPU != "10%" || got[0].Network != "no container" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if len(g.slept) != 1 {
		t.Fatalf("slept %v", g.slept)
	}
	for _, c := range g.fake.Calls() {
		if c.Name != "ps" {
			t.Fatalf("only ps runs when no session has a container: %v", c.Line())
		}
	}
}

func TestDetailAContainerTheEngineNoLongerListsIsNotInspected(t *testing.T) {
	g := newRig(t, []tmuxccy.Pane{{Session: "ccy-a", PID: 100}})
	onExact(g.fake, []string{"podman", "ps", "--filter", "label=ccy=true", "--format", sessprobe.DetailFormat}, ok(""))
	got, err := g.probe.Detail(context.Background())
	if err != nil || len(got) != 1 || got[0].Network != "no container" || got[0].CPU != "0%" {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, c := range g.fake.Calls() {
		if c.Name == "podman" && c.Args[0] == "inspect" {
			t.Fatalf("inspect ran for a container that is not running: %v", c.Line())
		}
	}
}

func TestDetailEnginesAreAskedInSortedOrder(t *testing.T) {
	g := newRig(t, []tmuxccy.Pane{{Session: "ccy-a", PID: 100}, {Session: "ccy-d", PID: 400}})
	g.fake.On([]string{"ps"}, ok(`100 1 bash
110 100 podman run --name a_yolo img
400 1 bash
410 400 docker run --name d_yolo img`))
	g.fake.On([]string{"podman"}, ok(""))
	g.fake.On([]string{"docker"}, ok(""))
	if _, err := g.probe.Detail(context.Background()); err != nil {
		t.Fatal(err)
	}
	var engines []string
	for _, c := range g.fake.Calls() {
		if c.Args[0] == "ps" && c.Name != "ps" {
			engines = append(engines, c.Name)
		}
	}
	if !slices.Equal(engines, []string{"docker", "podman"}) {
		t.Fatalf("engines asked in order %v", engines)
	}
}

func TestDetailDockerNameHasASlashStripped(t *testing.T) {
	g := newRig(t, []tmuxccy.Pane{{Session: "ccy-a", PID: 100}})
	g.fake.On([]string{"ps"}, ok(psTable()))
	onExact(g.fake, []string{"podman", "ps", "--filter", "label=ccy=true", "--format", sessprobe.DetailFormat}, ok("a_yolo|n||\n"))
	onExact(g.fake, []string{"podman", "inspect", "--format", sessprobe.PIDFormat, "a_yolo"}, ok("/a_yolo 700\n"))
	g.busy(t)
	got, err := g.probe.Detail(context.Background())
	if err != nil || got[0].CPU != "50%" {
		t.Fatalf("the container's tree counts: %+v, %v", got, err)
	}
}

func TestDetailFailures(t *testing.T) {
	panes := []tmuxccy.Pane{{Session: "ccy-a", PID: 100}}
	cases := []struct {
		name  string
		setup func(*rig)
		want  string
	}{
		{"panes", func(g *rig) { g.probe.Tmux = fixedPanes{err: errors.New("tmux list-panes failed: boom")} }, "tmux list-panes failed: boom"},
		{
			"ps", func(g *rig) { g.fake.On([]string{"ps"}, execx.Result{Stderr: []byte("no ps"), ExitCode: 1}) },
			"the process table could not be read, so no session can be matched to a container: no ps",
		},
		{"engine ps", func(g *rig) {
			g.fake.On([]string{"podman", "ps"}, execx.Result{Stderr: []byte("cannot connect"), ExitCode: 125})
		}, "podman ps failed: cannot connect"},
		{"inspect", func(g *rig) {
			g.fake.On([]string{"podman", "ps"}, ok("a_yolo|n||\n"))
			g.fake.On([]string{"podman", "inspect"}, execx.Result{Stderr: []byte("no such object"), ExitCode: 125})
		}, "podman inspect failed: no such object"},
		{"inspect output", func(g *rig) {
			g.fake.On([]string{"podman", "ps"}, ok("a_yolo|n||\n"))
			g.fake.On([]string{"podman", "inspect"}, ok("garbage\n"))
		}, "unreadable inspect line"},
		{"table", func(g *rig) { g.fake.On([]string{"ps"}, ok("x y z\n")) }, "unreadable process table line"},
		{"no time passes", func(g *rig) {
			g.fake.On([]string{"podman", "ps"}, ok(""))
			g.probe.Now = func() time.Time { return time.Unix(5, 0) }
		}, "a CPU sample needs time to pass and a tick rate; got 0 us at 100 ticks a second"},
		{"stat unreadable", func(g *rig) {
			g.fake.On([]string{"podman", "ps"}, ok(""))
			writeStat(t, g.probe.ProcRoot, 100, "garbage\n")
		}, "process 100"},
	}
	for _, c := range cases {
		g := newRig(t, panes)
		c.setup(g)
		_, err := g.probe.Detail(context.Background())
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
	}
}

func TestDetailDefaults(t *testing.T) {
	p := &sessprobe.Probe{Tmux: fixedPanes{}, Runner: &execx.Fake{}}
	if got, err := p.Detail(context.Background()); err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestDetailDefaultClockAndSleepAreReal(t *testing.T) {
	g := newRig(t, []tmuxccy.Pane{{Session: "cc-b", PID: 400}})
	g.probe.Now = nil
	g.probe.Sleep = nil
	g.probe.SampleFor = 20 * time.Millisecond
	g.probe.ClockTicks = 100
	got, err := g.probe.Detail(context.Background())
	if err != nil || len(got) != 1 || got[0].CPU != "0%" {
		t.Fatalf("got %+v, %v", got, err)
	}
}
