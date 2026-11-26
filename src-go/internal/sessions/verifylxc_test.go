package sessions

import (
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Plan 00266 C.3: verify-restore reports the LXC set the boot unit was to bring back, from
// `teamchat-admin lxc check-running` as root (its sudoers rule), wherever the boot unit is
// enabled. The record and the containers are root's, so nothing is read as the user.

var (
	bootEnabled  = []string{"systemctl", "is-enabled", "teamchat-lxc-boot.service"}
	checkRunning = []string{"sudo", "-n", "/usr/local/sbin/teamchat-admin", "lxc", "check-running"}
)

// lxcHost is verifyHost with no session restored, the LXC boot unit enabled and check-running
// answering out.
func lxcHost(t *testing.T, out string) *harness {
	t.Helper()
	h := verifyHost(t)
	writeEntries(t, h)
	h.fake.On(bootEnabled, execx.Result{Stdout: []byte("enabled\n")})
	h.fake.On(checkRunning, execx.Result{Stdout: []byte(out)})
	return h
}

// Every recorded container running passes; one running that the record does not name is told,
// and passes: the boot unit starts at least the recorded set, and lxc.start.auto or a host's own
// IaC may start more.
func TestVerifyRestoreReportsTheLXCSet(t *testing.T) {
	h := lxcHost(t, "UP\tc1\t\nUP\tc2\t\nUNRECORDED\tc4\t\n")
	report, code := verifyReport(h)
	if code != term.ExitOK || report != "lxc:c1 RUNNING|lxc:c2 RUNNING|lxc:c4 NOT-RECORDED" {
		t.Fatalf("%d %q %s", code, report, h.stderr())
	}
	contains(t, "unrecorded", h.stderr(), "lxc:c4 runs but the last shutdown did not record it")
}

// A recorded container not running fails, with its state, and so does a shutdown that recorded
// nothing: the set the boot unit was to bring back is unknown.
func TestVerifyRestoreFailsOnAnLXCContainerDownOrNoRecord(t *testing.T) {
	h := lxcHost(t, "UP\tc1\t\nDOWN\tc2\tSTOPPED\nDOWN\tc3\tABSENT\n")
	report, code := verifyReport(h)
	if code != term.ExitFailure || report != "lxc:c1 RUNNING|lxc:c2 NOT-RUNNING STOPPED|lxc:c3 NOT-RUNNING ABSENT" {
		t.Fatalf("%d %q", code, report)
	}
	contains(t, "down", h.stderr(), "journalctl -u teamchat-lxc-boot -b --no-pager | cat")

	h = lxcHost(t, "NO-RECORD\t\t/var/lib/teamchat-admin/_lxc-running.json\nUNRECORDED\tc1\t\n")
	report, code = verifyReport(h)
	if code != term.ExitFailure || report != "lxc NO-RECORD|lxc:c1 NOT-RECORDED" {
		t.Fatalf("no record: %d %q", code, report)
	}
	contains(t, "no record", h.stderr(), "/var/lib/teamchat-admin/_lxc-running.json")
}

// --wait looks again while a recorded container is not yet running (the boot unit waits up to a
// minute for each), and stops at the deadline with the last answer.
func TestVerifyRestoreWaitsForTheLXCSet(t *testing.T) {
	h := lxcHost(t, "")
	calls := 0
	h.fake.OnFunc(checkRunning, func(execx.Cmd) (execx.Result, error) {
		calls++
		if calls < 3 {
			return execx.Result{Stdout: []byte("DOWN\tc1\tSTARTING\n")}, nil
		}
		return execx.Result{Stdout: []byte("UP\tc1\t\n")}, nil
	})
	report, code := verifyReport(h, "--wait", "60")
	if code != term.ExitOK || report != "lxc:c1 RUNNING" || calls != 3 {
		t.Fatalf("%d %q after %d looks", code, report, calls)
	}

	h = lxcHost(t, "DOWN\tc1\tSTOPPED\n")
	h.env["CCY_SESSIONS_POLL_SECONDS"] = "5"
	report, code = verifyReport(h, "--wait", "12")
	if code != term.ExitFailure || report != "lxc:c1 NOT-RUNNING STOPPED" || len(h.pauses) != 3 {
		t.Fatalf("%d %q after %d pauses", code, report, len(h.pauses))
	}
}

// No boot unit enabled is no LXC restore here: said, and nothing asked of sudo.
func TestVerifyRestoreWithNoLXCRestoreAsksNothing(t *testing.T) {
	for _, word := range []string{"disabled", "not-found"} {
		h := verifyHost(t)
		writeEntries(t, h)
		h.fake.On(bootEnabled, execx.Result{Stdout: []byte(word + "\n"), ExitCode: 1})
		if report, code := verifyReport(h); code != term.ExitOK || report != "" {
			t.Fatalf("%s: %d %q", word, code, report)
		}
		contains(t, word, h.stderr(), "teamchat-lxc-boot.service is "+word+", so there is no LXC set to verify")
		if h.fake.Called("sudo") {
			t.Fatalf("%s: sudo was asked", word)
		}
	}
}

// Could not look is a failure, never a state: an unknown is-enabled word, a sudo that wants a
// password (the rule is missing), a check that fails, or a line it never prints.
func TestVerifyRestoreFailsWhenTheLXCSetCannotBeRead(t *testing.T) {
	h := lxcHost(t, "")
	h.fake.On(bootEnabled, execx.Result{Stdout: []byte("masked\n"), ExitCode: 1})
	if _, code := verifyReport(h); code != term.ExitFailure {
		t.Fatalf("masked: %d", code)
	}
	contains(t, "masked", h.stderr(), "teamchat-lxc-boot.service is masked")

	h = lxcHost(t, "")
	h.fake.OnError(bootEnabled, errors.New("no systemctl"))
	if _, code := verifyReport(h); code != term.ExitFailure {
		t.Fatalf("no systemctl: %d", code)
	}
	contains(t, "no systemctl", h.stderr(), "no systemctl")

	h = lxcHost(t, "")
	h.fake.On(checkRunning, execx.Result{Stderr: []byte("sudo: a password is required\n"), ExitCode: 1})
	if _, code := verifyReport(h); code != term.ExitFailure {
		t.Fatalf("password: %d", code)
	}
	contains(t, "password", h.stderr(), "/etc/sudoers.d/teamchat-lxc-check-running")

	h = lxcHost(t, "")
	h.fake.On(checkRunning, execx.Result{Stderr: []byte("teamchat-admin: the LXC seams: no kit\n"), ExitCode: 78})
	if _, code := verifyReport(h); code != term.ExitFailure {
		t.Fatalf("check failed: %d", code)
	}
	contains(t, "check failed", h.stderr(), "the LXC seams: no kit")

	for _, bad := range []string{"UP\tc1\n", "WEIRD\tc1\t\n", "DOWN\tc1\t\n", "UP\t\t\n"} {
		h = lxcHost(t, bad)
		if _, code := verifyReport(h); code != term.ExitFailure {
			t.Fatalf("%q: %d", bad, code)
		}
		contains(t, bad, h.stderr(), "teamchat-admin lxc check-running printed a line it never prints")
	}
}

// The LXC set is reported even when the sessions' half fails, and either half failing fails
// the run.
func TestVerifyRestoreReportsTheLXCSetBesideFailedSessions(t *testing.T) {
	h := lxcHost(t, "UP\tc1\t\n")
	writeEntries(t, h, kept("ccy-e", "ccy", "/srv/e"))
	h.sessions()
	report, code := verifyReport(h)
	if code != term.ExitFailure || report != "ccy-e DEAD session-not-running|lxc:c1 RUNNING" {
		t.Fatalf("%d %q %s", code, report, h.stderr())
	}
}
