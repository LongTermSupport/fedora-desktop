package sessions

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessclose"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// windingFake stands in for sessclose's wind-down: the names and deadline it was given, and
// each session's result by name (Compacted when not set).
type windingFake struct {
	asked   []string
	by      time.Time
	calls   int
	answers map[string]sessclose.Result
}

func (f *windingFake) CompactAll(_ context.Context, names []string, by time.Time) []sessclose.Result {
	f.calls++
	f.asked, f.by = append(f.asked, names...), by
	out := make([]sessclose.Result, len(names))
	for i, n := range names {
		r, ok := f.answers[n]
		if !ok {
			r = sessclose.Result{Outcome: sessclose.Compacted, Compacted: true}
		}
		r.Name = n
		out[i] = r
	}
	return out
}

// windRig is a harness with a wind-down fake and a record written for each of recorded.
func windRig(t *testing.T, recorded ...string) (*harness, *windingFake) {
	t.Helper()
	h := newHarness(t)
	store, err := registry.Locate(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range recorded {
		rec := registry.Record{Name: name, Dir: "/srv/a", Launcher: "/x/ccy", Prefix: "ccy", Restore: true}
		if err := store.Write(rec); err != nil {
			t.Fatal(err)
		}
	}
	fake := &windingFake{answers: map[string]sessclose.Result{}}
	h.app.WindDown = fake
	return h, fake
}

// Every live recorded session is wound down together, by the default deadline, one line each in
// the server's order; an unrecorded one is named as skipped, and nothing is ended.
func TestWindDownCompactsEveryLiveRecordedSession(t *testing.T) {
	h, fake := windRig(t, "ccy-a", "cc-b")
	h.sessions("ccy-a 0 /srv/a", "ccy-c 0 /srv/c", "cc-b 1 /srv/b")
	fake.answers["cc-b"] = sessclose.Result{Outcome: sessclose.Compacted, Compacted: true, Busy: true}
	exitCodeIs(t, h.run("wind-down"), term.ExitOK, h)
	if !slices.Equal(fake.asked, []string{"ccy-a", "cc-b"}) || !fake.by.Equal(h.clock.Add(600*time.Second)) {
		t.Fatalf("asked %v by %v", fake.asked, fake.by)
	}
	want := "ccy-a COMPACTED\nccy-c SKIPPED no session record, so no restore brings it back\ncc-b COMPACTED-AFTER-TURN\n"
	if h.stdout() != want {
		t.Fatalf("stdout %q", h.stdout())
	}
	contains(t, "said", h.stderr(), "Winding down 2 session(s)", "600 s")
	if h.count("tmux -L ccy kill-session") != 0 || h.count("tmux -L ccy send-keys") != 0 {
		t.Fatalf("ran %v", h.ran())
	}
}

// --wait sets the deadline; a session not finished by it, or failed, makes the exit 1, and each
// is named with why.
func TestWindDownReportsWhatDidNotFinish(t *testing.T) {
	h, fake := windRig(t, "ccy-a", "ccy-b", "ccy-c", "ccy-d")
	h.sessions("ccy-a 0 /srv/a", "ccy-b 0 /srv/a", "ccy-c 0 /srv/a", "ccy-d 0 /srv/a")
	fake.answers["ccy-a"] = sessclose.Result{Outcome: sessclose.Unfinished, Note: "it was still in a turn", Busy: true}
	fake.answers["ccy-b"] = sessclose.Result{Outcome: sessclose.Failed, Note: "its compaction could not be read"}
	fake.answers["ccy-c"] = sessclose.Result{Outcome: sessclose.Skipped, Note: "nothing since its last compaction"}
	fake.answers["ccy-d"] = sessclose.Result{Outcome: sessclose.Compacted, Note: "its end read from its pane"}
	exitCodeIs(t, h.run("wind-down", "--wait", "120"), term.ExitFailure, h)
	if !fake.by.Equal(h.clock.Add(120 * time.Second)) {
		t.Fatalf("by %v", fake.by)
	}
	want := "ccy-a NOT-FINISHED it was still in a turn\nccy-b FAILED its compaction could not be read\n" +
		"ccy-c SKIPPED nothing since its last compaction\nccy-d COMPACTED its end read from its pane\n"
	if h.stdout() != want {
		t.Fatalf("stdout %q", h.stdout())
	}
	contains(t, "said", h.stderr(), "2 session(s) were not compacted")
}

// An outcome a wind-down never gives is a failure, not a pass.
func TestWindDownCountsAnUnexpectedOutcomeAsAFailure(t *testing.T) {
	h, fake := windRig(t, "ccy-a")
	h.sessions("ccy-a 0 /srv/a")
	fake.answers["ccy-a"] = sessclose.Result{Outcome: sessclose.Exited}
	exitCodeIs(t, h.run("wind-down"), term.ExitFailure, h)
	contains(t, "line", h.stdout(), "ccy-a FAILED")
}

// No live session is nothing to do.
func TestWindDownWithNoSessions(t *testing.T) {
	h, fake := windRig(t)
	h.sessions()
	h.tmuxOut("list-sessions", "")
	exitCodeIs(t, h.run("wind-down"), term.ExitOK, h)
	if fake.calls != 0 || h.stdout() != "" {
		t.Fatalf("calls %d stdout %q", fake.calls, h.stdout())
	}
	contains(t, "said", h.stderr(), "No live session to wind down")
}

// The sessions not being listable is a failure, and nothing is wound down.
func TestWindDownFailsWhenTheSessionsCannotBeListed(t *testing.T) {
	h, fake := windRig(t, "ccy-a")
	h.tmuxFail("list-sessions", "error connecting to server")
	exitCodeIs(t, h.run("wind-down"), term.ExitFailure, h)
	if fake.calls != 0 {
		t.Fatal("wound down without a list")
	}
}

// An unreadable record is named as failed, and the others are still wound down.
func TestWindDownNamesAnUnreadableRecord(t *testing.T) {
	h, fake := windRig(t, "ccy-a")
	h.sessions("ccy-a 0 /srv/a", "ccy-b 0 /srv/a")
	h.write("state/sessions/ccy-b", "not a record\n")
	exitCodeIs(t, h.run("wind-down"), term.ExitFailure, h)
	if !slices.Equal(fake.asked, []string{"ccy-a"}) {
		t.Fatalf("asked %v", fake.asked)
	}
	contains(t, "line", h.stdout(), "ccy-a COMPACTED\n", "ccy-b FAILED its session record could not be read")
}

// A registry with no location is a failure before anything is typed.
func TestWindDownFailsWithNoRegistry(t *testing.T) {
	h, fake := windRig(t)
	h.env["CCY_STATE_DIR"] = "relative"
	h.sessions("ccy-a 0 /srv/a")
	exitCodeIs(t, h.run("wind-down"), term.ExitFailure, h)
	if fake.calls != 0 {
		t.Fatal("wound down without a registry")
	}
}

// A mistake in the arguments is a usage error, and nothing is looked at.
func TestWindDownUsage(t *testing.T) {
	for _, args := range [][]string{
		{"wind-down", "--wait"},
		{"wind-down", "--wait", "soon"},
		{"wind-down", "--wait", "0"},
		{"wind-down", "--wait", "86401"},
		{"wind-down", "--now"},
	} {
		h, fake := windRig(t)
		exitCodeIs(t, h.run(args...), term.ExitUsage, h)
		if fake.calls != 0 || len(h.ran()) != 0 {
			t.Fatalf("%v: ran %v", args, h.ran())
		}
	}
}
