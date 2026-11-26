// Package unitfiles is every file the host play installs for agent bus v2 under systemd's
// directory and the shared directory, each mode 0644 root (Plan 00246 INSTALL-PATH.md, stage A):
// the base units of a team's homeserver (DESIGN-v2-C-host.md 3.2.3) and of its TLS terminator
// (SPEC-cross-network X18a), the health and backup units and timers (C 3.11.1, 3.12.1), and the
// homeserver's resolver, two files in the directory its unit binds, and the two units that start
// the LXC seats again after a boot (Plan 00266 C.2). Go is their one source: `teamchat-admin units print` writes the
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
// read-only under ProtectSystem=strict. The IP filter allows loopback here; the drop-in team apply
// and team remove render (paths.HealthDropInFile, host.HealthDropIn) widens it to every applied
// team's listen addresses, which the reach probe dials (S13, Plan 00257 HOST-1).
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
# team apply renders teamchat-health.service.d/teams.conf with IPAddressAllow= lines (every
# applied team's listen addresses, which the reach probe dials): systemd adds each to this list,
# and the deny of any stays
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

// NoContainerStartFlag is `units print`'s one flag: the file as a host whose own IaC starts its
// LXC containers installs it (paths.HostVarLXCStartsContainers false, Plan 00266 C.2).
const NoContainerStartFlag = "--no-container-start"

// lxcService is LXC's own service, which starts the containers marked to start with the host and
// stops every container at shutdown.
const lxcService = "lxc.service"

// recordUnit is paths.LXCRecordService (Plan 00266 C.2): nothing at boot, and at shutdown the
// containers running then, which the boot unit starts at the next boot. A unit ordered after
// another stops before it, so this one records while lxc.service still runs the containers. It
// stays active once started (an inactive unit runs no ExecStop), and its own unit, not the boot
// unit, takes the record: a boot unit that failed is inactive, and would leave the last record.
const recordUnit = `[Unit]
Description=Record the LXC containers running at shutdown, which ` + paths.LXCBootService + ` starts at the next boot
# stops before lxc.service stops the containers: a unit ordered after another stops before it
After=` + lxcService + `

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStop=` + paths.AdminBinaryPath + ` lxc record-running
TimeoutStopSec=120
` + v1names.InaccessiblePaths + `

[Install]
WantedBy=multi-user.target
`

// bootUnit is paths.LXCBootService (Plan 00266 C.2): at boot, the containers that ran at the last
// shutdown started (when startContainers), then every recorded seat in a running container that
// is down started again. Both are the one command `lxc boot`, which reconciles even after a
// refused container start: a second ExecStart= would never run after a failed first one. Without
// the start it is `lxc reconcile`. It runs after multi-user.target, which wants it, so its start
// never holds boot (unit-holds-boot-target), and it is bounded. Its private mount namespace (the
// v1 paths made inaccessible) is no container's: the start has the host systemd start each one as
// its lxc@ instance.
func bootUnit(startContainers bool) string {
	what, verb := "the LXC seats that are down again", "reconcile"
	if startContainers {
		what, verb = "the LXC containers that ran at the last shutdown, then their seats", "boot"
	}
	return "[Unit]\n" +
		"Description=After a boot, start " + what + "\n" +
		"# after multi-user.target, which wants it, so its start never holds boot\n" +
		"After=" + lxcService + " multi-user.target\n" +
		"\n[Service]\n" +
		"Type=oneshot\n" +
		"ExecStart=" + paths.AdminBinaryPath + " lxc " + verb + "\n" +
		"TimeoutStartSec=30min\n" +
		v1names.InaccessiblePaths + "\n" +
		"\n[Install]\n" +
		"WantedBy=multi-user.target\n"
}

// containerDropInName is the drop-in of LXC's own lxc@.service, by the unit it extends.
const containerDropInName = "lxc@.service.conf"

// containerDropIn is paths.LXCContainerDropInFile (Plan 00266 C.2): every lxc@ instance, which
// is how the boot unit starts a container, ordered before the record unit. systemd stops units in
// the reverse of that order, so at shutdown the record unit records while every instance runs.
const containerDropIn = `[Unit]
# stops after ` + paths.LXCRecordService + `, which records the running containers first
Before=` + paths.LXCRecordService + `
`

// Files is every file the play installs, in the order `units list` gives them, as a host whose
// boot unit starts its containers has them.
func Files() []File { return FilesFor(true) }

// FilesFor is Files with the boot unit's container start in (startContainers) or left out: the
// host variable teamchat_lxc_seat_restore_starts_containers, false where the host's own IaC starts its
// containers. No other file differs.
func FilesFor(startContainers bool) []File {
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
		{paths.LXCRecordService, paths.LXCRecordServiceFile, recordUnit},
		{paths.LXCBootService, paths.LXCBootServiceFile, bootUnit(startContainers)},
		{containerDropInName, paths.LXCContainerDropInFile, containerDropIn},
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

// Lookup is the file named name, as Files has it.
func Lookup(name string) (File, bool) { return LookupFor(name, true) }

// LookupFor is the file named name, as FilesFor(startContainers) has it.
func LookupFor(name string, startContainers bool) (File, bool) {
	files := FilesFor(startContainers)
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
