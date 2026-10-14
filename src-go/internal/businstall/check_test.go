package businstall

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func installedForCheck(t *testing.T) *harness {
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

func TestCheckPrintsTheFactsAndChangesNothing(t *testing.T) {
	h := installedForCheck(t)
	if code := h.run("check", "--team", "alpha"); code != 0 {
		t.Fatalf("exit %d:\n%s", code, h.stderr())
	}
	for _, want := range []string{
		"CHECK\tActiveState\tok\tactive\n",
		"CHECK\tUnitFileState\tok\tenabled\n",
		"CHECK\tUser\tinfo\tstub-User\n",
		"CHECK\tIPAddressAllow\tinfo\tstub-IPAddressAllow\n",
		"CHECK\tSocketBindDeny\tinfo\tany any\n",
		"CHECK\tdrop-in\tok\t/etc/systemd/system/agent-bus-hs@alpha.service.d/network.conf\n",
		"CHECK\tserver_name\tok\talpha.agent-bus.internal\n",
		"CHECK\tlisten\tok\tport 8448 bound on: 127.0.0.1 192.0.2.10\n",
		"CHECK\tfirewalld\tok\t1 rule(s) recorded, 0 missing from the permanent configuration\n",
	} {
		contains(t, "stdout", h.stdout(), want)
	}
	lines := strings.Split(strings.TrimRight(h.stdout(), "\n"), "\n")
	order := []string{
		"ActiveState", "UnitFileState", "User", "IPAddressDeny", "IPAddressAllow", "RestrictAddressFamilies",
		"IPAccounting", "SocketBindDeny", "SocketBindAllow", "drop-in", "server_name", "listen", "firewalld", "binary",
	}
	if len(lines) != len(order) {
		t.Fatalf("lines %q", lines)
	}
	for i, line := range lines {
		if !strings.HasPrefix(line, "CHECK\t"+order[i]+"\t") {
			t.Errorf("line %d is %q, want fact %s", i, line, order[i])
		}
	}
	if !strings.Contains(lines[len(lines)-1], "tuwunel-1.9.3") || !strings.HasPrefix(lines[len(lines)-1], "CHECK\tbinary\tinfo\t") {
		t.Errorf("binary line %q", lines[len(lines)-1])
	}
	if n := h.sim.lines(`^(systemctl (start|restart|stop|enable|disable|daemon-reload)|firewall-cmd .*--(add|remove)-)`); n != 0 {
		t.Fatalf("check changed something: %v", h.sim.log)
	}
	if h.stderr() != "" {
		t.Fatalf("stderr %q", h.stderr())
	}
}

func TestCheckAFailingFactExitsOneAfterPrintingEveryFact(t *testing.T) {
	h := installedForCheck(t)
	h.sim.fwPerm = map[string]bool{}
	code := h.run("check", "--team", "alpha")
	if code != term.ExitFailure || h.stderr() != "" {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	contains(t, "stdout", h.stdout(), "CHECK\tfirewalld\tFAIL\t1 rule(s) recorded, 1 missing from the permanent configuration\n")
	contains(t, "stdout", h.stdout(), "CHECK\tbinary\tinfo\t")
}

func TestCheckOtherFailingFacts(t *testing.T) {
	h := installedForCheck(t)
	h.sim.units[hsUnit("alpha")].active = false
	h.sim.units[hsUnit("alpha")].enabled = false
	if err := removeAll(h.rel("etc/systemd/system/agent-bus-hs@alpha.service.d")); err != nil {
		t.Fatal(err)
	}
	h.write(h.rel("var/lib/agent-bus/alpha/team.json"), `{"team":"alpha","port":8448,"server_name":"alpha.example.com","listen":["192.0.2.10"]}`, 0o640)
	h.sim.ssAddrs = []string{"127.0.0.1"}
	if code := h.run("check", "--team", "alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{
		"CHECK\tActiveState\tFAIL\tinactive\n",
		"CHECK\tUnitFileState\tFAIL\tdisabled\n",
		"CHECK\tdrop-in\tFAIL\t/etc/systemd/system/agent-bus-hs@alpha.service.d/network.conf\n",
		"CHECK\tserver_name\tFAIL\talpha.example.com\n",
		"CHECK\tlisten\tFAIL\tport 8448 bound on: \n",
	} {
		contains(t, "stdout", h.stdout(), want)
	}
}

func TestCheckAServerNameWithoutADotIsCheckedWhole(t *testing.T) {
	h := installedForCheck(t)
	h.write(h.rel("var/lib/agent-bus/alpha/team.json"), `{"team":"alpha","port":8448,"server_name":"internal","listen":["192.0.2.10"]}`, 0o640)
	h.run("check", "--team", "alpha")
	contains(t, "stdout", h.stdout(), "CHECK\tserver_name\tok\tinternal\n")
}

func TestCheckWithNoFirewallRecordCountsNoRules(t *testing.T) {
	h := installedForCheck(t)
	if err := removeAll(h.rel("var/lib/agent-bus-install/alpha/firewalld.rules")); err != nil {
		t.Fatal(err)
	}
	h.run("check", "--team", "alpha")
	contains(t, "stdout", h.stdout(), "CHECK\tfirewalld\tok\t0 rule(s) recorded, 0 missing from the permanent configuration\n")
}

func TestCheckRefusesAnUnknownTeam(t *testing.T) {
	h := newHarness(t)
	code := h.run("check", "--team", "alpha")
	if code != term.ExitConfig || h.stdout() != "" {
		t.Fatalf("exit %d, stdout %q", code, h.stdout())
	}
}

func TestCheckEveryCommandItRunsPropagatesItsFailure(t *testing.T) {
	probe := installedForCheck(t)
	probe.run("check", "--team", "alpha")
	seen := map[string]bool{}
	for _, line := range probe.sim.log {
		seen[firstWords(line)] = true
	}
	for prefix := range seen {
		if strings.HasPrefix(prefix, "firewall-cmd") {
			continue // a refusal is an answer: the rule is missing
		}
		h := installedForCheck(t)
		h.sim.fails = []failRule{{prefix: prefix, code: 5}}
		if code := h.run("check", "--team", "alpha"); code == 0 {
			t.Errorf("a failing %q did not fail the run", prefix)
		}
	}
}
