package main

import (
	"bytes"
	"testing"
)

func TestRunReadsTheWorkingDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != 0 || out.Len() != 0 {
		t.Fatalf("outside a checkout: exit %d stdout %q stderr %q", code, out.String(), errOut.String())
	}
	if code := run([]string{"--fetch"}, &out, &errOut); code != 64 {
		t.Fatalf("an argument: exit %d", code)
	}
}
