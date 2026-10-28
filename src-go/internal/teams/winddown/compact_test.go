package winddown

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/compactfloor"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func tokens(n int) string { return strconv.Itoa(n) + " tokens" }

// compactLine is the /compact a session was typed, or "".
func compactLine(s *session) string {
	for _, line := range s.entered {
		if strings.HasPrefix(line, "/compact ") {
			return line
		}
	}
	return ""
}

// compactOrder is the sessions in the order they were typed /compact.
func (m *machine) compactOrder() []string {
	var names []string
	for _, line := range m.log {
		name, text, found := strings.Cut(line, ": ")
		if found && strings.HasPrefix(text, "/compact ") {
			names = append(names, name)
		}
	}
	return names
}

// other adds the running session ccy-other in seat dev of alpha, its context tokens.
func (m *machine) other(tokens int) *session {
	m.record("ccy-other", m.Checkout(), "dev@alpha")
	s := &session{name: "ccy-other", dir: m.Checkout(), pane: paneReady}
	m.sessions = append(m.sessions, s)
	m.converse(s, tokens)
	m.build()
	return s
}

// WC1, WC2: a session at or above the floor is typed /compact with the wind-down's instruction
// once it has answered the wind-down, and is done when its transcript records the compaction.
func TestASessionAtTheFloorIsCompactedOnceItHasAnswered(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.proj()
	m.converse(s, largeTokens)
	s.answerLooks, s.compactLooks = 4, 3
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if len(s.entered) != 3 || s.entered[0] != goalClear {
		t.Fatalf("entered %q", s.entered)
	}
	text := compactLine(s)
	for _, want := range []string{"being wound down", "keep what", "resume", "drop the rest"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the /compact lacks %q: %q", want, text)
		}
	}
	if strings.Contains(text, "\n") {
		t.Fatal("the /compact has a newline, which would submit it early")
	}
	m.errHas(t,
		"Then each session that has answered, and whose context is at or above the compact floor ("+tokens(floorTokens)+"), is typed /compact",
		"✓ ccy-proj: /goal clear and the wind-down typed; compacted (context "+tokens(largeTokens)+", floor "+tokens(floorTokens)+")",
		"alpha: 1 wound down, 0 failed, 2 not running.",
		"alpha: 1 compacted, 0 not compacted.",
	)
}

// WC2: below the floor, a session is not compacted, and the report gives both numbers.
func TestASessionBelowTheFloorIsNotCompacted(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if line := compactLine(m.proj()); line != "" {
		t.Fatalf("compacted below the floor: %q", line)
	}
	m.errHas(t,
		"✓ ccy-proj: /goal clear and the wind-down typed; not compacted: its context, "+tokens(smallTokens)+", is below the floor, "+tokens(floorTokens),
		"alpha: 0 compacted, 1 not compacted.",
	)
}

// WC2: the answer is the session idle again; one never seen busy is given the settle bound.
func TestAnAnswerNeverSeenBusyIsGivenTheSettleBound(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.proj()
	m.converse(s, largeTokens)
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if compactLine(s) == "" {
		t.Fatalf("not compacted: %q", s.entered)
	}
	var waited float64
	for _, d := range m.sleeps {
		waited += d.Seconds()
	}
	if waited < 30 {
		t.Fatalf("typed /compact %.1f s after the wind-down, within the settle bound", waited)
	}
}

// WC3: a host session idle again with no compact_boundary did not compact: a fault.
func TestACompactionThatWritesNoBoundaryFails(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.proj()
	m.converse(s, largeTokens)
	s.compactLooks, s.compactFails = 3, true
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ ccy-proj: /goal clear and the wind-down went in, but its compaction ended with no compact_boundary in its transcript")
}

// WC4a: a session still answering at the end of the window is not compacted, and that is not
// a failure; nothing is interrupted.
func TestASessionStillAnsweringAtTheWindowIsNotCompacted(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.proj()
	m.converse(s, largeTokens)
	s.busyAfterEntered = true
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if compactLine(s) != "" || s.escapes != 0 {
		t.Fatalf("entered %q escapes %d", s.entered, s.escapes)
	}
	m.errHas(t, "✓ ccy-proj: /goal clear and the wind-down typed; not compacted: it was still answering the wind-down at the end of the "+secs(900)+" window")
}

// WC4a: a compaction not finished in the window is reported, not failed.
func TestACompactionUnfinishedAtTheWindowIsReported(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.proj()
	m.converse(s, largeTokens)
	s.compactLooks = 100000
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "✓ ccy-proj: /goal clear and the wind-down typed; its compaction had not finished at the end of the "+secs(900)+" window")
}

// WC4: one compaction at a time, and the coordinator first of those that have answered.
func TestTheCoordinatorIsCompactedFirstAndOneAtATime(t *testing.T) {
	m := newMachine(t)
	m.coordinator()
	m.converse(m.proj(), largeTokens)
	o := m.other(largeTokens)
	m.proj().compactLooks, o.compactLooks = 5, 5
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if got := m.compactOrder(); !slices.Equal(got, []string{"ccy-proj", "ccy-other"}) {
		t.Fatalf("compacted in the order %q\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "alpha: 2 compacted, 0 not compacted.")
}

// WC4: a coordinator still answering does not hold up a worker that has answered, and the
// coordinator's compaction waits for the worker's to end.
func TestAWorkerThatHasAnsweredIsNotHeldUpByTheCoordinator(t *testing.T) {
	m := newMachine(t)
	m.coordinator()
	p := m.proj()
	m.converse(p, largeTokens)
	o := m.other(largeTokens)
	p.answerLooks, p.compactLooks = 40, 5
	o.answerLooks, o.compactLooks = 1, 60
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if got := m.compactOrder(); !slices.Equal(got, []string{"ccy-other", "ccy-proj"}) {
		t.Fatalf("compacted in the order %q", got)
	}
	// ccy-other's compaction shows busy for 60 looks, so ccy-proj, answered after 40, waits.
	at := slices.Index(m.log, "ccy-proj: "+compactLine(p))
	if at < 0 || len(o.entered) != 3 || o.busyLooks != 0 {
		t.Fatalf("log %q", m.log)
	}
	m.errHas(t, "alpha: 2 compacted, 0 not compacted.")
}

// WC2: a floor that cannot be read compacts nothing, says why, and fails the run; the
// wind-down itself still goes in.
func TestAnUnreadableFloorFailsTheRunAfterTheWindDown(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.On(showFloor, "\n", 0)
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	if len(m.proj().entered) != 2 {
		t.Fatalf("entered %q", m.proj().entered)
	}
	m.errHas(t,
		"  ccy-proj   seat 1 (proj.1+local.podman) in ~/proj, not to be compacted: "+compactfloor.Unit+" sets no "+compactfloor.Env,
		"✗ ccy-proj: /goal clear and the wind-down went in, but it was not compacted: "+compactfloor.Unit+" sets no "+compactfloor.Env,
	)
}

// WC2: a session whose conversation is not recorded is not compacted, and that fails the run.
func TestASessionWithNoRecordedConversationIsNotCompacted(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	if err := os.Remove(filepath.Join(membership.SessionDir(m.Checkout(), "ccy-proj"), conversation.File)); err != nil {
		t.Fatal(err)
	}
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ ccy-proj: /goal clear and the wind-down went in, but it was not compacted: ")
	if len(m.proj().entered) != 2 {
		t.Fatalf("entered %q", m.proj().entered)
	}
}

// WC2: the floor is asked for only when there is a host session to compact.
func TestTheFloorIsNotAskedForWithNoHostSession(t *testing.T) {
	m := newMachine(t)
	m.sessions = nil
	m.withLxc()
	m.run("alpha", "--yes")
	if m.Fake.Called(showFloor) {
		t.Fatal("the floor was read with no host session to compact")
	}
}

// A session that ends after its wind-down is not compacted, and that is not a failure.
func TestASessionEndingAfterItsWindDownIsNotCompacted(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.proj().endAfterText = true
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "✓ ccy-proj: /goal clear and the wind-down typed; not compacted: the session ended")
}

// WC3: an LXC member's transcript is in its container, so it is compacted whatever its
// context, and the end of its compaction is read from its pane.
func TestAnLxcMemberIsCompactedAndItsEndReadFromItsPane(t *testing.T) {
	m := newMachine(t)
	box := m.withLxc()
	box.answerLooks, box.compactLooks = 2, 4
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if compactLine(box) == "" || len(box.entered) != 3 {
		t.Fatalf("entered %q", box.entered)
	}
	m.errHas(t, "✓ agent@box: /goal clear and the wind-down typed; compacted (its transcript is in its container, "+
		"so its context is not read; the end was read from its pane)")
}

// WC2: a floor that is not a whole number of tokens is refused as one that is not there.
func TestAFloorThatIsNotTokensIsNotUsed(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.w.Floor = func(context.Context) (compactfloor.Floor, error) { return compactfloor.Parse("lots"), nil }
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "it was not compacted: the compact floor 'lots' is not a whole number of tokens")
}

// WC4, WC4a: a session whose turn to compact has not come by the end of the window is named.
func TestASessionWaitingForItsTurnAtTheWindowIsNamed(t *testing.T) {
	m := newMachine(t)
	m.coordinator()
	m.converse(m.proj(), largeTokens)
	o := m.other(largeTokens)
	m.proj().compactLooks = 100000
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if compactLine(o) != "" {
		t.Fatalf("two compactions at once: %q", m.log)
	}
	m.errHas(t, "✓ ccy-other: /goal clear and the wind-down typed; not compacted: its turn to compact had not come, or its box was not idle, at the end of the "+secs(900)+" window")
}

// WC1: a /compact left in the box gets the one more Enter 3.14 gives, then fails.
func TestACompactStuckInTheBoxFails(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.proj()
	m.converse(s, largeTokens)
	m.Fake.OnFunc([]string{"tmux", "-L", "ccy", "send-keys"}, func(c execx.Cmd) (execx.Result, error) {
		if strings.HasPrefix(c.Args[len(c.Args)-1], "/compact ") {
			s.dropEnters = 1000
		}
		m.press(s, c.Args[len(c.Args)-2:])
		return execx.Result{}, nil
	})
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ ccy-proj: /goal clear and the wind-down went in, but /compact stayed in its input box after a second Enter")
}
