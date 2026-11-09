package ansible

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// escapeFixtureDir is the repository's fixtures for the rule (src-go/qa/ansible is three below the root).
var escapeFixtureDir = filepath.Join("..", "..", "..", "tests", "fixtures", "ansible-regex-class-escape")

func readEscapeFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(escapeFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestRegexEscapeProvesItselfAgainstTheFlaggedFixture(t *testing.T) {
	body := readEscapeFixture(t, "flagged.yml")
	var want []int
	for i, line := range strings.Split(string(body), "\n") {
		if strings.HasSuffix(strings.TrimRight(line, " \r"), "# EXPECT-FINDING") {
			want = append(want, i+1)
		}
	}
	if len(want) < 20 {
		t.Fatalf("the flagged fixture marks only %d findings; it lost cases", len(want))
	}
	findings := regexEscapes("flagged.yml", body)
	if got := slices.Compact(findingLines(t, "flagged.yml", findings)); !slices.Equal(got, want) {
		t.Fatalf("finding lines\n got %v\nwant %v\n%s", got, want, strings.Join(findings, "\n"))
	}
}

func TestRegexEscapeIsSilentOnTheCleanFixture(t *testing.T) {
	findings := regexEscapes("clean.yml", readEscapeFixture(t, "clean.yml"))
	if len(findings) != 0 {
		t.Fatalf("clean fixture reported:\n%s", strings.Join(findings, "\n"))
	}
}

func TestRegexEscapeReportsEachWrongConstantOfTheOriginalDefect(t *testing.T) {
	text := "v: >-\n  {{ c | regex_findall('(?m)^(?:' ~ u ~ '):(\\\\d+):(\\\\d+)\\\\s*$') }}\n"
	got := regexEscapes("t.yml", []byte(text))
	want := []string{
		`t.yml:1: regex_findall pattern '):(\\d+):(\\d+)\\s*$' reaches the regex as a literal backslash and d (2 backslashes, not 1); ` +
			`in a block scalar's {{ }} write '):(\d+):(\d+)\s*$'`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}

func TestRegexEscapeNamesTheReceivedFormAndTheFix(t *testing.T) {
	cases := []struct{ name, yaml, want string }{
		{
			"a test in a double-quoted template",
			`v: "{{ s is regex('\\\\w') }}"` + "\n",
			`t.yml:1: regex test pattern '\\\\w' reaches the regex as a literal backslash and w (2 backslashes, not 1); ` +
				`in a double-quoted scalar's {{ }} write '\\w'`,
		},
		{
			"a backspace in a conditional",
			`when: s is search('\bx')` + "\n",
			`t.yml:1: search test pattern '\bx' reaches the regex as a backspace character, not \b; ` +
				`in a plain scalar's conditional write '\\bx'`,
		},
		{
			"four backslashes in a statement",
			`v: x{% if s is match('\\\\s') %}y{% endif %}` + "\n",
			`t.yml:1: match test pattern '\\\\s' reaches the regex as a literal backslash and s (2 backslashes, not 1); ` +
				`in a plain scalar's {% %} write '\\s'`,
		},
		{
			"a filter by name in a single-quoted template",
			`v: '{{ l | map("regex_replace", "\\s", "") }}'` + "\n",
			`t.yml:1: regex_replace pattern "\\s" reaches the regex as a literal backslash and s (2 backslashes, not 1); ` +
				`in a single-quoted scalar's {{ }} write "\s"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := regexEscapes("t.yml", []byte(c.yaml)); !slices.Equal(got, []string{c.want}) {
				t.Fatalf("\n got %q\nwant %q", got, []string{c.want})
			}
		})
	}
}

func TestRegexEscapeIgnoresWhatIsNotAPattern(t *testing.T) {
	for _, text := range []string{
		"v: >-\n" + `  {{ s | regex_search('a') }} '\\d'`,         // after the template ends
		"v: >-\n" + `  {{ s | regex_replace('a', 'b') ~ '\\d' }}`, // after the call
		"v: >-\n" + `  {{ s | regex_replace('a', '\\d') }}`,       // the replacement
		"v: >-\n" + `  {{ s | regex_search('a(b)', '\\1') }}`,     // a group argument (regex-backref-escape's)
		"v: >-\n" + `  {{ s | regex_search('(a)\\1') }}`,          // a backreference (regex-backref-escape's)
		"v: >-\n" + `  {{ s | map('upper', '\\d') }}`,             // map of another filter
		"v: >-\n" + `  {{ s is defined('\\d') }}`,                 // another test
		"v: >-\n" + `  {{ search('\\d') }}`,                       // a name, not a test
		"v: >-\n" + `  {{ s | regex_search(p ~ '\\\\') }}`,        // escaped backslashes at the end
		`v: !unsafe "{{ s | regex_search('\\\\d') }}"`,            // never templated
		`when: s is regex('\d\w\s')`,                              // unknown escapes Jinja keeps
		`when: s is regex('\n\t')`,                                // a newline is the same to the regex
		"- 1\n- [2, 3]\n",
		"",
	} {
		if got := regexEscapes("t.yml", []byte(text)); len(got) != 0 {
			t.Errorf("%q: got %q", text, got)
		}
	}
}

func TestRegexEscapeReportsYAMLItCannotParse(t *testing.T) {
	got := regexEscapes("t.yml", []byte("a: 1\nb: [1\n"))
	if len(got) != 1 || !strings.Contains(got[0], "cannot be parsed as YAML") {
		t.Fatalf("got %q", got)
	}
}
