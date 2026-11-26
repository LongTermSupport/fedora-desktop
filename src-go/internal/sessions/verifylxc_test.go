package sessions

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcset"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Plan 00266 C.3: verify-restore reports the LXC set the boot unit was to bring back, from the
// status the boot unit's verb writes to the owner group (lxcset), wherever the boot unit is
// enabled. Nothing is asked of root: no sudo, no teamchat-admin.

var bootEnabled = []string{"systemctl", "is-enabled", "teamchat-lxc-boot.service"}

// lxcHost is verifyHost with no session restored, the LXC boot unit enabled and a host root for
// the status, which holds none yet.
func lxcHost(t *testing.T) *harness {
	t.Helper()
	h := verifyHost(t)
	writeEntries(t, h)
	h.fake.On(bootEnabled, execx.Result{Stdout: []byte("enabled\n")})
	root := filepath.Join(h.root, "teamchat-host")
	if err := os.MkdirAll(root+paths.StatusRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	h.env[lxcHostRootEnv] = root
	return h
}

// setStatus writes the status the boot unit writes, in boot, with lines.
func setStatus(t *testing.T, h *harness, boot string, lines ...lxcset.Line) {
	t.Helper()
	if err := lxcset.Write(h.env[lxcHostRootEnv], lxcset.Status{BootID: boot, WrittenMS: 1_700_000_000_000, Lines: lines}, -1); err != nil {
		t.Fatal(err)
	}
}

func up(c string) lxcset.Line { return lxcset.Line{Kind: lxcset.Up, Container: c} }

func down(c, why string) lxcset.Line {
	return lxcset.Line{Kind: lxcset.Down, Container: c, Why: why}
}

// Every recorded container running passes; one running that the record does not name is told,
// and passes: the boot unit starts at least the recorded set, and lxc.start.auto or a host's own
// IaC may start more. No sudo is asked.
func TestVerifyRestoreReportsTheLXCSet(t *testing.T) {
	h := lxcHost(t)
	setStatus(t, h, "this-boot", up("c1"), up("c2"), lxcset.Line{Kind: lxcset.Unrecorded, Container: "c4"})
	report, code := verifyReport(h)
	if code != term.ExitOK || report != "lxc:c1 RUNNING|lxc:c2 RUNNING|lxc:c4 NOT-RECORDED" {
		t.Fatalf("%d %q %s", code, report, h.stderr())
	}
	contains(t, "unrecorded", h.stderr(), "lxc:c4 runs but the last shutdown did not record it", "as teamchat-lxc-boot.service found it")
	if h.fake.Called("sudo") {
		t.Fatal("sudo was asked")
	}
}

// A recorded container not running fails, with its state, and so does a shutdown that recorded
// nothing: the set the boot unit was to bring back is unknown.
func TestVerifyRestoreFailsOnAnLXCContainerDownOrNoRecord(t *testing.T) {
	h := lxcHost(t)
	setStatus(t, h, "this-boot", up("c1"), down("c2", "STOPPED"), down("c3", lxcset.Absent))
	report, code := verifyReport(h)
	if code != term.ExitFailure || report != "lxc:c1 RUNNING|lxc:c2 NOT-RUNNING STOPPED|lxc:c3 NOT-RUNNING ABSENT" {
		t.Fatalf("%d %q", code, report)
	}
	contains(t, "down", h.stderr(), "journalctl -u teamchat-lxc-boot -b --no-pager | cat")

	h = lxcHost(t)
	setStatus(t, h, "this-boot", lxcset.Line{Kind: lxcset.NoRecord, Why: paths.LXCRunningFile}, lxcset.Line{Kind: lxcset.Unrecorded, Container: "c1"})
	report, code = verifyReport(h)
	if code != term.ExitFailure || report != "lxc NO-RECORD|lxc:c1 NOT-RECORDED" {
		t.Fatalf("no record: %d %q", code, report)
	}
	contains(t, "no record", h.stderr(), "/var/lib/teamchat-admin/_lxc-running.json")
}

// A status from another boot, or none while the boot unit is enabled, is a finding, never a
// pass: the boot unit has not finished this boot, or failed before it wrote.
func TestVerifyRestoreFailsOnAStaleOrMissingLXCStatus(t *testing.T) {
	h := lxcHost(t)
	report, code := verifyReport(h)
	if code != term.ExitFailure || report != "lxc NO-STATUS" {
		t.Fatalf("missing: %d %q", code, report)
	}
	contains(t, "missing", h.stderr(), paths.LXCSetStatusFile, "journalctl -u teamchat-lxc-boot -b --no-pager | cat")

	setStatus(t, h, "earlier-boot", up("c1"))
	report, code = verifyReport(h)
	if code != term.ExitFailure || report != "lxc STALE" {
		t.Fatalf("stale: %d %q", code, report)
	}
	contains(t, "stale", h.stderr(), "written in another boot")
}

// --wait looks again while the status is missing or of another boot (the boot unit runs after
// multi-user.target and waits up to a minute for each container), and stops at the deadline. A
// status of this boot is final: the boot unit writes it once.
func TestVerifyRestoreWaitsForTheLXCStatus(t *testing.T) {
	h := lxcHost(t)
	setStatus(t, h, "earlier-boot", up("c1"))
	h.env["CCY_SESSIONS_POLL_SECONDS"] = "5"
	report, code := verifyReport(h, "--wait", "12")
	if code != term.ExitFailure || report != "lxc STALE" || len(h.pauses) != 3 {
		t.Fatalf("%d %q after %d pauses", code, report, len(h.pauses))
	}

	h = lxcHost(t)
	setStatus(t, h, "this-boot", down("c1", "STOPPED"))
	h.env["CCY_SESSIONS_POLL_SECONDS"] = "5"
	report, code = verifyReport(h, "--wait", "12")
	if code != term.ExitFailure || report != "lxc:c1 NOT-RUNNING STOPPED" || len(h.pauses) != 0 {
		t.Fatalf("final: %d %q after %d pauses", code, report, len(h.pauses))
	}
}

// No boot unit enabled is no LXC restore here: said, and no status read.
func TestVerifyRestoreWithNoLXCRestoreAsksNothing(t *testing.T) {
	for _, word := range []string{"disabled", "not-found"} {
		h := verifyHost(t)
		writeEntries(t, h)
		h.fake.On(bootEnabled, execx.Result{Stdout: []byte(word + "\n"), ExitCode: 1})
		h.env[lxcHostRootEnv] = filepath.Join(h.root, "no-such-host")
		if report, code := verifyReport(h); code != term.ExitOK || report != "" {
			t.Fatalf("%s: %d %q", word, code, report)
		}
		contains(t, word, h.stderr(), "teamchat-lxc-boot.service is "+word+", so there is no LXC set to verify")
	}
}

// Could not look is a failure, never a state: an unknown is-enabled word, no systemctl, a status
// that cannot be read (the user not in the owner group) or one Write never writes.
func TestVerifyRestoreFailsWhenTheLXCSetCannotBeRead(t *testing.T) {
	h := lxcHost(t)
	h.fake.On(bootEnabled, execx.Result{Stdout: []byte("masked\n"), ExitCode: 1})
	if _, code := verifyReport(h); code != term.ExitFailure {
		t.Fatalf("masked: %d", code)
	}
	contains(t, "masked", h.stderr(), "teamchat-lxc-boot.service is masked")

	h = lxcHost(t)
	h.fake.OnError(bootEnabled, errors.New("no systemctl"))
	if _, code := verifyReport(h); code != term.ExitFailure {
		t.Fatalf("no systemctl: %d", code)
	}
	contains(t, "no systemctl", h.stderr(), "no systemctl")

	h = lxcHost(t)
	file := h.env[lxcHostRootEnv] + paths.LXCSetStatusFile
	if err := os.WriteFile(file, []byte(`{"v":1}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if report, code := verifyReport(h); code != term.ExitFailure || report != "" {
		t.Fatalf("invalid: %d %q", code, report)
	}
	contains(t, "invalid", h.stderr(), "the LXC set cannot be read", "invalid LXC set status", "teamchat owner group")

	h = lxcHost(t)
	if err := os.Mkdir(h.env[lxcHostRootEnv]+paths.LXCSetStatusFile, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, code := verifyReport(h); code != term.ExitFailure {
		t.Fatalf("not a file: %d", code)
	}
	contains(t, "not a file", h.stderr(), "the LXC set cannot be read")

	h = lxcHost(t)
	setStatus(t, h, "this-boot", up("c1"))
	h.env["CCY_BOOT_ID_FILE"] = filepath.Join(h.root, "no-boot-id")
	if _, code := verifyReport(h); code != term.ExitFailure {
		t.Fatalf("no boot id: %d", code)
	}
	contains(t, "no boot id", h.stderr(), "this boot's id could not be read")
}

// The LXC set is reported even when the sessions' half fails, and either half failing fails
// the run.
func TestVerifyRestoreReportsTheLXCSetBesideFailedSessions(t *testing.T) {
	h := lxcHost(t)
	setStatus(t, h, "this-boot", up("c1"))
	writeEntries(t, h, kept("ccy-e", "ccy", "/srv/e"))
	h.sessions()
	report, code := verifyReport(h)
	if code != term.ExitFailure || report != "ccy-e DEAD session-not-running|lxc:c1 RUNNING" {
		t.Fatalf("%d %q %s", code, report, h.stderr())
	}
}
