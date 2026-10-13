package sessions

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const canReboot = "busctl --system call org.freedesktop.login1 /org/freedesktop/login1 org.freedesktop.login1.Manager CanReboot"

// logindSays scripts logind's CanReboot answer.
func logindSays(h *harness, word string) {
	h.fake.On([]string{"busctl"}, execx.Result{Stdout: []byte("s \"" + word + "\"\n")})
}

// systemctlRC scripts how `systemctl reboot` ends.
func systemctlRC(h *harness, code int) {
	h.fake.On([]string{"systemctl"}, execx.Result{ExitCode: code, Stderr: []byte("Failed to reboot\n")})
}

// rebootReady is two projects (h.p), logind saying yes, systemctl succeeding and restore on.
func rebootReady(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.p = twoProjects(h)
	logindSays(h, "yes")
	systemctlRC(h, 0)
	restoreOn(h, true)
	return h
}

// failWhen makes a project's CLI refuse any call whose arguments contain the text.
func failWhen(h *harness, dir, text string) {
	h.onCLI(dir, func(c execx.Cmd) execx.Result {
		if strings.Contains(strings.Join(c.Args, " "), text) {
			return execx.Result{ExitCode: 1}
		}
		return execx.Result{}
	})
}

func TestRebootWarnsCountsDownWarnsAtOneMinuteThenRebootsLast(t *testing.T) {
	h := rebootReady(t)
	p := h.p
	exitCodeIs(t, h.run("reboot", "--in", "3"), 0, h)
	for _, dir := range []string{p.a, p.b} {
		full := cli(dir) + " signal reboot-warning --minutes 3 --all-sessions --project-root " + dir
		one := cli(dir) + " signal reboot-warning --minutes 1 --all-sessions --project-root " + dir
		if h.count(full) != 1 || h.count(one) != 1 {
			t.Fatalf("each project is warned at 3 and at 1: %v", h.ran())
		}
	}
	ran := h.ran()
	if ran[0] != canReboot {
		t.Fatalf("logind is asked before anything else: %v", ran[0])
	}
	if ran[len(ran)-1] != "systemctl reboot" || h.count("systemctl reboot") != 1 {
		t.Fatalf("systemctl reboot is last, once: %v", ran)
	}
	if h.count(cli(p.a)+" signal reboot-cancelled") != 0 {
		t.Fatal("a reboot that went ahead withdraws nothing")
	}
	if len(h.pauses) != 2 || h.pauses[0] != 2*time.Minute || h.pauses[1] != time.Minute {
		t.Fatalf("pauses %v", h.pauses)
	}
	contains(t, "progress", h.stderr(), "Rebooting in 3 minute(s); 2 project(s) warned.\n", "Rebooting in 1 minute.\n")
	if h.stdout() != "" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func TestRebootInOneMinuteWarnsOnceBecauseThatWarningIsTheOneMinuteWarning(t *testing.T) {
	h := rebootReady(t)
	exitCodeIs(t, h.run("reboot", "--in", "1"), 0, h)
	if h.count(cli(h.p.a)+" signal reboot-warning --minutes 1 ") != 1 || h.count("systemctl reboot") != 1 {
		t.Fatalf("%v", h.ran())
	}
	lacks(t, "one minute", h.stderr(), "Rebooting in 1 minute.\n")
	if len(h.pauses) != 1 {
		t.Fatalf("pauses %v", h.pauses)
	}
}

func TestRebootWithRestoreOffWarnsShutdownBothTimes(t *testing.T) {
	h := newHarness(t)
	p := twoProjects(h)
	logindSays(h, "yes")
	systemctlRC(h, 0)
	exitCodeIs(t, h.run("reboot", "--in", "3"), 0, h)
	if h.count(cli(p.a)+" signal shutdown-warning --minutes ") != 2 || h.count(cli(p.a)+" signal reboot-warning") != 0 {
		t.Fatalf("%v", h.ran())
	}
}

func TestRebootDryRunChecksEverythingAndDoesNothing(t *testing.T) {
	h := rebootReady(t)
	p := h.p
	exitCodeIs(t, h.run("reboot", "--in", "3", "--dry-run"), 0, h)
	if h.count("systemctl") != 0 || h.count(cli(p.a)+" signal reboot") != 0 {
		t.Fatalf("%v", h.ran())
	}
	if h.count(cli(p.a)+" signal --help") != 1 || h.count("busctl") != 1 {
		t.Fatalf("a dry run asks logind and probes the CLIs: %v", h.ran())
	}
	contains(t, "dry run", h.stderr(), "would signal reboot-warning --minutes 3 to "+p.a,
		"would signal reboot-warning --minutes 3 to "+p.b, "would wait 2 minute(s), then warn again at one minute\n",
		"would reboot (systemctl reboot)\n")
	if len(h.pauses) != 0 {
		t.Fatalf("a dry run waits for nothing: %v", h.pauses)
	}
}

func TestRebootUsageMistakes(t *testing.T) {
	cases := []struct {
		args []string
		msg  string
	}{
		{[]string{"reboot"}, "'reboot' needs --in N."},
		{[]string{"reboot", "--dry-run"}, "'reboot' needs --in N."},
		{[]string{"reboot", "--in"}, "--in needs a number of minutes."},
		{[]string{"reboot", "--in", ""}, "--in needs a number of minutes."},
		{[]string{"reboot", "--in", "0"}, "--in needs a whole number of minutes, at least 1; got '0'."},
		{[]string{"reboot", "--in", "x"}, "--in needs a whole number of minutes, at least 1; got 'x'."},
		{[]string{"reboot", "--bogus"}, "unknown reboot option '--bogus'."},
	}
	for _, c := range cases {
		h := newHarness(t)
		exitCodeIs(t, h.run(c.args...), 64, h)
		if want := "ccy-sessions: " + c.msg + " See ccy-sessions --help.\n"; h.stderr() != want {
			t.Fatalf("%v: stderr %q", c.args, h.stderr())
		}
		if len(h.ran()) != 0 {
			t.Fatalf("%v: nothing runs: %v", c.args, h.ran())
		}
	}
}

func TestRebootIsRefusedUpFrontUnlessLogindSaysYes(t *testing.T) {
	for _, word := range []string{"challenge", "no", "na"} {
		h := newHarness(t)
		p := twoProjects(h)
		logindSays(h, word)
		systemctlRC(h, 0)
		exitCodeIs(t, h.run("reboot", "--in", "2"), 1, h)
		contains(t, word, h.stderr(), "CanReboot: "+word, "sudo /usr/local/bin/reboot-with-update --in N",
			"Nothing was signalled and nothing rebooted.")
		lacks(t, word, h.stderr(), "staying up", "cancelled")
		if h.count(cli(p.a)+" signal reboot") != 0 || h.count("systemctl") != 0 {
			t.Fatalf("%s: nothing is signalled or rebooted: %v", word, h.ran())
		}
	}
	h := newHarness(t)
	twoProjects(h)
	logindSays(h, "challenge")
	exitCodeIs(t, h.run("reboot", "--in", "2", "--dry-run"), 1, h)
	lacks(t, "dry run", h.stderr(), "would reboot")
}

func TestRebootRefusesWhenLogindCannotBeAsked(t *testing.T) {
	h := newHarness(t)
	twoProjects(h)
	h.fake.On([]string{"busctl"}, execx.Result{ExitCode: 1, Stderr: []byte("Failed to connect to bus: No such file or directory\n")})
	exitCodeIs(t, h.run("reboot", "--in", "2"), 1, h)
	contains(t, "logind", h.stderr(), "ERROR: could not ask logind whether this account may reboot: Failed to connect to bus")

	h = newHarness(t)
	twoProjects(h)
	h.fake.OnError([]string{"busctl"}, &execx.NotInstalledError{Name: "busctl"})
	exitCodeIs(t, h.run("reboot", "--in", "2"), 1, h)
	contains(t, "no busctl", h.stderr(), "could not ask logind whether this account may reboot: busctl is not installed")
}

func TestRebootNamesEverySessionThatCannotBeWarnedAlongsideAPermissionRefusal(t *testing.T) {
	h := newHarness(t)
	a, none := h.project("a", true), h.project("none", false)
	h.okCLI(a)
	h.sessions("cc-a 1 "+a, "cc-x 0 "+none)
	h.tmuxOut("list-panes", "")
	h.fake.On([]string{"ps"}, execx.Result{})
	logindSays(h, "challenge")
	exitCodeIs(t, h.run("reboot", "--in", "2"), 1, h)
	contains(t, "both", h.stderr(), "CanReboot: challenge", "  cc-x  in "+none, "Ctrl-X", "Nothing was signalled and nothing rebooted.")
	lacks(t, "both", h.stderr(), "  cc-a ")
}

func TestRebootRefusesWhenTheSessionListCannotBeRead(t *testing.T) {
	h := newHarness(t)
	h.tmuxFail("list-sessions", "lost server")
	logindSays(h, "yes")
	systemctlRC(h, 0)
	exitCodeIs(t, h.run("reboot", "--in", "1"), 1, h)
	contains(t, "list", h.stderr(), "could not be read")
	if h.count("systemctl") != 0 {
		t.Fatal("reboots nothing")
	}
}

func TestAFirstWarningThatFailsPartWayIsWithdrawnFromTheProjectAlreadyWarned(t *testing.T) {
	h := rebootReady(t)
	p := h.p
	failWhen(h, p.b, "reboot-warning --minutes 3")
	exitCodeIs(t, h.run("reboot", "--in", "3"), 1, h)
	if h.count(cli(p.a)+" signal reboot-cancelled --all-sessions --project-root "+p.a) != 1 {
		t.Fatalf("project a is told the warning is off: %v", h.ran())
	}
	if h.count("systemctl") != 0 {
		t.Fatal("reboots nothing")
	}
	contains(t, "withdraw", h.stderr(), "The machine is staying up; telling the sessions the reboot is off.")
}

func TestAWithdrawalThatFailsAtOneProjectStillReachesTheNext(t *testing.T) {
	h := rebootReady(t)
	p := h.p
	failWhen(h, p.b, "reboot-warning --minutes 3")
	failWhen(h, p.a, "reboot-cancelled")
	exitCodeIs(t, h.run("reboot", "--in", "3"), 1, h)
	if h.count(cli(p.b)+" signal reboot-cancelled --all-sessions --project-root "+p.b) != 1 {
		t.Fatalf("%v", h.ran())
	}
	contains(t, "partial", h.stderr(), "some still expect the reboot")
}

func TestARefusedSystemctlExitsWithItsStatusAndWithdrawsWithoutCallingItCancelled(t *testing.T) {
	h := rebootReady(t)
	p := h.p
	systemctlRC(h, 3)
	exitCodeIs(t, h.run("reboot", "--in", "1"), 3, h)
	if h.count(cli(p.a)+" signal reboot-cancelled") != 1 || h.count(cli(p.b)+" signal reboot-cancelled") != 1 {
		t.Fatalf("every warned project is told: %v", h.ran())
	}
	contains(t, "failed", h.stderr(), "ERROR: the reboot failed: systemctl reboot exited 3 (its reason is above). The machine is staying up.",
		"Telling the sessions the warned reboot is not happening.")
	lacks(t, "failed", h.stderr(), "Reboot cancelled", "reboot is off")
}

func TestASystemctlThatCannotStartIsAFailedReboot(t *testing.T) {
	h := rebootReady(t)
	h.fake.OnError([]string{"systemctl"}, &execx.NotInstalledError{Name: "systemctl"})
	exitCodeIs(t, h.run("reboot", "--in", "1"), 1, h)
	contains(t, "no systemctl", h.stderr(), "the reboot failed: systemctl is not installed. The machine is staying up.")
	if h.count(cli(h.p.a)+" signal reboot-cancelled") != 1 {
		t.Fatalf("%v", h.ran())
	}
}

func TestAFailedOneMinuteWarningWithdrawsTheFirst(t *testing.T) {
	h := rebootReady(t)
	p := h.p
	failWhen(h, p.a, "--minutes 1 ")
	exitCodeIs(t, h.run("reboot", "--in", "2"), 1, h)
	if h.count("systemctl") != 0 || h.count(cli(p.b)+" signal reboot-cancelled") != 1 {
		t.Fatalf("%v", h.ran())
	}
}

func TestRebootWithNoSessionsStillReboots(t *testing.T) {
	h := newHarness(t)
	h.sessions()
	logindSays(h, "yes")
	systemctlRC(h, 0)
	exitCodeIs(t, h.run("reboot", "--in", "2"), 0, h)
	contains(t, "empty", h.stderr(), "Rebooting in 2 minute(s); 0 project(s) warned.")
	if h.count("systemctl reboot") != 1 || h.count("busctl") != 1 {
		t.Fatalf("%v", h.ran())
	}
}

func TestRebootReachesAContainerProjectInsideItsContainer(t *testing.T) {
	h := newHarness(t)
	ccyOnly := h.project("ccy-only", true)
	h.sessions("ccy-c 0 " + ccyOnly)
	containerRoute(h)
	h.fake.On([]string{"podman", "exec"}, execx.Result{})
	logindSays(h, "yes")
	systemctlRC(h, 0)
	restoreOn(h, true)
	exitCodeIs(t, h.run("reboot", "--in", "2"), 0, h)
	inCt := "podman exec c_yolo /workspace/.claude/hooks-daemon/bin/hooks-daemon"
	if h.count(inCt+" signal reboot-warning --minutes 2 --all-sessions --project-root /workspace") != 1 ||
		h.count(inCt+" signal reboot-warning --minutes 1 --all-sessions --project-root /workspace") != 1 {
		t.Fatalf("%v", h.ran())
	}
	contains(t, "via", h.stderr(), "(in container c_yolo)")
}

func TestATermDuringTheCountdownCancelsWithdrawsAndExits130(t *testing.T) {
	h := rebootReady(t)
	p := h.p
	h.interruptAt = 1
	exitCodeIs(t, h.run("reboot", "--in", "3"), 130, h)
	contains(t, "cancel", h.stderr(), "\nReboot cancelled.\n", "The machine is staying up; telling the sessions the reboot is off.")
	if h.count(cli(p.a)+" signal reboot-cancelled") != 1 || h.count(cli(p.b)+" signal reboot-cancelled") != 1 {
		t.Fatalf("every warned project is told: %v", h.ran())
	}
	if h.count("systemctl") != 0 {
		t.Fatal("nothing reboots")
	}
}

func TestATermDuringTheFinalMinuteCancelsToo(t *testing.T) {
	h := rebootReady(t)
	h.interruptAt = 1
	exitCodeIs(t, h.run("reboot", "--in", "1"), 130, h)
	if h.count("systemctl") != 0 {
		t.Fatal("nothing reboots")
	}
}

func TestATermThatArrivesWhileAProjectIsBeingSignalledTakesEffectAfterIt(t *testing.T) {
	h := rebootReady(t)
	h.onCLI(h.p.a, func(execx.Cmd) execx.Result {
		h.sendSignal()
		return execx.Result{}
	})
	exitCodeIs(t, h.run("reboot", "--in", "3"), 130, h)
	if h.count("systemctl") != 0 || len(h.pauses) != 0 {
		t.Fatalf("%v %v", h.ran(), h.pauses)
	}
}

func TestATermDuringTheOneMinuteWarningCancelsAfterIt(t *testing.T) {
	h := rebootReady(t)
	warnings := 0
	h.onCLI(h.p.a, func(c execx.Cmd) execx.Result {
		if strings.Contains(strings.Join(c.Args, " "), "reboot-warning") {
			warnings++
			if warnings == 2 {
				h.sendSignal()
			}
		}
		return execx.Result{}
	})
	exitCodeIs(t, h.run("reboot", "--in", "3"), 130, h)
	if h.count("systemctl") != 0 {
		t.Fatal("nothing reboots")
	}
}

func TestATermWhileSystemctlRunsIsTheShutdownNotACancellation(t *testing.T) {
	h := rebootReady(t)
	h.fake.OnFunc([]string{"systemctl"}, func(execx.Cmd) (execx.Result, error) {
		h.sendSignal()
		return execx.Result{}, nil
	})
	exitCodeIs(t, h.run("reboot", "--in", "1"), 0, h)
	lacks(t, "term", h.stderr(), "Reboot cancelled")
	if h.count("reboot-cancelled") != 0 {
		t.Fatal("a shutdown reaching the process withdraws nothing")
	}
}

func TestADryRunDoesNotListenForSignals(t *testing.T) {
	h := rebootReady(t)
	listened := false
	h.app.Signals = func() (<-chan os.Signal, func()) {
		listened = true
		return nil, func() {}
	}
	exitCodeIs(t, h.run("reboot", "--in", "2", "--dry-run"), 0, h)
	if listened {
		t.Fatal("a dry run arms no handlers")
	}
}
