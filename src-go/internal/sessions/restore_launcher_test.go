package sessions

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
)

// Plan 00266 Task A.4: a restore starts the fixed table's launcher for the record's prefix, never
// the launcher= path the record holds. Any process of the user can write a record, and the next
// boot would otherwise run the program it names.
func TestRestoreRunsTheTablesLauncherNotTheRecordedPath(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a := realDir(t, h, "a")
	record(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/tmp/planted", Prefix: "ccy", Restore: true})
	exitCodeIs(t, h.run("restore"), 0, h)
	calls := startedSessions(h)
	if len(calls) != 1 {
		t.Fatalf("one session started: %v", h.ran())
	}
	command := strings.Join(calls[0].Args, " ")
	contains(t, "ccy-a", command, " ccy-tmux env CCY_SESSION_RESTORE=1 /launch/ccy ")
	lacks(t, "ccy-a", command, "/tmp/planted")
}

// A prefix the table does not hold is refused, by the restore and by --check alike, naming it.
func TestRestoreRefusesARecordWhosePrefixNamesNoLauncher(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	restoreOn(h, true)
	a := realDir(t, h, "a")
	recordOnly(t, h, registry.Record{Name: "odd-a", Dir: a, Launcher: "/tmp/planted", Prefix: "odd", Restore: true})
	exitCodeIs(t, h.run("restore", "--check"), 1, h)
	if h.stdout() != "odd-a UNRESTORABLE "+whyLauncherUnknown+"\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	contains(t, "check", h.stderr(), "its prefix 'odd' names no launcher a restore starts (ccy or cc)")
	h.out.Reset()
	exitCodeIs(t, h.run("restore"), 1, h)
	if len(startedSessions(h)) != 0 {
		t.Fatalf("nothing started: %v", h.ran())
	}
	contains(t, "restore", h.stderr(), "cannot restore odd-a: its prefix 'odd' names no launcher")
}
