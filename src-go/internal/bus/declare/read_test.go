package declare

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"
)

// The byte order mark and the line separator, spelled as bytes.
const (
	bom     = "\xef\xbb\xbf"
	lineSep = "\xe2\x80\xa8"
)

// files is a fake file system for Read: a path's bytes, or the error reading it gives.
type files map[string]any

func (f files) read(path string) ([]byte, error) {
	switch v := f[path].(type) {
	case string:
		return []byte(v), nil
	case error:
		return nil, v
	}
	return nil, &fs.PathError{Op: "open", Path: path, Err: syscall.ENOENT}
}

func TestReadIsPathReadTextWithUniversalNewlines(t *testing.T) {
	fake := files{"h.yml": "a: 1\r\nb: 2\rc: 3\r\r\nd\n" + bom + "e\x0bf" + lineSep}
	got, err := Read("h.yml", fake.read)
	if err != nil {
		t.Fatal(err)
	}
	if want := "a: 1\nb: 2\nc: 3\n\nd\n" + bom + "e\x0bf" + lineSep; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReadKeepsALeadingBOM(t *testing.T) {
	got, err := Read("h.yml", files{"h.yml": bom + "agent_bus_teams:\n"}.read)
	if err != nil || got != bom+"agent_bus_teams:\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestReadRefusesAMissingFileNamingItAsPathlibDoes(t *testing.T) {
	for given, shown := range map[string]string{
		"missing.yml":          "missing.yml",
		"./a//b/./missing.yml": "a/b/missing.yml",
		"/x/missing.yml/":      "/x/missing.yml",
		"//x/missing.yml":      "//x/missing.yml",
		"///x/missing.yml":     "/x/missing.yml",
	} {
		_, err := Read(given, files{}.read)
		e, ok := asError(err)
		want := shown + " does not exist (copy localhost.yml.dist to make it)"
		if !ok || e.Kind != KindRefused || e.Message != want {
			t.Errorf("%q: got %v, want refused %q", given, err, want)
		}
	}
}

func TestReadOpensThePathPathlibNormalised(t *testing.T) {
	var opened []string
	read := func(path string) ([]byte, error) {
		opened = append(opened, path)
		return nil, fmt.Errorf("read %s: %w", path, syscall.EISDIR)
	}
	_, err := Read("", read)
	if !errors.Is(err, ErrCrash) {
		t.Fatalf("a directory is python's IsADirectoryError, a crash: %v", err)
	}
	if len(opened) != 1 || opened[0] != "." {
		t.Fatalf("opened %q, want pathlib's \".\" for an empty path", opened)
	}
}

func TestReadCrashesWhereReadTextRaises(t *testing.T) {
	fake := files{
		"denied.yml": &fs.PathError{Op: "open", Path: "denied.yml", Err: syscall.EACCES},
		"notdir.yml": &fs.PathError{Op: "open", Path: "notdir.yml", Err: syscall.ENOTDIR},
		"latin1.yml": "caf\xe9: 1\n",
		"surr.yml":   "a: \xed\xa0\x80\n",
	}
	for _, path := range []string{"denied.yml", "notdir.yml", "latin1.yml", "surr.yml"} {
		_, err := Read(path, fake.read)
		if !errors.Is(err, ErrCrash) {
			t.Errorf("%s: got %v, want the crash class", path, err)
		}
	}
}

func TestPyPath(t *testing.T) {
	for in, want := range map[string]string{
		"": ".", ".": ".", "./": ".", "/": "/", "//": "//", "///": "/", "a/../b": "a/../b", "a/": "a",
	} {
		if got := pyPath(in); got != want {
			t.Errorf("pyPath(%q) = %q, want %q", in, got, want)
		}
	}
}
