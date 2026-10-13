package sessions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// pair is two projects, each with a working daemon CLI and a cc session.
type pair struct{ a, b string }

func twoProjects(h *harness) pair {
	p := pair{a: h.project("a", true), b: h.project("b", true)}
	h.okCLI(p.a)
	h.okCLI(p.b)
	h.sessions("cc-a 1 "+p.a, "cc-b 0 "+p.b)
	h.tmuxOut("list-panes", "")
	h.fake.On([]string{"ps"}, execx.Result{})
	return p
}

// restoreOn makes the home look as it does when the user's session restore is enabled.
func restoreOn(h *harness, wired bool) {
	dir := filepath.Join(h.env["HOME"], ".config", "systemd", "user")
	if err := os.MkdirAll(filepath.Join(dir, "default.target.wants"), 0o750); err != nil {
		h.t.Fatal(err)
	}
	if err := os.Symlink("../ccy-sessions-restore.service", filepath.Join(dir, "default.target.wants", "ccy-sessions-restore.service")); err != nil {
		h.t.Fatal(err)
	}
	if wired {
		if err := os.WriteFile(filepath.Join(dir, "ccy-sessions-restore.service"), nil, 0o600); err != nil {
			h.t.Fatal(err)
		}
	}
}

func TestNotifySignalsEachProjectOnceAndPrintsNothingOnStdout(t *testing.T) {
	h := newHarness(t)
	p := twoProjects(h)
	exitCodeIs(t, h.run("notify", "reboot-warning", "--minutes", "5"), 0, h)
	if h.count(cli(p.a)+" signal reboot-warning --minutes 5 --all-sessions --project-root "+p.a) != 1 ||
		h.count(cli(p.b)+" signal reboot-warning --minutes 5 --all-sessions --project-root "+p.b) != 1 {
		t.Fatalf("each project once: %v", h.ran())
	}
	if h.stdout() != "" || h.count("systemctl") != 0 {
		t.Fatalf("stdout %q ran %v", h.stdout(), h.ran())
	}
}

func TestNotifyCancelledNeedsNoMinutes(t *testing.T) {
	h := newHarness(t)
	p := twoProjects(h)
	exitCodeIs(t, h.run("notify", "reboot-cancelled"), 0, h)
	if h.count(cli(p.a)+" signal reboot-cancelled --all-sessions --project-root "+p.a) != 1 ||
		h.count(cli(p.b)+" signal reboot-cancelled --all-sessions --project-root "+p.b) != 1 {
		t.Fatalf("each project once: %v", h.ran())
	}
}

func TestNotifyUsageMistakesAreExit64WithTheirSentence(t *testing.T) {
	cases := []struct {
		args []string
		msg  string
	}{
		{[]string{"notify"}, "'notify' needs a signal kind: going-down, reboot-warning, shutdown-warning or reboot-cancelled."},
		{[]string{"notify", "shutdown-soon", "--minutes", "5"}, "unknown signal kind 'shutdown-soon'."},
		{[]string{"notify", "reboot-warning"}, "'notify reboot-warning' needs --minutes N."},
		{[]string{"notify", "reboot-warning", "--in", "5"}, "'notify reboot-warning' needs --minutes N."},
		{[]string{"notify", "going-down", "--minutes"}, "'notify going-down' needs --minutes N."},
		{[]string{"notify", "reboot-warning", "--minutes", "0"}, "--minutes needs a whole number of minutes, at least 1; got '0'."},
		{[]string{"notify", "reboot-warning", "--minutes", "soon"}, "--minutes needs a whole number of minutes, at least 1; got 'soon'."},
		{[]string{"notify", "reboot-warning", "--minutes", "99999999999999999999"}, "--minutes needs a whole number of minutes, at least 1; got '99999999999999999999'."},
		{[]string{"notify", "reboot-warning", "--minutes", "5", "--dry"}, "unexpected argument '--dry'."},
		{[]string{"notify", "reboot-warning", "--minutes", "5", "--dry-run", "x"}, "unexpected argument 'x'."},
		{[]string{"notify", "reboot-warning", "--minutes", "5", "--dry", "--dry-run"}, "unexpected argument '--dry'."},
		{[]string{"notify", "reboot-cancelled", "--dry-run"}, "'notify reboot-cancelled' takes no further arguments."},
	}
	for _, c := range cases {
		h := newHarness(t)
		exitCodeIs(t, h.run(c.args...), 64, h)
		want := "ccy-sessions: " + c.msg + " See ccy-sessions --help.\n"
		if h.stderr() != want {
			t.Fatalf("%v:\nwant %q\ngot  %q", c.args, want, h.stderr())
		}
		if len(h.ran()) != 0 {
			t.Fatalf("%v: nothing runs after a usage error: %v", c.args, h.ran())
		}
	}
}

func TestGoingDownFollowsTheRestoreOptIn(t *testing.T) {
	h := newHarness(t)
	p := twoProjects(h)
	restoreOn(h, true)
	exitCodeIs(t, h.run("notify", "going-down", "--minutes", "5"), 0, h)
	if h.count(cli(p.a)+" signal reboot-warning --minutes 5") != 1 {
		t.Fatalf("restore on: reboot-warning: %v", h.ran())
	}

	h = newHarness(t)
	p = twoProjects(h)
	exitCodeIs(t, h.run("notify", "going-down", "--minutes", "5"), 0, h)
	if h.count(cli(p.a)+" signal shutdown-warning --minutes 5") != 1 {
		t.Fatalf("restore off: shutdown-warning: %v", h.ran())
	}

	h = newHarness(t)
	p = twoProjects(h)
	restoreOn(h, false) // a dangling wants link pulls in nothing at boot
	exitCodeIs(t, h.run("notify", "going-down", "--minutes", "5"), 0, h)
	if h.count(cli(p.a)+" signal shutdown-warning --minutes 5") != 1 {
		t.Fatalf("dangling link is restore off: %v", h.ran())
	}
}

func TestNotifyDryRunStillChecksButSignalsNothing(t *testing.T) {
	h := newHarness(t)
	p := twoProjects(h)
	exitCodeIs(t, h.run("notify", "going-down", "--minutes", "5", "--dry-run"), 0, h)
	contains(t, "dry run", h.stderr(), "would signal shutdown-warning --minutes 5 to "+p.a)
	if h.count(cli(p.a)+" signal --help") != 1 || h.count(cli(p.a)+" signal shutdown") != 0 {
		t.Fatalf("the dry run probes but does not signal: %v", h.ran())
	}
}

func TestNotifyWithNoSessionsSucceedsAndSaysSo(t *testing.T) {
	h := newHarness(t)
	h.sessions()
	exitCodeIs(t, h.run("notify", "reboot-warning", "--minutes", "5"), 0, h)
	contains(t, "empty", h.stderr(), "No CCY or cc sessions")
}

func TestNotifyRefusesAProjectWithNoDaemonCLI(t *testing.T) {
	h := newHarness(t)
	a, none := h.project("a", true), h.project("none", false)
	h.okCLI(a)
	h.sessions("cc-a 1 "+a, "cc-x 0 "+none)
	h.tmuxOut("list-panes", "")
	h.fake.On([]string{"ps"}, execx.Result{})
	exitCodeIs(t, h.run("notify", "reboot-warning", "--minutes", "5"), 1, h)
	contains(t, "refusal", h.stderr(), none)
	if h.count(cli(a)+" signal reboot") != 0 {
		t.Fatal("not even the project with a CLI is signalled")
	}
}

func TestNotifyRefusesWhenTheListCannotBeReadEvenOnADryRun(t *testing.T) {
	for _, args := range [][]string{
		{"notify", "reboot-warning", "--minutes", "5"},
		{"notify", "going-down", "--minutes", "2", "--dry-run"},
	} {
		h := newHarness(t)
		h.tmuxFail("list-sessions", "lost server")
		exitCodeIs(t, h.run(args...), 1, h)
	}
}
