package fsx

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWriteAtomicLeavesTheWholeFileAndNoTemporary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "record")
	if err := WriteAtomic(path, "record", []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, "record", []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := root.ReadFile("record")
	if err != nil || string(got) != "two\n" {
		t.Fatalf("read %q, %v", got, err)
	}
	if err = root.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, %v", info, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries %v, %v: a temporary was left behind", entries, err)
	}
}

// A link planted at the name the temporary once had (<path>.tmp.<pid>) is not followed: the
// write never lands where it points (issue 417, rule project_temp_not_exclusive).
func TestWriteAtomicFollowsNoPlantedLink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "record")
	if err := os.Symlink(victim, path+".tmp."+strconv.Itoa(os.Getpid())); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, "record", []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(victim); err != nil || string(got) != "mine\n" {
		t.Fatalf("the planted link was followed: %q, %v", got, err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "new\n" {
		t.Fatalf("read %q, %v", got, err)
	}
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("the record is %v, %v", info, err)
	}
}

func TestWriteAtomicNamesTheThingWhenTheWriteFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent-dir", "record")
	err := WriteAtomic(path, "session record", []byte("x"), 0o600)
	if err == nil || !strings.Contains(err.Error(), "could not write the session record "+path) {
		t.Fatalf("got %v", err)
	}
}

func TestWriteAtomicNamesTheThingWhenTheMoveFailsAndRemovesTheTemporary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "keep"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := WriteAtomic(path, "manifest", []byte("x"), 0o600)
	if err == nil || !strings.Contains(err.Error(), "could not move the manifest into place at "+path) {
		t.Fatalf("got %v", err)
	}
	entries, rerr := os.ReadDir(dir)
	if rerr != nil || len(entries) != 1 {
		t.Fatalf("entries %v, %v: the temporary was not removed", entries, rerr)
	}
}

func TestRemoveIfPresent(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveIfPresent(file); err != nil {
		t.Fatal(err)
	}
	if err := RemoveIfPresent(file); err != nil {
		t.Fatalf("an absent file is not an error: %v", err)
	}
	full := filepath.Join(dir, "d")
	if err := os.Mkdir(full, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(full, "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveIfPresent(full); err == nil {
		t.Fatal("a directory that cannot be removed is an error")
	}
}
