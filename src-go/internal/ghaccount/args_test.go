package ghaccount

import (
	"strings"
	"testing"
)

func TestParseArgsReadsEachMode(t *testing.T) {
	cases := []struct {
		args []string
		want options
	}{
		{nil, options{help: true}},
		{[]string{"--setup-all"}, options{mode: modeSetupAll}},
		{[]string{"--check"}, options{mode: modeCheck}},
		{[]string{"--add=work:jane-doe"}, options{mode: modeAdd, alias: "work", username: "jane-doe"}},
		{[]string{"--check", "--help"}, options{help: true}},
		{[]string{"--frobnicate", "-h"}, options{help: true}},
	}
	for _, c := range cases {
		got, err := parseArgs(c.args)
		if err != nil {
			t.Fatalf("%q: %v", c.args, err)
		}
		if got != c.want {
			t.Errorf("%q: got %+v, want %+v", c.args, got, c.want)
		}
	}
}

func TestParseArgsRefusesWhatIsNotOneValidMode(t *testing.T) {
	cases := map[string][]string{
		"unknown option":        {"--frobnicate"},
		"two modes":             {"--check", "--setup-all"},
		"add twice":             {"--add=a:b", "--add=c:d"},
		"no colon":              {"--add=work"},
		"empty alias":           {"--add=:jane"},
		"empty username":        {"--add=work:"},
		"alias with a slash":    {"--add=../x:jane"},
		"alias leading hyphen":  {"--add=-x:jane"},
		"username with a colon": {"--add=work:ja:ne"},
		"username leading dash": {"--add=work:-jane"},
		"username trailing":     {"--add=work:jane-"},
		"username too long":     {"--add=work:" + strings.Repeat("a", 40)},
		"bare add":              {"--add"},
		"operand":               {"work"},
	}
	for name, args := range cases {
		if _, err := parseArgs(args); err == nil {
			t.Errorf("%s (%q): accepted", name, args)
		}
	}
}

func TestValidNamesFollowTheirRules(t *testing.T) {
	for _, ok := range []string{"work", "oss_2", "a-b", "A1"} {
		if !validAlias(ok) {
			t.Errorf("alias %q refused", ok)
		}
	}
	for _, bad := range []string{"", "_a", "a.b", "a/b", "a b"} {
		if validAlias(bad) {
			t.Errorf("alias %q accepted", bad)
		}
	}
	for _, ok := range []string{"a", "jane-doe", "J4ne", strings.Repeat("b", 39)} {
		if !validLogin(ok) {
			t.Errorf("login %q refused", ok)
		}
	}
	for _, bad := range []string{"", "-a", "a-", "a_b", "a.b", strings.Repeat("b", 40)} {
		if validLogin(bad) {
			t.Errorf("login %q accepted", bad)
		}
	}
}
