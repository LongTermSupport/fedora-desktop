package teams

import (
	"maps"
	"strings"
	"testing"
)

// bigWorld is three present teams, each with a homeserver that is up and the three people of
// busList: the shape whose redraw cost grows with the teams and the people.
func bigWorld(t *testing.T) harness {
	t.Helper()
	h := world(t)
	declared, units, status := "", "", ""
	for _, team := range []string{"alpha", "beta", "gamma"} {
		declared += "TEAM\t" + team + "\t6167\tpresent\t" + team + ".agent-bus.internal\thttp://192.0.2.10:6167\n"
		units += "agent-bus-hs@" + team + ".service loaded active running Homeserver " + team + "\n"
		status += "STATUS\t" + team + "\tcurrent\t-\n"
		h.on(sudoBus+"list "+team, busList, 0)
		for _, human := range []string{"owner", "guest", "ghost"} {
			h.on(sudoBus+"human devices "+team+" "+human, "DEVICE\ta\t-\n", 0)
		}
	}
	h.onDeclare("declared", declared)
	h.onDeclare("status", status)
	h.on("systemctl list-units", units, 0)
	return h
}

// spawned counts the processes the fake runner was asked to start since the last call, by
// program and subcommand.
func spawned(h harness) map[string]int {
	counts := map[string]int{}
	calls := h.fake.Calls()
	for _, c := range calls[*h.counted:] {
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
	*h.counted = len(calls)
	return counts
}

// The cost of drawing the list for three teams of three people, in processes started. Before
// the redraw was cut: 21 on every draw (declare.py declared and status, git, systemctl list-units
// and one is-active per team, tmux, the user's agent-bus seat list, one sudo agent-bus list per
// team and one sudo agent-bus human devices per person). A redraw happens after every key.
func TestARedrawStartsFewProcessesAndTheFirstDrawReadsEachPersonOnce(t *testing.T) {
	h := bigWorld(t)
	first := map[string]int{
		"declare.py overview": 1, "systemctl": 1, "tmux": 1, "git": 1,
		"agent-bus (user) seat list": 1, "agent-bus list": 3, "agent-bus human devices": 9,
	}
	redraw := map[string]int{
		"declare.py overview": 1, "systemctl": 1, "tmux": 1,
		"agent-bus (user) seat list": 1, "agent-bus list": 3,
	}
	if _, err := h.app.collect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := spawned(h); !maps.Equal(got, first) {
		t.Fatalf("first draw started %v, want %v", got, first)
	}
	if _, err := h.app.collect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := spawned(h); !maps.Equal(got, redraw) {
		t.Fatalf("redraw started %v, want %v", got, redraw)
	}
}

func TestADevicesReadIsKeptUntilSomethingThatChangesItIsDone(t *testing.T) {
	h := bigWorld(t)
	h.on(sudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	read := func() string {
		s, err := h.app.collect(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return s.SignedIn["alpha|owner"]
	}
	if got := read(); got != "1 signed in" {
		t.Fatalf("first read %q", got)
	}
	h.on(sudoBus+"human devices alpha owner", "DEVICE\ta\t-\nDEVICE\tb\t-\n", 0)
	if got := read(); got != "1 signed in" {
		t.Fatalf("kept read %q: the devices were asked for again", got)
	}
	h.app.forgetDevices("beta")
	if got := read(); got != "1 signed in" {
		t.Fatalf("another team's change must not drop alpha's read, got %q", got)
	}
	h.app.forgetDevices("alpha")
	if got := read(); got != "2 signed in" {
		t.Fatalf("after a change %q", got)
	}
	h.on(sudoBus+"human devices alpha owner", "", 0)
	h.app.forgetDevices("")
	if got := read(); got != "never signed in" {
		t.Fatalf("after everything changed %q", got)
	}
}

func TestAReadThatFailedIsAskedForAgain(t *testing.T) {
	h := bigWorld(t)
	h.onErr(sudoBus+"human devices alpha owner", "boom", 1)
	s, err := h.app.collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if s.SignedIn["alpha|owner"] == "1 signed in" {
		t.Fatalf("a failed read became a count: %v", s.SignedIn)
	}
	h.on(sudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	s, err = h.app.collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if s.SignedIn["alpha|owner"] != "1 signed in" {
		t.Fatalf("the failure was kept: %v", s.SignedIn)
	}
}
