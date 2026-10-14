package ansiblepause

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestE8PauseKeyForms(t *testing.T) {
	expectLines(t, "builtin", joinLines("- name: x", "  ansible.builtin.pause:", "    seconds: 1"), 2)
	expectLines(t, "legacy", joinLines("- name: x", "  ansible.legacy.pause:"), 2)
	expectLines(t, "bare", joinLines("- name: x", "  pause:"), 2)
	expectLines(t, "on the dash line", joinLines("- ansible.builtin.pause:", "    seconds: 1"), 1)
	expectLines(t, "value on the key line", joinLines("- name: x", "  pause: {seconds: 3}"), 2)
	expectLines(t, "prose", joinLines("- name: x", "  ansible.builtin.debug:", "    msg: uses ansible.builtin.pause in prose"))
	expectLines(t, "longer key", joinLines("- name: x", "  pausex:", "  unpause:", "  my.pause:"))
}

// E8/E10: a dash may be followed by more than one space; the task's keys then sit further right.
func TestE8ListIndentationVariants(t *testing.T) {
	expectLines(t, "wide dash, pause on the dash line", joinLines("-   pause:", "      seconds: 1"), 1)
	expectLines(t, "wide dash, builtin", joinLines("-   ansible.builtin.pause:", "      seconds: 1"), 1)
	expectLines(t, "wide dash, registered below", joinLines("-   pause:", "    register: r"))
	expectLines(t, "wide dash, register before", joinLines("-   register: r", "    pause:"))
	expectLines(t, "wide dash, register on the dash line", joinLines("-   register: r", "    pause:", "      seconds: 1"))
	expectLines(t, "wide dash head, key below", joinLines("-   name: a", "    pause:"), 2)
	expectLines(t, "wide dash head, registered", joinLines("-   name: a", "    pause:", "    register: r"))
	expectLines(t, "wide dash, register too shallow", joinLines("-   name: a", "    pause:", "  register: r"), 2)
	expectLines(t, "wide dash, annotated", joinLines("-   name: a  # PAUSE-OK: why", "    pause:"))
	expectLines(t, "indented list", joinLines("tasks:", "  -   name: a", "      pause:", "  - name: b", "    pause:"), 3, 5)
	expectLines(t, "dash alone then keys", joinLines("-", "  pause:"), 2)
	expectLines(t, "nested sequence under a key", joinLines("block:", "  - name: a", "    pause:", "    register: r"))
}

func TestE9TaskExtentAndHead(t *testing.T) {
	// A comment at the dash indent ends the task, so the next task's register is not this one's.
	expectLines(t, "next task registers", joinLines(
		"- name: a", "  pause:", "    seconds: 1",
		"# a comment at the dash indent",
		"- name: b", "  command: x", "  register: r"), 2)
	// A pause key not on a dash line belongs to the nearest less-indented head.
	expectLines(t, "key under head", joinLines("- name: a", "  when: x", "  pause:", "    seconds: 1", "  register: r"))
	// No head at all: not judged.
	expectLines(t, "no head", joinLines("pause:", "  seconds: 1"))
	// Blank and comment lines between do not hide the head.
	expectLines(t, "head past comment", joinLines("- name: a", "", "# note", "  pause:"), 4)
}

func TestE10Registered(t *testing.T) {
	expectLines(t, "after", joinLines("- name: a", "  pause:", "    prompt: p", "  register: r"))
	expectLines(t, "before", joinLines("- name: a", "  register: r", "  pause:", "    prompt: p"))
	expectLines(t, "dash line", joinLines("- register: r", "  pause:"))
	expectLines(t, "nested register", joinLines("- name: a", "  pause:", "    prompt: |", "      register: text"), 2)
	expectLines(t, "args register", joinLines("- name: a", "  pause:", "  args:", "    register: x"), 2)
	expectLines(t, "other task", joinLines("- name: a", "  pause:", "- name: b", "  register: r"), 2)
	expectLines(t, "in a block", joinLines(
		"- name: blk", "  block:", "    - name: a", "      pause:", "      register: r",
		"    - name: b", "      pause:"), 7)
}

func TestE11Annotated(t *testing.T) {
	expectLines(t, "on the name line", joinLines("- name: a  # PAUSE-OK: waits for login", "  pause:"))
	expectLines(t, "on the key line", joinLines("- name: a", "  pause:  # PAUSE-OK: confirm", "    prompt: p"))
	expectLines(t, "on a later line", joinLines("- name: a", "  pause:", "    seconds: 1  # PAUSE-OK: socket wait"))
	expectLines(t, "above", joinLines("# PAUSE-OK: fixed wait", "- name: a", "  pause:"))
	expectLines(t, "above run", joinLines("# PAUSE-OK: fixed wait", "# more words", "- name: a", "  pause:"))
	expectLines(t, "blank breaks the run", joinLines("# PAUSE-OK: fixed wait", "", "- name: a", "  pause:"), 4)
	expectLines(t, "empty reason", joinLines("- name: a  # PAUSE-OK:", "  pause:"), 2)
	expectLines(t, "blank reason", joinLines("- name: a  # PAUSE-OK:   ", "  pause:"), 2)
	expectLines(t, "annotation of the previous task", joinLines(
		"- name: a  # PAUSE-OK: yes", "  pause:", "- name: b", "  pause:"), 4)
	expectLines(t, "far comment", joinLines("# PAUSE-OK: x", "- name: z", "  debug:", "# other", "- name: a", "  pause:"), 6)
}

func TestE7LineEndsAndIndent(t *testing.T) {
	expectLines(t, "crlf", "- name: a\r\n  pause:\r\n    seconds: 1\r\n", 2)
	expectLines(t, "crlf annotated", "- name: a  # PAUSE-OK: why\r\n  pause:\r\n")
	expectLines(t, "no final newline", "- name: a\n  pause:", 2)
	expectLines(t, "tab is not indent", "- name: a\n\tpause:\n")
}

func TestE12OneFindingPerPauseKey(t *testing.T) {
	expectLines(t, "two", joinLines("- name: a", "  pause:", "- name: b", "  pause:"), 2, 4)
	expectLines(t, "empty", "")
}

// fixtureDir is the repository's fixtures for the rule (src-go/qa/ansiblepause is three below the root).
var fixtureDir = filepath.Join("..", "..", "..", "tests", "fixtures", "ansible-pause")

func TestE17RuleProvesItselfAgainstTheFlaggedFixture(t *testing.T) {
	body, err := fs.ReadFile(os.DirFS(fixtureDir), "flagged.yml")
	if err != nil {
		t.Fatal(err)
	}
	var want []int
	for i, line := range strings.Split(string(body), "\n") {
		if strings.HasSuffix(strings.TrimRight(line, " \r"), "# EXPECT-FINDING") {
			want = append(want, i+1)
		}
	}
	if len(want) == 0 {
		t.Fatal("flagged.yml marks no EXPECT-FINDING line; the red case proves nothing")
	}
	if got := findingsOf(string(body)); !slices.Equal(got, want) {
		t.Fatalf("flagged fixture: reported %v, expected %v", got, want)
	}
}

func TestE17RuleProvesItselfAgainstTheCleanFixture(t *testing.T) {
	body, err := fs.ReadFile(os.DirFS(fixtureDir), "clean.yml")
	if err != nil {
		t.Fatal(err)
	}
	if got := findingsOf(string(body)); len(got) != 0 {
		t.Fatalf("clean fixture reported lines %v", got)
	}
}
