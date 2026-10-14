package businstall

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func installedWithRule(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.software()
	h.sim.routeDev = "wg0"
	file := h.teamFile("alpha", []string{"192.0.2.10"}, []string{allowA})
	if code := h.run("team", "--team-file", file); code != 0 {
		t.Fatalf("team exit %d:\n%s", code, h.stderr())
	}
	return h
}

func TestRemoveStopsDisablesAndTakesAwayWhatTheInstallerAdded(t *testing.T) {
	h := installedWithRule(t)
	if code := h.run("remove", "--team", "alpha"); code != 0 {
		t.Fatalf("exit %d:\n%s", code, h.stderr())
	}
	if h.sim.lines(`^systemctl disable --now agent-bus-hs@alpha.service agent-bus-backup@alpha.timer$`) != 1 {
		t.Fatalf("log %v", h.sim.log)
	}
	if h.exists("etc/systemd/system/agent-bus-hs@alpha.service.d") {
		t.Error("the drop-in is still there")
	}
	if len(h.sim.fwPerm) != 0 || len(h.sim.fwRun) != 0 || h.exists("var/lib/agent-bus-install/alpha/firewalld.rules") {
		t.Errorf("the rich rule or its record is still there: %v %v", h.sim.fwPerm, h.sim.fwRun)
	}
	if !h.exists("var/lib/agent-bus/alpha/db") {
		t.Error("the data went without --purge")
	}
	if h.exists("var/lib/agent-bus-applied/alpha.json") {
		t.Error("the stamp is still there")
	}
	for _, want := range []string{
		"CHANGED\tstopped and disabled agent-bus-hs@alpha.service and agent-bus-backup@alpha.timer\n",
		"CHANGED\tremoved the agent-bus-hs@alpha.service drop-in\n",
		"CHANGED\tremoved the alpha applied stamp\n",
		"CHANGED\tfirewalld permanent trusted: removed ",
		"CHANGED\tfirewalld runtime trusted: removed ",
	} {
		contains(t, "stdout", h.stdout(), want)
	}
	onlyChangedLines(t, h.stdout())
	if h.sim.lines(`^systemctl daemon-reload$`) != 1 {
		t.Error("systemd was not reloaded for the removed drop-in")
	}
}

func TestRemoveASecondTimeIsSilent(t *testing.T) {
	h := installedWithRule(t)
	h.run("remove", "--team", "alpha")
	if code := h.run("remove", "--team", "alpha"); code != 0 || h.stdout() != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, h.stdout(), h.stderr())
	}
}

func TestRemoveOfATeamThatNeverExistedIsASilentSuccess(t *testing.T) {
	h := newHarness(t)
	if code := h.run("remove", "--team", "ghost", "--purge"); code != 0 || h.stdout() != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, h.stdout(), h.stderr())
	}
	h.software()
	if code := h.run("remove", "--team", "ghost"); code != 0 || h.stdout() != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, h.stdout(), h.stderr())
	}
}

func TestRemovePurgeDeletesTheDataAndTheInstallState(t *testing.T) {
	h := installedWithRule(t)
	if code := h.run("remove", "--team", "alpha", "--purge"); code != 0 {
		t.Fatalf("exit %d:\n%s", code, h.stderr())
	}
	if h.exists("var/lib/agent-bus/alpha") || h.exists("var/lib/agent-bus-install/alpha") {
		t.Error("the data or the install state is still there")
	}
	contains(t, "stdout", h.stdout(), "CHANGED\tpurged /var/lib/agent-bus/alpha\n")
	contains(t, "stdout", h.stdout(), "CHANGED\tpurged /var/lib/agent-bus-install/alpha\n")
	if !h.exists("var/lib/agent-bus") {
		t.Error("the shared state directory went too")
	}
}

func TestRemoveDoesNotFollowASymlinkWhenPurging(t *testing.T) {
	h := installedWithRule(t)
	outside := t.TempDir()
	h.write(outside+"/keep", "precious", 0o600)
	h.run("remove", "--team", "alpha")
	if err := removeAll(h.rel("var/lib/agent-bus/alpha")); err != nil {
		t.Fatal(err)
	}
	if err := symlink(outside, h.rel("var/lib/agent-bus/alpha")); err != nil {
		t.Fatal(err)
	}
	if code := h.run("remove", "--team", "alpha", "--purge"); code != 0 {
		t.Fatalf("exit %d:\n%s", code, h.stderr())
	}
	if !fileExists(outside + "/keep") {
		t.Fatal("purge followed a symlink out of the state directory")
	}
}

func TestRemoveStopsAnActiveTeamEvenWhenNothingIsEnabled(t *testing.T) {
	h := newHarness(t)
	h.sim.unit(hsUnit("alpha")).active = true
	if code := h.run("remove", "--team", "alpha"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if h.sim.lines(`^systemctl disable --now`) != 1 {
		t.Fatalf("log %v", h.sim.log)
	}
	h2 := newHarness(t)
	h2.sim.unit(timerUnit("alpha")).enabled = true
	h2.run("remove", "--team", "alpha")
	if h2.sim.lines(`^systemctl disable --now`) != 1 {
		t.Fatalf("log %v", h2.sim.log)
	}
}

func TestRemoveANonRootCallerIsRefused(t *testing.T) {
	h := newHarness(t)
	h.app.cfg.Prefix = ""
	if code := h.run("remove", "--team", "alpha"); code != term.ExitNotRoot {
		t.Fatalf("exit %d", code)
	}
}

func TestRemoveEveryCommandItRunsPropagatesItsFailure(t *testing.T) {
	probe := installedWithRule(t)
	probe.run("remove", "--team", "alpha")
	seen := map[string]bool{}
	for _, line := range probe.sim.log {
		seen[firstWords(line)] = true
	}
	for prefix := range seen {
		if strings.HasPrefix(prefix, "firewall-cmd") || prefix == "systemctl is-enabled" {
			continue // a refusal is an answer for these
		}
		h := installedWithRule(t)
		h.sim.fails = []failRule{{prefix: prefix, code: 5}}
		if code := h.run("remove", "--team", "alpha"); code != term.ExitFailure {
			t.Errorf("a failing %q ended with exit %d", prefix, code)
		}
	}
	h := installedWithRule(t)
	h.sim.fails = []failRule{{prefix: "firewall-cmd --permanent --zone=trusted --remove-rich-rule", code: 5}}
	if code := h.run("remove", "--team", "alpha"); code != term.ExitFailure {
		t.Errorf("a failing removal ended with exit %d", code)
	}
}
