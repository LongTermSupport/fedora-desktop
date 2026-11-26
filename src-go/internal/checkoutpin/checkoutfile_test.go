package checkoutpin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// plantLink puts a link at name in dir, pointing at target.
func plantLink(t *testing.T, dir, name, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
}

const hostText = "HOST ONLY\n"

// hostFile is a host file a container would like the launcher to act on, outside the checkout.
func hostFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(checkout(t), "host-only")
	if err := os.WriteFile(path, []byte(hostText), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// keptHost fails the test unless the host file is as hostFile made it.
func keptHost(t *testing.T, path string) {
	t.Helper()
	if raw, err := os.ReadFile(path); err != nil || string(raw) != hostText {
		t.Fatalf("the host file %s was changed: %q %v", path, raw, err)
	}
}

// C1: a checkout file is read through an os.Root of the checkout: absent is not present, and a
// link at the name, a non-regular file, or a linked directory leaving the checkout is refused,
// naming the file, never followed.
func TestReadFileRefusesLinks(t *testing.T) {
	dir := checkout(t)
	if _, present, err := ReadFile(dir, EnvLocal); err != nil || present {
		t.Fatalf("no file: %v %v", present, err)
	}
	write(t, dir, EnvLocal, "export A=1\n")
	if content, present, err := ReadFile(dir, EnvLocal); err != nil || !present || string(content) != "export A=1\n" {
		t.Fatalf("a regular file: %q %v %v", content, present, err)
	}
	linked := checkout(t)
	plantLink(t, linked, EnvLocal, hostFile(t))
	content, _, err := ReadFile(linked, EnvLocal)
	if err == nil || !strings.Contains(err.Error(), linked+"/"+EnvLocal) || len(content) != 0 {
		t.Fatalf("a planted link was read: %q %v", content, err)
	}
	notFile := checkout(t)
	if err := os.MkdirAll(filepath.Join(notFile, EnvLocal), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadFile(notFile, EnvLocal); err == nil {
		t.Fatal("a directory in the file's place was read")
	}
	escaped := checkout(t)
	outside := filepath.Dir(hostFile(t))
	plantLink(t, escaped, ".claude/ccy", outside)
	if err := os.WriteFile(filepath.Join(outside, "ccy.env.local"), []byte(hostText), 0o600); err != nil {
		t.Fatal(err)
	}
	if content, _, err := ReadFile(escaped, EnvLocal); err == nil {
		t.Fatalf("a linked .claude/ccy leaving the checkout was read: %q", content)
	}
}

// C2: a checkout file is written whole through an os.Root (an exclusive temporary renamed into
// place, its directories made inside the checkout); a link at the name or a linked directory
// leaving the checkout is refused, naming the file, and the host file it points at is untouched.
func TestWriteFileRefusesLinks(t *testing.T) {
	dir := checkout(t)
	if err := WriteFile(dir, Gitignore, []byte("*\n")); err != nil {
		t.Fatalf("a new file: %v", err)
	}
	if err := WriteFile(dir, Gitignore, []byte("*\n!mounts\n")); err != nil {
		t.Fatalf("a replaced file: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, Gitignore)); err != nil || string(raw) != "*\n!mounts\n" {
		t.Fatalf("the file holds %q %v", raw, err)
	}
	if entries, err := os.ReadDir(filepath.Join(dir, ".claude/ccy")); err != nil || len(entries) != 1 {
		t.Fatalf("a temporary was left behind: %v %v", entries, err)
	}
	linked, host := checkout(t), hostFile(t)
	plantLink(t, linked, Gitignore, host)
	err := WriteFile(linked, Gitignore, []byte("*\n"))
	if err == nil || !strings.Contains(err.Error(), linked+"/"+Gitignore) || !strings.Contains(err.Error(), "link") {
		t.Fatalf("a planted link was written: %v", err)
	}
	keptHost(t, host)
	dangling := checkout(t)
	absent := filepath.Join(checkout(t), "made-by-the-launcher")
	plantLink(t, dangling, Dist, absent)
	if err := WriteFile(dangling, Dist, []byte("x\n")); err == nil {
		t.Fatal("a dangling link was written")
	}
	if _, err := os.Lstat(absent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the write created the link's target: %v", err)
	}
	escaped, outside := checkout(t), checkout(t)
	plantLink(t, escaped, ".claude/ccy", outside)
	if err := WriteFile(escaped, Gitignore, []byte("*\n")); err == nil {
		t.Fatal("a write through a linked .claude/ccy left the checkout")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("the write reached %s: %v %v", outside, entries, err)
	}
}

// C3: CopyOut copies a checkout file, read as ReadFile reads it, into a private file of a host
// directory, so what is bound is the bytes judged and not a checkout path a container can
// change; no file is no copy, and a link is refused.
func TestCopyOutCopiesOnlyTheRegularFile(t *testing.T) {
	dir, into := checkout(t), checkout(t)
	if path, present, err := CopyOut(dir, EnvLocal, into); err != nil || present || path != "" {
		t.Fatalf("no file: %q %v %v", path, present, err)
	}
	write(t, dir, EnvLocal, "export ROLE=a\n")
	path, present, err := CopyOut(dir, EnvLocal, into)
	if err != nil || !present || path != filepath.Join(into, "ccy.env.local") {
		t.Fatalf("CopyOut = %q %v %v", path, present, err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("the copy is not a private regular file: %v %v", info, err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "export ROLE=a\n" {
		t.Fatalf("the copy holds %q %v", raw, err)
	}
	if _, _, err := CopyOut(dir, EnvLocal, into); err == nil {
		t.Fatal("a second copy over the first was made")
	}
	linked, again := checkout(t), checkout(t)
	plantLink(t, linked, EnvLocal, hostFile(t))
	if path, _, err := CopyOut(linked, EnvLocal, again); err == nil || path != "" {
		t.Fatalf("a planted link was copied: %q %v", path, err)
	}
	if entries, err := os.ReadDir(again); err != nil || len(entries) != 0 {
		t.Fatalf("a refused copy left %v %v", entries, err)
	}
}
