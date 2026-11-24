package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/shellword"
)

// fedora-desktop#398: a record's name= is not held to its file's name (a record edited or renamed
// by hand), so whatever acts on a record acts on the file it was read from, never on
// <registry>/<name>.

// moveRecord moves the record named name to the registry file called file, as a person renaming
// it by hand would, and returns the file's path.
func moveRecord(t *testing.T, from, file string) string {
	t.Helper()
	to := filepath.Join(filepath.Dir(from), file)
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	return to
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Ending a session in the picker removes the file its record was read from, and leaves the file
// named after the session when that file holds another session's record.
func TestEndingASessionRemovesTheFileItsRecordWasReadFrom(t *testing.T) {
	h := newHarness(t)
	s := newServer(h)
	s.listSessions("/srv/a", "ccy-a")
	notInProject(h)
	a := realDir(t, h, "a")
	moved := moveRecord(t, record(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/l", Prefix: "ccy", Restore: true}), "renamed-a")
	other := moveRecord(t, record(t, h, registry.Record{Name: "ccy-other", Dir: a, Launcher: "/l", Prefix: "ccy", Restore: true}), "ccy-a")
	newPickerRig(h, "ctrl-x|ccy-a", "|YES", "|ABORT")
	exitCodeIs(t, h.run(), 0, h)
	contains(t, "ended", h.stderr(), "Ended 'ccy-a'.")
	if exists(moved) {
		t.Fatal("the ended session's record is left behind as a stale record")
	}
	if !exists(other) {
		t.Fatal("another session's record, in the file named after the ended one, was removed")
	}
}

// The restore's refusal names the file the record was read from.
func TestARestoreRefusalNamesTheFileTheRecordWasReadFrom(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	gone := filepath.Join(h.root, "gone")
	file := moveRecord(t, record(t, h, registry.Record{Name: "ccy-b", Dir: gone, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true}), "renamed-b")
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "restore", h.stderr(), "ERROR: cannot restore ccy-b: its directory no longer exists: "+gone+
		" (the record is kept at "+file+"; remove it if the project is gone).")

	h.errOut.Reset()
	restoreOn(h, true)
	exitCodeIs(t, h.run("restore", "--check"), 1, h)
	contains(t, "check", h.stderr(), "(the record is kept at "+file+"; remove it if the project is gone).")
}

// A restored session's trampoline removes the file its record was read from when it ends, and a
// start that fails names that file.
func TestARestoredSessionIsHandedTheFileItsRecordWasReadFrom(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a := realDir(t, h, "a")
	file := moveRecord(t, record(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true}), "renamed-a")
	exitCodeIs(t, h.run("restore"), 0, h)
	calls := startedSessions(h)
	if len(calls) != 1 || !strings.Contains(strings.Join(calls[0].Args, " "), "rm -f -- "+shellword.Quote(file)+";") {
		t.Fatalf("the trampoline does not remove %s: %v", file, calls)
	}

	h = newHarness(t)
	restoreHost(h)
	a = realDir(t, h, "a")
	file = moveRecord(t, record(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true}), "renamed-a")
	h.fake.On([]string{"systemd-run"}, execx.Result{ExitCode: 1, Stderr: []byte("Failed to start scope")})
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "start failure", h.stderr(), "ERROR: could not start ccy-a in "+a+" (the record is kept at "+file+").")
}

// declare's refusal names the file of the record already answering to the name, and names no
// file when there is none: nothing was declared, so no record is kept.
func TestADeclareRefusalNamesOnlyARecordThatIsThere(t *testing.T) {
	h := newHarness(t)
	dir := declareHost(h)
	h.write("work/proj/docker-compose.yml", "services: {}\n")
	exitCodeIs(t, h.run("declare", "--fresh-if-no-conversation", dir, "--", "--no-ssh"), 1, h)
	contains(t, "refusal", h.stderr(), "provisioned without a compose answer")
	lacks(t, "no record", h.stderr(), "the record is kept at")

	h = newHarness(t)
	dir = declareHost(h)
	h.write("work/proj/docker-compose.yml", "services: {}\n")
	file := moveRecord(t, recordOnly(t, h, registry.Record{Name: "ccy-proj", Dir: dir, Launcher: "/l", Prefix: "ccy", Compose: "started", Restore: true}), "renamed-proj")
	exitCodeIs(t, h.run("declare", "--replace", "--fresh-if-no-conversation", dir, "--", "--no-ssh"), 1, h)
	contains(t, "renamed", h.stderr(), "(the record is kept at "+file+").")
}
