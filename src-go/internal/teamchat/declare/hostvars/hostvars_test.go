package hostvars

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
)

// valid is a host_vars file holding v1 keys, a vaulted value and anchors beside the v2 keys.
const valid = `---
# the owner's host_vars
user_login: someone
some_password: !vault |
  $ANSIBLE_VAULT;1.1;AES256
  6162636465
legacy_flag: yes
base: &base {a: 1}
derived: *base
teamchat_address: 192.0.2.10
teamchat_teams:
  - team: alpha
    state: present
    port: 8448
    description: "The team that builds the thing."
    listen: [192.0.2.10]
    allow_from:
      - 192.0.2.0/24
    humans: [alice, 'bob']
    coordinator: myrepo.1+desk.podman
    trust:
      kind: github
      org: example-org
      repos:
        - repo: example-org/thing
          branches: [main]
  - team: gone
    state: absent
    purge: true
teamchat_lxc_members:
  - team: alpha
    container: box-1
    user: agent
    checkout: /srv/checkouts/thing
    seat: 3
    human_text: false
teamchat_element_teams:
  - team: alpha
`

func TestReadValid(t *testing.T) {
	d, err := Read([]byte(valid))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if d.Address != "192.0.2.10" || len(d.Teams) != 2 || len(d.LXC) != 1 || len(d.Element) != 1 {
		t.Fatalf("decoded %+v", d)
	}
	a := d.Teams[0]
	if a.Port != 8448 || a.Humans[1] != "bob" || a.Listen[0] != "192.0.2.10" || a.URL != "http://192.0.2.10:8448" ||
		a.Trust.Repos[0].Branches[0] != "main" {
		t.Fatalf("team %+v", a)
	}
	if m := d.LXC[0]; m.Seat != 3 || m.HumanText {
		t.Fatalf("lxc %+v", m)
	}
	if !d.Teams[1].Purge {
		t.Fatalf("absent team %+v", d.Teams[1])
	}
}

func TestReadNoV2Keys(t *testing.T) {
	d, err := Read([]byte("user_login: someone\nflag: on\n"))
	if err != nil || len(d.Teams) != 0 || d.Address != "" {
		t.Fatalf("no v2 keys: %+v, %v", d, err)
	}
}

// with puts value as an absent team's name, so the scalar rules are tried where a string is
// wanted.
func with(value string) string {
	return "teamchat_teams:\n  - team: alpha\n    state: absent\n    name: " + value + "\n"
}

func TestReadRefusesNonStringPlainScalars(t *testing.T) {
	for _, s := range []string{
		"yes", "No", "ON", "off", "y", "N", "True", "FALSE", "null", "Null", "~", "", "012", "0x1F", "0o17",
		"0b101", "1:30", "190:20:30", "1.5", "1e3", ".inf", "-.Inf", ".nan", "2001-01-01", "2001-1-1 10:00:00",
		"<<", "=", "+5", "1_000", "-0", "99999999999999999999",
	} {
		_, err := Read([]byte(with(s)))
		fe, ok := errors.AsType[*declare.FieldError](err)
		if !ok || fe.Field != "teamchat_teams[0].name" {
			t.Errorf("%q: err = %v, want a FieldError at teamchat_teams[0].name", s, err)
		}
	}
}

func TestReadScalarTypes(t *testing.T) {
	// A quoted scalar is a string whatever it holds; a plain decimal integer is an integer.
	d, err := Read([]byte("teamchat_teams:\n  - team: alpha\n    state: absent\n    name: 'yes'\n    port: 8448\n"))
	if err != nil || d.Teams[0].Name != "yes" || d.Teams[0].Port != 8448 {
		t.Fatalf("quoted yes: %+v, %v", d, err)
	}
	if _, err = Read([]byte("teamchat_teams:\n  - team: alpha\n    state: absent\n    port: '8448'\n")); err == nil {
		t.Fatal("a quoted port was read as an integer")
	}
	d, err = Read([]byte("teamchat_teams:\n  - team: alpha\n    state: absent\n    purge: true\n    name: |-\n      Alpha\n"))
	if err != nil || !d.Teams[0].Purge || d.Teams[0].Name != "Alpha" {
		t.Fatalf("block scalar and true: %+v, %v", d, err)
	}
	d, err = Read([]byte("teamchat_teams:\n  - {team: alpha, state: absent, port: -1}\n"))
	if fe, ok := errors.AsType[*declare.FieldError](err); !ok || fe.Field != "teamchat_teams[0].port" {
		t.Fatalf("a negative integer reaches the declaration's port rule: %+v, %v", d, err)
	}
}

func TestReadRefusesByName(t *testing.T) {
	cases := map[string]struct{ doc, field, problem string }{
		"vault": {
			"teamchat_teams:\n  - team: alpha\n    state: absent\n    name: !vault |\n      $ANSIBLE_VAULT;1.1;AES256\n      61\n",
			"teamchat_teams[0].name", "vault",
		},
		"tag":       {"teamchat_teams:\n  - team: !!str alpha\n    state: absent\n", "teamchat_teams[0].team", "tag"},
		"anchor":    {"teamchat_teams:\n  - &a {team: alpha, state: absent}\n", "teamchat_teams[0]", "anchor"},
		"alias":     {"x: &a {team: alpha, state: absent}\nteamchat_teams:\n  - *a\n", "teamchat_teams[0]", "alias"},
		"duplicate": {"teamchat_teams:\n  - team: alpha\n    team: beta\n    state: absent\n", "teamchat_teams[0].team", "twice"},
		"top duplicate": {
			"teamchat_teams: []\nteamchat_teams: []\n", "teamchat_teams", "twice",
		},
		"merge key": {
			"teamchat_teams:\n  - <<: {team: alpha}\n    state: absent\n", "teamchat_teams[0].<<", "merge",
		},
		"top merge key": {"<<: {teamchat_teams: []}\n", "<<", "merge"},
		"top alias key": {"x: &k teamchat_teams\n*k : []\n", "the file", "alias"},
		// PyYAML merges by the key's tag, not its text, so any tagged key is refused.
		"top tagged merge key": {"!!merge \"<<\": {teamchat_teams: []}\n", "<<", "tag"},
		"top tagged other key": {"!!merge hidden: {teamchat_teams: []}\n", "hidden", "tag"},
		"top tag handle key": {
			"%TAG !m! tag:yaml.org,2002:\n---\n!m!merge hidden: {teamchat_teams: []}\n", "hidden", "tag",
		},
		// Ansible templates every string it reads, so the play would not see the text read here.
		"template": {
			"teamchat_teams:\n  - team: alpha\n    state: absent\n    name: \"{{ x }}\"\n", "teamchat_teams[0].name", "templat",
		},
		"template statement": {
			"teamchat_teams:\n  - team: alpha\n    state: absent\n    name: a {% if x %}b{% endif %}\n", "teamchat_teams[0].name", "templat",
		},
		"template comment": {
			"teamchat_teams:\n  - team: alpha\n    state: absent\n    name: |\n      a {# x #}\n", "teamchat_teams[0].name", "templat",
		},
		"map key":     {"teamchat_teams:\n  - ? [a]\n    : 1\n", "teamchat_teams[0]", "key"},
		"unknown key": {"teamchat_teams:\n  - team: alpha\n    state: absent\n    base_url: x\n", "teamchat_teams[0].base_url", "known"},
	}
	for name, c := range cases {
		_, err := Read([]byte(c.doc))
		fe, ok := errors.AsType[*declare.FieldError](err)
		if !ok || fe.Field != c.field || !strings.Contains(fe.Problem, c.problem) {
			t.Errorf("%s: err = %v, want %s %q", name, err, c.field, c.problem)
		}
	}
}

func TestReadRefusesAFileThatIsNotOneMapping(t *testing.T) {
	for name, doc := range map[string]string{
		"list":      "- a\n",
		"scalar":    "hello\n",
		"empty":     "",
		"two docs":  "a: 1\n---\nb: 2\n",
		"not yaml":  "a: [\n",
		"oversized": "a: '" + strings.Repeat("x", maxFile) + "'\n",
	} {
		if _, err := Read([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "box.yml")
	if err := os.WriteFile(file, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if d, err := ReadFile(file); err != nil || len(d.Teams) != 2 {
		t.Fatalf("ReadFile: %+v, %v", d, err)
	}
	if _, err := ReadFile(filepath.Join(dir, "none.yml")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "box"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(file); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("a host_vars directory beside the file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "other.yml"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(filepath.Join(dir, "other.yml")); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("a directory at the path: %v", err)
	}
}

func TestReadFileRefuses(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.yml")
	if err := os.WriteFile(big, []byte("a: '"+strings.Repeat("x", maxFile)+"'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(big); err == nil {
		t.Fatal("an oversized file was read")
	}
	bad := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(bad, []byte("teamchat_teams: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(bad); err == nil || !strings.Contains(err.Error(), bad) {
		t.Fatalf("a refused file is not named: %v", err)
	}
}
