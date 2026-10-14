package businstall

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const allowA = "198.51.100.0/24"

func TestTeamFirstInstall(t *testing.T) {
	h := newHarness(t)
	h.software()
	h.sim.routeDev = "wg0"
	file := h.teamFile("alpha", []string{"192.0.2.10"}, []string{allowA})
	if code := h.run("team", "--team-file", file); code != 0 {
		t.Fatalf("exit %d:\n%s", code, h.stderr())
	}
	team := "var/lib/agent-bus/alpha"
	for path, mode := range map[string]os.FileMode{
		team: 0o700, team + "/db": 0o700, team + "/backups": 0o700, team + "/secrets": 0o700,
		team + "/tuwunel.toml": 0o640, team + "/team.json": 0o640,
		team + "/secrets/registration_shared_secret": 0o600,
		"var/lib/agent-bus-install/alpha":            0o700,
		"var/lib/agent-bus-install/alpha/binary":     0o600, "var/lib/agent-bus-install/alpha/unit-sha256": 0o600,
		"var/lib/agent-bus-install/alpha/firewalld.rules":              0o600,
		"etc/systemd/system/agent-bus-hs@alpha.service.d":              0o755,
		"etc/systemd/system/agent-bus-hs@alpha.service.d/network.conf": 0o644,
		"var/lib/agent-bus-applied":                                    0o755, "var/lib/agent-bus-applied/alpha.json": 0o644,
	} {
		if got := h.mode(path); got != mode {
			t.Errorf("%s: mode %o, want %o", path, got, mode)
		}
	}
	secret := h.read(team + "/secrets/registration_shared_secret")
	if !regexp.MustCompile(`^[0-9a-f]{128}$`).MatchString(secret) {
		t.Errorf("the shared secret is %q", secret)
	}
	if strings.Contains(h.stdout()+h.stderr(), secret) {
		t.Error("the secret reached stdout or stderr")
	}
	if strings.Contains(h.read("var/lib/agent-bus-applied/alpha.json"), secret) {
		t.Error("the secret is in the stamp")
	}
	contains(t, "tuwunel.toml", h.read(team+"/tuwunel.toml"), `"team":"alpha"`)
	contains(t, "drop-in", h.read("etc/systemd/system/agent-bus-hs@alpha.service.d/network.conf"), "--interface=192.0.2.10=agentbus0")
	resolved := h.read("var/lib/agent-bus-install/alpha/binary")
	if resolved != filepath.Join(evalDir(t, h.rel("usr/local/lib/agent-bus")), "tuwunel-1.9.3")+"\n" {
		t.Errorf("the binary record is %q", resolved)
	}
	sum := sha256Hex([]byte(hsUnitText)) + "\n"
	if h.read("var/lib/agent-bus-install/alpha/unit-sha256") != sum {
		t.Errorf("the unit record is %q, want %q", h.read("var/lib/agent-bus-install/alpha/unit-sha256"), sum)
	}
	wantRule := `rule family="ipv4" source address="198.51.100.0/24" port port="8448" protocol="tcp" accept`
	if h.read("var/lib/agent-bus-install/alpha/firewalld.rules") != "trusted\t"+wantRule+"\n" {
		t.Errorf("the record is %q", h.read("var/lib/agent-bus-install/alpha/firewalld.rules"))
	}
	for name, want := range map[string]string{
		"permanent rule": `^firewall-cmd --permanent --zone=trusted --add-rich-rule=` + regexp.QuoteMeta(wantRule) + `$`,
		"runtime rule":   `^firewall-cmd --zone=trusted --add-rich-rule=` + regexp.QuoteMeta(wantRule) + `$`,
		"enable hs":      `^systemctl enable agent-bus-hs@alpha.service$`,
		"enable timer":   `^systemctl enable agent-bus-backup@alpha.timer$`,
		"start hs":       `^systemctl start agent-bus-hs@alpha.service$`,
		"start timer":    `^systemctl start agent-bus-backup@alpha.timer$`,
		"readiness":      `^curl .* http://127.0.0.1:8448/_tuwunel/server_version$`,
		"bootstrap":      `^agent-bus bootstrap alpha$`,
		"reload":         `^systemctl daemon-reload$`,
		"stamp":          `^agent-bus render stamp --zipapp .*/usr/local/lib/agent-bus/agent-bus.pyz --tuwunel 1.9.3 --installer-sha256 ` + h.digest + `$`,
		"check":          `^agent-bus render check$`,
		"toml":           `^agent-bus render toml$`,
		"dropin":         `^agent-bus render dropin --interface=192.0.2.10=agentbus0$`,
	} {
		if h.sim.lines(want) != 1 {
			t.Errorf("%s: want one line matching %s in %q", name, want, h.sim.log)
		}
	}
	if h.sim.lines(`^firewall-cmd .*--add-rich-rule`) != 2 {
		t.Error("more was opened than the one rule")
	}
	onlyChangedLines(t, h.stdout())
	contains(t, "stdout", h.stdout(), "CHANGED\tteam alpha applied stamp\n")
	contains(t, "stdout", h.stdout(), "CHANGED\tregistration shared secret created\n")
	contains(t, "stdout", h.stdout(), "CHANGED\tstarted agent-bus-hs@alpha.service\n")
}

func evalDir(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestTeamNothingChangedIsQuiet(t *testing.T) {
	h := newHarness(t)
	file := h.installTeam([]string{allowA})
	secret := h.read("var/lib/agent-bus/alpha/secrets/registration_shared_secret")
	if code := h.run("team", "--team-file", file); code != 0 {
		t.Fatalf("exit %d:\n%s", code, h.stderr())
	}
	if h.stdout() != "" {
		t.Fatalf("a second run printed %q", h.stdout())
	}
	if h.sim.lines(`^systemctl (start|restart|stop|daemon-reload)`) != 0 || h.sim.lines(`^firewall-cmd .*--(add|remove)-rich-rule`) != 0 {
		t.Fatalf("a second run changed something: %v", h.sim.log)
	}
	if h.read("var/lib/agent-bus/alpha/secrets/registration_shared_secret") != secret {
		t.Fatal("the secret changed")
	}
	if h.sim.lines(`_tuwunel/server_version`) != 1 {
		t.Fatal("readiness was not checked")
	}
}

func TestTeamPassesBootstrapsChangedLinesThrough(t *testing.T) {
	h := newHarness(t)
	file := h.installTeam(nil)
	h.sim.bootstrap = "CHANGED\tbootstrap stub\n"
	h.run("team", "--team-file", file)
	if h.stdout() != "CHANGED\tbootstrap stub\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func TestTeamStartsAStoppedHomeserver(t *testing.T) {
	h := newHarness(t)
	file := h.installTeam(nil)
	h.sim.units[hsUnit("alpha")].active = false
	h.run("team", "--team-file", file)
	if h.sim.lines(`^systemctl start agent-bus-hs@alpha.service$`) != 1 || h.sim.lines(`^systemctl restart`) != 0 {
		t.Fatalf("log %v", h.sim.log)
	}
	if h.stdout() != "CHANGED\tstarted agent-bus-hs@alpha.service\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func TestTeamAChangedAllowFromRestartsOnceAndReplacesExactlyTheOldRule(t *testing.T) {
	h := newHarness(t)
	h.software()
	h.sim.routeDev = "wg0"
	file := h.teamFile("alpha", []string{"192.0.2.10"}, []string{allowA})
	h.run("team", "--team-file", file)
	file = h.teamFile("alpha", []string{"192.0.2.10"}, []string{"203.0.113.0/24"})
	if code := h.run("team", "--team-file", file); code != 0 {
		t.Fatalf("exit %d:\n%s", code, h.stderr())
	}
	if h.sim.lines(`^systemctl restart agent-bus-hs@alpha.service$`) != 1 || h.sim.lines(`^systemctl daemon-reload$`) != 1 {
		t.Fatalf("log %v", h.sim.log)
	}
	if len(h.sim.fwPerm) != 1 || len(h.sim.fwRun) != 1 {
		t.Fatalf("permanent %v runtime %v", h.sim.fwPerm, h.sim.fwRun)
	}
	for key := range h.sim.fwPerm {
		contains(t, "permanent rule", key, "203.0.113.0/24")
	}
}

func TestTeamANewTuwunelRestartsTheHomeserverOnce(t *testing.T) {
	h := newHarness(t)
	file := h.installTeam(nil)
	h.writePin("1.9.4", sha256Hex(h.asset))
	h.software()
	h.run("team", "--team-file", file)
	if h.sim.lines(`^systemctl restart agent-bus-hs@alpha.service$`) != 1 {
		t.Fatalf("log %v", h.sim.log)
	}
	h.run("team", "--team-file", file)
	if h.sim.lines(`^systemctl (start|restart)`) != 0 {
		t.Fatalf("a run after that restarted again: %v", h.sim.log)
	}
}

func TestTeamAChangedBaseUnitRestartsOnTheNextTeamRunNotInSoftware(t *testing.T) {
	h := newHarness(t)
	file := h.installTeam(nil)
	h.write(filepath.Join(h.source, "files/etc/systemd/system/agent-bus-hs@.service"), hsUnitText+"# fix\n", 0o644)
	h.run("software", "--source", h.source)
	if h.sim.lines(`^systemctl (restart|start|stop)`) != 0 {
		t.Fatalf("software restarted something: %v", h.sim.log)
	}
	h.run("team", "--team-file", file)
	if h.sim.lines(`^systemctl restart agent-bus-hs@alpha.service$`) != 1 {
		t.Fatalf("log %v", h.sim.log)
	}
	h.run("team", "--team-file", file)
	if h.sim.lines(`^systemctl (start|restart)`) != 0 {
		t.Fatalf("log %v", h.sim.log)
	}
}

func TestTeamAChangedTuwunelTomlRestartsButTeamJSONAloneDoesNot(t *testing.T) {
	h := newHarness(t)
	h.installTeam(nil)
	// A different humans/ignored field would change only team.json in the real tool; the
	// simulated renderer puts the team file into tuwunel.toml, so change team.json by hand.
	h.write(h.rel("var/lib/agent-bus/alpha/team.json"), `{"team":"alpha","port":8448,"state":"present","listen":["192.0.2.10"],"x":1}`, 0o640)
	file := h.teamFile("alpha", []string{"192.0.2.10"}, nil)
	h.run("team", "--team-file", file)
	if h.sim.lines(`^systemctl (start|restart)`) != 0 {
		t.Fatalf("team.json alone restarted: %v", h.sim.log)
	}
	contains(t, "stdout", h.stdout(), "CHANGED\tteam alpha team.json\n")
}

func TestTeamRefusals(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(h *harness) string
		wantCode int
		wantMsg  string
	}{
		{"render check refuses with 78", func(h *harness) string {
			h.sim.renderRefuse = 78
			return h.teamFile("alpha", []string{"192.0.2.10"}, nil)
		}, 78, "team file %s refused (above)"},
		{"render check refuses with 64", func(h *harness) string {
			h.sim.renderRefuse = 64
			return h.teamFile("alpha", []string{"192.0.2.10"}, nil)
		}, 64, "team file %s refused (above)"},
		{"render check leaks an unrelated status", func(h *harness) string {
			h.sim.renderRefuse = 2
			return h.teamFile("alpha", []string{"192.0.2.10"}, nil)
		}, 1, "team file %s refused (above)"},
		{"not JSON", func(h *harness) string {
			path := filepath.Join(t.TempDir(), "t.json")
			h.write(path, `{"team": "alpha", `, 0o644)
			return path
		}, 78, "no team name"},
		{"no team name", func(h *harness) string {
			path := filepath.Join(t.TempDir(), "t.json")
			h.write(path, `{"port": 8448}`, 0o644)
			return path
		}, 78, "no team name: .team is not a string"},
		{"a numeric team name", func(h *harness) string {
			path := filepath.Join(t.TempDir(), "t.json")
			h.write(path, `{"team": 7}`, 0o644)
			return path
		}, 78, "no team name"},
		{"an array", func(h *harness) string {
			path := filepath.Join(t.TempDir(), "t.json")
			h.write(path, `[1]`, 0o644)
			return path
		}, 78, "no team name"},
		{"a bad team name", func(h *harness) string {
			path := filepath.Join(t.TempDir(), "t.json")
			h.write(path, `{"team": "Bad_Name"}`, 0o644)
			return path
		}, 78, "team 'Bad_Name' is not a team name"},
		{"a team that is absent", func(h *harness) string {
			path := filepath.Join(t.TempDir(), "t.json")
			h.write(path, `{"team": "alpha", "state": "absent", "port": 8448}`, 0o644)
			return path
		}, 78, "team alpha is state absent: run agent-bus-install remove --team alpha"},
		{"a listen address on no interface", func(h *harness) string {
			return h.teamFile("alpha", []string{"198.51.100.7"}, nil)
		}, 78, "listen address 198.51.100.7 is on no interface of this host"},
		{"a listen address on two interfaces", func(h *harness) string {
			h.sim.ipAddr = `[{"ifname":"b0","addr_info":[{"local":"192.0.2.10"}]},{"ifname":"a0","addr_info":[{"local":"192.0.2.10"}]}]`
			return h.teamFile("alpha", []string{"192.0.2.10"}, nil)
		}, 78, "listen address 192.0.2.10 is on more than one interface: a0, b0"},
		{"an unreadable address listing", func(h *harness) string {
			h.sim.ipAddr = "not json"
			return h.teamFile("alpha", []string{"192.0.2.10"}, nil)
		}, 1, "ip addr show"},
		{"a port in use on the host", func(h *harness) string {
			h.sim.units[hsUnit("alpha")] = &unitState{}
			h.sim.ssAddrs = []string{"127.0.0.1"}
			h.sim.units["agent-bus-hs@other.service"] = &unitState{active: true}
			return h.teamFile("alpha", []string{"192.0.2.10"}, nil)
		}, 78, "port 8448 is already in use on this host"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.software()
			file := tc.setup(h)
			if code := h.run("team", "--team-file", file); code != tc.wantCode {
				t.Fatalf("exit %d, stderr %q", code, h.stderr())
			}
			contains(t, "stderr", h.stderr(), strings.Replace(tc.wantMsg, "%s", file, 1))
			if h.exists("var/lib/agent-bus/alpha") || h.sim.lines(`^systemctl (start|restart|enable)`) != 0 {
				t.Errorf("a refused team left a directory or started a unit: %v", h.sim.log)
			}
		})
	}
}

func TestTeamRefusesAFileItCannotRead(t *testing.T) {
	h := newHarness(t)
	h.software()
	for _, file := range []string{filepath.Join(t.TempDir(), "absent"), t.TempDir()} {
		code := h.run("team", "--team-file", file)
		if code != term.ExitUsage || !strings.Contains(h.stderr(), "--team-file "+file+" is not a readable file") {
			t.Errorf("%s: exit %d, stderr %q", file, code, h.stderr())
		}
	}
}

func TestTeamNeedsSoftwareFirstAndRunningServices(t *testing.T) {
	h := newHarness(t)
	file := h.teamFile("alpha", []string{"192.0.2.10"}, nil)
	code := h.run("team", "--team-file", file)
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "/usr/local/lib/agent-bus/tuwunel is missing: run agent-bus-install software first") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	h.software()
	h.sim.units["firewalld.service"].active = false
	code = h.run("team", "--team-file", file)
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "firewalld.service is inactive; the homeserver needs it running") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
}

func TestTeamNeedsEachPieceOfSoftware(t *testing.T) {
	for _, gone := range []string{
		"usr/local/lib/agent-bus/agent-bus.pyz", "usr/local/bin/agent-bus", "etc/systemd/system/agent-bus-hs@.service",
	} {
		h := newHarness(t)
		h.software()
		if err := os.Remove(h.rel(gone)); err != nil {
			t.Fatal(err)
		}
		code := h.run("team", "--team-file", h.teamFile("alpha", []string{"192.0.2.10"}, nil))
		if code != term.ExitFailure || !strings.Contains(h.stderr(), "/"+gone+" is missing: run agent-bus-install software first") {
			t.Errorf("%s: exit %d, stderr %q", gone, code, h.stderr())
		}
	}
}

func TestTeamARefusedPortHeldByAnotherTeam(t *testing.T) {
	h := newHarness(t)
	h.installTeam(nil)
	other := h.teamFile("gamma", []string{"192.0.2.10"}, nil)
	code := h.run("team", "--team-file", other)
	if code != term.ExitConfig || !strings.Contains(h.stderr(), "port 8448 is team alpha's") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
}

func TestTeamAMalformedTeamFileOfAnotherTeamIsAConfigurationError(t *testing.T) {
	h := newHarness(t)
	h.software()
	h.write(h.rel("var/lib/agent-bus/other/team.json"), "{not json", 0o640)
	code := h.run("team", "--team-file", h.teamFile("alpha", []string{"192.0.2.10"}, nil))
	if code != term.ExitConfig || !strings.Contains(h.stderr(), "/var/lib/agent-bus/other/team.json: not readable JSON") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
}

func TestTeamIgnoresOtherDirectoriesWithoutATeamFileAndPorts(t *testing.T) {
	h := newHarness(t)
	h.software()
	h.mkdir("var/lib/agent-bus/empty")
	h.write(h.rel("var/lib/agent-bus/stray"), "a file, not a team", 0o600)
	h.write(h.rel("var/lib/agent-bus/noport/team.json"), `{"team":"noport"}`, 0o640)
	h.write(h.rel("var/lib/agent-bus/self/team.json"), `{"team":"alpha","port":8448}`, 0o640)
	if code := h.run("team", "--team-file", h.teamFile("alpha", []string{"192.0.2.10"}, nil)); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
}

func TestTeamAnUnexpectedBoundAddressFailsBeforeBootstrap(t *testing.T) {
	h := newHarness(t)
	h.software()
	h.sim.ssAddrs = []string{"127.0.0.1", "192.0.2.10", "198.51.100.9"}
	code := h.run("team", "--team-file", h.teamFile("alpha", []string{"192.0.2.10"}, nil))
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "198.51.100.9") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	if h.sim.lines(`^agent-bus bootstrap`) != 0 || h.exists("var/lib/agent-bus-applied/alpha.json") {
		t.Fatal("bootstrap ran or a stamp was written")
	}
}

func TestTeamAUnitThatFailsToStartFailsTheRun(t *testing.T) {
	h := newHarness(t)
	h.software()
	h.sim.failStart[hsUnit("beta")] = true
	code := h.run("team", "--team-file", h.teamFile("beta", []string{"192.0.2.10"}, nil))
	if code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	contains(t, "stderr", h.stderr(), "agent-bus-hs@beta.service is failed (auto-restart)")
	contains(t, "stderr", h.stderr(), "journalctl -u agent-bus-hs@beta.service")
	contains(t, "stderr", h.stderr(), "stub journal: tuwunel exited")
	if h.sim.lines(`^agent-bus bootstrap`) != 0 {
		t.Fatal("bootstrap ran")
	}
}

func TestTeamAFailedBootstrapKeepsTheAdminToolsStatusAndStampsNothing(t *testing.T) {
	h := newHarness(t)
	h.software()
	h.fake.OnFunc([]string{"agent-bus", "bootstrap"}, func(execx.Cmd) (execx.Result, error) {
		return refuse(69, "unreachable")
	})
	code := h.run("team", "--team-file", h.teamFile("alpha", []string{"192.0.2.10"}, nil))
	if code != 69 || h.exists("var/lib/agent-bus-applied/alpha.json") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
}

func TestTeamAFirewallAddThatFailsLeavesTheRuleInTheRecordForRemove(t *testing.T) {
	h := newHarness(t)
	h.software()
	h.sim.fwFailAdd = "203.0.113.0/24"
	file := h.teamFile("alpha", []string{"192.0.2.10"}, []string{allowA, "203.0.113.0/24"})
	if code := h.run("team", "--team-file", file); code != term.ExitFailure {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	record := h.read("var/lib/agent-bus-install/alpha/firewalld.rules")
	contains(t, "record", record, allowA)
	contains(t, "record", record, "203.0.113.0/24")
	if len(h.sim.fwPerm) != 1 {
		t.Fatalf("permanent %v", h.sim.fwPerm)
	}
	h.sim.fwFailAdd = ""
	if code := h.run("remove", "--team", "alpha"); code != 0 {
		t.Fatalf("remove exit %d", code)
	}
	if len(h.sim.fwPerm) != 0 || len(h.sim.fwRun) != 0 {
		t.Fatalf("permanent %v runtime %v", h.sim.fwPerm, h.sim.fwRun)
	}
}

func TestTeamAnInterfaceWithNoZoneUsesTheDefaultZone(t *testing.T) {
	h := newHarness(t)
	h.software()
	h.run("team", "--team-file", h.teamFile("alpha", []string{"192.0.2.10"}, []string{allowA}))
	if _, ok := mapKeyWith(h.sim.fwPerm, "public\t"); !ok {
		t.Fatalf("permanent %v", h.sim.fwPerm)
	}
}

func mapKeyWith(m map[string]bool, prefix string) (string, bool) {
	for k := range m {
		if strings.HasPrefix(k, prefix) {
			return k, true
		}
	}
	return "", false
}

func TestTeamNoRouteToAnAllowFromNetworkFails(t *testing.T) {
	h := newHarness(t)
	h.software()
	h.sim.routeDev = ""
	code := h.run("team", "--team-file", h.teamFile("alpha", []string{"192.0.2.10"}, []string{allowA}))
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "no route to allow_from "+allowA) {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	if h.sim.lines(`^firewall-cmd .*--add-rich-rule`) != 0 {
		t.Fatal("a rule was added before the route was known")
	}
}

func TestTeamRefusesAnAllowFromThatIsNotACIDR(t *testing.T) {
	h := newHarness(t)
	h.software()
	code := h.run("team", "--team-file", h.teamFile("alpha", []string{"192.0.2.10"}, []string{`198.51.100.0/24" accept`}))
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "is not a network in CIDR form") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	if h.sim.lines(`^firewall-cmd`) != 0 {
		t.Fatalf("firewall-cmd ran: %v", h.sim.log)
	}
}

func TestTeamRepairsAWrongModeSharedSecretWithoutTouchingItsContent(t *testing.T) {
	h := newHarness(t)
	file := h.installTeam(nil)
	sharedPath := "var/lib/agent-bus/alpha/secrets/registration_shared_secret"
	before := h.read(sharedPath)
	if err := setMode(h.rel(sharedPath), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := h.run("team", "--team-file", file); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	if h.mode(sharedPath) != 0o600 || h.read(sharedPath) != before {
		t.Fatal("the secret was not repaired in place")
	}
	if h.stdout() != "CHANGED\tregistration shared secret mode and owner\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func TestTeamRefusesASharedSecretThatIsASymlink(t *testing.T) {
	h := newHarness(t)
	file := h.installTeam(nil)
	sharedPath := h.rel("var/lib/agent-bus/alpha/secrets/registration_shared_secret")
	if err := os.Remove(sharedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", sharedPath); err != nil {
		t.Fatal(err)
	}
	if code := h.run("team", "--team-file", file); code != term.ExitFailure {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
}

func TestTeamFailsWhenTheRandomSourceFails(t *testing.T) {
	h := newHarness(t)
	h.software()
	h.app.cfg.Rand = strings.NewReader("short")
	code := h.run("team", "--team-file", h.teamFile("alpha", []string{"192.0.2.10"}, nil))
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "could not read 64 random bytes") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
}

func TestTeamNeedsASourceDigestToStamp(t *testing.T) {
	h := newHarness(t)
	h.software()
	h.app.cfg.SourceDigest = ""
	code := h.run("team", "--team-file", h.teamFile("alpha", []string{"192.0.2.10"}, nil))
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "source digest") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	if h.exists("var/lib/agent-bus-applied/alpha.json") {
		t.Fatal("a stamp was written")
	}
}

func TestTeamPassesThePreviousInstalledTeamJSONToTheCheck(t *testing.T) {
	h := newHarness(t)
	file := h.installTeam(nil)
	h.run("team", "--team-file", file)
	want := "agent-bus render check --previous=" + h.rel("var/lib/agent-bus/alpha/team.json")
	if h.sim.lines("^"+regexp.QuoteMeta(want)+"$") != 1 {
		t.Fatalf("log %v", h.sim.log)
	}
}

func TestTeamEveryCommandItRunsPropagatesItsFailure(t *testing.T) {
	seen := map[string]bool{}
	// The first install of a team runs every command.
	first := newHarness(t)
	first.software()
	first.sim.routeDev = "wg0"
	first.run("team", "--team-file", first.teamFile("alpha", []string{"192.0.2.10"}, []string{allowA}))
	for _, line := range first.sim.log {
		seen[firstWords(line)] = true
	}
	for prefix := range seen {
		switch prefix {
		case "systemctl is-enabled", "firewall-cmd --permanent", "firewall-cmd --zone=trusted", "curl --fail":
			continue // a refusal is an answer for these
		}
		h := newHarness(t)
		h.software()
		h.sim.routeDev = "wg0"
		file := h.teamFile("alpha", []string{"192.0.2.10"}, []string{allowA})
		h.sim.fails = []failRule{{prefix: prefix, code: 5}}
		if code := h.run("team", "--team-file", file); code == 0 {
			t.Errorf("a failing %q did not fail the run", prefix)
		}
	}
}

func TestTeamAFailingChildNeverLeaksTheInstallersOwnCodes(t *testing.T) {
	for _, prefix := range []string{"systemctl enable", "systemctl start", "systemctl daemon-reload", "ss ", "ip "} {
		h := newHarness(t)
		h.software()
		h.sim.fails = []failRule{{prefix: prefix, code: 78}}
		code := h.run("team", "--team-file", h.teamFile("alpha", []string{"192.0.2.10"}, nil))
		if code != term.ExitFailure {
			t.Errorf("%s: exit %d", prefix, code)
		}
	}
	h := newHarness(t)
	h.software()
	h.sim.fails = []failRule{{prefix: "agent-bus render toml", code: 78}}
	if code := h.run("team", "--team-file", h.teamFile("alpha", []string{"192.0.2.10"}, nil)); code != term.ExitFailure {
		t.Errorf("render toml: exit %d", code)
	}
}

func TestTeamReportsOwnersThatCannotBeResolved(t *testing.T) {
	h := newHarness(t)
	h.software()
	h.app.svc = nil
	h.app.cfg.SvcOwner = func() (ownerT, error) { return ownerT{}, errors.New("no such user") }
	code := h.run("team", "--team-file", h.teamFile("alpha", []string{"192.0.2.10"}, nil))
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "no such user") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
}
