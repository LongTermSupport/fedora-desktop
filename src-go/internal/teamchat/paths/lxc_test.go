package paths

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The sample seat: team ops, checkout /srv/checkouts/thing, seat 1, whose key is the first 16
// hex of SHA-256 of "ops\n/srv/checkouts/thing\n1".
const (
	sampleCheckout = "/srv/checkouts/thing"
	sampleKeyText  = "aeb4d70a3a7e7c04"
)

// lxcNames is every name of lxc.go, for the sample container and seat; sampleNames includes
// it, so the v1 and derivation checks cover these names too.
func lxcNames(t *testing.T) map[string][]string {
	t.Helper()
	key := sampleSeatKey(t)
	layout, err := LXCLayout("/home/u", GuestRuntimeDir(key), key)
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]string{
		"LXCDesiredCopy": {LXCDesiredCopy(sampleContainer(t), key)},
		"LXCSession":     {LXCSession(key).String()},
		"LXCSeatKey":     {LXCSeatKey(mustTeam(t, "ops"), sampleCheckout, 1).String()},
		"LXCRepoLabel":   {LXCRepoLabel(sampleCheckout)},
		"LXCLayout":      {layout.State(), layout.Desired(), layout.RunRoot()},
	}
}

// lxcZeroDerivations is lxc.go's functions of a zero container, team or seat key, which
// zeroDerivations includes; each zero argument of LXCDesiredCopy alone is the test below.
func lxcZeroDerivations() map[string]func() string {
	var (
		container ContainerName
		key       SeatKey
		team      TeamID
	)
	return map[string]func() string{
		"LXCDesiredCopy": func() string { return LXCDesiredCopy(container, key) },
		"LXCSession":     func() string { return LXCSession(key).String() },
		"LXCSeatKey":     func() string { return LXCSeatKey(team, sampleCheckout, 1).String() },
		"LXCLayout": func() string {
			l, err := LXCLayout("/home/u", "/run/user/1000", key)
			if err != nil {
				return err.Error()
			}
			return l.State()
		},
	}
}

// Either zero argument of LXCDesiredCopy panics on its own.
func TestLXCDesiredCopyPanicsOnEitherZeroArgument(t *testing.T) {
	good, key := sampleContainer(t), sampleSeatKey(t)
	for name, f := range map[string]func() string{
		"zero container": func() string { return LXCDesiredCopy(ContainerName{}, key) },
		"zero key":       func() string { return LXCDesiredCopy(good, SeatKey{}) },
	} {
		if did, got := panics(f); !did {
			t.Errorf("%s: LXCDesiredCopy returned %q", name, got)
		}
	}
}

// The LXC names of C 3.9.1's names table.
func TestLXCNames(t *testing.T) {
	key := sampleSeatKey(t)
	for got, want := range map[string]string{
		LXCDesiredCopy(sampleContainer(t), key): "/var/lib/teamchat-admin/_lxc/box-1.dev/" + sampleKeyText + ".json",
		LXCSession(key).String():                "lxc-" + sampleKeyText,
	} {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
	session, err := ParseSessionName(LXCSession(key).String())
	if err != nil || session != LXCSession(key) {
		t.Fatalf("the seat's session %q does not pass ParseSessionName: %v", LXCSession(key), err)
	}
}

// C 3.9.1: the key is the first 16 hex of SHA-256 of `<team>\n<checkout>\n<seat>`, so each of
// the three makes a seat of its own, and the user is not part of it.
func TestLXCSeatKey(t *testing.T) {
	ops := mustTeam(t, "ops")
	if got := LXCSeatKey(ops, sampleCheckout, 1); got != sampleSeatKey(t) {
		t.Fatalf("LXCSeatKey = %s, want %s", got, sampleKeyText)
	}
	keys := map[SeatKey]string{}
	for name, k := range map[string]SeatKey{
		"sample":         LXCSeatKey(ops, sampleCheckout, 1),
		"another seat":   LXCSeatKey(ops, sampleCheckout, 2),
		"another repo":   LXCSeatKey(ops, "/srv/checkouts/other", 1),
		"another team":   LXCSeatKey(mustTeam(t, "dev"), sampleCheckout, 1),
		"seat 11, not 1": LXCSeatKey(ops, sampleCheckout, 11),
	} {
		if prior, seen := keys[k]; seen {
			t.Errorf("%s and %s share the key %s", name, prior, k)
		}
		keys[k] = name
	}
}

// A seat key is made from declare's values only: any other checkout or seat panics, as a zero
// team does, since the key would name no declared seat.
func TestLXCSeatKeyPanicsOnAnInputDeclareRefuses(t *testing.T) {
	ops := mustTeam(t, "ops")
	for name, f := range map[string]func() string{
		"relative":   func() string { return LXCSeatKey(ops, "srv/x", 1).String() },
		"not clean":  func() string { return LXCSeatKey(ops, "/srv/../x", 1).String() },
		"root":       func() string { return LXCSeatKey(ops, "/", 1).String() },
		"two lines":  func() string { return LXCSeatKey(ops, "/srv/a\nb", 1).String() },
		"seat 0":     func() string { return LXCSeatKey(ops, sampleCheckout, 0).String() },
		"seat 10000": func() string { return LXCSeatKey(ops, sampleCheckout, 10000).String() },
	} {
		if did, got := panics(f); !did {
			t.Errorf("%s: LXCSeatKey returned %q", name, got)
		}
	}
}

// C 3.9.1: the repo label is the checkout's base name through NormaliseRepo, cut to 24, trailing
// `-` and `_` trimmed, `checkout` when nothing is left.
func TestLXCRepoLabel(t *testing.T) {
	for checkout, want := range map[string]string{
		"/srv/checkouts/thing": "thing",
		"/home/u/My.Repo":      "my-repo",
		"/srv/__x__":           "x",
		"/srv/" + strings.Repeat("a", 23) + "-bcd": strings.Repeat("a", 23),
		"/srv/" + strings.Repeat("a", 23) + "_-z":  strings.Repeat("a", 23),
		"/srv/" + strings.Repeat("b", 30):          strings.Repeat("b", 24),
		"/srv/" + strings.Repeat("c", 24) + "-tl":  strings.Repeat("c", 24),
		"/srv/---":    "checkout",
		"/srv/...":    "checkout",
		"/srv/réseau": "r-seau",
	} {
		if got := LXCRepoLabel(checkout); got != want {
			t.Errorf("LXCRepoLabel(%q) = %q, want %q", checkout, got, want)
		}
	}
}

// LXCLayout is the seat's guest roots under ~/.config/teamchat/lxc/<key>, and the run root under
// the runtime directory the process received (LX5).
func TestLXCLayout(t *testing.T) {
	key := sampleSeatKey(t)
	l, err := LXCLayout("/home/u", GuestRuntimeDir(key), key)
	if err != nil {
		t.Fatal(err)
	}
	for got, want := range map[string]string{
		l.State():                        "/home/u/.config/teamchat/lxc/" + sampleKeyText + "/seats",
		l.Desired():                      "/home/u/.config/teamchat/lxc/" + sampleKeyText + "/membership",
		l.RunRoot():                      "/run/teamchat-session-" + sampleKeyText + "/teamchat/run",
		l.Seat(mustTeam(t, "ops")).Dir(): "/home/u/.config/teamchat/lxc/" + sampleKeyText + "/seats/ops",
		l.KitRoot():                      HostKitRoot,
	} {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
	hand, err := LXCLayout("/home/u", "/run/user/1001", key)
	if err != nil || hand.RunRoot() != "/run/user/1001/teamchat/run" || hand.State() != l.State() {
		t.Fatalf("a hand-run seat's layout: %+v, %v", hand, err)
	}
	for _, c := range [][2]string{{"home", "/run/user/1"}, {"/home/u/", "/run/user/1"}, {"/home/u", "run"}, {"/home/u", "/"}} {
		if _, err := LXCLayout(c[0], c[1], key); !errors.Is(err, ErrInvalid) {
			t.Errorf("LXCLayout(%q, %q) = %v, want ErrInvalid", c[0], c[1], err)
		}
	}
}

func TestParseSeatKeyIsSixteenLowercaseHex(t *testing.T) {
	if k, err := ParseSeatKey(sampleKeyText); err != nil || k.String() != sampleKeyText {
		t.Fatalf("a 16-hex seat key: %q, %v", k, err)
	}
	for _, s := range []string{"", "0123456789abcde", "0123456789abcdef0", "0123456789ABCDEF", "../x", "0123456789abcdeg"} {
		if _, err := ParseSeatKey(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseSeatKey(%q) = %v, want ErrInvalid", s, err)
		}
	}
}

// A seat key decodes only through its Parse, round-trips, and its zero value is not encoded.
func TestSeatKeyDecodesOnlyThroughItsParse(t *testing.T) {
	var got struct {
		K SeatKey `json:"k"`
	}
	if err := json.Unmarshal([]byte(`{"k":"`+sampleKeyText+`"}`), &got); err != nil || got.K != sampleSeatKey(t) {
		t.Fatalf("decode: %q, %v", got.K, err)
	}
	if again, err := json.Marshal(got); err != nil || string(again) != `{"k":"`+sampleKeyText+`"}` {
		t.Fatalf("round trip: %s, %v", again, err)
	}
	var into struct {
		K SeatKey `json:"k"`
	}
	if err := json.Unmarshal([]byte(`{"k":"../x"}`), &into); !errors.Is(err, ErrInvalid) {
		t.Errorf("a bad key decoded: %v", err)
	}
	if _, err := json.Marshal(SeatKey{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("SeatKey{} encoded: %v", err)
	}
}

// A container name is declare's grammar: one path segment that is never "." or "..", and never
// reads as an option.
func TestParseContainerNameIsLXCsGrammar(t *testing.T) {
	for _, s := range []string{"a", "box-1.dev", "Box_2", "9", strings.Repeat("a", 64)} {
		c, err := ParseContainerName(s)
		if err != nil || c.String() != s {
			t.Errorf("ParseContainerName(%q) = %q, %v", s, c, err)
		}
	}
	for _, s := range []string{"", ".", "..", ".a", "-a", "_a", "a/b", "a b", "a\x00b", "a\nb", "a:b", strings.Repeat("a", 65)} {
		if _, err := ParseContainerName(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseContainerName(%q) = %v, want ErrInvalid", s, err)
		}
	}
}

// A container name decodes only through its Parse, round-trips, and its zero value is not
// encoded.
func TestContainerNameDecodesOnlyThroughItsParse(t *testing.T) {
	var got struct {
		C ContainerName `json:"c"`
	}
	if err := json.Unmarshal([]byte(`{"c":"box-1.dev"}`), &got); err != nil || got.C != sampleContainer(t) {
		t.Fatalf("decode: %q, %v", got.C, err)
	}
	if again, err := json.Marshal(got); err != nil || string(again) != `{"c":"box-1.dev"}` {
		t.Fatalf("round trip: %s, %v", again, err)
	}
	for _, bad := range []string{`{"c":""}`, `{"c":"../x"}`, `{"c":"-x"}`} {
		var into struct {
			C ContainerName `json:"c"`
		}
		if err := json.Unmarshal([]byte(bad), &into); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s decoded: %v", bad, err)
		}
	}
	if _, err := json.Marshal(ContainerName{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("ContainerName{} encoded: %v", err)
	}
}

func sampleContainer(t *testing.T) ContainerName {
	t.Helper()
	v, err := ParseContainerName("box-1.dev")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func sampleSeatKey(t *testing.T) SeatKey {
	t.Helper()
	v, err := ParseSeatKey(sampleKeyText)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
