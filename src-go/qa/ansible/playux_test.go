package ansible

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// playUXFixtureDir is the repository's fixtures for the four play-UX rules.
var playUXFixtureDir = filepath.Join("..", "..", "..", "tests", "fixtures", "ansible-play-ux")

func readPlayUXFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(playUXFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// expectMarker is a fixture line's "# EXPECT-FINDING <rule-id> [<rule-id>...]".
var expectMarker = regexp.MustCompile(`# EXPECT-FINDING ([a-z -]+?)\s*$`)

// expectedFindings is the lines a fixture marks, by rule.
func expectedFindings(body []byte) map[string][]int {
	want := map[string][]int{}
	for i, line := range strings.Split(string(body), "\n") {
		if m := expectMarker.FindStringSubmatch(line); m != nil {
			for rule := range strings.FieldsSeq(m[1]) {
				want[rule] = append(want[rule], i+1)
			}
		}
	}
	return want
}

// findingShape is "<rel>:<line>: <rule-id>: <why>".
var findingShape = regexp.MustCompile(`^[^:]+:([0-9]+): ([a-z-]+): `)

// gotFindings is the finding lines by rule.
func gotFindings(t *testing.T, findings []string) map[string][]int {
	t.Helper()
	got := map[string][]int{}
	for _, f := range findings {
		m := findingShape.FindStringSubmatch(f)
		if m == nil {
			t.Fatalf("finding %q is not <file>:<line>: <rule>: <why>", f)
		}
		line, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatal(err)
		}
		got[m[2]] = append(got[m[2]], line)
	}
	return got
}

func TestPlayUXProvesEachRuleAgainstTheFlaggedFixture(t *testing.T) {
	body := readPlayUXFixture(t, "flagged.yml")
	want := expectedFindings(body)
	minimum := map[string]int{ruleSettingLate: 6, ruleVaultByHand: 4, ruleCLIChdir: 5, ruleNoLogHides: 6, ruleProbeFirst: 3}
	for rule, n := range minimum {
		if len(want[rule]) < n {
			t.Fatalf("the flagged fixture marks only %d %s findings; it lost cases", len(want[rule]), rule)
		}
	}
	findings := playUX("flagged.yml", body, nil)
	got := gotFindings(t, findings)
	for rule := range minimum {
		if !slices.Equal(got[rule], want[rule]) {
			t.Errorf("%s finding lines\n got %v\nwant %v", rule, got[rule], want[rule])
		}
	}
	if len(got) != len(minimum) {
		t.Errorf("unexpected rules reported: %v\n%s", got, strings.Join(findings, "\n"))
	}
}

func TestPlayUXIsSilentOnTheCleanFixture(t *testing.T) {
	findings := playUX("clean.yml", readPlayUXFixture(t, "clean.yml"), nil)
	if len(findings) != 0 {
		t.Fatalf("clean fixture reported:\n%s", strings.Join(findings, "\n"))
	}
}

func TestPlayUXTakesTheRepositoryVarsAsNoSetting(t *testing.T) {
	play := "- hosts: all\n  tasks:\n    - ansible.builtin.dnf:\n        name: x\n" +
		"    - ansible.builtin.assert:\n        that: fedora_version is defined\n"
	if got := playUX("p.yml", []byte(play), nil); len(got) != 1 {
		t.Fatalf("without repository vars: %q", got)
	}
	if got := playUX("p.yml", []byte(play), map[string]bool{"fedora_version": true}); len(got) != 0 {
		t.Fatalf("fedora_version is a repository var: %q", got)
	}
}

func TestPlayUXNamesTheRuleTheLineAndTheFix(t *testing.T) {
	cases := []struct{ name, yaml, want string }{
		{
			"the cli rule", "- ansible.builtin.command:\n    argv: [ansible-vault, encrypt_string]\n",
			"p.yml:1: " + ruleCLIChdir + ": ansible-vault runs outside the checkout, so it cannot find ansible.cfg " +
				"(whose vault password file, inventory and roles path are relative to it); add chdir: \"{{ root_dir }}\"",
		},
		{
			"the no_log rule", "- ansible.builtin.copy:\n    content: s\n    dest: /d\n  no_log: true\n",
			"p.yml:1: " + ruleNoLogHides + ": no_log hides why this copy task failed, so the run stops with no reason; " +
				"register it with failed_when: false (# FAIL-FAST-OK) and follow it with a fail task whose msg gives the non-secret reason",
		},
		{
			"the late rule", "- hosts: all\n  tasks:\n    - ansible.builtin.dnf:\n        name: x\n" +
				"    - ansible.builtin.assert:\n        that: some_password is defined\n",
			"p.yml:5: " + ruleSettingLate + ": the setting some_password is checked here, after the dnf task at line 3 has " +
				"already changed the system; check it, or ask for it, before the first change",
		},
		{
			"the by-hand rule", "- ansible.builtin.assert:\n    that: some_password is defined\n    fail_msg: run ansible-vault edit\n",
			"p.yml:1: " + ruleVaultByHand + ": the secret some_password is demanded by hand (the message sends the human " +
				"to ansible-vault); ask for it and save it with tasks/ask-and-save-secret.yml",
		},
	}
	for _, c := range cases {
		got := playUX("p.yml", []byte(c.yaml), nil)
		if !slices.Equal(got, []string{c.want}) {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestPlayUXReportsAFileThatIsNotYAML(t *testing.T) {
	got := playUX("bad.yml", []byte("a: [\n"), nil)
	if len(got) != 1 || !strings.HasPrefix(got[0], "bad.yml:") || !strings.Contains(got[0], "cannot be parsed as YAML") {
		t.Fatalf("got %q", got)
	}
}

func TestRepositoryVarsReadsTheTopLevelKeys(t *testing.T) {
	root := writeTree(t, map[string]string{
		"vars/a.yml": "---\nfedora_version: 44\nnested:\n  inner: 1\n",
		"environment/localhost/group_vars/desktop.yml":       "provisioning_profile: desktop\n",
		"environment/localhost/host_vars/localhost.yml.dist": "user_login: x\n",
	})
	got, err := repositoryVars(os.DirFS(root))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"fedora_version", "nested", "provisioning_profile"} {
		if !got[name] {
			t.Errorf("%s is not read: %v", name, got)
		}
	}
	if got["inner"] || got["user_login"] {
		t.Errorf("a nested key or a host setting is read as a repository var: %v", got)
	}
}
