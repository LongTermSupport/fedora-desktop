package state

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
)

// classOf is the Class of err, or "" when it is no *Error.
func classOf(err error) Class {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Class
	}
	return ""
}

func wantClass(t *testing.T, what string, err error, want Class) {
	t.Helper()
	if got := classOf(err); got != want {
		t.Errorf("%s: class %q (%v), want %q", what, got, err, want)
	}
}

func writeRaw(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// S4: a regular file, never through a symlink, never blocking on a FIFO, at most its cap.
func TestReadRegularRefusesWhatIsNotARegularFileWithinItsCap(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	writeRaw(t, good, "12345")
	if raw, found, err := readRegular(good, 5); err != nil || !found || string(raw) != "12345" {
		t.Errorf("a file at its cap: %q %v %v", raw, found, err)
	}
	_, _, err := readRegular(good, 4)
	wantClass(t, "a file past its cap", err, ClassSize)
	_, found, err := readRegular(filepath.Join(dir, "absent"), 5)
	if found || err != nil {
		t.Errorf("an absent file: %v %v", found, err)
	}
	link := filepath.Join(dir, "link")
	if err = os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	_, _, err = readRegular(link, 5)
	wantClass(t, "a symlink", err, ClassFile)
	fifo := filepath.Join(dir, "fifo")
	if err = syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = readRegular(fifo, 5)
	wantClass(t, "a FIFO", err, ClassFile)
	_, _, err = readRegular(dir, 5)
	wantClass(t, "a directory", err, ClassFile)
	if _, _, err := readRegular(filepath.Join(good, "under"), 5); err == nil || classOf(err) != "" {
		t.Errorf("a path through a file is an I/O error, not a format class: %v", err)
	}
}

// S1: an error names the file and its class, never a value read from it.
func TestErrorNamesTheFileAndTheClass(t *testing.T) {
	err := refuse("/x/sync.json", ClassGrammar, "next_batch is not a sync token")
	if got := err.Error(); got != "/x/sync.json: next_batch is not a sync token (grammar)" {
		t.Errorf("got %q", got)
	}
}

// S15: v1's temporary name, so v1's inbox listing skips it.
func TestTemporaryNameIsTheOneV1Skips(t *testing.T) {
	v1 := regexp.MustCompile(`^\..+\.[0-9a-f]{16}\.tmp$`)
	a, err := tmpName("$x.json")
	if err != nil {
		t.Fatal(err)
	}
	b, err := tmpName("$x.json")
	if err != nil {
		t.Fatal(err)
	}
	if !v1.MatchString(a) || !strings.HasPrefix(a, ".$x.json.") || a == b {
		t.Errorf("temporaries %q and %q", a, b)
	}
}

// S15, S16, SB8: written whole, mode exactly 0600 under a umask that would take the owner's
// write bit, nothing left behind.
func TestWriteAtomicReplacesTheFileWithModeExactly0600(t *testing.T) {
	old := syscall.Umask(0o277)
	t.Cleanup(func() { syscall.Umask(old) })
	dir := t.TempDir()
	path := filepath.Join(dir, "f.json")
	writeRaw(t, path, "old")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "new" {
		t.Fatalf("%q %v", raw, err)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v %v", info.Mode(), err)
	}
	onlyFiles(t, dir, "f.json")
}

func onlyFiles(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("%s holds %v, want %v", dir, names, want)
	}
}

// S15: a failed write leaves the target as it was and no temporary.
func TestWriteAtomicRemovesItsTemporaryOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "d")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	writeRaw(t, filepath.Join(target, "x"), "")
	if err := writeAtomic(target, []byte("x")); err == nil {
		t.Fatal("renamed over a non-empty directory")
	}
	onlyFiles(t, dir, "d")
	if err := writeAtomic(filepath.Join(dir, "absent", "f"), []byte("x")); err == nil {
		t.Fatal("wrote into a missing directory")
	}
}

// S5: CPython's json.loads over strict UTF-8, duplicate keys refused (SB1), each failure
// classed.
func TestDecodeJSONClassesWhatItRefuses(t *testing.T) {
	cases := map[string]Class{
		"\xff":             ClassSyntax,
		`{"a":1`:           ClassSyntax,
		`{"a":1,"a":1}`:    ClassDuplicateKey,
		`[{"b":1,"b":2}]`:  ClassDuplicateKey,
		"\"\xed\xa0\x80\"": ClassSyntax,
	}
	for text, want := range cases {
		_, err := decodeJSON("/f", []byte(text))
		wantClass(t, text, err, want)
	}
	v, err := decodeJSON("/f", []byte(`{"b":NaN,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if o, objErr := object("/f", v); objErr != nil || len(o) != 2 {
		t.Errorf("an object: %v %v", o, objErr)
	}
	_, err = object("/f", []any{})
	wantClass(t, "a list as the file's object", err, ClassType)
}
