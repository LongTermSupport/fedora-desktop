package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunReadsTheArgumentsAndStdin(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, strings.NewReader(""), &out, &errOut); code != term.ExitUsage || out.Len() != 0 {
		t.Fatalf("no arguments: exit %d stdout %q", code, out.String())
	}
	errOut.Reset()
	// Refused either way: as root before the list is read, as anyone else for the list.
	if code := run([]string{"check"}, strings.NewReader("{"), &out, &errOut); code != term.ExitConfig ||
		!strings.HasPrefix(errOut.String(), "agent-bus-lightning: ") {
		t.Fatalf("a bad list: exit %d stderr %q", code, errOut.String())
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
