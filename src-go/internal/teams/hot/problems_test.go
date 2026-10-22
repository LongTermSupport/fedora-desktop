package hot

import (
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestStateWordSaysWhatIsWrongFirst(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name                       string
		alive                      bool
		aliveErr, seatsErr, repErr error
		down, unheld               []string
		want                       string
	}{
		{"processes unreadable", true, boom, nil, nil, nil, nil, "its processes cannot be read: boom"},
		{"keeper stopped", false, nil, boom, boom, []string{"a"}, nil, "unguarded: ccy-teams session repair s"},
		{"seats unreadable", true, nil, boom, boom, nil, nil, "its seats cannot be read: boom"},
		{"report unreadable", true, nil, nil, boom, []string{"a"}, nil, "its seat keeper's report cannot be read: boom"},
		{"teams down", true, nil, nil, nil, []string{"a", "b"}, []string{"3@a"}, "a,b down"},
		{"seats not held", true, nil, nil, nil, nil, []string{"3@a", "4@b"}, "3@a,4@b not held"},
		{"nothing wrong", true, nil, nil, nil, nil, nil, "ok"},
	}
	for _, c := range cases {
		if got := stateWord("s", c.alive, c.aliveErr, c.seatsErr, c.repErr, c.down, c.unheld); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPluralCountsSeats(t *testing.T) {
	if plural(1, "seat") != "1 seat" || plural(2, "team") != "2 teams" {
		t.Fatalf("%q %q", plural(1, "seat"), plural(2, "team"))
	}
}

func TestABusyLockNamesItsHolder(t *testing.T) {
	if got := (&busyError{holder: "pid 7"}).Error(); got != "held by pid 7" {
		t.Fatalf("%q", got)
	}
}

func TestLastLineOfABusCommandIsItsOwnWording(t *testing.T) {
	err := &agentbus.CommandError{Tool: "agent-bus", Code: 2, Stderr: "no such team\nsecond line"}
	if got := lastLine(err); got != err.Error() {
		t.Fatalf("%q, want %q", got, err.Error())
	}
}

func TestListOnATerminalPrintsATableAndWhoCanBeChanged(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.labelled("abc123\trunning\t" + session + "\n")
	r.x.Host.IsTerminal = func(any) bool { return true }
	if code := r.run(t, "list"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if out := r.Out.String(); !strings.HasPrefix(out, "SESSION") || !strings.Contains(out, "alpha (3, ") {
		t.Fatalf("the table:\n%s", out)
	}
	if !strings.Contains(r.ErrOut.String(), OnlyTmuxLine) {
		t.Fatalf("who can be changed is said once:\n%s", r.ErrOut.String())
	}
}
