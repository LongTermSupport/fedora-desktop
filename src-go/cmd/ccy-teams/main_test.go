package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func noEnv(string) string { return "" }

func TestRunDispatchesThroughTheApp(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"--help"}, noEnv, "/checkout", &stderr); code != term.ExitOK {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}, noEnv, "/checkout", &stderr); code != term.ExitUsage {
		t.Fatalf("--nope: exit %d", code)
	}
}

func TestRunRefusesABinaryBuiltWithoutARootDir(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"--help"}, noEnv, "", &stderr); code != term.ExitFailure {
		t.Fatalf("empty RootDir: exit %d, want %d", code, term.ExitFailure)
	}
	for _, want := range []string{"-X main.RootDir", "play-claude-yolo.yml"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("the error does not name %q: %q", want, stderr.String())
		}
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
}
