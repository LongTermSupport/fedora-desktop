package sessions

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/restore"
)

// verifyHost scripts the host for verify-restore: ccy-a runs a container (a_yolo_1) that is up,
// cc-c runs on the host, and the other sessions have no pane.
func verifyHost(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.env["CCY_SESSIONS_POLL_SECONDS"] = "0"
	h.tmuxOut("list-panes", "ccy-a 100\ncc-c 300\nccy-b 200\n")
	h.fake.On([]string{"ps"}, execx.Result{Stdout: []byte(strings.Join([]string{
		"100 1 bash -c trampoline", "101 100 /var/local/claude-yolo/claude-yolo",
		"102 101 podman run --rm -it --name a_yolo_1 claude-yolo:latest",
		"200 1 bash -c trampoline", "300 1 bash -c trampoline",
	}, "\n") + "\n")})
	h.fake.On([]string{"podman", "ps"}, execx.Result{Stdout: []byte("a_yolo_1\n")})
	return h
}

// kept is a session the restore found already running: nothing for set-going to do.
func kept(name, prefix, dir string) restore.Entry {
	e := pending(name, prefix, dir)
	e.Going = restore.GoingNone
	return e
}

func verifyReport(h *harness, args ...string) (string, int) {
	h.out.Reset()
	h.errOut.Reset()
	code := h.run(append([]string{"verify-restore"}, args...)...)
	return strings.ReplaceAll(strings.TrimSuffix(h.stdout(), "\n"), "\n", "|"), code
}

func TestVerifyRestoreRefusesWhenNoRestoreWasRecordedOrItIsFromAnEarlierBoot(t *testing.T) {
	h := verifyHost(t)
	if _, code := verifyReport(h); code != 1 {
		t.Fatalf("exit %d", code)
	}
	contains(t, "none", h.stderr(), "no session restore has been recorded")

	h = verifyHost(t)
	if err := os.WriteFile(h.env["CCY_BOOT_ID_FILE"], []byte("earlier-boot\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeEntries(t, h, kept("ccy-a", "ccy", "/srv/a"))
	if err := os.WriteFile(h.env["CCY_BOOT_ID_FILE"], []byte("this-boot\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, code := verifyReport(h); code != 1 {
		t.Fatalf("exit %d", code)
	}
	contains(t, "earlier", h.stderr(), "ERROR: the last session restore ran in an earlier boot, so it says nothing about this one; nothing restored these sessions since this machine started.")

	h = verifyHost(t)
	writeEntries(t, h, kept("ccy-a", "ccy", "/srv/a"))
	h.env["CCY_BOOT_ID_FILE"] = filepath.Join(h.root, "no-boot-id")
	h.app.cfg.BootIDFile = ""
	if _, code := verifyReport(h); code != 1 {
		t.Fatalf("exit %d", code)
	}
	contains(t, "no boot id", h.stderr(), "this boot's id could not be read")
}

func TestVerifyRestoreWithNothingRestoredPassesQuietly(t *testing.T) {
	h := verifyHost(t)
	writeEntries(t, h)
	report, code := verifyReport(h)
	if code != 0 || report != "" {
		t.Fatalf("%d %q", code, report)
	}
	contains(t, "empty", h.stderr(), "This boot's restore brought up no sessions; nothing to verify.")
}

func TestVerifyRestoreNamesEachSessionByItsState(t *testing.T) {
	h := verifyHost(t)
	a, b := "/srv/a", "/srv/b"
	writeEntries(t, h, kept("ccy-a", "ccy", a), kept("ccy-b", "ccy", b), kept("cc-c", "cc", a), kept("ccy-d", "ccy", b), kept("ccy-e", "ccy", a))
	h.sessions("ccy-a 0 "+a, "ccy-b 0 "+b, "cc-c 0 "+a, "ccy-d 0 "+b)
	h.tmuxOut("list-panes", "ccy-a 100\nccy-b 200\ncc-c 300\nccy-d 400\n")
	showScreen(h, "ccy-a", "claude is running\n")
	showScreen(h, "ccy-b", "Found previous launch configuration\nUse same configuration? [Y/n] \n\n\n")
	showScreen(h, "cc-c", "Select Claude token [0-2]: \n")
	showScreen(h, "ccy-d", "ccy exited with status 1. Press Enter to close this session.\n")
	report, code := verifyReport(h)
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{
		"ccy-a OK", "ccy-b WAITING-AT-PROMPT quick-launch", "ccy-d DEAD launcher-exited", "ccy-e DEAD session-not-running",
	} {
		contains(t, "report", report, want)
	}
	if strings.Count(report, "|") != 4 {
		t.Fatalf("one line per restored session: %q", report)
	}
	contains(t, "why", h.stderr(), "Not every restored session is running and past its prompts; attach with ccy-sessions to answer or look.")
}

func TestVerifyRestorePassesWhenEverythingIsPastItsPrompts(t *testing.T) {
	h := verifyHost(t)
	writeEntries(t, h, kept("ccy-a", "ccy", "/srv/a"), kept("cc-c", "cc", "/srv/a"))
	h.sessions("ccy-a 0 /srv/a", "cc-c 0 /srv/a")
	showScreen(h, "ccy-a", "claude is running\n")
	showScreen(h, "cc-c", "claude is running\n")
	report, code := verifyReport(h)
	if code != 0 || report != "ccy-a OK|cc-c OK" {
		t.Fatalf("%d %q", code, report)
	}
	if h.stderr() != "" {
		t.Fatalf("stderr %q", h.stderr())
	}
}

func TestACcySessionWhoseContainerIsNotUpIsStartingAndWaitGivesUpAtTheDeadline(t *testing.T) {
	h := verifyHost(t)
	writeEntries(t, h, kept("ccy-b", "ccy", "/srv/b"))
	h.sessions("ccy-b 0 /srv/b")
	showScreen(h, "ccy-b", "Building image...\n")
	report, code := verifyReport(h, "--wait", "0")
	if code != 1 || report != "ccy-b STARTING" {
		t.Fatalf("%d %q", code, report)
	}
}

func TestVerifyRestoreWaitPollsUntilTheRestoreHasSettled(t *testing.T) {
	// A settled failure ends the wait at once: the same answer twice, nothing transient.
	h := verifyHost(t)
	e := kept("ccy-a", "ccy", "/srv/a")
	e.Going, e.Detail = restore.GoingUntouched, "no-transcript"
	writeEntries(t, h, e)
	h.sessions("ccy-a 0 /srv/a")
	showScreen(h, "ccy-a", "claude is running\n")
	report, code := verifyReport(h, "--wait", "60")
	if code != 1 || report != "ccy-a NOT-SET-GOING no-transcript" || len(h.pauses) != 1 {
		t.Fatalf("%d %q %v", code, report, h.pauses)
	}

	// A pending one keeps it waiting to the end.
	h = verifyHost(t)
	h.env["CCY_SESSIONS_POLL_SECONDS"] = "1"
	writeEntries(t, h, pending("ccy-a", "ccy", "/srv/a"))
	h.sessions("ccy-a 0 /srv/a")
	showScreen(h, "ccy-a", "claude is running\n")
	report, code = verifyReport(h, "--wait", "3")
	if code != 1 || report != "ccy-a SETTING-GOING" || len(h.pauses) != 3 || h.pauses[0] != time.Second {
		t.Fatalf("%d %q %v", code, report, h.pauses)
	}
}

func TestVerifyRestoreWaitSeesWhatSetGoingWritesMeanwhile(t *testing.T) {
	h := verifyHost(t)
	h.env["CCY_SESSIONS_POLL_SECONDS"] = "1"
	writeEntries(t, h, pending("ccy-a", "ccy", "/srv/a"))
	h.sessions("ccy-a 0 /srv/a")
	showScreen(h, "ccy-a", "claude is running\n")
	h.app.Pause = func(d time.Duration, _ <-chan os.Signal) bool {
		h.pauses = append(h.pauses, d)
		h.clock = h.clock.Add(d)
		writeEntries(t, h, kept("ccy-a", "ccy", "/srv/a"))
		return false
	}
	report, code := verifyReport(h, "--wait", "30")
	if code != 0 || report != "ccy-a OK" {
		t.Fatalf("%d %q", code, report)
	}
}

func TestVerifyRestoreChecksThatWhatWasTypedWasTaken(t *testing.T) {
	h := verifyHost(t)
	a := realDir(t, h, "a")
	conv := ccyTranscript(a)
	writeTranscript(t, conv, 5000)
	at := h.clock.Unix()
	e := kept("ccy-a", "ccy", a)
	e.Going, e.At, e.Detail, e.Transcript = restore.GoingCompact, strconv.FormatInt(at, 10), "5000", conv
	writeEntries(t, h, e)
	h.sessions("ccy-a 0 " + a)
	showScreen(h, "ccy-a", "claude is running\n")

	h.env["CCY_SESSIONS_START_WINDOW"] = "3600"
	if report, code := verifyReport(h); code != 1 || report != "ccy-a SETTING-GOING" {
		t.Fatalf("typed, not yet taken, inside the window: %d %q", code, report)
	}
	h.env["CCY_SESSIONS_START_WINDOW"] = "0"
	if report, code := verifyReport(h); code != 1 || report != "ccy-a NOT-SET-GOING compact-not-started" {
		t.Fatalf("not taken by the end of the window: %d %q", code, report)
	}
	tookInput(h, conv)
	if report, code := verifyReport(h); code != 0 || report != "ccy-a OK" {
		t.Fatalf("taken: %d %q", code, report)
	}
}

func TestVerifyRestoreReportsWhatSetGoingLeftAlone(t *testing.T) {
	h := verifyHost(t)
	e1 := kept("ccy-a", "ccy", "/srv/a")
	e1.Going, e1.Detail = restore.GoingUntouched, "busy-before-set-going"
	writeEntries(t, h, e1, pending("cc-c", "cc", "/srv/a"))
	h.sessions("ccy-a 0 /srv/a", "cc-c 0 /srv/a")
	showScreen(h, "ccy-a", "claude is running\n")
	showScreen(h, "cc-c", "claude is running\n")
	report, code := verifyReport(h)
	if code != 1 || report != "ccy-a NOT-SET-GOING busy-before-set-going|cc-c SETTING-GOING" {
		t.Fatalf("%d %q", code, report)
	}
}

func TestVerifyRestoreIsAFailureWhenAProbeFails(t *testing.T) {
	// A screen that cannot be read fails the whole report: "could not look" is not "dead".
	h := verifyHost(t)
	writeEntries(t, h, kept("ccy-b", "ccy", "/srv/b"))
	h.sessions("ccy-b 0 /srv/b")
	h.tmuxFail("capture-pane", "can't find pane")
	if _, code := verifyReport(h); code != 1 {
		t.Fatalf("exit %d", code)
	}
	contains(t, "screen", h.stderr(), "screen of ccy-b could not be read")

	h = verifyHost(t)
	writeEntries(t, h, kept("ccy-b", "ccy", "/srv/b"))
	h.tmuxFail("list-sessions", "lost server")
	if _, code := verifyReport(h); code != 1 {
		t.Fatalf("exit %d", code)
	}

	h = verifyHost(t)
	writeEntries(t, h, kept("ccy-b", "ccy", "/srv/b"))
	h.sessions("ccy-b 0 /srv/b")
	h.fake.On([]string{"ps"}, execx.Result{ExitCode: 1, Stderr: []byte("ps gone")})
	if _, code := verifyReport(h); code != 1 {
		t.Fatalf("exit %d", code)
	}

	h = verifyHost(t)
	writeEntries(t, h, kept("ccy-a", "ccy", "/srv/a"))
	h.sessions("ccy-a 0 /srv/a")
	showScreen(h, "ccy-a", "claude is running\n")
	h.fake.On([]string{"podman", "ps"}, execx.Result{ExitCode: 1, Stderr: []byte("podman: cannot connect")})
	if _, code := verifyReport(h); code != 1 {
		t.Fatalf("exit %d", code)
	}
	contains(t, "engine", h.stderr(), "ERROR: podman ps failed: podman: cannot connect")

	h = verifyHost(t)
	e := kept("ccy-a", "ccy", "/srv/a")
	e.Going, e.At, e.Transcript = restore.GoingContinue, "1", filepath.Join(h.root, "no-such-transcript")
	writeEntries(t, h, e)
	h.sessions("ccy-a 0 /srv/a")
	showScreen(h, "ccy-a", "claude is running\n")
	if _, code := verifyReport(h); code != 1 {
		t.Fatalf("exit %d", code)
	}
}

func TestVerifyRestoreUsageMistakes(t *testing.T) {
	cases := []struct {
		args []string
		msg  string
	}{
		{[]string{"--wait", "soon"}, "--wait needs a whole number of seconds; got 'soon'."},
		{[]string{"--wait"}, "--wait needs a whole number of seconds; got ''."},
		{[]string{"--wait", "-1"}, "--wait needs a whole number of seconds; got '-1'."},
		{[]string{"--bogus"}, "unknown verify-restore option '--bogus'."},
	}
	for _, c := range cases {
		h := verifyHost(t)
		exitCodeIs(t, h.run(append([]string{"verify-restore"}, c.args...)...), 64, h)
		if want := "ccy-sessions: " + c.msg + " See ccy-sessions --help.\n"; h.stderr() != want {
			t.Fatalf("%v: %q", c.args, h.stderr())
		}
	}
}
