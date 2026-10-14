package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// restorePath makes run's reset of PATH undone at the end of the test.
func restorePath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", os.Getenv("PATH"))
}

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestRunPrintsTheUsageOfTheProductionBinary(t *testing.T) {
	restorePath(t)
	var out, errw bytes.Buffer
	if code := run([]string{"--help"}, env(nil), &out, &errw); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errw.String())
	}
	if !strings.HasPrefix(out.String(), "Usage: agent-bus-install COMMAND [OPTIONS]") {
		t.Fatalf("stdout %q", out.String())
	}
}

func TestRunPutsTheSourceDigestInTheVersion(t *testing.T) {
	restorePath(t)
	old := SourceDigest
	SourceDigest = strings.Repeat("c", 64)
	t.Cleanup(func() { SourceDigest = old })
	var out, errw bytes.Buffer
	if code := run([]string{"--version"}, env(nil), &out, &errw); code != 0 || out.String() != strings.Repeat("c", 64)+"\n" {
		t.Fatalf("exit %d, stdout %q", code, out.String())
	}
}

func TestRunResetsPATHToTheSystemDirectories(t *testing.T) {
	restorePath(t)
	t.Setenv("PATH", "/tmp/planted")
	var out, errw bytes.Buffer
	run([]string{"--help"}, env(nil), &out, &errw)
	if got := os.Getenv("PATH"); got != "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin" {
		t.Fatalf("PATH is %q", got)
	}
}

func TestRunUsageErrorExitsWithTheUsageStatus(t *testing.T) {
	restorePath(t)
	var out, errw bytes.Buffer
	if code := run([]string{"frobnicate"}, env(nil), &out, &errw); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
}
