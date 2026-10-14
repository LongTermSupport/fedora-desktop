package main

import (
	"bytes"
	"io"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--help"}); code != term.ExitOK {
		t.Fatalf("--help: exit %d", code)
	}
	// A usage mistake exits 1 (the bash tool's status, kept at cut-over), before anything runs.
	if code := run([]string{"--nope"}); code != term.ExitFailure {
		t.Fatalf("--nope: exit %d", code)
	}
}

func TestExecuteAnswersVersionWithTheSourceDigest(t *testing.T) {
	old := SourceDigest
	SourceDigest = "feedbeef"
	t.Cleanup(func() { SourceDigest = old })
	var out bytes.Buffer
	if code := execute([]string{"--version"}, &out, io.Discard); code != term.ExitOK || out.String() != "feedbeef\n" {
		t.Fatalf("exit %d, stdout %q", code, out.String())
	}
	if code := execute([]string{"--help"}, io.Discard, io.Discard); code != term.ExitOK {
		t.Fatalf("--help: exit %d", code)
	}
}
