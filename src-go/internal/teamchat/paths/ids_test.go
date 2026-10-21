package paths

import (
	"errors"
	"strings"
	"testing"
)

func TestParseTeamIDAcceptsTheGrammar(t *testing.T) {
	for _, s := range []string{"a", "x1", "team-1", "a-", "abcdefghijklmnopqrstuv"} {
		id, err := ParseTeamID(s)
		if err != nil {
			t.Errorf("ParseTeamID(%q): %v", s, err)
			continue
		}
		if id.String() != s {
			t.Errorf("ParseTeamID(%q).String() = %q", s, id.String())
		}
	}
}

func TestParseTeamIDRefusesOutsideTheGrammar(t *testing.T) {
	for _, s := range []string{
		"", "1team", "-a", "Team", "a_b", "a.b", "a/b", "a b", "é",
		"abcdefghijklmnopqrstuvw", // 23 characters: one over A 2.2's 22
		"a\n",
	} {
		if _, err := ParseTeamID(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseTeamID(%q) = %v, want ErrInvalid", s, err)
		}
	}
}

// The homeserver's dynamic user is teamchat-<team>, which must fit systemd's 31-character user
// name for the longest id the grammar allows (A 2.2, C 3.2.3).
func TestLongestTeamIDFitsTheSystemdUserLimit(t *testing.T) {
	id := mustTeam(t, strings.Repeat("a", 22))
	if n := len(HomeserverUser(id)); n > 31 {
		t.Fatalf("HomeserverUser of a 22-character id is %d characters, over systemd's 31", n)
	}
}

func TestParseRunIDIsSixteenLowercaseHex(t *testing.T) {
	if _, err := ParseRunID("0123456789abcdef"); err != nil {
		t.Fatalf("a 16-hex run id: %v", err)
	}
	for _, s := range []string{"", "0123456789abcde", "0123456789abcdef0", "0123456789ABCDEF", "0123456789abcdeg"} {
		if _, err := ParseRunID(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseRunID(%q) = %v, want ErrInvalid", s, err)
		}
	}
}

func TestParseCheckoutKeyIsThirtyTwoLowercaseHex(t *testing.T) {
	if _, err := ParseCheckoutKey(strings.Repeat("0a", 16)); err != nil {
		t.Fatalf("a 32-hex checkout key: %v", err)
	}
	for _, s := range []string{"", strings.Repeat("a", 31), strings.Repeat("a", 33), strings.Repeat("A", 32)} {
		if _, err := ParseCheckoutKey(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseCheckoutKey(%q) = %v, want ErrInvalid", s, err)
		}
	}
}

func TestParseGuestUserIsAPortableUserName(t *testing.T) {
	for _, s := range []string{"a", "_svc", "dev-1", "u_2", strings.Repeat("a", 32)} {
		if _, err := ParseGuestUser(s); err != nil {
			t.Errorf("ParseGuestUser(%q): %v", s, err)
		}
	}
	for _, s := range []string{"", "1a", "-a", "A", "a.b", "a/b", strings.Repeat("a", 33)} {
		if _, err := ParseGuestUser(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseGuestUser(%q) = %v, want ErrInvalid", s, err)
		}
	}
}

// The identifiers the design gives no grammar for are held to one safe path segment.
func TestSegmentIdentifiersRefuseWhatCouldLeaveTheirDirectory(t *testing.T) {
	parsers := map[string]func(string) error{
		"SetID":         func(s string) error { _, err := ParseSetID(s); return err },
		"SessionName":   func(s string) error { _, err := ParseSessionName(s); return err },
		"KitDigest":     func(s string) error { _, err := ParseKitDigest(s); return err },
		"ServerVersion": func(s string) error { _, err := ParseServerVersion(s); return err },
	}
	for name, parse := range parsers {
		for _, s := range []string{"set-1", "0.4.5", "abc_DEF.9"} {
			if err := parse(s); err != nil {
				t.Errorf("%s(%q): %v", name, s, err)
			}
		}
		for _, s := range []string{"", ".", "..", "a/b", "a\x00b", "a\nb", "a b", "-a", strings.Repeat("a", 129)} {
			if err := parse(s); !errors.Is(err, ErrInvalid) {
				t.Errorf("%s(%q) = %v, want ErrInvalid", name, s, err)
			}
		}
	}
}

// A value that fails its grammar is never rendered, not even in the error (A 2.2).
func TestInvalidValueIsNotEchoed(t *testing.T) {
	_, err := ParseTeamID("Secret/Value")
	if err == nil || strings.Contains(err.Error(), "Secret") {
		t.Fatalf("error = %v; it must refuse without echoing the value", err)
	}
}

func TestParsePortIsOneTo65535(t *testing.T) {
	for _, n := range []int{1, 8448, 65535} {
		p, err := ParsePort(n)
		if err != nil || int(p) != n {
			t.Errorf("ParsePort(%d) = %d, %v", n, p, err)
		}
	}
	for _, n := range []int{0, -1, 65536} {
		if _, err := ParsePort(n); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParsePort(%d) = %v, want ErrInvalid", n, err)
		}
	}
}

func mustTeam(t *testing.T, s string) TeamID {
	t.Helper()
	id, err := ParseTeamID(s)
	if err != nil {
		t.Fatalf("ParseTeamID(%q): %v", s, err)
	}
	return id
}
