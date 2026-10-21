package main

import (
	"bytes"
	"testing"
)

func TestRunRefusesArgumentsBeforeRunningGit(t *testing.T) {
	t.Chdir(t.TempDir())
	var errOut bytes.Buffer
	if code := run([]string{"origin"}, &errOut); code != 64 || errOut.Len() == 0 {
		t.Fatalf("an argument: exit %d stderr %q", code, errOut.String())
	}
}

func TestRunFailsOutsideACheckout(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GIT_SSH_COMMAND", "false")
	t.Setenv("GIT_CEILING_DIRECTORIES", t.TempDir())
	var errOut bytes.Buffer
	if code := run(nil, &errOut); code != 1 || errOut.Len() == 0 {
		t.Fatalf("outside a checkout: exit %d stderr %q", code, errOut.String())
	}
}
