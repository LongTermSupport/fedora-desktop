package sessions

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/restore"
)

// staleHost is a registry holding one session that starts (ccy-ok) and one stale record
// (ccy-stale: its project is there, but it has no conversation to resume and is not marked
// fresh-if-no-conversation).
func staleHost(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	restoreHost(h)
	record(t, h, registry.Record{Name: "ccy-ok", Dir: realDir(t, h, "ok"), Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	recordOnly(t, h, registry.Record{Name: "ccy-stale", Dir: realDir(t, h, "stale"), Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	return h
}

// recordFile is the file the registry keeps the named record in.
func recordFile(t *testing.T, h *harness, name string) string {
	t.Helper()
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(regdir, name)
}

// fedora-desktop#396: a restore given names restores and judges only those records, so a stale
// neighbour cannot fail the restore of a session that started.
func TestRestoreOfNamedSessionsJudgesOnlyThoseRecords(t *testing.T) {
	h := staleHost(t)
	exitCodeIs(t, h.run("restore", "ccy-ok"), 0, h)
	calls := startedSessions(h)
	if len(calls) != 1 || !strings.Contains(strings.Join(calls[0].Args, " "), "-s ccy-ok ") {
		t.Fatalf("only ccy-ok starts: %v", h.ran())
	}
	contains(t, "scoped", h.stderr(), "ccy session restore: 1 started, 0 failed.\n")
	lacks(t, "scoped", h.stderr(), "ccy-stale")
	if got := goingOf(manifestOf(t, h)); !slices.Equal(got, []string{"ccy-ok pending "}) {
		t.Fatalf("only the named session is in the manifest: %q", got)
	}
	if _, err := os.Stat(recordFile(t, h, "ccy-stale")); err != nil {
		t.Fatalf("the record not named is left alone: %v", err)
	}

	// The stale record named fails, and with no names every record is judged, as before.
	h = staleHost(t)
	exitCodeIs(t, h.run("restore", "ccy-stale"), 1, h)
	if len(startedSessions(h)) != 0 {
		t.Fatalf("nothing starts: %v", h.ran())
	}
	h = staleHost(t)
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "every record", h.stderr(), "ccy session restore: 1 started, 1 failed.\n")
}

// A name with no record is a usage error that names it, and nothing is started or written.
func TestRestoreOfANameWithNoRecordStartsNothing(t *testing.T) {
	h := staleHost(t)
	exitCodeIs(t, h.run("restore", "ccy-ok", "ccy-ghost"), 64, h)
	if want := "ccy-sessions: no session record named 'ccy-ghost'. See ccy-sessions --help.\n"; h.stderr() != want {
		t.Fatalf("stderr %q", h.stderr())
	}
	if len(startedSessions(h)) != 0 {
		t.Fatalf("nothing starts: %v", h.ran())
	}
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(restore.NewStore(regdir, "").Path); statErr == nil {
		t.Fatal("no manifest is written")
	}
}

// An unreadable record is named by its file, as the restore names it.
func TestRestoreOfAnUnreadableRecordByName(t *testing.T) {
	h := staleHost(t)
	if err := os.WriteFile(recordFile(t, h, "ccy-bad"), []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("restore", "ccy-bad"), 1, h)
	if got := goingOf(manifestOf(t, h)); !slices.Equal(got, []string{"ccy-bad not-started record-unreadable"}) {
		t.Fatalf("%q", got)
	}
}

// A record no restore can ever start is called stale, and the message says how to remove it.
// A record whose directory is gone, and one launched without the bus but marked
// fresh-if-no-conversation, are not called stale: only an unmarked record with no conversation.
func TestAStaleRecordIsNamedWithHowToForgetIt(t *testing.T) {
	h := staleHost(t)
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "restore", h.stderr(), "ERROR: cannot restore ccy-stale: ccy-stale: no conversation is recorded",
		"(the record is kept at "+recordFile(t, h, "ccy-stale")+").",
		"ccy-stale is a stale record: no restore can start it. Remove it with: ccy-sessions forget ccy-stale")

	h = staleHost(t)
	restoreOn(h, true)
	exitCodeIs(t, h.run("restore", "--check"), 1, h)
	contains(t, "check", h.stderr(), "ccy-stale is a stale record: no restore can start it. Remove it with: ccy-sessions forget ccy-stale")

	h = newHarness(t)
	restoreHost(h)
	record(t, h, registry.Record{Name: "ccy-gone", Dir: filepath.Join(h.root, "gone"), Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	recordOnly(t, h, registry.Record{
		Name: "ccy-marked", Dir: realDir(t, h, "m"), Launcher: "/launch/ccy", Prefix: "ccy", Restore: true,
		FreshIfNoConversation: true, Args: []string{"--no-bus"},
	})
	exitCodeIs(t, h.run("restore"), 1, h)
	lacks(t, "not stale", h.stderr(), "stale record", "ccy-sessions forget")
}

// A dry run is a gate: one stdout line for each record that would fail, and exit 1, scoped to
// the names given.
func TestRestoreDryRunListsEachRecordThatWouldFail(t *testing.T) {
	h := staleHost(t)
	record(t, h, registry.Record{Name: "ccy-gone", Dir: filepath.Join(h.root, "gone"), Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	if err := os.WriteFile(recordFile(t, h, "ccy-bad"), []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("restore", "--dry-run"), 1, h)
	want := "would fail ccy-bad: record-unreadable\nwould fail ccy-gone: directory-gone\nwould fail ccy-stale: no-conversation\n"
	if h.stdout() != want {
		t.Fatalf("stdout %q", h.stdout())
	}
	contains(t, "dry run", h.stderr(), "would start ccy-ok in ")
	if len(startedSessions(h)) != 0 {
		t.Fatal("a dry run starts nothing")
	}

	h = staleHost(t)
	exitCodeIs(t, h.run("restore", "--dry-run", "ccy-ok"), 0, h)
	if h.stdout() != "" {
		t.Fatalf("stdout %q", h.stdout())
	}
	h = staleHost(t)
	exitCodeIs(t, h.run("restore", "--dry-run", "ccy-stale"), 1, h)
	if h.stdout() != "would fail ccy-stale: no-conversation\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
}
