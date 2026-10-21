package board

import (
	"maps"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// bigWorld is three present teams, each with a homeserver that is up and the three people of
// teamtest.BusList: the shape whose redraw cost grows with the teams and the people.
func bigWorld(t *testing.T) rig {
	t.Helper()
	h := populated(t)
	var declared, units, status strings.Builder
	for _, team := range []string{"alpha", "beta", "gamma"} {
		declared.WriteString("TEAM\t" + team + "\t6167\tpresent\t" + team + ".agent-bus.internal\thttp://192.0.2.10:6167\t-\n")
		units.WriteString("agent-bus-hs@" + team + ".service loaded active running Homeserver " + team + "\n")
		status.WriteString("STATUS\t" + team + "\tcurrent\t-\n")
		h.On(teamtest.SudoBus+"list "+team, teamtest.BusList, 0)
		for _, human := range []string{"owner", "guest", "ghost"} {
			h.On(teamtest.SudoBus+"human devices "+team+" "+human, "DEVICE\ta\t-\n", 0)
		}
	}
	h.OnDeclare("declared", declared.String())
	h.OnDeclare("status", status.String())
	h.On("systemctl list-units", units.String(), 0)
	return h
}

// The cost of drawing the list for three teams of three people, in processes started. Before
// the redraw was cut: 21 on every draw (declare.py declared and status, git, systemctl list-units
// and one is-active per team, tmux, the user's agent-bus seat list, one sudo agent-bus list per
// team and one sudo agent-bus human devices per person). A redraw happens after every key.
// The seats are read in-process, so no seat list process starts.
func TestARedrawStartsFewProcessesAndTheFirstDrawReadsEachPersonOnce(t *testing.T) {
	h := bigWorld(t)
	first := map[string]int{
		"declare.py overview": 1, "systemctl": 1, "tmux": 1, "git": 1,
		"agent-bus list": 3, "agent-bus human devices": 9,
	}
	redraw := map[string]int{
		"declare.py overview": 1, "systemctl": 1, "tmux": 1,
		"agent-bus list": 3,
	}
	if _, err := h.c.Collect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := h.Spawned(); !maps.Equal(got, first) {
		t.Fatalf("first draw started %v, want %v", got, first)
	}
	if _, err := h.c.Collect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := h.Spawned(); !maps.Equal(got, redraw) {
		t.Fatalf("redraw started %v, want %v", got, redraw)
	}
}

func TestADevicesReadIsKeptUntilSomethingThatChangesItIsDone(t *testing.T) {
	h := bigWorld(t)
	h.On(teamtest.SudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	read := func() string {
		s, err := h.c.Collect(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return s.SignedIn["alpha|owner"]
	}
	if got := read(); got != "1 signed in" {
		t.Fatalf("first read %q", got)
	}
	h.On(teamtest.SudoBus+"human devices alpha owner", "DEVICE\ta\t-\nDEVICE\tb\t-\n", 0)
	if got := read(); got != "1 signed in" {
		t.Fatalf("kept read %q: the devices were asked for again", got)
	}
	h.c.ForgetDevices("beta")
	if got := read(); got != "1 signed in" {
		t.Fatalf("another team's change must not drop alpha's read, got %q", got)
	}
	h.c.ForgetDevices("alpha")
	if got := read(); got != "2 signed in" {
		t.Fatalf("after a change %q", got)
	}
	h.On(teamtest.SudoBus+"human devices alpha owner", "", 0)
	h.c.ForgetDevices("")
	if got := read(); got != "never signed in" {
		t.Fatalf("after everything changed %q", got)
	}
}

func TestAReadThatFailedIsAskedForAgain(t *testing.T) {
	h := bigWorld(t)
	h.OnErr(teamtest.SudoBus+"human devices alpha owner", "boom", 1)
	s, err := h.c.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if s.SignedIn["alpha|owner"] == "1 signed in" {
		t.Fatalf("a failed read became a count: %v", s.SignedIn)
	}
	h.On(teamtest.SudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	s, err = h.c.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if s.SignedIn["alpha|owner"] != "1 signed in" {
		t.Fatalf("the failure was kept: %v", s.SignedIn)
	}
}
