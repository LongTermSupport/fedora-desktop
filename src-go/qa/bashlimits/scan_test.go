package bashlimits

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

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
	// scripts/test-x.bash and the plan-folder script are exempt; run.bash is measured now.
	if len(scan.Exempt) != 2 || !scan.Exempt["scripts/test-x.bash"] || !scan.Exempt["CLAUDE/Plan/001-x/a.bash"] {
		t.Errorf("exempt = %v", scan.Exempt)
	}
	if len(scan.Measured) != 4 || scan.Measured["scripts/qa-long.bash"].Code != 70 || scan.Measured["run.bash"].Code != 70 ||
		scan.Measured["files/bin/tool"].Code != 1 || scan.Measured["scripts/small.bash"].Code != 1 {
		t.Errorf("measured = %v", scan.Measured)
	}
	if !scan.Present["README.md"] || scan.Present["gone.bash"] || scan.Present["link.bash"] {
		t.Errorf("present = %v", scan.Present)
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

func TestScanTreeReadsTheGoCommandSuitesTable(t *testing.T) {
	table := "# comment\n\nccy-x\tscripts/test-a.bash scripts/test-b.bash\nlib\tqa-go\n"
	fsys := fstest.MapFS{suitesTable: {Data: []byte(table)}}
	scan, err := ScanTree(fsys, []string{suitesTable})
	if err != nil {
		t.Fatal(err)
	}
	if !scan.Suites["ccy-x"]["scripts/test-a.bash"] || !scan.Suites["ccy-x"]["scripts/test-b.bash"] || !scan.Suites["lib"]["qa-go"] || len(scan.Suites) != 2 {
		t.Fatalf("suites = %v", scan.Suites)
	}
	none, err := ScanTree(fstest.MapFS{}, nil)
	if err != nil || len(none.Suites) != 0 {
		t.Fatalf("a tree without the table has no suites: %v, %v", none.Suites, err)
	}
}
