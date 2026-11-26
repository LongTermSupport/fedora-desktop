package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunReadsTheArgumentsAndStdin(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(t.Context(), nil, strings.NewReader(""), &out, &errOut); code != term.ExitUsage || out.Len() != 0 {
		t.Fatalf("no arguments: exit %d stdout %q", code, out.String())
	}
	errOut.Reset()
	// Refused either way: as root before the list is read, as anyone else for the list.
	if code := run(t.Context(), []string{"check"}, strings.NewReader("{"), &out, &errOut); code != term.ExitConfig ||
		!strings.HasPrefix(errOut.String(), "agent-bus-lightning: ") {
		t.Fatalf("a bad list: exit %d stderr %q", code, errOut.String())
	}
}

// TestRunWatchesTheRealProcUntilStopped holds that watch is given the real /proc and ends with 0
// once its context is stopped (as root it is refused instead).
func TestRunWatchesTheRealProcUntilStopped(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out, errOut bytes.Buffer
	want := term.ExitOK
	if os.Geteuid() == 0 {
		want = term.ExitConfig
	}
	if code := run(ctx, []string{"watch"}, strings.NewReader(""), &out, &errOut); code != want || out.Len() != 0 {
		t.Fatalf("exit %d stdout %q stderr %q", code, out.String(), errOut.String())
	}
}

func TestPasswdHomeIsTheUsersHome(t *testing.T) {
	home, err := passwdHome(os.Geteuid())
	if err != nil || !strings.HasPrefix(home, "/") {
		t.Fatalf("home %q, %v", home, err)
	}
	if _, err := passwdHome(-2); err == nil {
		t.Fatal("a uid with no passwd entry has a home")
	}
}
