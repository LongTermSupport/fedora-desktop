package gates

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// shippedTree is a repository root that ships ccy-token-usable as a Go command and tool-x as a
// file of files/home/.local/bin.
func shippedTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for path, body := range map[string]string{
		"scripts/go-binary-suites.tsv": "# command\tsuites\n\nccy-token-usable\tqa-go\n",
		"files/home/.local/bin/tool-x": "#!/usr/bin/bash\n",
	} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// scriptsOf is the scripts directory of a repository root.
func scriptsOf(root string) string { return filepath.Join(root, "scripts") }

// isolationOver is NewIsolation over a fresh shippedTree.
func isolationOver(t *testing.T, path, scratch string) Isolation {
	t.Helper()
	root := shippedTree(t)
	iso, err := NewIsolation(root, scriptsOf(root), path, scratch)
	if err != nil {
		t.Fatal(err)
	}
	return iso
}

// binDir is a directory of executables with the given names.
func binDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\necho "+name+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func envValue(env []string, key string) (string, bool) {
	for _, kv := range slices.Backward(env) {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v, true
		}
	}
	return "", false
}

func TestShippedCommandsAreTheGoCommandsAndTheUserBinFiles(t *testing.T) {
	root := shippedTree(t)
	names, err := ShippedCommands(root, scriptsOf(root))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names, []string{"ccy-token-usable", "tool-x"}) {
		t.Fatalf("got %v", names)
	}
}

func TestShippedCommandsOfTheRealTreeHoldTheLeakedOnes(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	names, err := ShippedCommands(root, scriptsOf(root))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ccy-token-usable", "claude-yolo", "ccy-sessions", "wsi-setting"} {
		if !slices.Contains(names, want) {
			t.Errorf("the real tree's shipped commands lack %s: %v", want, names)
		}
	}
}

func TestShippedCommandsRefusesATreeItCannotRead(t *testing.T) {
	root := shippedTree(t)
	if err := os.WriteFile(filepath.Join(root, "scripts", "go-binary-suites.tsv"), []byte("# only a comment\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ShippedCommands(root, scriptsOf(root)); err == nil || !strings.Contains(err.Error(), "no command") {
		t.Fatalf("an empty command table must be refused, got %v", err)
	}
	if _, err := ShippedCommands(root, t.TempDir()); err == nil {
		t.Fatal("a tree without the command table must be refused")
	}
	root = shippedTree(t)
	if err := os.RemoveAll(filepath.Join(root, "files")); err != nil {
		t.Fatal(err)
	}
	if _, err := ShippedCommands(root, scriptsOf(root)); err == nil {
		t.Fatal("a tree without files/home/.local/bin must be refused")
	}
}

// The locale and the shell's inheritance: a suite sees C.UTF-8 whatever the machine's locale,
// and none of the machine's exported functions or startup files.
func TestIsolationPinsTheLocaleAndDropsTheShellsInheritance(t *testing.T) {
	c := isolationOver(t, "/usr/bin:/bin", t.TempDir()).Apply(proc.Cmd{Argv: []string{"bash", "x"}, Env: []string{"QA_JSON_OUT=/r"}})
	if v, ok := envValue(c.Env, "LC_ALL"); !ok || v != SuiteLocale {
		t.Errorf("LC_ALL is %q, want %s", v, SuiteLocale)
	}
	if v, ok := envValue(c.Env, "QA_JSON_OUT"); !ok || v != "/r" {
		t.Errorf("the caller's own Env was lost: %v", c.Env)
	}
	if c.Drop == nil {
		t.Fatal("no Drop: the machine's environment reaches the suite whole")
	}
	for _, key := range []string{"LANG", "LANGUAGE", "LC_COLLATE", "LC_ALL", "BASH_FUNC_which%%", "BASH_ENV", "ENV", "PATH"} {
		if !c.Drop(key) {
			t.Errorf("%s is inherited", key)
		}
	}
	for _, key := range []string{"HOME", "TMPDIR", "QA_FULL_PRIORITY", "GOPATH"} {
		if c.Drop(key) {
			t.Errorf("%s is dropped", key)
		}
	}
}

func TestIsolationHidesTheShippedCommandsFromPathAndKeepsTheRest(t *testing.T) {
	user := binDir(t, "ccy-token-usable", "tool-x", "semgrep")
	plain := binDir(t, "unrelated")
	scratch := t.TempDir()
	iso := isolationOver(t, strings.Join([]string{user, plain, "/usr/bin", ""}, ":"), scratch)
	path, ok := envValue(iso.Apply(proc.Cmd{}).Env, "PATH")
	if !ok {
		t.Fatal("no PATH set")
	}
	dirs := strings.Split(path, ":")
	if len(dirs) != 4 || dirs[1] != plain || dirs[2] != "/usr/bin" || dirs[3] != "" {
		t.Fatalf("PATH %q: only a directory holding a shipped command is replaced, in place", path)
	}
	if !strings.HasPrefix(dirs[0], scratch+string(os.PathSeparator)) {
		t.Fatalf("%s holds shipped commands but was kept: %q", user, path)
	}
	entries, err := os.ReadDir(dirs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "semgrep" {
		t.Fatalf("the stand-in for %s holds %v, want semgrep alone", user, entries)
	}
	target, err := os.Readlink(filepath.Join(dirs[0], "semgrep"))
	if err != nil || target != filepath.Join(user, "semgrep") {
		t.Fatalf("semgrep links to %q (%v)", target, err)
	}
}

func TestIsolationLeavesAnEmptyPathAlone(t *testing.T) {
	c := isolationOver(t, "", t.TempDir()).Apply(proc.Cmd{})
	if _, ok := envValue(c.Env, "PATH"); ok || c.Drop("PATH") {
		t.Fatalf("an unset PATH must stay unset, not become an empty one: %v", c.Env)
	}
}

// End to end through the real process seam: the machine's locale, an exported function and an
// installed copy of a shipped command on PATH, as the host that found the class had them.
func TestASuiteRunUnderIsolationSeesNoneOfTheMachine(t *testing.T) {
	user := binDir(t, "ccy-token-usable", "semgrep")
	t.Setenv("LANG", "en_GB.UTF-8")
	t.Setenv("LC_COLLATE", "en_GB.UTF-8")
	t.Setenv("BASH_FUNC_which%%", "() {  command which \"$@\"\n}")
	t.Setenv("PATH", user+":/usr/bin:/bin")
	iso := isolationOver(t, os.Getenv("PATH"), t.TempDir())
	script := `echo "LC_ALL=$LC_ALL LANG=${LANG-unset} LC_COLLATE=${LC_COLLATE-unset}"
declare -F which >/dev/null && echo "which=exported" || echo "which=none"
command -v ccy-token-usable >/dev/null && echo "ccy-token-usable=found" || echo "ccy-token-usable=absent"
semgrep`
	res, err := proc.OS{}.Run(context.Background(), iso.Apply(proc.Cmd{Argv: []string{"bash", "-c", script}, Merge: true}))
	if err != nil {
		t.Fatal(err)
	}
	want := "LC_ALL=C.UTF-8 LANG=unset LC_COLLATE=unset\nwhich=none\nccy-token-usable=absent\nsemgrep\n"
	if res.Stdout != want {
		t.Fatalf("the suite saw\n%s\nwant\n%s", res.Stdout, want)
	}
}
