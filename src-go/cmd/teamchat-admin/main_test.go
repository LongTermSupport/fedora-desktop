package main

import (
	"bytes"
	"strings"
	"testing"

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
