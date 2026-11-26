package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunOverAPlainDirectoryIsAFault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	var errOut bytes.Buffer
	if code := run([]string{dir, "--changed"}, &errOut); code != 1 || !strings.Contains(errOut.String(), "cannot judge the checkout") {
		t.Fatalf("exit %d stderr %q", code, errOut.String())
	}
	errOut.Reset()
	if code := run(nil, &errOut); code != 64 {
		t.Fatalf("no arguments: exit %d", code)
	}
}
