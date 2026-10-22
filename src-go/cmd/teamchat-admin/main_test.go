package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunWiresTheCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	digest := strings.Repeat("ab", 32)
	if code := run([]string{"version"}, strings.NewReader(""), &stdout, &stderr, digest); code != term.ExitOK ||
		stdout.String() != digest+"\n" || stderr.Len() != 0 {
		t.Fatalf("version: %d %q %q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := run(nil, strings.NewReader(""), &stdout, &stderr, digest); code != term.ExitUsage || stdout.Len() != 0 {
		t.Fatalf("no verb: %d %q", code, stdout.String())
	}
}

// WR3: run's Env carries the production wiring, which builds without reading the host beyond
// the running binary.
func TestRunPassesTheWiring(t *testing.T) {
	e := newEnv(nil, strings.NewReader(""), io.Discard, io.Discard, strings.Repeat("ab", 32))
	if e.Wire == nil || e.Sys != nil || e.Proc == nil {
		t.Fatalf("env %+v", e)
	}
	sys, err := e.Wire()
	if err != nil || sys.Host.Root != "" || sys.Host.LockDir != paths.AdminRunDir {
		t.Fatalf("Wire = %+v, %v", sys, err)
	}
}
