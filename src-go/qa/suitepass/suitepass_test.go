package suitepass

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, text string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func key(t *testing.T, root string, files []string, extra ...string) string {
	t.Helper()
	k, err := Key(root, files, extra...)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// The key is the inputs, not the order they were listed in.
func TestKeyIgnoresTheOrderOfTheFiles(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a", "1")
	write(t, root, "b/c", "2")
	if key(t, root, []string{"a", "b/c"}) != key(t, root, []string{"b/c", "a", "a"}) {
		t.Fatal("the same inputs in another order gave another key")
	}
}

// Any change to an input is a new key: content, a file added, a file gone, a file renamed, the
// extra words (the suite's command line, the toolchain).
func TestKeyChangesWithEveryInput(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a", "1")
	write(t, root, "b", "2")
	base := key(t, root, []string{"a", "b"}, "argv")
	seen := map[string]string{"base": base}
	check := func(name, k string) {
		t.Helper()
		for other, v := range seen {
			if v == k {
				t.Fatalf("%s gave the same key as %s", name, other)
			}
		}
		seen[name] = k
	}
	check("extra", key(t, root, []string{"a", "b"}, "argv2"))
	check("one file", key(t, root, []string{"a"}, "argv"))
	check("missing file", key(t, root, []string{"a", "b", "gone"}, "argv"))
	write(t, root, "b", "3")
	check("content", key(t, root, []string{"a", "b"}, "argv"))
	write(t, root, "c", "3")
	check("renamed", key(t, root, []string{"a", "c"}, "argv"))
	write(t, root, "gone", "")
	check("empty, not missing", key(t, root, []string{"a", "b", "gone"}, "argv"))
}

// Path and content cannot be traded: "ab" holding "c" is not "a" holding "bc".
func TestKeyFramesPathsAndContent(t *testing.T) {
	one, two := t.TempDir(), t.TempDir()
	write(t, one, "ab", "c")
	write(t, two, "a", "bc")
	if key(t, one, []string{"ab"}) == key(t, two, []string{"a"}) {
		t.Fatal("a path and its content ran together")
	}
}

// A directory or an unreadable input is an error, never a key.
func TestKeyRefusesAnInputItCannotRead(t *testing.T) {
	root := t.TempDir()
	write(t, root, "d/x", "1")
	if _, err := Key(root, []string{"d"}); err == nil {
		t.Fatal("a directory gave a key")
	}
}

// A store remembers a pass by key, and only that key.
func TestStoreRemembersAPassByKey(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a", "1")
	k1, k2 := key(t, root, []string{"a"}), key(t, root, []string{"a"}, "x")
	store := Store{Dir: filepath.Join(t.TempDir(), "not", "yet")}
	if passed, err := store.Passed(k1); err != nil || passed {
		t.Fatalf("fresh store: %v %v", passed, err)
	}
	if err := store.Record(k1); err != nil {
		t.Fatal(err)
	}
	if passed, err := store.Passed(k1); err != nil || !passed {
		t.Fatalf("recorded key: %v %v", passed, err)
	}
	if passed, err := store.Passed(k2); err != nil || passed {
		t.Fatalf("other key: %v %v", passed, err)
	}
}

// A key that is not a hex digest is refused, so a key can never name a path outside the store.
func TestStoreRefusesAKeyThatIsNotADigest(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	for _, bad := range []string{"", "../x", "a/b", "ZZ"} {
		if err := store.Record(bad); err == nil {
			t.Errorf("recorded %q", bad)
		}
		if _, err := store.Passed(bad); err == nil {
			t.Errorf("looked up %q", bad)
		}
	}
}

// The store lives where QA_SUITE_PASSES says, else under the user's cache directory; with
// neither, there is no store.
func TestDirFollowsTheEnvironment(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	if _, ok := Dir(get); ok {
		t.Fatal("no HOME, no XDG_CACHE_HOME, no QA_SUITE_PASSES: there is no store")
	}
	env["HOME"] = "/h"
	if d, ok := Dir(get); !ok || d != "/h/.cache/fedora-desktop-qa/suite-passes" {
		t.Fatalf("HOME: %q %v", d, ok)
	}
	env["XDG_CACHE_HOME"] = "/x"
	if d, ok := Dir(get); !ok || d != "/x/fedora-desktop-qa/suite-passes" {
		t.Fatalf("XDG_CACHE_HOME: %q %v", d, ok)
	}
	env["QA_SUITE_PASSES"] = "/q"
	if d, ok := Dir(get); !ok || d != "/q" {
		t.Fatalf("QA_SUITE_PASSES: %q %v", d, ok)
	}
}
