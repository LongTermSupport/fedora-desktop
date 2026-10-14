package fsx

import (
	"os"
	"path/filepath"
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
