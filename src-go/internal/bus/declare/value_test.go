package declare

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/pycompat"
)

func TestValueReadsTheSubset(t *testing.T) {
	for _, c := range []struct {
		rest   string
		nested []string
		want   string // repr() of the value
	}{
		{" alpha", nil, "'alpha'"},
		{" true", nil, "True"},
		{" false # c", nil, "False"},
		{" 'true'", nil, "'true'"},
		{` "a # b" # c`, nil, "'a # b'"},
		{` "a\"b"`, nil, `'a"b'`},
		{` "a"#c`, nil, "'a'"},
		{` "a" b`, nil, `'"a" b'`},
		{` "abc`, nil, `'"abc'`},
		{" -12", nil, "-12"},
		{" 007", nil, "7"},
		{" 1.5", nil, "'1.5'"},
		{" null", nil, "'null'"},
		{" ~", nil, "'~'"},
		{" &anchor value", nil, "'&anchor value'"},
		{" *alias", nil, "'*alias'"},
		{` ["a", 1, null, 1e400, {"k": [true]}]`, nil, "['a', 1, None, inf, {'k': [True]}]"},
		{` {"a": 1, "a": 2}`, nil, "{'a': 2}"},
		{" '[1]'", nil, "'[1]'"},
		{"", []string{"      - a", "      - 2 # c", "      # skipped", "      - repo: x/y", "        branches: [\"main\"]", "      - 'q'"}, "['a', 2, {'repo': 'x/y', 'branches': ['main']}, 'q']"},
		{" # just a comment", []string{"    - a"}, "['a']"},
	} {
		v, err := value(c.rest, c.nested, "w")
		if err != nil {
			t.Errorf("%q %q: %v", c.rest, c.nested, err)
			continue
		}
		if got := pycompat.PyRepr(v); got != c.want {
			t.Errorf("%q %q: got %s, want %s", c.rest, c.nested, got, c.want)
		}
	}
}

func TestValueRefusals(t *testing.T) {
	for _, c := range []struct {
		rest   string
		nested []string
		kind   Kind
		msg    string
	}{
		{"", nil, KindRefused, "w has no value"},
		{" # c", nil, KindRefused, "w has no value"},
		{" x", []string{"  - a"}, KindRefused, "w has a value and lines nested under it"},
		{" !vault |", []string{"  abc"}, KindRefused, "w is tagged (vault-encrypted?); ccy-teams reads only plain values"},
		{" [a]", nil, KindRefused, "w is a flow value ccy-teams cannot read (write it as JSON)"},
		{` ["a #b"]`, nil, KindRefused, "w is a flow value ccy-teams cannot read (write it as JSON)"},
		{" {}x", nil, KindRefused, "w is a flow value ccy-teams cannot read (write it as JSON)"},
		{"", []string{"  - a", " - b"}, KindRefused, "w is not a block list ccy-teams can read"},
		{"", []string{"  -a"}, KindRefused, "w is not a block list ccy-teams can read"},
		{"", []string{"    x: 1"}, KindRefused, "w is not a block list ccy-teams can read"},
		{"", []string{"  - a: 1", "    a: 2"}, KindRefused, "w holds a list item ccy-teams cannot read"},
		{"", []string{"  - a: 1", "    [b]"}, KindRefused, "w holds a list item ccy-teams cannot read"},
		{"", []string{"  - a:", "  - [x"}, KindRefused, "w is a flow value ccy-teams cannot read (write it as JSON)"},
		{"", []string{"  - a:", "  - b"}, KindRefused, "w: a has no value"},
		{"", []string{"  - # c"}, KindRefused, "w has no value"},
		{"", []string{"  - !vault x"}, KindRefused, "w is tagged (vault-encrypted?); ccy-teams reads only plain values"},
		{" " + strings.Repeat("9", 4301), nil, KindCrash, ""},
		{" [" + strings.Repeat("9", 4301) + "]", nil, KindCrash, ""},
		{" " + strings.Repeat("[", 1000) + strings.Repeat("]", 1000), nil, KindCrash, ""},
	} {
		_, err := value(c.rest, c.nested, "w")
		e, ok := asError(err)
		if !ok || e.Kind != c.kind || (c.msg != "" && e.Message != c.msg) {
			t.Errorf("%q %q: got %v, want kind %d %q", c.rest, c.nested, err, c.kind, c.msg)
		}
	}
}

func TestValueDigitLimitIsPythons(t *testing.T) {
	if _, err := value(" -"+strings.Repeat("9", 4300), nil, "w"); err != nil {
		t.Errorf("4300 digits and a sign is an int: %v", err)
	}
}

func TestRawValueNamesTheEntryKeyAndLine(t *testing.T) {
	e := entry{team: "alpha", raw: []rawKey{{key: "org", rest: " !vault |", index: 6}, {key: "repos", rest: " []", index: 7}}}
	if v, err := rawValue(e, "missing"); v != (pycompat.None{}) || err != nil {
		t.Errorf("an absent key is no value: %v, %v", v, err)
	}
	if v, err := rawValue(e, "repos"); err != nil || pycompat.PyRepr(v) != "[]" {
		t.Errorf("repos: %v, %v", v, err)
	}
	_, err := rawValue(e, "org")
	if err == nil || err.Error() != "alpha's org on line 7 is tagged (vault-encrypted?); ccy-teams reads only plain values" {
		t.Errorf("org: %v", err)
	}
}
