package host

import (
	"slices"
	"strings"
	"testing"
)

// baseUnitText is C 3.2.3's base unit as DESIGN-v2-C-host.md gives it. BaseSandbox is pinned
// against its [Service] section; a change to either is a change to both.
const baseUnitText = `[Unit]
Description=v2 team homeserver %i
StartLimitIntervalSec=0
AssertPathExists=!/var/lib/teamchat-admin/%i/restore.block
Before=lxc.service

[Service]
Type=notify
NotifyAccess=main
WatchdogSec=30s
TimeoutAbortSec=30s
TimeoutStopSec=330
Restart=always
RestartSec=5s
RestartSteps=5
RestartMaxDelaySec=60s
DynamicUser=yes
User=teamchat-%i
StateDirectory=teamchat/%i
StateDirectoryMode=0700
LoadCredential=config.toml:/var/lib/teamchat-admin/%i/config.toml
Environment=TUWUNEL_CONFIG=%d/config.toml
UMask=0077
NoNewPrivileges=yes
CapabilityBoundingSet=
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
PrivatePIDs=yes
PrivateIPC=yes
ProtectProc=invisible
ProcSubset=pid
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
SystemCallArchitectures=native
SystemCallFilter=@system-service
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
IPAccounting=yes
IPAddressDeny=any
SocketBindDeny=any
TemporaryFileSystem=/run/systemd/resolve:ro
BindReadOnlyPaths=/usr/local/share/teamchat/resolv.conf:/run/systemd/resolve/stub-resolv.conf
BindReadOnlyPaths=/usr/local/share/teamchat/resolv.conf:/run/systemd/resolve/resolv.conf
InaccessiblePaths=-/run/dbus
`

// notSandbox are the [Service] keys n2g-D17 keeps per unit: the lifecycle, and the credential
// and environment, whose paths differ between the server and the staging unit.
var notSandbox = []string{
	"Type", "NotifyAccess", "WatchdogSec", "TimeoutAbortSec", "TimeoutStopSec", "Restart",
	"RestartSec", "RestartSteps", "RestartMaxDelaySec", "LoadCredential", "Environment",
}

// keyOf is a unit line's key, the text before its first =.
func keyOf(line string) string { return strings.SplitN(line, "=", 2)[0] }

// n2g-D17: BaseSandbox is exactly C 3.2.3's [Service] lines less the lifecycle, the credential
// and the environment, in order.
func TestBaseSandboxIsTheBaseUnitsSandbox(t *testing.T) {
	unitSection, service, found := strings.Cut(baseUnitText, "[Service]\n")
	if !found || !strings.HasPrefix(unitSection, "[Unit]\n") {
		t.Fatal("not a [Unit] section then a [Service] section")
	}
	var want []string
	for line := range strings.SplitSeq(strings.TrimSpace(service), "\n") {
		if !slices.Contains(notSandbox, keyOf(line)) {
			want = append(want, line)
		}
	}
	base := BaseSandbox()
	if !slices.Equal(base, want) {
		t.Fatalf("BaseSandbox =\n%s\nwant\n%s", strings.Join(base, "\n"), strings.Join(want, "\n"))
	}
	if base[0] != "DynamicUser=yes" || base[len(base)-1] != "InaccessiblePaths=-/run/dbus" {
		t.Fatalf("BaseSandbox runs from %q to %q", base[0], base[len(base)-1])
	}
	base[0] = "changed"
	if BaseSandbox()[0] != "DynamicUser=yes" {
		t.Fatal("a caller's change reached BaseSandbox")
	}
}

// RU1: the team's sandbox names the team where the template has %i, and holds no specifier, no
// lifecycle line, no credential and no environment.
func TestTeamSandbox(t *testing.T) {
	lines := TeamSandbox(ops(t))
	if len(lines) != len(BaseSandbox()) {
		t.Fatalf("%d lines, want %d", len(lines), len(BaseSandbox()))
	}
	for _, want := range []string{"User=teamchat-ops", "StateDirectory=teamchat/ops", "IPAddressDeny=any", "SocketBindDeny=any", "UMask=0077"} {
		if !slices.Contains(lines, want) {
			t.Errorf("no %q in %q", want, lines)
		}
	}
	for _, line := range lines {
		if strings.Contains(line, "%") || slices.Contains(notSandbox, keyOf(line)) {
			t.Errorf("%q does not belong in a team's sandbox", line)
		}
	}
}
