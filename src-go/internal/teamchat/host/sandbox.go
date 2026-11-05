package host

import (
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// baseSandbox is the identity, hardening and deny lines of C 3.2.3's base unit, in its order
// (n2g-D17). The base unit's template is to be rendered from it (gate A), so the restore's
// staging unit cannot drift from it. The lifecycle lines stay per unit, since the base unit's
// Restart=always would let a staging restart wipe the secured copy; so do the credential and
// the environment, whose paths differ.
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
	"PrivatePIDs=yes",
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
	"TemporaryFileSystem=/run/systemd/resolve:ro",
	"BindReadOnlyPaths=" + paths.ResolverStub + ":/run/systemd/resolve/stub-resolv.conf",
	"BindReadOnlyPaths=" + paths.ResolverStub + ":/run/systemd/resolve/resolv.conf",
	"InaccessiblePaths=-/run/dbus",
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
