package paths

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// lxcNames is every name of lxc.go, for the sample container and guest user; sampleNames
// includes it, so the v1 and derivation checks cover these names too.
func lxcNames(t *testing.T) map[string][]string {
	t.Helper()
	user := sampleGuestUser(t)
	return map[string][]string{
		"LXCDesiredCopy": {LXCDesiredCopy(sampleContainer(t), user)},
		"LXCSession":     {LXCSession(user).String()},
	}
}

// lxcZeroDerivations is lxc.go's functions of a zero container or guest user, which
// zeroDerivations includes; each zero argument of LXCDesiredCopy alone is the test below.
func lxcZeroDerivations() map[string]func() string {
	var (
		container ContainerName
		user      GuestUser
	)
	return map[string]func() string{
		"LXCDesiredCopy": func() string { return LXCDesiredCopy(container, user) },
		"LXCSession":     func() string { return LXCSession(user).String() },
	}
}

// Either zero argument of LXCDesiredCopy panics on its own.
func TestLXCDesiredCopyPanicsOnEitherZeroArgument(t *testing.T) {
	good, user := sampleContainer(t), sampleGuestUser(t)
	for name, f := range map[string]func() string{
		"zero container": func() string { return LXCDesiredCopy(ContainerName{}, user) },
		"zero user":      func() string { return LXCDesiredCopy(good, GuestUser{}) },
	} {
		if did, got := panics(f); !did {
			t.Errorf("%s: LXCDesiredCopy returned %q", name, got)
		}
	}
}

// The LXC names of C 3.9.3 and C 3.7.5.
func TestLXCNames(t *testing.T) {
	user := sampleGuestUser(t)
	for got, want := range map[string]string{
		LXCDesiredCopy(sampleContainer(t), user): "/var/lib/teamchat-admin/_lxc/box-1.dev/dev.json",
		LXCSession(user).String():                "lxc-dev",
	} {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
	long, err := ParseGuestUser(strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	session, err := ParseSessionName(LXCSession(long).String())
	if err != nil || session != LXCSession(long) {
		t.Fatalf("the longest user's session %q does not pass ParseSessionName: %v", LXCSession(long), err)
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
