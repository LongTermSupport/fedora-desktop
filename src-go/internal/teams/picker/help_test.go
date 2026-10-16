package picker

import (
	"context"
	"strings"
	"testing"
)

func TestHelpIsTheVerbatimTextOnStderrThenWaits(t *testing.T) {
	h := newRig(t)
	h.p.showHelp(context.Background())
	got := h.ErrOut.String()
	if !strings.HasPrefix(got, "\nccy-teams: the agent teams on this machine\nEach team is a bold row: its homeserver (up, down, or not installed yet), its port, and\n") {
		t.Fatalf("start %q", got[:min(len(got), 160)])
	}
	for _, want := range []string{
		"  Ctrl-U   on a team row: update every team out of date or not built (never takes one\n",
		"  decide: absent      declared absent but still running; decide: undeclared, running but\n",
		"Colours: green is working, yellow wants a look, red is not working.\n",
		"\"need sudo\": reading the members needs root; play-agent-bus.yml installs a rule that\nlets ccy-teams read them without a password.\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("help lacks %q", want)
		}
	}
	if !strings.HasSuffix(got, "Press Enter to go back to the list.\n") {
		t.Fatalf("end %q", got[max(0, len(got)-80):])
	}
	if h.Out.String() != "" {
		t.Fatalf("stdout %q", h.Out.String())
	}
}
