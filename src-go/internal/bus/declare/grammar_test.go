package declare

import (
	"errors"
	"strings"
	"testing"
)

func TestIsOwnerRepo(t *testing.T) {
	for value, want := range map[string]bool{
		"example-org/proj": true, "a/b": true, "a/b.c_d-e": true, "0/0": true,
		"Example-Org/proj": false, "example-org/Proj": false, "-a/b": false, "a-/b": false,
		"a/.": false, "a/..": false, "a/b.git": false, "a/...": true, "a": false, "a/b/c": false,
		strings.Repeat("a", 39) + "/b": true, strings.Repeat("a", 40) + "/b": false,
		"a/" + strings.Repeat("b", 100): true, "a/" + strings.Repeat("b", 101): false,
		"a/b\n": false, "": false,
	} {
		if got := isOwnerRepo(value); got != want {
			t.Errorf("isOwnerRepo(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestGithubRemote(t *testing.T) {
	for url, want := range map[string]string{
		"git@github.com:Example-Org/Proj.git":         "example-org/proj",
		"git@github.com-work:example-org/proj":        "example-org/proj",
		"ssh://git@github.com/example-org/proj.git/":  "example-org/proj",
		"ssh://git@github.com:22/example-org/proj":    "example-org/proj",
		"https://github.com/example-org/proj.git.git": "", // proj.git: a repo name ending .git
		"  https://github.com/example-org/proj\n":     "example-org/proj",
		"https://github.com/example-org/proj.git":     "example-org/proj",
		"https://example.com/example-org/proj":        "",
		"https://github.com/example-org/a/b":          "",
		"https://github.com/example_org/proj":         "",
		"http://github.com/example-org/proj":          "",
		"git@github.com:example-org/.git":             "",
	} {
		if got := GithubRemote(url); got != want {
			t.Errorf("GithubRemote(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestNormaliseRepoAndResolveHost(t *testing.T) {
	if got, err := normaliseRepo("--My.Repo_"); err != nil || got != "my-repo_" {
		t.Errorf("normaliseRepo = %q, %v", got, err)
	}
	if got, err := normaliseRepo(strings.Repeat("x", 60)); err != nil || got != strings.Repeat("x", 48) {
		t.Errorf("normaliseRepo cuts at 48: %q, %v", got, err)
	}
	if _, err := normaliseRepo("-_."); err == nil || err.Error() != "repository name '-_.' is empty once normalised" {
		t.Errorf("normaliseRepo(-_.) = %v", err)
	}
	// U+0130 lowers to i and a combining dot: two code points, so i and one dash.
	if got, err := resolveHost("\xc4\xb0x"); err != nil || got != "i-x" {
		t.Errorf("resolveHost = %q, %v", got, err)
	}
	if got, err := resolveHost("Work Laptop"); err != nil || got != "work-laptop" {
		t.Errorf("resolveHost = %q, %v", got, err)
	}
	if _, err := resolveHost(""); err == nil || err.Error() != "no role is set for this install: pass --host" {
		t.Errorf("resolveHost(\"\") = %v", err)
	}
	if _, err := resolveHost("-x"); err == nil || err.Error() != "host '-x' does not fit [a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])? once normalised" {
		t.Errorf("resolveHost(-x) = %v", err)
	}
}

func TestIsDescription(t *testing.T) {
	for value, want := range map[string]bool{
		"Builds the example site": true, "": false, " x": false, "x\xc2\xa0": false,
		strings.Repeat("e", 120): true, strings.Repeat("e", 121): false,
		strings.Repeat("\xc3\xa9", 120): true, "a\tb": false, "a\xe2\x80\x8bb": false,
		"a\xe2\x80\xaeb": false, "a\xed\xa0\x80b": true,
	} {
		if got := isDescription(value); got != want {
			t.Errorf("isDescription(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestPortNumberIsIsdigitThenInt(t *testing.T) {
	for value, want := range map[string]int{
		"6167": 6167, "007": 7, "0": 0, "\xd9\xa1\xd9\xa2": 12, "1\xef\xbc\x92": 12,
		"9223372036854775807": 9223372036854775807,
	} {
		if got, err := portNumber(value, 4); err != nil || got != want {
			t.Errorf("portNumber(%q) = %d, %v; want %d", value, got, err, want)
		}
	}
	notNumber := "line 4: the port is not a number"
	for value, kind := range map[string]Kind{
		"": KindRefused, "-1": KindRefused, "61 67": KindRefused, "6e3": KindRefused,
		"9223372036854775808": KindRefused, // B-S6: beyond an int
		"\xc2\xb2":            KindCrash, "1\xe2\x91\xa0": KindCrash, strings.Repeat("1", 4301): KindCrash,
		"\xc2\xb2x": KindRefused,
	} {
		_, err := portNumber(value, 4)
		e, ok := asError(err)
		if !ok || e.Kind != kind || e.Message != notNumber {
			t.Errorf("portNumber(%q) = %v, want kind %d", value, err, kind)
		}
	}
}

func TestOtherDigitsTableIsSorted(t *testing.T) {
	for _, r := range []rune{0xB2, 0xB3, 0xB9, 0x2070, 0x2460, 0x24FF, 0x1F10A} {
		if !isDigitChar(r) || isDecimal(r) {
			t.Errorf("%U is a digit that is not decimal", r)
		}
	}
	for _, r := range []rune{'0', 0x0661, 0xFF12} {
		if !isDigitChar(r) || !isDecimal(r) {
			t.Errorf("%U is a decimal digit", r)
		}
	}
	for _, r := range []rune{'a', 0xBC, 0x2150, 0x1F10B} {
		if isDigitChar(r) {
			t.Errorf("%U is not a digit", r)
		}
	}
}

func TestCutComment(t *testing.T) {
	for rest, want := range map[string]string{
		" x # c": " x ", "#c": "", " #c": " ", "a#b": "a#b", "a #b #c": "a ", "a\t#b": "a\t",
		"a\xc2\xa0#b": "a\xc2\xa0", "\"a # b\"": "\"a ", "": "",
	} {
		if got := cutComment(rest); got != want {
			t.Errorf("cutComment(%q) = %q, want %q", rest, got, want)
		}
	}
}

func TestScalar(t *testing.T) {
	for rest, want := range map[string]string{
		" alpha": "alpha", " 'alpha' # c": "alpha", ` "a"b"`: `a"b`, ` "`: `"`, ` ""`: "",
		` 'a"`: `'a"`, " \xe3\x80\x80x\xe3\x80\x80": "x", ` "\u00e9"`: `\u00e9`,
	} {
		if got, err := scalar(rest, "w"); err != nil || got != want {
			t.Errorf("scalar(%q) = %q, %v; want %q", rest, got, err, want)
		}
	}
	_, err := scalar(" !vault |", "the team on line 3")
	if !errors.Is(err, ErrVaulted) || err.Error() != "the team on line 3 is tagged (vault-encrypted?); ccy-teams reads only plain values" {
		t.Errorf("scalar(!vault) = %v", err)
	}
}

func TestKeyLine(t *testing.T) {
	for body, want := range map[string][2]string{
		"team: a": {"team", " a"}, "team:": {"team", ""}, "a_b:\tx": {"a_b", "\tx"},
		"team:\xc2\xa0x": {"team", "\xc2\xa0x"},
	} {
		key, rest, ok := keyLine(body)
		if !ok || key != want[0] || rest != want[1] {
			t.Errorf("keyLine(%q) = %q %q %v", body, key, rest, ok)
		}
	}
	for _, body := range []string{"team:a", "Team: a", ": a", "team a", "te-am: a", "team :a", ""} {
		if _, _, ok := keyLine(body); ok {
			t.Errorf("keyLine(%q) matched", body)
		}
	}
}
