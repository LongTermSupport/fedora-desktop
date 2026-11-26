package hostvars

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const sample = `# top comment
alpha: 1
listed:
  - a
  - b
# a comment inside the block
  - c

omega: [1, 2]
`

const vaultOut = `item: !vault |
          $ANSIBLE_VAULT;1.2;AES256;localhost
          6162636465
`

func mustTop(t *testing.T, text string) *yaml.Node {
	t.Helper()
	top, err := TopMapping([]byte(text))
	if err != nil {
		t.Fatalf("TopMapping: %v", err)
	}
	return top
}

func TestTopMapping(t *testing.T) {
	if top := mustTop(t, ""); top.Kind != yaml.MappingNode || len(top.Content) != 0 {
		t.Errorf("empty file = %+v, want an empty mapping", top)
	}
	if v := TopValue(mustTop(t, sample), "omega"); v == nil || v.Kind != yaml.SequenceNode {
		t.Errorf("omega = %+v", v)
	}
	if v := TopValue(mustTop(t, sample), "absent"); v != nil {
		t.Errorf("absent key = %+v", v)
	}
	for name, bad := range map[string]string{
		"not yaml": "a: [\n",
		"a list":   "- a\n",
		"a scalar": "word\n",
		"twice":    "a: 1\na: 2\n",
	} {
		if _, err := TopMapping([]byte(bad)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestUniqueKeys(t *testing.T) {
	var n yaml.Node
	if err := yaml.Unmarshal([]byte("x: 1\ny: 2\n"), &n); err != nil {
		t.Fatal(err)
	}
	if err := UniqueKeys(n.Content[0]); err != nil {
		t.Errorf("distinct keys refused: %v", err)
	}
}

func TestSplitLines(t *testing.T) {
	if got := SplitLines(""); got != nil {
		t.Errorf("empty = %q", got)
	}
	got := SplitLines("a\nb")
	if len(got) != 2 || got[1] != "b\n" {
		t.Errorf("no final newline = %q", got)
	}
}

func TestReplaceBlock(t *testing.T) {
	lines := SplitLines(sample)
	out := strings.Join(ReplaceBlock(lines, "listed", []string{"listed:\n", "  - z\n"}), "")
	want := "# top comment\nalpha: 1\nlisted:\n  - z\n\nomega: [1, 2]\n"
	if out != want {
		t.Errorf("replaced:\n%s\nwant:\n%s", out, want)
	}
	added := strings.Join(ReplaceBlock(SplitLines("a: 1\n"), "b", []string{"b: 2\n"}), "")
	if added != "a: 1\n\nb: 2\n" {
		t.Errorf("added = %q", added)
	}
	if got := ReplaceBlock(SplitLines("a: 1\n"), "b", nil); len(got) != 1 {
		t.Errorf("removing an absent key changed the text: %q", got)
	}
	removed := strings.Join(ReplaceBlock(SplitLines("a: 1\n\nb: 2\n\nc: 3\n"), "b", nil), "")
	if removed != "a: 1\n\nc: 3\n" {
		t.Errorf("removed = %q", removed)
	}
	if got := strings.Join(ReplaceBlock(nil, "b", []string{"b: 2\n"}), ""); got != "b: 2\n" {
		t.Errorf("into an empty file = %q", got)
	}
	// A key that only starts with the name is another key.
	if got := strings.Join(ReplaceBlock(SplitLines("listed_more: 1\n"), "listed", []string{"listed: 2\n"}), ""); got != "listed_more: 1\n\nlisted: 2\n" {
		t.Errorf("prefix key = %q", got)
	}
}

func TestSameNodeAndOthers(t *testing.T) {
	a, b := mustTop(t, sample), mustTop(t, sample)
	if !SameNode(a, b) {
		t.Error("the same text differs")
	}
	if SameNode(a, nil) || !SameNode(nil, nil) {
		t.Error("nil handling")
	}
	if err := SameOthers(a, b, "listed"); err != nil {
		t.Errorf("unchanged others: %v", err)
	}
	changed := mustTop(t, strings.Replace(sample, "omega: [1, 2]", "omega: [1, 3]", 1))
	if err := SameOthers(a, changed, "listed"); err == nil || !strings.Contains(err.Error(), "omega") {
		t.Errorf("a changed key: %v", err)
	}
	lost := mustTop(t, strings.Replace(sample, "omega: [1, 2]\n", "", 1))
	if err := SameOthers(a, lost, "listed"); err == nil || !strings.Contains(err.Error(), "other top-level keys") {
		t.Errorf("a lost key: %v", err)
	}
	if err := SameOthers(a, mustTop(t, strings.Replace(sample, "  - c\n", "", 1)), "listed"); err != nil {
		t.Errorf("an excepted key's change counted: %v", err)
	}
	aliased := mustTop(t, "x: &a [1]\ny: *a\n")
	if !SameNode(aliased, mustTop(t, "x: &b [1]\ny: *b\n")) {
		t.Error("equal aliases differ")
	}
}

func TestParseVault(t *testing.T) {
	v, err := ParseVault([]byte(vaultOut), "item")
	if err != nil {
		t.Fatalf("good output refused: %v", err)
	}
	if !strings.HasPrefix(v.Value, "$ANSIBLE_VAULT;1.2;AES256;localhost\n") {
		t.Errorf("value = %q", v.Value)
	}
	for _, bad := range []string{"", "item: plain\n", "other: !vault |\n  x\n", vaultOut + "extra: 1\n", "[\n", "item: !vault ''\n"} {
		if _, err := ParseVault([]byte(bad), "item"); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestWriteKeepingMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "vars.yml")
	if err := os.WriteFile(target, []byte("old\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o640); err != nil { // past the umask
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.yml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteKeepingMode(link, []byte("new\n")); err != nil {
		t.Fatalf("WriteKeepingMode: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "new\n" {
		t.Errorf("target = %q, %v", got, err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, %v", info.Mode(), err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced: %v", err)
	}
	if err := WriteKeepingMode(filepath.Join(dir, "missing.yml"), nil); err == nil {
		t.Error("a missing file was written")
	}
}
