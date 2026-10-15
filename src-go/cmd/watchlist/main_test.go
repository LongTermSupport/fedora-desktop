package main

import (
	"bytes"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunDispatchesThroughTheApp(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--help"}, &execx.Fake{}, &out, &errOut); code != term.ExitOK || out.Len() == 0 {
		t.Fatalf("--help: exit %d, out %q", code, out.String())
	}
	if code := run([]string{"--nope"}, &execx.Fake{}, &out, &errOut); code != term.ExitUsage {
		t.Fatalf("--nope: exit %d", code)
	}
}

func TestRootDefaultsToTheWorkingDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	var out, errOut bytes.Buffer
	// No list under the working directory: a loud failure, not an empty scan.
	if code := run([]string{"sync"}, &execx.Fake{}, &out, &errOut); code != term.ExitFailure || errOut.Len() == 0 {
		t.Fatalf("exit %d, err %q", code, errOut.String())
	}
}
