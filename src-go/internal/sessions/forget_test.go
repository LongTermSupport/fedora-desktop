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

// The registry does not hold a record's name= to its file's name, so forget removes the file the
// record was read from: never a file another record lives in, and never reports a record
// forgotten that is still on disk.
func TestForgetRemovesTheFileTheRecordWasReadFrom(t *testing.T) {
	h := staleHost(t)
	claimed := recordFile(t, h, "ccy-file")
	body := "ccy-session-record 1\nname=ccy-claimed\ndir=" + h.root + "\nlauncher=/l\nprefix=ccy\nrestore=yes\n"
	if err := os.WriteFile(claimed, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("forget", "ccy-claimed"), 0, h)
	if h.stdout() != "ccy-claimed FORGOTTEN\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	if _, err := os.Stat(claimed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the file the record was read from is removed: %v", err)
	}

	// A record in file ccy-ok that says name=ccy-stale: forgetting ccy-stale would find two
	// records of that name; nothing is removed, and the files are named.
	h = staleHost(t)
	body = "ccy-session-record 1\nname=ccy-stale\ndir=" + h.root + "\nlauncher=/l\nprefix=ccy\nrestore=yes\n"
	other := recordFile(t, h, "ccy-other")
	if err := os.WriteFile(other, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("forget", "ccy-stale"), 1, h)
	contains(t, "two", h.stderr(), other, recordFile(t, h, "ccy-stale"))
	for _, f := range []string{other, recordFile(t, h, "ccy-stale"), recordFile(t, h, "ccy-ok")} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("%s is kept: %v", f, err)
		}
	}

	// A record in file ccy-ok's place under another name: forgetting it leaves ccy-ok's own
	// file alone.
	h = staleHost(t)
	body = "ccy-session-record 1\nname=ccy-ok\ndir=" + h.root + "\nlauncher=/l\nprefix=ccy\nrestore=yes\n"
	moved := recordFile(t, h, "ccy-moved")
	if err := os.WriteFile(moved, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(recordFile(t, h, "ccy-ok")); err != nil {
		t.Fatal(err)
	}
	impostor := "ccy-session-record 1\nname=ccy-third\ndir=" + h.root + "\nlauncher=/l\nprefix=ccy\nrestore=yes\n"
	if err := os.WriteFile(recordFile(t, h, "ccy-ok"), []byte(impostor), 0o600); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("forget", "ccy-ok"), 0, h)
	if _, err := os.Stat(moved); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the file the record was read from is removed: %v", err)
	}
	if _, err := os.Stat(recordFile(t, h, "ccy-ok")); err != nil {
		t.Fatalf("the other record's file is kept: %v", err)
	}
}

func TestForgetHelpPrintsTheUsage(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		h := staleHost(t)
		exitCodeIs(t, h.run("forget", flag), 0, h)
		contains(t, flag, h.stdout(), "ccy-sessions forget [--] NAME")
	}
	// After --, a name that looks like an option is a name.
	h := staleHost(t)
	exitCodeIs(t, h.run("forget", "--", "--help"), 64, h)
	contains(t, "dash", h.stderr(), "no session record named '--help'.")
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
