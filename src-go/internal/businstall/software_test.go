package businstall

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func onlyChangedLines(t *testing.T, stdout string) {
	t.Helper()
	for line := range strings.Lines(stdout) {
		if !strings.HasPrefix(line, "CHANGED\t") {
			t.Errorf("stdout carries a line that is not CHANGED: %q", line)
		}
	}
}

func TestSoftwareInstallsEverything(t *testing.T) {
	h := newHarness(t)
	if code := h.run("software", "--source", h.source, "--bus-address", "192.0.2.10"); code != 0 {
		t.Fatalf("exit %d:\n%s", code, h.stderr())
	}
	lib := "usr/local/lib/agent-bus/"
	for path, mode := range map[string]os.FileMode{
		lib + "tuwunel-1.9.3": 0o755, lib + "tuwunel-1.9.3.asset-sha256": 0o644,
		lib + "agent-bus.pyz": 0o755, lib + "pingbus.pyz": 0o755,
		"usr/local/bin/agent-bus": 0o755, "usr/local/sbin/agent-bus-install": 0o755,
		"usr/local/share/agent-bus/tuwunel.pin": 0o644, "usr/local/share/agent-bus/resolv.conf": 0o644,
		"usr/local/share/agent-bus/kit/pingbus": 0o755, "usr/local/share/agent-bus/kit/agent-bus-claude": 0o755,
		"usr/local/share/agent-bus/kit/settings.json":                          0o644,
		"usr/local/share/agent-bus/kit/plugin/pingbus/skills/pingbus/SKILL.md": 0o644,
		"usr/local/share/agent-bus/kit/README.vm":                              0o644,
		"etc/systemd/system/agent-bus-hs@.service":                             0o644,
		"etc/systemd/system/agent-bus-backup@.service":                         0o644,
		"etc/systemd/system/agent-bus-backup@.timer":                           0o644,
		"var/lib/agent-bus": 0o700, "var/lib/agent-bus-install": 0o700,
		"usr/local/lib/agent-bus": 0o755, "usr/local/share/agent-bus/kit/plugin/pingbus/.claude-plugin": 0o755,
	} {
		if got := h.mode(path); got != mode {
			t.Errorf("%s: mode %o, want %o", path, got, mode)
		}
	}
	if link, err := os.Readlink(h.rel(lib + "tuwunel")); err != nil || link != "tuwunel-1.9.3" {
		t.Errorf("tuwunel link %q, %v", link, err)
	}
	if link, err := os.Readlink(h.rel("usr/local/bin/pingbus")); err != nil || link != "../lib/agent-bus/pingbus.pyz" {
		t.Errorf("pingbus link %q, %v", link, err)
	}
	if link, err := os.Readlink(h.rel("usr/local/bin/agent-bus-claude")); err != nil || link != "../share/agent-bus/kit/agent-bus-claude" {
		t.Errorf("launcher link %q, %v", link, err)
	}
	if h.read("usr/local/sbin/agent-bus-install") != "the installer binary\n" {
		t.Error("the installer did not install itself")
	}
	if h.read(lib+"tuwunel-1.9.3") != string(h.asset) {
		t.Error("the binary is not the decompressed asset")
	}
	onlyChangedLines(t, h.stdout())
	contains(t, "stdout", h.stdout(), "CHANGED\tpackages python3 firewalld NetworkManager zstd curl jq tcpdump util-linux\n")
	contains(t, "stdout", h.stdout(), "CHANGED\tsystem user agent-bus\n")
	contains(t, "stdout", h.stdout(), "CHANGED\tdirectory /var/lib/agent-bus\n")
	contains(t, "stdout", h.stdout(), "CHANGED\ttuwunel 1.9.3\n")
	contains(t, "stdout", h.stdout(), "CHANGED\tzipapp agent-bus\n")
	contains(t, "stdout", h.stdout(), "CHANGED\tagent-bus-install\n")
	contains(t, "stdout", h.stdout(), "CHANGED\tagentbus0 dummy connection with 192.0.2.10\n")
	for unit, want := range map[string]string{
		"dnf install":         `^dnf install -y python3 firewalld NetworkManager zstd curl jq tcpdump util-linux$`,
		"useradd":             `^useradd --system --user-group --no-create-home --home-dir /var/lib/agent-bus --shell /usr/sbin/nologin --comment Agent team bus homeserver agent-bus$`,
		"one download":        `^curl --fail --silent --show-error --location --proto =https --tlsv1.2 --max-time 600 -o .*/v1.9.3-release-all-x86_64-v1-linux-gnu-tuwunel.zst https://github.com/matrix-construct/tuwunel/releases/download/v1.9.3/v1.9.3-release-all-x86_64-v1-linux-gnu-tuwunel.zst$`,
		"zstd":                `^zstd -q -d -f -o .*/tuwunel -- .*/v1.9.3-release-all-x86_64-v1-linux-gnu-tuwunel.zst$`,
		"reload":              `^systemctl daemon-reload$`,
		"add":                 `^nmcli connection add type dummy ifname agentbus0 con-name agentbus0 connection.autoconnect yes ipv4.method manual ipv4.addresses 192.0.2.10/32 ipv6.method disabled ipv6.addresses $`,
		"up":                  `^nmcli connection up agentbus0$`,
		"bundle agent-bus":    `^python3 -B -s -m helpers.pingbus.bundle --source .* --out .*/usr/local/lib/agent-bus/agent-bus.pyz agent-bus$`,
		"bundle pingbus":      `^python3 -B -s -m helpers.pingbus.bundle --source .* --out .*/usr/local/lib/agent-bus/pingbus.pyz pingbus$`,
		"machine":             `^uname -m$`,
		"state of connection": `^nmcli -g GENERAL.STATE connection show agentbus0$`,
	} {
		if h.sim.lines(want) != 1 {
			t.Errorf("%s: want one line matching %s in %q", unit, want, h.sim.log)
		}
	}
	if h.sim.lines(`^systemctl (start|restart|enable)`) != 0 {
		t.Error("software started or enabled something")
	}
}

func TestSoftwareASecondRunIsQuiet(t *testing.T) {
	h := newHarness(t)
	h.software()
	if code := h.run("software", "--source", h.source); code != 0 {
		t.Fatalf("exit %d:\n%s", code, h.stderr())
	}
	if h.stdout() != "" {
		t.Fatalf("a second run printed %q", h.stdout())
	}
	if n := h.sim.lines(`^(curl|dnf|useradd|zstd|systemctl daemon-reload|nmcli connection (add|modify|up)) `); n != 0 {
		t.Fatalf("a second run ran %d changing commands: %v", n, h.sim.log)
	}
	contains(t, "stderr", h.stderr(), "Tuwunel v1.9.3 is installed from the pinned asset; nothing to download")
}

func TestSoftwareASecondRunWithTheSameBusAddressLeavesTheInterfaceAlone(t *testing.T) {
	h := newHarness(t)
	for range 2 {
		if code := h.run("software", "--source", h.source, "--bus-address", "192.0.2.10"); code != 0 {
			t.Fatalf("exit %d:\n%s", code, h.stderr())
		}
	}
	if h.stdout() != "" || h.sim.lines(`^nmcli connection (add|modify|up)`) != 0 {
		t.Fatalf("stdout %q, log %v", h.stdout(), h.sim.log)
	}
}

func TestSoftwareANewBusAddressModifiesTheConnection(t *testing.T) {
	h := newHarness(t)
	h.run("software", "--source", h.source, "--bus-address", "192.0.2.10")
	if code := h.run("software", "--source", h.source, "--bus-address", "192.0.2.11"); code != 0 {
		t.Fatalf("exit %d:\n%s", code, h.stderr())
	}
	if h.sim.lines(`^nmcli connection modify agentbus0 ipv4.method manual ipv4.addresses 192.0.2.11/32`) != 1 ||
		h.sim.lines(`^nmcli connection up agentbus0$`) != 1 {
		t.Fatalf("log %v", h.sim.log)
	}
	if h.stdout() != "CHANGED\tagentbus0 address now 192.0.2.11\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func TestSoftwareAnIPv6BusAddressIsComparedAsAnAddress(t *testing.T) {
	h := newHarness(t)
	h.run("software", "--source", h.source, "--bus-address", "2001:db8::10")
	contains(t, "log", strings.Join(h.sim.log, "\n"), "ipv4.method disabled ipv4.addresses  ipv6.method manual ipv6.addresses 2001:db8::10/128")
	if code := h.run("software", "--source", h.source, "--bus-address", "2001:db8::10"); code != 0 || h.stdout() != "" {
		t.Fatalf("exit %d, stdout %q", code, h.stdout())
	}
	// nmcli's terse mode escapes the colons; the address is still the same address.
	h.sim.nmAddrs = "\n2001\\:db8\\:\\:10/128\n"
	if code := h.run("software", "--source", h.source, "--bus-address", "2001:db8::10"); code != 0 || h.stdout() != "" {
		t.Fatalf("exit %d, stdout %q", code, h.stdout())
	}
}

func TestSoftwareBringsAnInterfaceThatIsDownUp(t *testing.T) {
	h := newHarness(t)
	h.run("software", "--source", h.source, "--bus-address", "192.0.2.10")
	h.sim.nmUp = false
	if code := h.run("software", "--source", h.source, "--bus-address", "192.0.2.10"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if h.stdout() != "CHANGED\tagentbus0 up\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func TestSoftwareRefusesUnusableBusAddressesBeforeRunningAnything(t *testing.T) {
	for _, bad := range []string{
		"0.0.0.0", "::", "192.0.2.300", "2001:db8::zz", "127.0.0.1", "192.0.2.010",
		"2001:db8::1%eth0", "::ffff:192.0.2.1", "fe80::1", "224.0.0.1", "192.0.2.10/32", " 192.0.2.10",
	} {
		h := newHarness(t)
		code := h.run("software", "--source", h.source, "--bus-address="+bad)
		want := "agent-bus-install: --bus-address '" + bad + "' is not a concrete, canonical IP address\n"
		if code != term.ExitUsage || h.stderr() != want {
			t.Errorf("%q: exit %d, stderr %q", bad, code, h.stderr())
		}
		if len(h.sim.log) != 0 {
			t.Errorf("%q: ran %v", bad, h.sim.log)
		}
	}
}

func TestSoftwareChecksTheBusAddressBeforeTheHostIsLookedAt(t *testing.T) {
	h := newHarness(t)
	h.write(h.rel("etc/os-release"), "ID=debian\n", 0o644)
	if code := h.run("software", "--source", h.source, "--bus-address", "0.0.0.0"); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
}

func TestSoftwareRefusesAHostThatIsNotFedora(t *testing.T) {
	h := newHarness(t)
	h.write(h.rel("etc/os-release"), "ID=debian\n", 0o644)
	code := h.run("software", "--source", h.source)
	if code != term.ExitConfig {
		t.Fatalf("exit %d", code)
	}
	contains(t, "stderr", h.stderr(), "os-release")
	if len(h.sim.log) != 0 {
		t.Fatalf("ran %v", h.sim.log)
	}
}

func TestSoftwareRefusesARelativeSource(t *testing.T) {
	h := newHarness(t)
	h.write(h.rel("etc/os-release"), "ID=debian\n", 0o644) // usage errors come before the host is looked at
	if code := h.run("software", "--source", "relative/checkout"); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	contains(t, "stderr", h.stderr(), "--source must be an absolute path")
}

func TestSoftwareRefusesASourceThatIsNotACheckoutBeforeChangingAnything(t *testing.T) {
	for _, missing := range append([]string{
		"helpers/pingbus/bundle.py", "files/usr/local/bin/agent-bus",
		"files/usr/local/share/agent-bus/tuwunel.pin", "files/usr/local/share/agent-bus/resolv.conf",
	}, prefixAll("files/opt/claude-yolo/optional/agent-bus/", kitFilesForTest)...) {
		h := newHarness(t)
		if err := os.Remove(filepath.Join(h.source, missing)); err != nil {
			t.Fatal(err)
		}
		code := h.run("software", "--source", h.source)
		want := "--source " + h.source + " holds no " + missing + ": not a fedora-desktop checkout"
		if code != term.ExitConfig || !strings.Contains(h.stderr(), want) {
			t.Errorf("%s: exit %d, stderr %q", missing, code, h.stderr())
		}
		if len(h.sim.log) != 0 || h.exists("usr/local/share/agent-bus") {
			t.Errorf("%s: something was done: %v", missing, h.sim.log)
		}
	}
}

func prefixAll(prefix string, names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = prefix + n
	}
	return out
}

func TestSoftwareDoesNotNeedTheInstallerInTheSource(t *testing.T) {
	h := newHarness(t)
	if _, err := os.Stat(filepath.Join(h.source, "files/usr/local/sbin/agent-bus-install")); err == nil {
		t.Fatal("the harness source holds an installer")
	}
	h.software()
}

func TestSoftwareAcceptsASbinThatIsASymlinkToBin(t *testing.T) {
	h := newHarness(t)
	if err := os.Remove(h.rel("usr/local/sbin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("bin", h.rel("usr/local/sbin")); err != nil {
		t.Fatal(err)
	}
	h.software()
	if h.mode("usr/local/bin/agent-bus-install") != 0o755 {
		t.Fatal("the installer did not land through the symlink")
	}
	if link, err := os.Readlink(h.rel("usr/local/sbin")); err != nil || link != "bin" {
		t.Fatalf("the symlink became %q (%v)", link, err)
	}
}

func TestSoftwareFailsOnAMissingSystemDirectoryAndCreatesNothing(t *testing.T) {
	for _, dir := range []string{"usr/local/bin", "usr/local/sbin", "etc/systemd/system"} {
		h := newHarness(t)
		if err := os.RemoveAll(h.rel(dir)); err != nil {
			t.Fatal(err)
		}
		code := h.run("software", "--source", h.source)
		f := "/" + dir + " is not a directory on this host"
		if code != term.ExitFailure || !strings.Contains(h.stderr(), f) {
			t.Errorf("%s: exit %d, stderr %q", dir, code, h.stderr())
		}
		if h.exists(dir) || len(h.sim.log) != 0 {
			t.Errorf("%s: the installer created it or ran %v", dir, h.sim.log)
		}
	}
}

func TestSoftwareRefusesBadPins(t *testing.T) {
	h := newHarness(t)
	h.write(filepath.Join(h.source, "files/usr/local/share/agent-bus/tuwunel.pin"), "tuwunel_version: 1.9.3\n", 0o644)
	if code := h.run("software", "--source", h.source); code != term.ExitConfig {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	contains(t, "stderr", h.stderr(), "tuwunel_version")
	if len(h.sim.log) != 0 {
		t.Fatalf("ran %v", h.sim.log)
	}
}

func TestSoftwareRefusesAnUnpinnedArchitecture(t *testing.T) {
	h := newHarness(t)
	h.sim.machine = "riscv64"
	if code := h.run("software", "--source", h.source); code != term.ExitConfig {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	contains(t, "stderr", h.stderr(), "no pinned Tuwunel build for architecture riscv64")
}

func TestSoftwareUsesTheAarch64AssetOnArm(t *testing.T) {
	h := newHarness(t)
	h.sim.machine = "aarch64"
	h.writePin("1.9.3", strings.Repeat("1", 64))
	h.run("software", "--source", h.source)
	contains(t, "log", strings.Join(h.sim.log, "\n"), "v1.9.3-release-all-aarch64-v8-linux-gnu-tuwunel.zst")
}

func TestSoftwareABadHashInstallsNothing(t *testing.T) {
	h := newHarness(t)
	h.writePin("1.9.4", strings.Repeat("1", 64))
	code := h.run("software", "--source", h.source)
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "sha256") || !strings.Contains(h.stderr(), "nothing installed") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
	if h.exists("usr/local/lib/agent-bus/tuwunel-1.9.4") {
		t.Error("a binary was installed")
	}
	if _, err := os.Lstat(h.rel("usr/local/lib/agent-bus/tuwunel")); err == nil {
		t.Error("a tuwunel link exists")
	}
}

func TestSoftwareAChangedPinnedHashDownloadsAgain(t *testing.T) {
	h := newHarness(t)
	h.software()
	h.asset = []byte("a rebuilt release asset\n")
	h.sim.asset = h.asset
	h.writePin("1.9.3", sha256Hex(h.asset))
	if code := h.run("software", "--source", h.source); code != 0 {
		t.Fatalf("exit %d:\n%s", code, h.stderr())
	}
	if h.sim.lines(`^curl .*releases/download`) != 1 || h.read("usr/local/lib/agent-bus/tuwunel-1.9.3") != string(h.asset) {
		t.Fatalf("log %v", h.sim.log)
	}
}

func TestSoftwareANewTuwunelVersionLeavesTheOldBinary(t *testing.T) {
	h := newHarness(t)
	h.software()
	h.writePin("1.9.4", sha256Hex(h.asset))
	h.software()
	if !h.exists("usr/local/lib/agent-bus/tuwunel-1.9.3") || !h.exists("usr/local/lib/agent-bus/tuwunel-1.9.4") {
		t.Fatal("a version is missing")
	}
	if link, err := os.Readlink(h.rel("usr/local/lib/agent-bus/tuwunel")); err != nil || link != "tuwunel-1.9.4" {
		t.Fatalf("link %q, %v", link, err)
	}
}

func TestSoftwareReloadsSystemdOnlyForAChangedUnit(t *testing.T) {
	h := newHarness(t)
	h.software()
	unit := filepath.Join(h.source, "files/etc/systemd/system/agent-bus-hs@.service")
	h.write(unit, hsUnitText+"# a hardening fix\n", 0o644)
	if code := h.run("software", "--source", h.source); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if h.sim.lines(`^systemctl daemon-reload$`) != 1 || h.sim.lines(`^systemctl (start|restart|stop)`) != 0 {
		t.Fatalf("log %v", h.sim.log)
	}
	if h.stdout() != "CHANGED\tunit agent-bus-hs@.service\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func TestSoftwareRefusesToWriteThroughASymlinkedDestination(t *testing.T) {
	h := newHarness(t)
	h.software()
	if err := os.Remove(h.rel("usr/local/bin/agent-bus")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", h.rel("usr/local/bin/agent-bus")); err != nil {
		t.Fatal(err)
	}
	code := h.run("software", "--source", h.source)
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "/usr/local/bin/agent-bus is a symlink; refusing to write through it") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
}

func TestSoftwareReportsAZipappBuilderThatPrintsNoMarker(t *testing.T) {
	h := newHarness(t)
	h.sim.bundleBad = "something else\n"
	code := h.run("software", "--source", h.source)
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "the zipapp builder printed no marker for agent-bus: something else") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
}

func TestSoftwareTheBundlerRunsWithACleanPythonEnvironment(t *testing.T) {
	h := newHarness(t)
	h.software()
	var seen bool
	for _, c := range h.fake.Calls() {
		if c.Name == "python3" {
			seen = true
			if c.Dir != h.source || len(c.Unset) == 0 {
				t.Errorf("bundler dir %q, unset %v", c.Dir, c.Unset)
			}
		}
	}
	if !seen {
		t.Fatal("the bundler never ran")
	}
}

func TestSoftwareAFailedChildIsExitOneNamingTheStep(t *testing.T) {
	steps := []struct{ prefix, want string }{
		{"dnf ", "dnf install"},
		{"getent ", "getent passwd"},
		{"useradd ", "useradd"},
		{"uname ", "uname -m"},
		{"curl ", "curl https://github.com"},
		{"zstd ", "zstd"},
		{"python3 ", "helpers.pingbus.bundle"},
		{"systemctl daemon-reload", "daemon-reload"},
	}
	for _, step := range steps {
		h := newHarness(t)
		// 77 and 78 are the installer's own codes; a child's must never leak as one.
		h.sim.fails = []failRule{{prefix: step.prefix, code: 78}}
		code := h.run("software", "--source", h.source)
		if code != term.ExitFailure || !strings.Contains(h.stderr(), step.want) {
			t.Errorf("%s: exit %d, stderr %q", step.prefix, code, h.stderr())
		}
	}
	h := newHarness(t)
	h.sim.fails = []failRule{{prefix: "nmcli ", code: 77}}
	code := h.run("software", "--source", h.source, "--bus-address", "192.0.2.10")
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "nmcli") {
		t.Errorf("nmcli: exit %d, stderr %q", code, h.stderr())
	}
}

func TestSoftwareUnreadableNMOutputFailsInsteadOfAssumingADifference(t *testing.T) {
	h := newHarness(t)
	h.run("software", "--source", h.source, "--bus-address", "192.0.2.10")
	h.sim.nmAddrs = "garbage\n"
	code := h.run("software", "--source", h.source, "--bus-address", "192.0.2.10")
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "nmcli addresses") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
}

func TestSoftwareFailsWhenTheOwnersCannotBeResolved(t *testing.T) {
	h := newHarness(t)
	h.app.cfg.SvcOwner = func() (ownerT, error) { return ownerT{}, errors.New("no such user") }
	code := h.run("software", "--source", h.source)
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "the agent-bus account") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
}

func TestSoftwareFailsWhenItCannotFindItsOwnExecutable(t *testing.T) {
	h := newHarness(t)
	h.app.cfg.Executable = func() (string, error) { return "", errors.New("no /proc") }
	code := h.run("software", "--source", h.source)
	if code != term.ExitFailure || !strings.Contains(h.stderr(), "no /proc") {
		t.Fatalf("exit %d, stderr %q", code, h.stderr())
	}
}

func TestSoftwareEveryCommandItRunsPropagatesItsFailure(t *testing.T) {
	probe := newHarness(t)
	if code := probe.run("software", "--source", probe.source, "--bus-address", "192.0.2.10"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	seen := map[string]bool{}
	for _, line := range probe.sim.log {
		seen[firstWords(line)] = true
	}
	for prefix := range seen {
		if prefix == "rpm -q" {
			continue // any status of rpm -q is an answer: the package is missing
		}
		h := newHarness(t)
		h.sim.fails = []failRule{{prefix: prefix, code: 5}}
		if code := h.run("software", "--source", h.source, "--bus-address", "192.0.2.10"); code != term.ExitFailure {
			t.Errorf("a failing %q ended with exit %d, stderr %q", prefix, code, h.stderr())
		}
	}
}

var firstTwo = regexp.MustCompile(`^(\S+( \S+)?)`)

func firstWords(line string) string { return firstTwo.FindString(line) }
