package sessions

import (
	"errors"
	"os"
	"testing"
)

// fedora-desktop#396: `forget NAME` removes that one registry record, so a stale record no
// longer fails every later restore. One line on stdout.
func TestForgetRemovesOneRecord(t *testing.T) {
	h := staleHost(t)
	exitCodeIs(t, h.run("forget", "ccy-stale"), 0, h)
	if h.stdout() != "ccy-stale FORGOTTEN\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	if _, err := os.Stat(recordFile(t, h, "ccy-stale")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the record is removed: %v", err)
	}
	if _, err := os.Stat(recordFile(t, h, "ccy-ok")); err != nil {
		t.Fatalf("the other record is kept: %v", err)
	}
	// With the stale record gone, the restore passes.
	exitCodeIs(t, h.run("restore"), 0, h)
}

// An unreadable record can be forgotten too: it is the file the restore names.
func TestForgetRemovesAnUnreadableRecord(t *testing.T) {
	h := staleHost(t)
	bad := recordFile(t, h, "ccy-bad")
	if err := os.WriteFile(bad, []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("forget", "ccy-bad"), 0, h)
	if h.stdout() != "ccy-bad FORGOTTEN\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	if _, err := os.Stat(bad); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the record is removed: %v", err)
	}
}

// A live session's record is its own; forgetting it is refused, naming it, and it is kept.
func TestForgetRefusesALiveSession(t *testing.T) {
	h := staleHost(t)
	h.sessions("ccy-stale 0 " + h.root)
	exitCodeIs(t, h.run("forget", "ccy-stale"), 1, h)
	contains(t, "live", h.stderr(), "ERROR: ccy-stale is a live session")
	if h.stdout() != "" {
		t.Fatalf("stdout %q", h.stdout())
	}
	if _, err := os.Stat(recordFile(t, h, "ccy-stale")); err != nil {
		t.Fatalf("the record is kept: %v", err)
	}
}

// A live set that cannot be read forgets nothing: the record might be a running session's.
func TestForgetWithAnUnreadableLiveSetForgetsNothing(t *testing.T) {
	h := staleHost(t)
	h.tmuxFail("list-sessions", "boom")
	exitCodeIs(t, h.run("forget", "ccy-stale"), 1, h)
	if _, err := os.Stat(recordFile(t, h, "ccy-stale")); err != nil {
		t.Fatalf("the record is kept: %v", err)
	}
}

func TestForgetUsageMistakes(t *testing.T) {
	cases := []struct {
		args []string
		msg  string
	}{
		{[]string{"forget"}, "forget takes one session name."},
		{[]string{"forget", "a", "b"}, "forget takes one session name."},
		{[]string{"forget", "ccy-ghost"}, "no session record named 'ccy-ghost'."},
		{[]string{"forget", ""}, "no session record named ''."},
		{[]string{"forget", "a/b"}, "no session record named 'a/b'."},
	}
	for _, c := range cases {
		h := staleHost(t)
		exitCodeIs(t, h.run(c.args...), 64, h)
		if want := "ccy-sessions: " + c.msg + " See ccy-sessions --help.\n"; h.stderr() != want {
			t.Fatalf("%v: %q", c.args, h.stderr())
		}
	}
}
