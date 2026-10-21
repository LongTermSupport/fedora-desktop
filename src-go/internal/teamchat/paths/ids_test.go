package paths

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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

// A team id that would make a team-derived name equal a fixed one is reserved: team "ctl"'s
// homeserver user would be teamchat-ctl, the user holding every team's admin token.
func TestReservedTeamIDsAreRefused(t *testing.T) {
	for _, s := range reservedTeamIDs {
		if _, err := ParseTeamID(s); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("ParseTeamID(%q) = %v, want a reserved refusal", s, err)
		}
	}
	if _, err := ParseTeamID("ctl-1"); err != nil {
		t.Errorf("an id that only begins with a reserved one is refused: %v", err)
	}
}

// The display name is A 2.2's: 1-64 Unicode scalar values, no control, format or newline
// characters.
func TestParseDisplayName(t *testing.T) {
	for _, s := range []string{"Ops", "Équipe 2", strings.Repeat("é", 64)} {
		if _, err := ParseDisplayName(s); err != nil {
			t.Errorf("ParseDisplayName(%q): %v", s, err)
		}
	}
	for _, s := range []string{"", "a\nb", "a\tb", "a\u200bb", "a\x7fb", strings.Repeat("a", 65), "\xff"} {
		if _, err := ParseDisplayName(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseDisplayName(%q) = %v, want ErrInvalid", s, err)
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
		if err != nil || p.Int() != n {
			t.Errorf("ParsePort(%d) = %d, %v", n, p, err)
		}
	}
	for _, n := range []int{0, -1, 65536} {
		if _, err := ParsePort(n); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParsePort(%d) = %v, want ErrInvalid", n, err)
		}
	}
}

// An id that arrives decoded (desired.json, team.json, a bundle) is checked as its Parse checks
// it: a reserved, malformed or empty one fails the decode, and a valid one round-trips.
func TestIDsDecodeOnlyThroughTheirParse(t *testing.T) {
	type ids struct {
		Team    TeamID        `json:"team"`
		Run     RunID         `json:"run"`
		Key     CheckoutKey   `json:"key"`
		User    GuestUser     `json:"user"`
		Set     SetID         `json:"set"`
		Session SessionName   `json:"session"`
		Digest  Digest        `json:"digest"`
		Display DisplayName   `json:"display"`
		Version ServerVersion `json:"version"`
	}
	valid := map[string]string{
		"team": "ops", "run": "0123456789abcdef", "key": strings.Repeat("ab", 16), "user": "dev",
		"set": "s1", "session": "sess-1", "digest": strings.Repeat("0a", 8), "display": "Ops",
		"version": "1.2.3",
	}
	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	var got ids
	if err = json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode of valid ids: %v", err)
	}
	for field, id := range map[string]fmt.Stringer{
		"team": got.Team, "run": got.Run, "key": got.Key, "user": got.User, "set": got.Set,
		"session": got.Session, "digest": got.Digest, "display": got.Display, "version": got.Version,
	} {
		if id.String() != valid[field] {
			t.Errorf("%s decoded as %q, want %q", field, id.String(), valid[field])
		}
	}
	again, err := json.Marshal(got)
	if err != nil || string(again) != `{"team":"ops","run":"0123456789abcdef","key":"`+strings.Repeat("ab", 16)+
		`","user":"dev","set":"s1","session":"sess-1","digest":"`+strings.Repeat("0a", 8)+`","display":"Ops","version":"1.2.3"}` {
		t.Fatalf("round trip: %s, %v", again, err)
	}
	invalid := map[string][]string{
		"team": {"", "ctl", "../x", "Ops"}, "run": {"", "../x", "0123"}, "key": {"", "../x"},
		"user": {"", "../x"}, "set": {"", ".."}, "session": {"", "a/b"}, "digest": {"", "current", "../x"},
		"display": {"", "a\nb"}, "version": {"", "../x"},
	}
	for field, bads := range invalid {
		for _, value := range bads {
			doc := maps.Clone(valid)
			doc[field] = value
			bad, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			var into ids
			if err = json.Unmarshal(bad, &into); !errors.Is(err, ErrInvalid) {
				t.Errorf("%s %q decoded: %v", field, value, err)
			}
		}
	}
}

// A failed decode leaves the id it was decoding into as it was.
func TestAFailedUnmarshalTextLeavesTheID(t *testing.T) {
	id := mustTeam(t, "ops")
	if err := id.UnmarshalText([]byte("ctl")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("UnmarshalText(ctl) = %v, want ErrInvalid", err)
	}
	if id.String() != "ops" {
		t.Fatalf("after a failed decode the id is %q", id)
	}
}

// The zero value of an id was never parsed: encoding one is refused, not written as "".
func TestAZeroIDIsNotEncoded(t *testing.T) {
	for name, v := range map[string]any{
		"TeamID": TeamID{}, "RunID": RunID{}, "CheckoutKey": CheckoutKey{}, "GuestUser": GuestUser{},
		"SetID": SetID{}, "SessionName": SessionName{}, "Digest": Digest{}, "DisplayName": DisplayName{},
		"ServerVersion": ServerVersion{},
	} {
		if _, err := json.Marshal(v); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s{} encoded: %v", name, err)
		}
	}
}

// A kit digest is lowercase hex, so no digest can be the current link or leave the kit root.
func TestParseDigestIsLowercaseHex(t *testing.T) {
	for _, s := range []string{strings.Repeat("a", 16), strings.Repeat("0f", 32)} {
		d, err := ParseDigest(s)
		if err != nil {
			t.Errorf("ParseDigest(%q): %v", s, err)
			continue
		}
		if HostKitDir(d) == HostKitCurrent() {
			t.Errorf("HostKitDir(%q) is the current link", s)
		}
	}
	for _, s := range []string{"current", "", strings.Repeat("a", 15), strings.Repeat("a", 65), strings.Repeat("A", 16), ".."} {
		if _, err := ParseDigest(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseDigest(%q) = %v, want ErrInvalid", s, err)
		}
	}
}

func mustDisplay(t *testing.T, s string) DisplayName {
	t.Helper()
	d, err := ParseDisplayName(s)
	if err != nil {
		t.Fatalf("ParseDisplayName(%q): %v", s, err)
	}
	return d
}

func mustTeam(t *testing.T, s string) TeamID {
	t.Helper()
	id, err := ParseTeamID(s)
	if err != nil {
		t.Fatalf("ParseTeamID(%q): %v", s, err)
	}
	return id
}
