package main

import (
	"bytes"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunWiresTheCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	digest := strings.Repeat("ab", 32)
	if code := run([]string{"--version"}, strings.NewReader(""), &stdout, &stderr, digest); code != term.ExitOK ||
		stdout.String() != digest+"\n" || stderr.Len() != 0 {
		t.Fatalf("--version: %d %q %q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := run(nil, strings.NewReader(""), &stdout, &stderr, digest); code != term.ExitUsage || stdout.Len() != 0 {
		t.Fatalf("no verb: %d %q", code, stdout.String())
	}
}

// The verbs built outside cli are passed in, every one (n3c-D5).
func TestRunPassesTheVerbsBuiltOutsideCLI(t *testing.T) {
	if got, want := slices.Sorted(maps.Keys(verbs)), slices.Sorted(slices.Values(cli.Outside())); !slices.Equal(got, want) {
		t.Fatalf("verbs = %q, want %q", got, want)
	}
}
