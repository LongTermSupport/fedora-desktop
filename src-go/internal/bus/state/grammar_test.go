package state

import (
	"strings"
	"testing"
)

const (
	testEvent = "$" + "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
	testRoom  = "!" + "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
	testAgent = "@myrepo.1+workstation.podman:alpha.example.org"
)

// The protocol's grammars, as protocol.py writes them (section 10, until bus/proto lands).
func TestGrammarsAreProtocolPys(t *testing.T) {
	checks := []struct {
		name string
		fn   func(string) bool
		good []string
		bad  []string
	}{
		{"team", isTeamName, []string{"a", "alpha-2", "a" + strings.Repeat("b", 23)}, []string{"", "A", "1a", "a_b", "a" + strings.Repeat("b", 24)}},
		{"seat", isSeatName, []string{"1", "999999", "a", "a12345678901"}, []string{"0", "1000000", "a123456789012", "-", "A"}},
		{"handle", isHandle, []string{"myrepo.1+workstation.podman", "r.ab+h.host"}, []string{"myrepo.1+workstation.pod", "Myrepo.1+h.lxc", "r.0+h.vm", "r.1+-h.vm"}},
		{"human", isHumanLocalpart, []string{"alice", "a_b-c"}, []string{"admin", "conduit", "Alice", "1a"}},
		{"room", isRoomID, []string{testRoom}, []string{testRoom[:43], testEvent}},
		{"event", isEventID, []string{testEvent}, []string{testEvent + "a", testRoom}},
		{"user text", isUserIDText, []string{"@a:b", "@ab:c", "@" + strings.Repeat("x", 252) + ":y"}, []string{"@:b", "a:b", "@a b:c", "@ab", "@" + strings.Repeat("x", 253) + ":y"}},
		{"token", isPrintableToken, []string{"a", "!~"}, []string{"", "a b", "a\n", "é"}},
		{"reason", isReason, []string{"a b", strings.Repeat("r", 200)}, []string{"", strings.Repeat("r", 201), "a\tb", "é"}},
		{"description", isDescription, []string{"x", "a b", "\xed\xa0\x80", strings.Repeat("é", 120)}, []string{"", " a", "a ", "a" + string(rune(0x200b)) + "b", "a\nb", strings.Repeat("é", 121), "a" + string(rune(0xa0))}},
		{"verb", isAckVerb, []string{"fetch", "sync", "review", "run-qa", "halt"}, []string{"ack", "nack", "done", "blocked", "x"}},
		{"drop", isDropReason, []string{"version", "provenance"}, []string{"not-addressed", "x"}},
		{"unheard", isUnheardReason, []string{"not-addressed"}, []string{"schema"}},
	}
	for _, c := range checks {
		for _, s := range c.good {
			if !c.fn(s) {
				t.Errorf("%s refuses %.30q", c.name, s)
			}
		}
		for _, s := range c.bad {
			if c.fn(s) {
				t.Errorf("%s accepts %.30q", c.name, s)
			}
		}
	}
}

func TestUserIDIsParseUserIDOnItsServer(t *testing.T) {
	cases := map[string]string{
		testAgent:                    "myrepo.1+workstation.podman",
		"@admin:alpha.example.org":   "admin",
		"@alice:alpha.example.org":   "alice",
		"@alice:beta.example.org":    "",
		"@Alice:alpha.example.org":   "",
		"alice:alpha.example.org":    "",
		"@alice":                     "",
		"@alice:alpha.example.org:1": "",
	}
	for in, want := range cases {
		got, ok := parseUserID(in, "alpha.example.org")
		if got != want || ok != (want != "") {
			t.Errorf("%s: %q %v, want %q", in, got, ok, want)
		}
	}
	if !isAgentUserID(testAgent, "alpha.example.org") || isAgentUserID("@alice:alpha.example.org", "alpha.example.org") {
		t.Error("isAgentUserID")
	}
}
