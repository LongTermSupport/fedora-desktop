package ghaccount

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestReadAccountsInFileOrder(t *testing.T) {
	text := "user_login: someone\n" +
		"secret: !vault |\n  $ANSIBLE_VAULT;1.1;AES256\n  6162\n" +
		"github_accounts:\n  work: \"jane-corp\"\n  oss: jane\n  num: 1234\n"
	got, err := parseAccounts([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	want := []account{{"work", "jane-corp"}, {"oss", "jane"}, {"num", "1234"}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestReadAccountsNoneIsNotAnError(t *testing.T) {
	for _, text := range []string{"a: 1\n", "github_accounts:\n", "github_accounts: {}\n", "github_accounts: ~\n", ""} {
		got, err := parseAccounts([]byte(text))
		if err != nil || len(got) != 0 {
			t.Errorf("%q: got %v, %v", text, got, err)
		}
	}
}

func TestReadAccountsRefusesWhatCannotBeUsed(t *testing.T) {
	cases := map[string]string{
		"not a mapping":     "github_accounts: [a, b]\n",
		"null username":     "github_accounts:\n  work:\n",
		"vaulted username":  "github_accounts:\n  work: !vault |\n    $ANSIBLE_VAULT;1.1;AES256\n    61\n",
		"list username":     "github_accounts:\n  work: [a]\n",
		"repeated alias":    "github_accounts:\n  work: a\n  work: b\n",
		"bad alias":         "github_accounts:\n  ../x: a\n",
		"bad username":      "github_accounts:\n  work: a_b\n",
		"not yaml":          "github_accounts: [\n",
		"top is not a map":  "- a\n",
		"case-repeated key": "github_accounts:\n  Work: a\n  work: b\n",
	}
	for name, text := range cases {
		if _, err := parseAccounts([]byte(text)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAddAccountText(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{
			"after the block's last entry",
			"a: 1\ngithub_accounts:\n  work: \"x\"\n  # note\n  oss: \"y\"\n\nother: 2\n",
			"a: 1\ngithub_accounts:\n  work: \"x\"\n  # note\n  oss: \"y\"\n  new: \"jane\"\n\nother: 2\n",
		},
		{
			"keeps the block's indent",
			"github_accounts:\n    work: x\nz: 1\n",
			"github_accounts:\n    work: x\n    new: \"jane\"\nz: 1\n",
		},
		{
			"an empty flow mapping becomes a block",
			"github_accounts: {}\nz: 1\n",
			"github_accounts:\n  new: \"jane\"\nz: 1\n",
		},
		{
			"a key with no value",
			"github_accounts:\nz: 1\n",
			"github_accounts:\n  new: \"jane\"\nz: 1\n",
		},
		{
			"no key: appended",
			"z: 1",
			"z: 1\n\ngithub_accounts:\n  new: \"jane\"\n",
		},
		{
			"a nested key of the same name is not the top-level one",
			"top:\n  github_accounts:\n    work: x\n",
			"top:\n  github_accounts:\n    work: x\n\ngithub_accounts:\n  new: \"jane\"\n",
		},
		{
			"at the end of the file",
			"github_accounts:\n  work: x",
			"github_accounts:\n  work: x\n  new: \"jane\"\n",
		},
	}
	for _, c := range cases {
		got, err := addAccountText([]byte(c.in), account{"new", "jane"})
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s:\ngot  %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestAddAccountTextRefusesWhatItCannotProve(t *testing.T) {
	cases := map[string]string{
		"a flow mapping with entries": "github_accounts: {work: x}\n",
		"an anchor and alias":         "base: &b {work: x}\ngithub_accounts: *b\n",
		"already holds the alias":     "github_accounts:\n  new: jane\n",
	}
	for name, in := range cases {
		if _, err := addAccountText([]byte(in), account{"new", "jane"}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAddToConfigWritesThroughASymlinkAndKeepsTheMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.yml")
	if err := os.WriteFile(target, []byte("github_accounts:\n  work: x\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	// Past the umask, which may narrow what WriteFile made.
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "localhost.yml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := addToConfig(link, account{"oss", "jane"}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was replaced: %v %v", fi, err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "  oss: \"jane\"\n") {
		t.Fatalf("not added: %q", data)
	}
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

func TestAddToConfigLeavesAnUnprovableFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "localhost.yml")
	in := "github_accounts: {work: x}\n"
	if err := os.WriteFile(path, []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := addToConfig(path, account{"oss", "jane"}); err == nil {
		t.Fatal("accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != in {
		t.Fatalf("changed: %q", data)
	}
	if err := addToConfig(filepath.Join(t.TempDir(), "missing.yml"), account{"oss", "jane"}); err == nil {
		t.Fatal("a missing file was accepted")
	}
}
