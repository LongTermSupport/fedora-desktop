package ansible

import (
	"slices"
	"testing"
)

// The cases of scripts/test-qa-ansible-failfast.bash, which read the pattern out of the bash.
func TestFailFastPatternClassifies(t *testing.T) {
	cases := []struct {
		label string
		line  string
		want  bool
	}{
		{"failed_when: false", "      failed_when: false", true},
		{"failed_when: no", "      failed_when: no", true},
		{"failed_when: off", "      failed_when: off", true},
		{"failed_when: NO (case)", "      failed_when: NO", true},
		{"ignore_errors: true", "      ignore_errors: true", true},
		{"ignore_errors: yes", "      ignore_errors: yes", true},
		{"ignore_errors: on", "      ignore_errors: on", true},
		{"ignore_unreachable: true", "      ignore_unreachable: true", true},
		{"ignore_unreachable: yes", "      ignore_unreachable: yes", true},
		{"ignore_unreachable: on", "      ignore_unreachable: on", true},
		{"ignore_errors templated", `      ignore_errors: "{{ maybe }}"`, true},
		{"ignore_unreachable templated", `      ignore_unreachable: "{{ maybe }}"`, true},
		{"ignore_errors templated unquoted", "      ignore_errors: {{ maybe }}", true},
		{"ignore_errors double-quoted true", `      ignore_errors: "true"`, true},
		{"ignore_errors single-quoted yes", `      ignore_errors: 'yes'`, true},
		{"failed_when double-quoted false", `      failed_when: "false"`, true},
		{"failed_when single-quoted no", `      failed_when: 'no'`, true},
		{"ignore_unreachable single-quoted on", `      ignore_unreachable: 'on'`, true},
		{"ignore_errors single-quoted template", `      ignore_errors: '{{ maybe }}'`, true},
		{"ignore_unreachable single-quoted template", `      ignore_unreachable: '{{ maybe }}'`, true},
		{"quoted false is still off", `      ignore_errors: "false"`, false},
		{"quoted not is a condition", `      failed_when: "not foo.stat.exists"`, false},
		{"failed_when: not foo", "      failed_when: not foo.stat.exists", false},
		{"failed_when: nothing_matched", "      failed_when: nothing_matched", false},
		{"failed_when: true", "      failed_when: true", false},
		{"ignore_errors: false", "      ignore_errors: false", false},
		{"unrelated key", "      changed_when: false", false},
		{"tab separated", "failed_when:\tfalse", true},
		{"no separator", "failed_when:false", false},
		{"ignore_unreachable: false", "      ignore_unreachable: false", false},
		{"word boundary on", "      ignore_errors: online", false},
	}
	for _, c := range cases {
		if got := failFastPattern.MatchString(c.line); got != c.want {
			t.Errorf("%s: match=%v, want %v", c.label, got, c.want)
		}
	}
}

func TestStripCommentCutsAtAHashAfterSpaceOrAtTheStart(t *testing.T) {
	cases := map[string]string{
		"a: b # c":         "a: b ",
		"a: b\t# c":        "a: b\t",
		"# only":           "",
		"a: b#c":           "a: b#c",
		"a: '#x' # y":      "a: '#x' ",
		"  failed_when: x": "  failed_when: x",
		"a #b # c":         "a ",
	}
	for in, want := range cases {
		if got := stripComment(in); got != want {
			t.Errorf("stripComment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFailFastViolationsSkipAnnotationsAndComments(t *testing.T) {
	hits := []hit{
		{"tasks/a.yml", 1, "      failed_when: false"},
		{"tasks/a.yml", 2, "      failed_when: false  # FAIL-FAST-OK: probe"},
		{"tasks/a.yml", 3, "      failed_when: false  # fail-fast-ok: probe"},
		{"tasks/a.yml", 4, "      # failed_when: false was removed"},
		{"tasks/a.yml", 5, "      failed_when: false  # still bad"},
		{"tasks/fail-fast-ok.yml", 6, "      failed_when: false"},
		{"tasks/a:b.yml", 7, "      failed_when: false # not a comment cut at the path's colon"},
		{"tasks/a.yml", 8, "      msg: \"see # failed_when: false\""},
	}
	got := failFastViolations(hits)
	want := []string{
		"tasks/a.yml:1:      failed_when: false",
		"tasks/a.yml:5:      failed_when: false  # still bad",
		"tasks/fail-fast-ok.yml:6:      failed_when: false",
		"tasks/a:b.yml:7:      failed_when: false # not a comment cut at the path's colon",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}
