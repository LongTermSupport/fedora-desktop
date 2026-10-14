package lxcfreeze

// The parsers and the labels: lxcf_parse_state, lxcf_parse_bridge, lxcf_parse_ipv4,
// lxcf_bridge_label, lxcf_ipv4_label of the bash suite, with the table-driven shape of Go.

import (
	"strings"
	"testing"
)

type parseCase struct{ name, raw, want string }

func runParse(t *testing.T, parse func(string) string, cases []parseCase) {
	t.Helper()
	for _, c := range cases {
		if got := parse(c.raw); got != c.want {
			t.Errorf("%s: parse(%q) = %q, want %q", c.name, c.raw, got, c.want)
		}
	}
}

// lxc-info -n NAME -s: the shapes it emits.
func TestParseStateReadsWhatLxcInfoSays(t *testing.T) {
	runParse(t, parseState, []parseCase{
		{"a running container", "State:          RUNNING", "RUNNING"},
		{"a frozen container", "State:          FROZEN", "FROZEN"},
		{"a stopped container", "State:          STOPPED", "STOPPED"},
		{"a single space", "State: RUNNING", "RUNNING"},
		{"a tab", "State:\tRUNNING", "RUNNING"},
		{"trailing whitespace", "State:   RUNNING   ", "RUNNING"},
		{"leading whitespace on the key", "  State: FROZEN", "FROZEN"},
		{"amongst other lines", "Name: x\nState: FROZEN\nPID: 3", "FROZEN"},
	})
}

// Silence is not a state: a probe that could not answer must not resolve to a state, least of all
// to STOPPED, which would quietly drop the container.
func TestParseStateSilenceIsNotAState(t *testing.T) {
	runParse(t, parseState, []parseCase{
		{"no output at all", "", ""},
		{"whitespace only", "  \n\t ", ""},
		{"the error lxc-info gives for an unknown container", "lxc-info: nosuch: tools/lxc_info.c: main: 133 Container is not defined", ""},
		{"a state word this tool does not know", "State:          ABORTING", ""},
		{"the key without a value", "State:", ""},
		{"a different key entirely", "PID:            1234", ""},
		{"the first State line wins, known or not", "State: ABORTING\nState: RUNNING", ""},
	})
	if parseState("State:          RUNNING") == parseState("") {
		t.Error("a known state and a refusal must be distinct answers")
	}
}

func TestParseBridgeReadsTheContainersOwnConfig(t *testing.T) {
	runParse(t, parseBridge, []parseCase{
		{"the documented key", "lxc.net.0.link = lxcbr0", "lxcbr0"},
		{"no spaces around =", "lxc.net.0.link=lxcbr0", "lxcbr0"},
		{"amongst other keys", "lxc.uts.name = box\nlxc.net.0.type = veth\nlxc.net.0.link = virbr1\nlxc.net.0.flags = up", "virbr1"},
		{"a commented key is not a value", "#lxc.net.0.link = lxcbr0\nlxc.net.0.type = veth", ""},
		{"a commented key with a space", "# lxc.net.0.link = lxcbr0", ""},
		{"the first interface wins", "lxc.net.0.link = lxcbr0\nlxc.net.1.link = virbr1", "lxcbr0"},
		{"only a later interface has a link", "lxc.net.1.link = virbr1", ""},
		{"no network configured", "lxc.uts.name = box", ""},
		{"an empty config", "", ""},
		{"the pre-2.1 key is not honoured", "lxc.network.link = lxcbr0", ""},
		{"an include is not followed (documented limit)", "lxc.include = /usr/share/lxc/config/common.conf", ""},
		{"the first of repeated keys wins", "lxc.net.0.link = a\nlxc.net.0.link = b", "a"},
		{"surrounding whitespace is trimmed", "  lxc.net.0.link =   lxcbr0  ", "lxcbr0"},
		{"a longer key is another key", "lxc.net.0.link.extra = x", ""},
	})
}

// "No network" and "could not read" are not one answer.
func TestBridgeLabelKeepsTheThreeAnswersApart(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
		raw  string
		want string
	}{
		{"a readable config naming a bridge", true, "lxc.net.0.link = lxcbr0", "lxcbr0"},
		{"a readable config with no network", true, "lxc.uts.name = box", bridgeNone},
		{"an empty but readable config", true, "", bridgeNone},
		{"an unreadable config", false, "cat: …/config: Permission denied", bridgeUnreadable},
		{"unreadable wins over parseable content", false, "lxc.net.0.link = lxcbr0", bridgeUnreadable},
	}
	for _, c := range cases {
		if got := bridgeLabel(c.ok, c.raw); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if bridgeNone == bridgeUnreadable {
		t.Error("the two non-bridge labels are distinct")
	}
}

// Both labels share the table with real bridge names, and bridgeNames filters on exactly these
// strings: whitespace is what makes that safe, since an interface name cannot contain any.
func TestTheNonBridgeLabelsCannotCollideWithAnInterfaceName(t *testing.T) {
	for _, label := range []string{bridgeNone, bridgeUnreadable} {
		if !strings.ContainsAny(label, " \t") {
			t.Errorf("%q has no whitespace, so a bridge could legally be called this", label)
		}
	}
}

// lxc-info -n NAME -iH.
func TestParseIPv4(t *testing.T) {
	runParse(t, parseIPv4, []parseCase{
		{"one IPv4 line", "192.0.2.11", "192.0.2.11"},
		{"surrounding whitespace is trimmed", "  192.0.2.11  ", "192.0.2.11"},
		{"no address at all", "", ""},
		{"IPv6 first is skipped", "2001:db8::1\n192.0.2.11\n", "192.0.2.11"},
		{"IPv6 only is no IPv4", "2001:db8::1", ""},
		{"the first IPv4 of several", "192.0.2.11\n198.51.100.5\n", "192.0.2.11"},
		{"an error message is not an address", "lxc-info: container not running", ""},
		// Bug 18: the dotted-quad regex took 999.1.1.1, and -o -m1 could yield two matches.
		{"an out-of-range quad is not an address", "999.1.1.1\n192.0.2.11", "192.0.2.11"},
		{"only an out-of-range quad", "999.1.1.1", ""},
		{"two on one line: the first only", "192.0.2.11 198.51.100.5", "192.0.2.11"},
		{"an IPv4-mapped IPv6 address is IPv6", "::ffff:192.0.2.1", ""},
		{"an address glued to text is not bounded by whitespace", "inet192.0.2.11", ""},
		{"a dotted quad with a fifth group", "192.0.2.11.5", ""},
	})
}

// A failed probe is not a container with no address: "(unknown)" and blank are different answers,
// and the blank next to RUNNING is the expired-lease symptom the column exists for.
func TestIPv4LabelKeepsNoAddressApartFromCouldNotAsk(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
		raw  string
		want string
	}{
		{"an address is the address", true, "192.0.2.11", "192.0.2.11"},
		{"a readable probe with no address is BLANK", true, "", ""},
		{"a failed probe is not blank", false, "lxc-info: unrecognised option", ipv4Unreadable},
		{"a failed probe with empty output is not blank", false, "", ipv4Unreadable},
	}
	for _, c := range cases {
		if got := ipv4Label(c.ok, c.raw); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if ipv4Unreadable == "" {
		t.Error("the unreadable label is not itself blank")
	}
	if len(ipv4Unreadable) >= tableColWidth {
		t.Errorf("%q must fit the %d-wide column with a gap after it", ipv4Unreadable, tableColWidth)
	}
}
