package ansible

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// backrefFixtureDir is the repository's fixtures for the rule (src-go/qa/ansible is three below the root).
var backrefFixtureDir = filepath.Join("..", "..", "..", "tests", "fixtures", "ansible-regex-backref")

func readBackrefFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(backrefFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// findingLines is the line number of each finding, parsed back out of "<rel>:<line>: ...".
func findingLines(t *testing.T, rel string, findings []string) []int {
	t.Helper()
	lines := make([]int, 0, len(findings))
	for _, f := range findings {
		rest, ok := strings.CutPrefix(f, rel+":")
		if !ok {
			t.Fatalf("finding %q does not start with %s:", f, rel)
		}
		head, _, found := strings.Cut(rest, ":")
		number, err := strconv.Atoi(head)
		if !found || err != nil {
			t.Fatalf("finding %q: %v", f, err)
		}
		lines = append(lines, number)
	}
	return lines
}

func TestRegexBackrefProvesItselfAgainstTheFlaggedFixture(t *testing.T) {
	body := readBackrefFixture(t, "flagged.yml")
	var want []int
	for i, line := range strings.Split(string(body), "\n") {
		if strings.HasSuffix(strings.TrimRight(line, " \r"), "# EXPECT-FINDING") {
			want = append(want, i+1)
		}
	}
	if len(want) < 15 {
		t.Fatalf("the flagged fixture marks only %d findings; it lost cases", len(want))
	}
	findings := regexBackrefs("flagged.yml", body)
	if got := findingLines(t, "flagged.yml", findings); !slices.Equal(got, want) {
		t.Fatalf("finding lines\n got %v\nwant %v\n%s", got, want, strings.Join(findings, "\n"))
	}
}

func TestRegexBackrefIsSilentOnTheCleanFixture(t *testing.T) {
	findings := regexBackrefs("clean.yml", readBackrefFixture(t, "clean.yml"))
	if len(findings) != 0 {
		t.Fatalf("clean fixture reported:\n%s", strings.Join(findings, "\n"))
	}
}

func TestRegexBackrefNamesTheReceivedCountAndTheFix(t *testing.T) {
	// Each literal and each fix is quoted as it is written in the file.
	cases := []struct{ name, yaml, want string }{
		{
			"the original defect: a folded template",
			`v: >-` + "\n" + `  {{ s | regex_search('^V="([0-9.]*)"', '\\1', multiline=True) }}` + "\n",
			`t.yml:1: regex_search group argument '\\1' reaches the filter as 2 backslashes, not 1; ` +
				`in a block scalar's {{ }} write '\1'`,
		},
		{
			"a double-quoted conditional",
			`when: "s | regex_search('a(b)', '\\1')"` + "\n",
			`t.yml:1: regex_search group argument '\\1' reaches the filter as a control character, not a backslash; ` +
				`in a double-quoted scalar's conditional write '\\\\1'`,
		},
		{
			"a replacement in a single-quoted template",
			`v: '{{ s | regex_replace("a(b)", "\\1") }}'` + "\n",
			`t.yml:1: regex_replace argument "\\1" reaches the filter as 2 backslashes, not 1; ` +
				`in a single-quoted scalar's {{ }} write "\1"`,
		},
		{
			"a plain statement",
			`v: x{% set r = s | regex_search('a(b)', '\1') %}` + "\n",
			`t.yml:1: regex_search group argument '\1' reaches the filter as a control character, not a backslash; ` +
				`in a plain scalar's {% %} write '\\1'`,
		},
		{
			"a literal-block conditional with three backslashes",
			"that:\n  - |\n" + `    s | regex_search('a(b)', '\\\1')` + "\n",
			`t.yml:2: regex_search group argument '\\\1' reaches the filter as a control character, not a backslash; ` +
				`in a block scalar's conditional write '\\1'`,
		},
		{
			"a double-quoted template with four backslashes",
			`v: "{{ s | regex_search('a(b)', '\\\\1') }}"` + "\n",
			`t.yml:1: regex_search group argument '\\\\1' reaches the filter as 2 backslashes, not 1; ` +
				`in a double-quoted scalar's {{ }} write '\\1'`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := regexBackrefs("t.yml", []byte(c.yaml)); !slices.Equal(got, []string{c.want}) {
				t.Fatalf("\n got %q\nwant %q", got, []string{c.want})
			}
		})
	}
}

func TestRegexBackrefReadsEveryDocumentAndNestedTask(t *testing.T) {
	text := "---\na: 1\n---\n- hosts: all\n  tasks:\n    - block:\n        - ansible.builtin.set_fact:\n" +
		"            v: >-\n              {{ s | regex_search('a(b)', '\\\\1') }}\n"
	got := regexBackrefs("t.yml", []byte(text))
	if lines := findingLines(t, "t.yml", got); !slices.Equal(lines, []int{8}) {
		t.Fatalf("got %q", got)
	}
}

func TestRegexBackrefIgnoresWhatIsNotARegexFilterArgument(t *testing.T) {
	for _, text := range []string{
		`v: "{{ s | regex_search('a\\.(b)') }}"`,                                   // a pattern escape, not a group
		"v: >-\n" + `  {{ s | regex_findall('(a)(b)') }} '\\1'`,                    // after the template ends
		"v: >-\n" + `  {{ regex_search }}{{ x('\\1') }}`,                           // the name, never called
		"v: >-\n" + `  {{ s | regex_replace('a', 'b') ~ '\\1' }}`,                  // after the call's parentheses
		"v: >-\n" + `  {# regex_search('a(b)', '\\1') #}`,                          // a Jinja comment
		"v: >-\n" + `  {{ s | map('upper', '\\1') }}`,                              // map of another filter
		`v: !unsafe "{{ s | regex_search('a(b)', '\\\\1') }}"`,                     // never templated
		"v: >-\n" + `  {% raw %}{{ s | regex_search('a(b)', '\\1') }}{% endraw %}`, // a raw block
		"- 1\n- [2, 3]\n",
		"",
	} {
		if got := regexBackrefs("t.yml", []byte(text)); len(got) != 0 {
			t.Errorf("%q: got %q", text, got)
		}
	}
}

func TestRegexBackrefFlagsWhatANaiveReadingWouldMiss(t *testing.T) {
	for _, text := range []string{
		`v: >-` + "\n" + `  {{ l | map('ansible.builtin.regex_replace', 'a(b)', '\\1') | list }}` + "\n",
		`v: >-` + "\n" + `  {{ "}}" ~ s | regex_search('a(b)', '\\1') }}` + "\n", // a quoted close does not end it
		`v: >-` + "\n" + `  {{ s | regex_search('a(b)', 'x' ~ '\\1') }}` + "\n",  // a concatenated argument
		`v: >-` + "\n" + `  {{ s | regex_replace('(a)\\1', 'b') }}` + "\n",       // a backreference in a pattern
		`v: >-` + "\n" + `  {% raw %}x{% endraw %}{{ s | regex_search('a(b)', '\\1') }}` + "\n",
	} {
		if got := regexBackrefs("t.yml", []byte(text)); len(got) != 1 {
			t.Errorf("%q: got %q", text, got)
		}
	}
}

func TestRegexBackrefAllowsAnEscapedBackslashBeforeAReplacementBackreference(t *testing.T) {
	// A replacement may put a literal backslash before group 1: three reach the filter.
	if got := regexBackrefs("t.yml", []byte(`v: >-`+"\n"+`  {{ s | regex_replace('a(b)', '\\\1') }}`+"\n")); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestRegexBackrefStopsAtAnUnterminatedTemplate(t *testing.T) {
	if got := regexBackrefs("t.yml", []byte("v: >-\n  {{ s | regex_search('a(b)', '\\\\1')\n")); len(got) != 1 {
		t.Fatalf("an unterminated template is still read to its end: got %q", got)
	}
}

func TestRegexBackrefReportsYAMLItCannotParse(t *testing.T) {
	got := regexBackrefs("t.yml", []byte("a: 1\nb: [1\n"))
	if len(got) != 1 || !strings.HasPrefix(got[0], "t.yml:") || !strings.Contains(got[0], "cannot be parsed as YAML") {
		t.Fatalf("got %q", got)
	}
	if got := regexBackrefs("t.yml", []byte("a: \"\x00\"\n")); len(got) != 1 || !strings.HasPrefix(got[0], "t.yml:1: ") {
		t.Fatalf("a parse error naming no line is reported at line 1: got %q", got)
	}
}
