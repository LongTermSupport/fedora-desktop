package freezetest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoRootFindsTheDirectoryHoldingTheBashTools(t *testing.T) {
	root := RepoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "files/home/.local/bin/podfreeze")); err != nil {
		t.Fatalf("not the repo root: %s: %v", root, err)
	}
}

func TestBashUsageIsTheEchoLinesOfUsageNotTheHeaderComment(t *testing.T) {
	got := BashUsage(t, "lxcfreeze")
	if !strings.HasPrefix(got, "Usage: lxcfreeze [list|freeze|thaw] [target] [options]\n") {
		t.Fatalf("got %q", got[:min(len(got), 80)])
	}
	if strings.Contains(got, "\\\"") {
		t.Error("bash's \\\" escapes are unescaped")
	}
	if !strings.Contains(got, `("FREEZE 3", "THAW 1")`) {
		t.Error("escaped quotes come out as quotes")
	}
	if strings.Contains(got, "WHY THIS EXISTS") {
		t.Error("the header comment is not the usage")
	}
}

func TestParseUsageRefusesALineItDoesNotUnderstand(t *testing.T) {
	if _, err := parseUsage("usage() {\n    printf 'x'\n}\n"); err == nil {
		t.Fatal("a line that is not an echo is an error")
	}
	if _, err := parseUsage("no function here\n"); err == nil {
		t.Fatal("a missing usage() is an error")
	}
}
