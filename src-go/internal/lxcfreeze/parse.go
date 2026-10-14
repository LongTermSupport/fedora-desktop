package lxcfreeze

import (
	"net/netip"
	"strings"
)

// The labels shown where a real value could not be. Each contains whitespace, which a Linux
// interface name cannot, so filtering on these exact strings is safe.
const (
	bridgeNone       = "(no network)"
	bridgeUnreadable = "(could not read)"
	ipv4Unreadable   = "(unknown)"
)

// tableColWidth is the width of both engine columns; it must exceed the longest label (16).
const tableColWidth = 18

const (
	stateRunning = "RUNNING"
	stateFrozen  = "FROZEN"
	stateStopped = "STOPPED"

	blanks = " \t\r\v\f"
)

// parseState is the state word lxc-info -s reports: the value of the first line that begins (after
// optional blanks) with "State:", if it is one of the three words this tool knows. Anything else,
// including no output at all, is "no state": silence must never resolve to a state.
func parseState(raw string) string {
	for line := range strings.SplitSeq(raw, "\n") {
		rest, found := strings.CutPrefix(strings.TrimLeft(line, blanks), "State:")
		if !found {
			continue
		}
		switch value := strings.Trim(rest, blanks); value {
		case stateRunning, stateFrozen, stateStopped:
			return value
		}
		return ""
	}
	return ""
}

// parseBridge is the value of the first lxc.net.0.link key of a container config, trimmed. A
// commented key, a later interface and the pre-2.1 spelling do not match, and an include is not
// followed (a container whose network lives in an included file reads as having none).
func parseBridge(raw string) string {
	for line := range strings.SplitSeq(raw, "\n") {
		rest, found := strings.CutPrefix(strings.TrimLeft(line, blanks), "lxc.net.0.link")
		if !found {
			continue
		}
		value, isAssign := strings.CutPrefix(strings.TrimLeft(rest, blanks), "=")
		if isAssign {
			return strings.Trim(value, blanks)
		}
	}
	return ""
}

// parseIPv4 is the first IPv4 address among the whitespace-separated words of lxc-info -iH. An
// IPv6 address, an error message and an out-of-range quad are not one.
func parseIPv4(raw string) string {
	for word := range strings.FieldsSeq(raw) {
		if addr, err := netip.ParseAddr(word); err == nil && addr.Is4() {
			return word
		}
	}
	return ""
}

// bridgeLabel is the BRIDGE cell: the bridge, or why there is none. A config that could not be
// read is its own answer and its text is never parsed.
func bridgeLabel(readable bool, raw string) string {
	if !readable {
		return bridgeUnreadable
	}
	if bridge := parseBridge(raw); bridge != "" {
		return bridge
	}
	return bridgeNone
}

// ipv4Label is the IPV4 cell. A readable probe with no address is BLANK (a RUNNING row with a
// blank address is a lease that expired during a freeze); a probe that failed is "(unknown)".
func ipv4Label(readable bool, raw string) string {
	if !readable {
		return ipv4Unreadable
	}
	return parseIPv4(raw)
}
