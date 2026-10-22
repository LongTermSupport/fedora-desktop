package restore

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// systemdRun is the transient units' runner (RU1, RU2).
const systemdRun = "/usr/bin/systemd-run"

// loopbackAllow is what both units may reach: the staging server's own loopback.
const loopbackAllow = "IPAddressAllow=127.0.0.1/32 ::1/128"

// Staging is what the staging unit is made from (RU1): the team, the backup, the server
// version units.json names, the team's port, and host.BaseSandbox's lines for the team.
type Staging struct {
	Team    paths.TeamID
	Backup  int64
	Version paths.ServerVersion
	Port    int
	Sandbox []string
}

// lifecycleKeys are the directives the staging unit sets itself, so the sandbox may not carry
// them (n2g-D17): a base unit's Restart=always would let a staging restart wipe the secured
// copy.
var lifecycleKeys = []string{"Type", "NotifyAccess", "WatchdogSec", "LoadCredential", "Environment", "ExecStart"}

// StagingArgv is the staging unit's systemd-run argv (RU1); restoring adds --restore-backup N,
// and RS3's verify runs the same argv without it.
func StagingArgv(s Staging, restoring bool) ([]string, error) {
	if err := checkSandbox(s.Team, s.Sandbox); err != nil {
		return nil, err
	}
	if s.Port < 1 || s.Port > 65535 || s.Backup < 1 {
		return nil, fmt.Errorf("restore: the staging unit of team %s needs a port and a backup number", s.Team)
	}
	unit := paths.RestoreUnit(s.Team)
	argv := []string{systemdRun, "--unit=" + unit, "--collect", "--service-type=notify", "--quiet"}
	props := slices.Concat(s.Sandbox, []string{
		"Restart=no", "NotifyAccess=main", "TimeoutStartSec=15min", "TimeoutStopSec=330", "PrivateNetwork=yes",
		"LoadCredential=config.toml:" + paths.RestoreConfig(s.Team),
		"Environment=TUWUNEL_CONFIG=" + paths.RestoreUnitCredential(s.Team),
		loopbackAllow, "SocketBindAllow=tcp:" + strconv.Itoa(s.Port),
	})
	for _, p := range props {
		argv = append(argv, "--property="+p)
	}
	argv = append(argv, "--", paths.ServerBinary(s.Version))
	if restoring {
		argv = append(argv, "--restore-backup", strconv.FormatInt(s.Backup, 10))
	}
	return argv, nil
}

// checkSandbox refuses a sandbox that is not C 3.2.3's identity, hardening and deny lines for
// team: each a Key=value line with no specifier, the identity lines present, no lifecycle,
// credential, environment or command line.
func checkSandbox(team paths.TeamID, lines []string) error {
	identity := []string{"DynamicUser=yes", "User=" + paths.HomeserverUser(team), "StateDirectory=" + strings.TrimPrefix(paths.HomeserverState(team), "/var/lib/")}
	for _, want := range identity {
		if !slices.Contains(lines, want) {
			return fmt.Errorf("restore: the base sandbox lacks %s", want)
		}
	}
	for _, line := range lines {
		key, _, ok := strings.Cut(line, "=")
		switch {
		case !ok || key == "" || strings.ContainsAny(line, "%\n\r"):
			return fmt.Errorf("restore: the base sandbox line %q is not one directive without a specifier", line)
		case strings.HasPrefix(key, "Restart") || strings.HasPrefix(key, "Timeout") || slices.Contains(lifecycleKeys, key):
			return fmt.Errorf("restore: the base sandbox holds %s, which the staging unit sets itself", key)
		case strings.HasPrefix(key, "User") && line != identity[1]:
			return fmt.Errorf("restore: the base sandbox's %s is not team %s's user", key, team)
		}
	}
	return nil
}

// SecureArgv is the secure unit's systemd-run argv (RU2): teamchat-admin's own copy self as
// the teamchat-ctl child in the staging unit's network, with a cleared environment; the
// request goes on stdin.
func SecureArgv(team paths.TeamID, self string) ([]string, error) {
	if !filepath.IsAbs(self) || filepath.Clean(self) != self || strings.ContainsAny(self, " \t\n") {
		return nil, errors.New("restore: the secure unit's command is not an absolute, clean path")
	}
	argv := []string{systemdRun, "--unit=" + paths.RestoreCtlUnit(team), "--pipe", "--wait", "--collect", "--quiet"}
	for _, p := range []string{
		"JoinsNamespaceOf=" + paths.RestoreUnit(team), "PrivateNetwork=yes", "User=" + paths.CtlUser, "Group=" + paths.CtlUser,
		"WorkingDirectory=/", "NoNewPrivileges=yes", "IPAddressDeny=any", loopbackAllow,
	} {
		argv = append(argv, "--property="+p)
	}
	return append(argv, "--", "/usr/bin/env", "-i", "PATH=/usr/bin:/bin", self, "ctl-child"), nil
}

// unitCommand is a unit's systemd-run through execx, with a clean environment (RU3).
func unitCommand(argv []string, stdin io.Reader, stdout io.Writer) execx.Cmd {
	return execx.Cmd{
		Name: argv[0], Args: argv[1:], CleanEnv: true, Env: []string{"PATH=/usr/bin:/bin", "LANG=C"},
		Stdin: stdin, Stdout: stdout,
	}
}
