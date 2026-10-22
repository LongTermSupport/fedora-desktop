package restore

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// sandbox is C 3.2.3's identity, hardening and deny lines for ops, as host.BaseSandbox gives
// them (n2g-D17), abridged.
func sandbox() []string {
	return []string{
		"DynamicUser=yes", "User=teamchat-ops", "StateDirectory=teamchat/ops", "StateDirectoryMode=0700", "UMask=0077",
		"NoNewPrivileges=yes", "CapabilityBoundingSet=", "PrivatePIDs=yes", "IPAddressDeny=any", "SocketBindDeny=any",
		"InaccessiblePaths=-/run/dbus",
	}
}

func staging(t *testing.T) Staging {
	t.Helper()
	v, err := paths.ParseServerVersion("1.9.4")
	if err != nil {
		t.Fatal(err)
	}
	return Staging{Team: opsTeam(t), Backup: 7, Version: v, Port: 8448, Sandbox: sandbox()}
}

// properties are the argv's --property= values.
func properties(argv []string) []string {
	var out []string
	for _, a := range argv {
		if p, ok := strings.CutPrefix(a, "--property="); ok {
			out = append(out, p)
		}
	}
	return out
}

// RU1: the staging unit's argv: its name, the transient flags, every sandbox line, its own
// lifecycle with Restart=no and no other Restart line, its network and credential, and the
// binary with --restore-backup N only when restoring.
func TestStagingArgv(t *testing.T) {
	s := staging(t)
	argv, err := StagingArgv(s, true)
	if err != nil {
		t.Fatal(err)
	}
	head := []string{"/usr/bin/systemd-run", "--unit=teamchat-restore@ops.service", "--collect", "--service-type=notify", "--quiet"}
	if !slices.Equal(argv[:len(head)], head) {
		t.Errorf("head %q", argv[:len(head)])
	}
	props := properties(argv)
	for _, want := range append(sandbox(),
		"Restart=no", "NotifyAccess=main", "TimeoutStartSec=15min", "TimeoutStopSec=330", "PrivateNetwork=yes",
		"LoadCredential=config.toml:/var/lib/teamchat-admin/ops/config.restore.toml",
		"Environment=TUWUNEL_CONFIG=/run/credentials/teamchat-restore@ops.service/config.toml",
		"IPAddressAllow=127.0.0.1/32 ::1/128", "SocketBindAllow=tcp:8448") {
		if !slices.Contains(props, want) {
			t.Errorf("no --property=%s", want)
		}
	}
	restarts := slices.DeleteFunc(slices.Clone(props), func(p string) bool { return !strings.HasPrefix(p, "Restart") })
	if !slices.Equal(restarts, []string{"Restart=no"}) {
		t.Errorf("Restart lines %q, want Restart=no alone", restarts)
	}
	tail := []string{"--", "/usr/local/lib/teamchat/server/tuwunel-1.9.4", "--restore-backup", "7"}
	if !slices.Equal(argv[len(argv)-len(tail):], tail) {
		t.Errorf("tail %q", argv[len(argv)-len(tail):])
	}
	plain, err := StagingArgv(s, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(plain, argv[:len(argv)-2]) {
		t.Errorf("the verify's argv is not the same without --restore-backup:\n%q\n%q", plain, argv)
	}
}

// RU1, n2g-D17: a sandbox that lacks the identity lines, or carries a lifecycle, credential,
// environment or command line, or a specifier a transient unit does not expand, is refused.
func TestStagingArgvRefusesASandbox(t *testing.T) {
	for name, edit := range map[string]func([]string) []string{
		"no DynamicUser":    func(s []string) []string { return drop(s, "DynamicUser=yes") },
		"another user":      func(s []string) []string { return append(drop(s, "User=teamchat-ops"), "User=teamchat-dev") },
		"no StateDirectory": func(s []string) []string { return drop(s, "StateDirectory=teamchat/ops") },
		"a Restart line":    func(s []string) []string { return append(s, "Restart=always") },
		"a RestartSec":      func(s []string) []string { return append(s, "RestartSec=5s") },
		"a Type":            func(s []string) []string { return append(s, "Type=notify") },
		"a timeout":         func(s []string) []string { return append(s, "TimeoutStopSec=330") },
		"a watchdog":        func(s []string) []string { return append(s, "WatchdogSec=30s") },
		"a credential":      func(s []string) []string { return append(s, "LoadCredential=x:/y") },
		"an environment":    func(s []string) []string { return append(s, "Environment=A=b") },
		"a command":         func(s []string) []string { return append(s, "ExecStart=/bin/true") },
		"a specifier":       func(s []string) []string { return append(s, "ReadOnlyPaths=%d") },
		"not a line":        func(s []string) []string { return append(s, "NoEquals") },
		"a newline":         func(s []string) []string { return append(s, "A=b\nC=d") },
	} {
		s := staging(t)
		s.Sandbox = edit(sandbox())
		if _, err := StagingArgv(s, true); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	s := staging(t)
	s.Port = 0
	if _, err := StagingArgv(s, true); err == nil {
		t.Error("port 0 accepted")
	}
}

func drop(list []string, item string) []string {
	return slices.DeleteFunc(list, func(s string) bool { return s == item })
}

// RU2: the secure unit's argv, joined to the staging unit's network, as teamchat-ctl, with a
// cleared environment, and the request on stdin.
func TestSecureArgv(t *testing.T) {
	argv, err := SecureArgv(opsTeam(t), "/usr/local/lib/teamchat/teamchat-admin-abc")
	if err != nil {
		t.Fatal(err)
	}
	head := []string{"/usr/bin/systemd-run", "--unit=teamchat-restore-ctl@ops.service", "--pipe", "--wait", "--collect", "--quiet"}
	if !slices.Equal(argv[:len(head)], head) {
		t.Errorf("head %q", argv[:len(head)])
	}
	want := []string{
		"JoinsNamespaceOf=teamchat-restore@ops.service", "PrivateNetwork=yes", "User=teamchat-ctl", "Group=teamchat-ctl",
		"WorkingDirectory=/", "NoNewPrivileges=yes", "IPAddressDeny=any", "IPAddressAllow=127.0.0.1/32 ::1/128",
	}
	if got := properties(argv); !slices.Equal(got, want) {
		t.Errorf("properties %q, want %q", got, want)
	}
	tail := []string{"--", "/usr/bin/env", "-i", "PATH=/usr/bin:/bin", "/usr/local/lib/teamchat/teamchat-admin-abc", "ctl-child"}
	if !slices.Equal(argv[len(argv)-len(tail):], tail) {
		t.Errorf("tail %q", argv[len(argv)-len(tail):])
	}
	for _, self := range []string{"", "teamchat-admin", "/a/../b", "/a b"} {
		if _, err := SecureArgv(opsTeam(t), self); err == nil {
			t.Errorf("self %q accepted", self)
		}
	}
}

// RU3: a unit's systemd-run runs with a clean environment of PATH and LANG alone, and no secret
// is in either argv (the config is a credential file, the request is on stdin).
func TestUnitCommand(t *testing.T) {
	argv, err := StagingArgv(staging(t), true)
	if err != nil {
		t.Fatal(err)
	}
	c := unitCommand(argv, nil, nil)
	if c.Name != argv[0] || !slices.Equal(c.Args, argv[1:]) || !c.CleanEnv || !slices.Equal(c.Env, []string{"PATH=/usr/bin:/bin", "LANG=C"}) {
		t.Errorf("command %+v", c)
	}
	for _, a := range argv {
		if strings.Contains(a, "registration_shared_secret") || strings.Contains(a, "token") {
			t.Errorf("argv element %q looks like a secret", a)
		}
	}
}
