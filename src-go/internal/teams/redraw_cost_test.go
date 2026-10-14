package teams

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// bigWorld is three present teams, each with a homeserver that is up and the three people of
// busList: the shape whose redraw cost grows with teams and people.
func bigWorld(t *testing.T) harness {
	t.Helper()
	h := world(t)
	declared, units, status := "", "", ""
	for _, team := range []string{"alpha", "beta", "gamma"} {
		declared += "TEAM\t" + team + "\t6167\tpresent\t" + team + ".agent-bus.internal\thttp://192.0.2.10:6167\n"
		units += "agent-bus-hs@" + team + ".service loaded active running Homeserver " + team + "\n"
		status += "STATUS\t" + team + "\tcurrent\t-\n"
		h.on("systemctl is-active agent-bus-hs@"+team+".service", "active\n", 0)
		h.on(sudoBus+"list "+team, busList, 0)
		for _, human := range []string{"owner", "guest", "ghost"} {
			h.on(sudoBus+"human devices "+team+" "+human, "DEVICE\ta\t-\n", 0)
		}
	}
	h.onDeclare("declared", declared)
	h.on("systemctl list-units", units, 0)
	h.onDeclare("status", status)
	return h
}

// spawned counts the processes the fake runner was asked to start, by program and subcommand.
func spawned(h harness) map[string]int {
	counts := map[string]int{}
	for _, c := range h.fake.Calls() {
		key := c.Name
		switch {
		case c.Name == "env" && len(c.Args) > 4 && strings.HasPrefix(c.Args[0], "PYTHONPATH="):
			key = "declare.py " + c.Args[4]
		case c.Name == "sudo" && len(c.Args) > 2:
			key = "sudo agent-bus " + c.Args[2]
			if c.Args[2] == "human" {
				key += " " + c.Args[3]
			}
		}
		counts[key]++
	}
	return counts
}

func TestMeasureRedraw(t *testing.T) {
	h := bigWorld(t)
	if _, err := h.app.collect(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Log(fmt.Sprint(spawned(h)), len(h.fake.Calls()), filepath.Base(h.root))
}
