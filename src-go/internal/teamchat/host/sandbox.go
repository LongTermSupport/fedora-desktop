package host

import (
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/v1names"
)

// baseSandbox is the identity, hardening and deny lines of C 3.2.3's base unit, in its order
// (n2g-D17), ending with the v1 paths no v2 unit may reach (D 4.2, from v1names). The base unit's
// template is to be rendered from it (gate A), so the restore's staging unit cannot drift from it.
// The lifecycle lines stay per unit, since the base unit's Restart=always would let a staging
// restart wipe the secured copy; so do the credential and the environment, whose paths differ.
//
// It has no PrivatePIDs=: systemd cannot start one with DynamicUser=yes and a StateDirectory=
// (pidns-idmapped-state, CLAUDE/QA.md; systemd issue 41014). Each team's homeserver is its own
// dynamic user, and ProtectProc=invisible hides every other process from it, so a PID namespace
// would add little; the TLS terminator, which keeps no state, adds it back.
var baseSandbox = []string{
	"DynamicUser=yes",
	"User=" + paths.Product + "-%i",
	"StateDirectory=" + paths.ServerStateDirectory,
	"StateDirectoryMode=0700",
	"UMask=0077",
	"NoNewPrivileges=yes",
	"CapabilityBoundingSet=",
	"ProtectSystem=strict",
	"ProtectHome=yes",
	"PrivateTmp=yes",
	"PrivateDevices=yes",
	"PrivateIPC=yes",
	"ProtectProc=invisible",
	"ProcSubset=pid",
	"ProtectKernelTunables=yes",
	"ProtectKernelModules=yes",
	"ProtectKernelLogs=yes",
	"ProtectControlGroups=yes",
	"ProtectClock=yes",
	"ProtectHostname=yes",
	"RestrictNamespaces=yes",
	"RestrictRealtime=yes",
	"RestrictSUIDSGID=yes",
	"LockPersonality=yes",
	"SystemCallArchitectures=native",
	"SystemCallFilter=@system-service",
	"RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX",
	"IPAccounting=yes",
	"IPAddressDeny=any",
	"SocketBindDeny=any",
	// A directory over the whole of /run/systemd/resolve, not a read-only tmpfs with the stub bound
	// onto its two files: systemd cannot make a file mount point inside a read-only tmpfs (226/NAMESPACE).
	"BindReadOnlyPaths=" + paths.ResolverDir + ":/run/systemd/resolve",
	"InaccessiblePaths=-/run/dbus",
	v1names.InaccessiblePaths,
}

// BaseSandbox is the base unit's sandbox lines as the template has them, with %i for the team:
// a copy, so no caller can change it.
func BaseSandbox() []string { return append([]string(nil), baseSandbox...) }

// TeamSandbox is BaseSandbox for one team, %i replaced by its id, for a transient unit, whose
// properties systemd does not expand (RU1).
func TeamSandbox(t paths.TeamID) []string {
	lines := BaseSandbox()
	for i, line := range lines {
		lines[i] = strings.ReplaceAll(line, "%i", t.String())
	}
	return lines
}
