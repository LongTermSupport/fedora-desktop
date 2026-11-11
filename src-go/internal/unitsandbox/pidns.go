package unitsandbox

import (
	"fmt"
	"strings"
)

// PIDNamespaceID is the rule pidns-idmapped-state (CLAUDE/QA.md): no systemd unit sets
// DynamicUser=yes, a StateDirectory= and PrivatePIDs=yes together. Since systemd 257 a dynamic
// user's state directory is an idmapped mount, and with PrivatePIDs= systemd forks into the new
// PID namespace before it writes the idmap's uid_map by pid through the host's /proc, which the
// kernel refuses: the unit exits 226/NAMESPACE (systemd issue 41014, open). Any two of the three
// start.
const PIDNamespaceID = "pidns-idmapped-state"

// PIDFinding is a unit text that sets all three, at the line of its PrivatePIDs=.
type PIDFinding struct{ Line int }

func (f PIDFinding) String() string {
	return fmt.Sprintf("%s: line %d sets PrivatePIDs=yes with DynamicUser=yes and a StateDirectory=; "+
		"systemd cannot start the three together (systemd issue 41014, 226/NAMESPACE): drop PrivatePIDs=", PIDNamespaceID, f.Line)
}

// CheckPIDNamespace is the finding in text, if it sets the three, each by its last assignment as
// systemd reads them; it reads the line forms Check reads.
func CheckPIDNamespace(text string) []PIDFinding {
	var dynamic, state, pids bool
	pidsLine := 0
	for i, raw := range strings.Split(text, "\n") {
		key, value, ok := keyValue(raw)
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "DynamicUser":
			dynamic = systemdTrue(value)
		case "StateDirectory":
			state = value != ""
		case "PrivatePIDs":
			pids, pidsLine = systemdTrue(value), i+1
		}
	}
	if dynamic && state && pids {
		return []PIDFinding{{Line: pidsLine}}
	}
	return nil
}

// systemdTrue is whether value is one of systemd's spellings of true.
func systemdTrue(value string) bool {
	switch strings.ToLower(value) {
	case "1", "yes", "y", "true", "t", "on":
		return true
	}
	return false
}
