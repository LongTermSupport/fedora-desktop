package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func noEnv(string) string { return "" }

// A command the app does not know is a usage error before anything is asked of git or GitHub.
func TestRunRefusesAnUnknownCommandBeforeAskingAnything(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"frobnicate"}, noEnv, &out, &errOut); code != term.ExitUsage {
		t.Fatalf("exit %d, want usage", code)
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), "unknown command") {
		t.Fatalf("stdout %q, stderr %q", out.String(), errOut.String())
	}
}

// Run in a directory that is no checkout, decide is a refusal with nothing on stdout: git's
// failing answer is an error, never a plan.
func TestRunInNoCheckoutRefusesToDecide(t *testing.T) {
	var out, errOut bytes.Buffer
	t.Chdir(t.TempDir())
	code := run([]string{"decide", "--branch", "F44"}, noEnv, &out, &errOut)
	if code != term.ExitFailure || out.Len() != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q: want a refusal", code, out.String(), errOut.String())
	}
}
