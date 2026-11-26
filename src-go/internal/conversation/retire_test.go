package conversation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fedora-desktop#431 review: a v1 record under a name no session owns any more is retired by
// moving it aside, beside itself, so the restore no longer counts it and its id is kept for a
// person to read; a marked record in the checkout then starts fresh.
func TestRetireMovesTheRecordAsideAndTheCheckoutStartsFresh(t *testing.T) {
	rec := renamed(t)
	rec.FreshIfNoConversation = true
	got, retired, err := Retire(rec.Dir, "ccy-proj")
	if err != nil || !retired || got != recordPath(rec.Dir) {
		t.Fatalf("Retire = %q, %v; want %s", got, err, recordPath(rec.Dir))
	}
	if _, err := os.Lstat(recordPath(rec.Dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the record is still there: %v", err)
	}
	kept, err := os.ReadFile(filepath.Join(filepath.Dir(recordPath(rec.Dir)), RetiredFile))
	if err != nil || !strings.Contains(string(kept), id) {
		t.Fatalf("the retired record = %q, %v; want it to hold %s", kept, err, id)
	}
	start, err := StartPlan(rec, none)
	if err != nil || !start.Fresh {
		t.Fatalf("StartPlan = %+v, %v; want a fresh start", start, err)
	}
}

// A name with no record, in a checkout or one that is not there, retires nothing and is no error;
// the path is the one looked at.
func TestRetireOfNothingRetiresNothing(t *testing.T) {
	for _, checkout := range []string{t.TempDir(), "/nonexistent/checkout"} {
		got, retired, err := Retire(checkout, "ccy-proj")
		if err != nil || retired || got != filepath.Join(checkout, ".claude/ccy/pingbus/sessions/ccy-proj", File) {
			t.Fatalf("%s: Retire = %q, %v, %v; want nothing retired, naming the path", checkout, got, retired, err)
		}
	}
}

// A link anywhere on the way to the record, or a record that is not a regular file, is refused,
// never followed: a container can write the checkout's .claude/ccy.
func TestRetireRefusesALink(t *testing.T) {
	rec := renamed(t)
	target := recordPath(rec.Dir)
	moved := target + ".real"
	if err := os.Rename(target, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, target); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Retire(rec.Dir, "ccy-proj"); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("Retire of a linked record = %v, want a refusal", err)
	}

	rec = renamed(t)
	sessions := filepath.Dir(filepath.Dir(recordPath(rec.Dir)))
	if err := os.Rename(sessions, sessions+".real"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sessions+".real", sessions); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Retire(rec.Dir, "ccy-proj"); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("Retire through a linked directory = %v, want a refusal", err)
	}
	if _, err := os.Lstat(filepath.Join(sessions+".real", "ccy-proj", File)); err != nil {
		t.Fatalf("the record behind the link is kept: %v", err)
	}
}

// A name that is not a session's is refused: it would name a path outside the session records.
func TestRetireRefusesANameThatIsNotASessions(t *testing.T) {
	for _, name := range []string{"", "..", "a/b", "-x"} {
		if _, _, err := Retire(t.TempDir(), name); err == nil {
			t.Fatalf("Retire(%q) gave no error", name)
		}
	}
}

// RetireOwn is the forget path's: the record's own name in its own checkout (a cc record's git
// top), and nothing when its folder is gone.
func TestRetireOwnRetiresTheRecordsOwnConversation(t *testing.T) {
	rec := ccyCase(t)
	got, err := RetireOwn(rec)
	if err != nil || got != recordPath(rec.Dir) {
		t.Fatalf("RetireOwn = %q, %v", got, err)
	}

	checkout := t.TempDir()
	write(t, filepath.Join(checkout, ".git", "HEAD"), "ref: refs/heads/main\n")
	cc := record("cc-proj", "cc", filepath.Join(checkout, "sub"), "--bus")
	own := filepath.Join(checkout, ".claude/ccy/pingbus/sessions/cc-proj", File)
	write(t, own, recorded("cc-proj", id))
	if err := os.MkdirAll(cc.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err = RetireOwn(cc); err != nil || got != own {
		t.Fatalf("RetireOwn(cc) = %q, %v; want %s", got, err, own)
	}

	gone := record("ccy-gone", "ccy", "/nonexistent/checkout")
	if got, err = RetireOwn(gone); err != nil || got != "" {
		t.Fatalf("RetireOwn of a folder that is gone = %q, %v", got, err)
	}
}
