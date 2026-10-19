package bashlimits

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// DaemonOwnedFiles are single files outside the daemon's own trees that the hooks daemon
// deploys and overwrites on every upgrade, so their size is the daemon's to choose and they
// must not be edited here. Each entry is one exact path, never a pattern, with the reason it is
// here. It is the one way a file on the ratchet allowlist may leave it while the file lives on,
// and so it is held to proof: the gate counts an entry daemon-owned only while a hooks-daemon
// upgrade commit in the history names the file in its UPGRADE_METADATA modified_files list
// (VerifyDaemonOwned). A file only named like one of these, or named by an upgrade but not
// listed here, is measured as any other.
var DaemonOwnedFiles = []Exemption{
	{"CLAUDE/Plan/mkplan.bash", "daemon-owned plan scaffolder: the hooks daemon deploys it from its install/templates/mkplan.bash and overwrites it on every upgrade, so it must not be edited here"},
}

// upgradeMetadata are the markers around the metadata block of a hooks-daemon upgrade commit,
// and the key that lists the files the upgrade wrote.
const (
	upgradeMetadataOpen  = "<<<UPGRADE_METADATA"
	upgradeMetadataClose = "UPGRADE_METADATA>>>"
	modifiedFilesKey     = "modified_files="
)

// IsDaemonOwnedFile reports whether p is exactly one of DaemonOwnedFiles.
func IsDaemonOwnedFile(p string) bool {
	return daemonOwnedReason(p) != ""
}

func daemonOwnedReason(p string) string {
	for _, e := range DaemonOwnedFiles {
		if e.Pattern == p {
			return e.Reason
		}
	}
	return ""
}

// ParseUpgradeMetadata is the set of files named by the modified_files line of every
// UPGRADE_METADATA block in log, the NUL-separated bodies of commit messages. A modified_files
// line outside a block is not metadata and names nothing.
func ParseUpgradeMetadata(log string) map[string]bool {
	shipped := map[string]bool{}
	for body := range strings.SplitSeq(log, "\x00") {
		inBlock := false
		for line := range strings.SplitSeq(body, "\n") {
			line = strings.TrimSpace(line)
			switch {
			case line == upgradeMetadataOpen:
				inBlock = true
			case line == upgradeMetadataClose:
				inBlock = false
			case inBlock && strings.HasPrefix(line, modifiedFilesKey):
				for p := range strings.SplitSeq(strings.TrimPrefix(line, modifiedFilesKey), ",") {
					if p = strings.TrimSpace(p); p != "" {
						shipped[p] = true
					}
				}
			}
		}
	}
	return shipped
}

// VerifyDaemonOwned keeps in scan.DaemonOwned only the files a hooks-daemon upgrade shipped.
// Each other one is a finding, and losing its standing it is held as any exempt file: an
// allowlist entry for it cannot be dropped.
func VerifyDaemonOwned(scan *Scan, shipped map[string]bool) []Finding {
	var out []Finding
	for _, p := range slices.Sorted(maps.Keys(scan.DaemonOwned)) {
		if shipped[p] {
			continue
		}
		delete(scan.DaemonOwned, p)
		out = append(out, Finding{p, KindUnshipped, fmt.Sprintf("%s: listed daemon-owned (DaemonOwnedFiles, src-go/qa/bashlimits/daemonowned.go), but no hooks-daemon upgrade commit names it in its %s list; a file is daemon-owned only if the daemon ships it", p, strings.TrimSuffix(modifiedFilesKey, "="))})
	}
	return out
}
