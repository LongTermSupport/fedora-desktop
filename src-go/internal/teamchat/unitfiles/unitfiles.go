// Package unitfiles is every file the host play installs for agent bus v2 under systemd's
// directory and the shared directory, each mode 0644 root (Plan 00246 INSTALL-PATH.md, stage A):
// the base units of a team's homeserver (DESIGN-v2-C-host.md 3.2.3) and of its TLS terminator
// (SPEC-cross-network X18a), the health and backup units and timers (C 3.11.1, 3.12.1), and the
// homeserver's resolver, two files in the directory its unit binds. Go is their one source: `teamchat-admin units print` writes the
// text the play installs, so the bytes on a host are the bytes the unit gates judge
// (v1names' units test).
package unitfiles

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/backup"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/tlsterm"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/v1names"
)

// File is one installed file: its name, where the play puts it, and its text.
type File struct {
	Name, Path, Text string
}

// adminStateTemplate is a team's root-only state directory with %i for the team, as a template
// unit names it (paths.AdminState of a team).
const adminStateTemplate = paths.AdminStateRoot + "/%i"

// serverUnitSection is C 3.2.3's [Unit] section.
const serverUnitSection = "[Unit]\n" +
	"Description=v2 team homeserver %i\n" +
	"StartLimitIntervalSec=0\n" +
	"AssertPathExists=!" + adminStateTemplate + "/restore.block\n" +
	"# stop after the LXC members at shutdown, so their goodbyes reach a live server\n" +
	"Before=lxc.service\n"

// serverLifecycle is the server's [Service] lines before the sandbox: the lifecycle lines
// host.BaseSandbox leaves to each unit (n2g-D17).
const serverLifecycle = "Type=notify\n" +
	"NotifyAccess=main\n" +
	"WatchdogSec=30s\n" +
	"TimeoutAbortSec=30s\n" +
	"TimeoutStopSec=330\n" +
	restartLines

// restartLines are how both team units restart: always, and never given up on.
const restartLines = "Restart=always\n" +
	"RestartSec=5s\n" +
	"RestartSteps=5\n" +
	"RestartMaxDelaySec=60s\n"

// installLines end both team units: team apply enables each team's instances, and
// multi-user.target, which every boot reaches, then starts them (issue 247). Without them the
// units are static, enable does nothing, and a reboot leaves the team down.
const installLines = "\n[Install]\n" +
	"WantedBy=multi-user.target\n"

// serverCredential is the config, with its secret, and where Tuwunel is told to read it; C 3.2.3
// puts them after the identity lines, which end at StateDirectoryMode.
var serverCredential = []string{
	"LoadCredential=config.toml:" + adminStateTemplate + "/config.toml",
	"Environment=TUWUNEL_CONFIG=%d/config.toml",
}

// serverUnit is `teamchat-server@.service`: C 3.2.3's block, its sandbox host.BaseSandbox's.
func serverUnit() string {
	sandbox := host.BaseSandbox()
	at := slices.IndexFunc(sandbox, func(l string) bool { return strings.HasPrefix(l, "StateDirectoryMode=") }) + 1
	return serverUnitSection +
		"\n[Service]\n" +
		serverLifecycle +
		strings.Join(slices.Concat(sandbox[:at], serverCredential, sandbox[at:]), "\n") + "\n" +
		installLines
}

// terminatorFileLimit is the terminator's descriptor limit: each connection it holds is two
// descriptors, the client's and the homeserver's, beside its listeners, its standard streams and
// the Go runtime's own (X18a).
const terminatorFileLimit = 2*tlsterm.DefaultMaxConns + 64

// terminatorDropped are the keys of host.BaseSandbox the terminator does not take (X18a): its
// dynamic user is its own, not the homeserver's named one; it keeps no state; it resolves no
// name, so it has no resolver and no AF_UNIX (its own address family line replaces the base's).
var terminatorDropped = []string{"User=", "StateDirectory=", "StateDirectoryMode=", "BindReadOnlyPaths=", "RestrictAddressFamilies="}

// terminatorDrops is whether the terminator leaves a line of host.BaseSandbox out.
func terminatorDrops(line string) bool {
	return slices.ContainsFunc(terminatorDropped, func(key string) bool { return strings.HasPrefix(line, key) })
}

// terminatorUnit is `teamchat-tls@.service` (X18a): no ExecStart= and no User=, which the team's
// drop-in and DynamicUser= give; after the homeserver and wanting it, never bound to it, so a
// backup that stops the homeserver leaves the listeners up.
func terminatorUnit() string {
	server := strings.TrimSuffix(paths.ServerUnitTemplate, "@.service") + "@%i.service"
	var sandbox []string
	for _, line := range host.BaseSandbox() {
		switch {
		case strings.HasPrefix(line, "SocketBindDeny="):
			sandbox = append(sandbox, line, "LimitNOFILE="+strconv.Itoa(terminatorFileLimit))
		case strings.HasPrefix(line, "IPAccounting="):
			sandbox = append(sandbox, "RestrictAddressFamilies=AF_INET AF_INET6", line)
		case strings.HasPrefix(line, "PrivateDevices="):
			// Its own PID namespace, which the base lacks only for its state directory
			// (pidns-idmapped-state): the terminator keeps none.
			sandbox = append(sandbox, line, "PrivatePIDs=yes")
		case !terminatorDrops(line):
			sandbox = append(sandbox, line)
		}
	}
	return "[Unit]\n" +
		"Description=v2 team TLS terminator %i\n" +
		"StartLimitIntervalSec=0\n" +
		"After=" + server + "\n" +
		"Wants=" + server + "\n" +
		"# stop after the LXC members at shutdown, as the homeserver does: the homeserver listens on loopback\n" +
		"# alone, so a guest's member reaches it only through this unit, and its goodbyes need this unit up\n" +
		"Before=lxc.service\n" +
		"\n[Service]\n" +
		restartLines +
		strings.Join(sandbox, "\n") + "\n" +
		installLines
}

// healthService is `teamchat-health.service` (C 3.11.1). Beyond the design's text, the TLS state
// root is writable: the tick renews a team's leaf there as team apply writes it (S12, X31), and
// systemd takes no pattern for one directory per team. That root holds leaves alone, so the admin
// state, with the CA key the tick reads to sign and the approvals it must not change, stays
// read-only under ProtectSystem=strict. The IP filter is left for a drop-in to widen to the teams'
// listen addresses the reach probe dials (S13).
const healthService = `[Unit]
Description=Health of every ` + paths.Product + ` team

[Service]
Type=oneshot
ExecStart=` + paths.AdminBinaryPath + ` health --all
TimeoutStartSec=120
SyslogIdentifier=` + paths.SyslogHealth + `
NoNewPrivileges=yes
ProtectSystem=strict
# the TLS state root holds every team's leaf, which the tick renews as team apply makes it; the admin
# state (the CA key the tick signs with, the approvals) is read-only
ReadWritePaths=` + paths.StatusRoot + ` ` + paths.AdminRunDir + ` ` + paths.TLSStateRoot + `
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
CapabilityBoundingSet=CAP_DAC_READ_SEARCH CAP_CHOWN CAP_SETUID CAP_SETGID CAP_SYS_PTRACE
SystemCallFilter=~ptrace process_vm_readv process_vm_writev
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
# a drop-in may add IPAddressAllow= lines (the teams' listen addresses the reach probe dials):
# systemd adds each to this list, and the deny of any stays
IPAddressDeny=any
IPAddressAllow=localhost
` + v1names.InaccessiblePaths + "\n"

// healthTimer is `teamchat-health.timer` (C 3.11.1).
const healthTimer = `[Unit]
Description=Health ticks of every ` + paths.Product + ` team

[Timer]
OnActiveSec=30s
OnUnitActiveSec=60s
AccuracySec=5s

[Install]
WantedBy=timers.target
`

// resolverStub is the homeserver's resolver: the text of both files in paths.ResolverDir, which its
// unit binds over /run/systemd/resolve.
const resolverStub = `# The ` + paths.Product + ` homeserver's resolver (DESIGN-v2-C-host.md 3.2.3): its directory is bound over
# /run/systemd/resolve inside ` + paths.ServerUnitTemplate + `. Nothing listens on this address and the unit's IP
# filter allows it only on loopback, so every lookup fails inside the host.
nameserver 127.0.0.1
`

// tmpfilesText makes, at every boot and when the play runs systemd-tmpfiles, the two directories
// a unit writes that nothing else makes: the lock directory under /run, which a reboot empties,
// and the leaves' root. A unit that writes a directory that is not there fails its namespace setup,
// so the units name these without a "-" and this file makes them. Each line is a root-only
// directory: type, path, mode, user, group, and no age.
func tmpfilesText() string {
	var out strings.Builder
	out.WriteString("# The directories the " + paths.Product + " units write that nothing else makes (Plan 00246 INSTALL-PATH.md).\n")
	for _, dir := range []string{paths.AdminRunDir, paths.TLSStateRoot} {
		out.WriteString(strings.Join([]string{"d", dir, "0700", "root", "root", "-"}, " ") + "\n")
	}
	return out.String()
}

// Files is every file the play installs, in the order `units list` gives them.
func Files() []File {
	return []File{
		{paths.ServerUnitTemplate, paths.ServerUnitTemplateFile, serverUnit()},
		{paths.TLSUnitTemplate, paths.TLSUnitTemplateFile, terminatorUnit()},
		{paths.HealthService, paths.HealthServiceFile, healthService},
		{paths.HealthTimer, paths.HealthTimerFile, healthTimer},
		{paths.BackupService, paths.BackupServiceFile, backup.ServiceUnit},
		{paths.BackupTimer, paths.BackupTimerFile, backup.TimerUnit},
		{filepath.Base(paths.ResolverStubFile), paths.ResolverStubFile, resolverStub},
		{filepath.Base(paths.ResolverFile), paths.ResolverFile, resolverStub},
		{filepath.Base(paths.TmpfilesFile), paths.TmpfilesFile, tmpfilesText()},
	}
}

// Names is the files' names, in Files' order.
func Names() []string {
	files := Files()
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name)
	}
	return names
}

// Lookup is the file named name.
func Lookup(name string) (File, bool) {
	files := Files()
	i := slices.IndexFunc(files, func(f File) bool { return f.Name == name })
	if i < 0 {
		return File{}, false
	}
	return files[i], true
}

// ExecBinaries is the binary each ExecStart= line of a unit text names, in order: the first word
// of the command, its systemd prefixes (-, @, :, +, !) taken off. An empty ExecStart=, which
// resets the list, names none. A test that every binary a unit names is one the play installs
// reads it, for these texts and for a team's drop-ins (host.DropIns).
func ExecBinaries(text string) []string {
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found || strings.TrimSpace(key) != "ExecStart" {
			continue
		}
		if fields := strings.Fields(strings.TrimLeft(strings.TrimSpace(value), "-@:+!")); len(fields) > 0 {
			out = append(out, fields[0])
		}
	}
	return out
}
