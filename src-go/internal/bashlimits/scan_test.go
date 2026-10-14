package bashlimits

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestListFiles(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"},
		execx.Result{Stdout: []byte("a.bash\x00dir/b\x00a.bash\x00")})
	got, err := ListFiles(context.Background(), f, "/repo")
	if err != nil || !slices.Equal(got, []string{"a.bash", "dir/b"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if f.Calls()[0].Dir != "/repo" {
		t.Fatalf("ran in %q", f.Calls()[0].Dir)
	}
}

func TestListFilesGitFailure(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "ls-files"}, execx.Result{ExitCode: 128, Stderr: []byte("fatal")})
	if _, err := ListFiles(context.Background(), f, "/repo"); err == nil {
		t.Fatal("a failing git was not an error")
	}
}

func TestScanTree(t *testing.T) {
	long := "#!/usr/bin/bash\n" + strings.Repeat("echo x\n", 70)
	fsys := fstest.MapFS{
		"scripts/qa-long.bash":     {Data: []byte(long)},
		"scripts/small.bash":       {Data: []byte("#!/usr/bin/bash\necho hi\n")},
		"scripts/test-x.bash":      {Data: []byte(long)},
		"run.bash":                 {Data: []byte(long)},
		"files/bin/tool":           {Data: []byte("#!/bin/bash\necho hi\n")},
		"files/bin/not-bash":       {Data: []byte("#!/usr/bin/python3\nprint(1)\n")},
		"README.md":                {Data: []byte("# hi\n")},
		"link.bash":                {Mode: fs.ModeSymlink | 0o777, Data: []byte("scripts/small.bash")},
		"CLAUDE/Plan/001-x/a.bash": {Data: []byte(long)},
	}
	files := []string{
		"scripts/qa-long.bash", "scripts/small.bash", "scripts/test-x.bash", "run.bash",
		"files/bin/tool", "files/bin/not-bash", "README.md", "link.bash", "gone.bash",
		"CLAUDE/Plan/001-x/a.bash",
	}
	scan, err := ScanTree(fsys, files)
	if err != nil {
		t.Fatal(err)
	}
	if scan.Exempt != 3 {
		t.Errorf("exempt = %d, want 3", scan.Exempt)
	}
	if len(scan.Measured) != 3 || scan.Measured["scripts/qa-long.bash"].Code != 70 ||
		scan.Measured["files/bin/tool"].Code != 1 || scan.Measured["scripts/small.bash"].Code != 1 {
		t.Errorf("measured = %v", scan.Measured)
	}
}

// unreadableFS lists files but refuses to open any.
type unreadableFS struct{ fs.FS }

func (unreadableFS) Open(string) (fs.File, error) { return nil, errors.New("denied") }

func TestScanTreeReadErrorIsAnError(t *testing.T) {
	inner := fstest.MapFS{"a/tool": {Data: []byte("#!/bin/bash\n")}}
	// A file that cannot be read must stop the scan, not be skipped as if it were not bash.
	if _, err := ScanTree(unreadableFS{inner}, []string{"a/tool"}); err == nil {
		t.Fatal("an unreadable file was skipped")
	}
}
