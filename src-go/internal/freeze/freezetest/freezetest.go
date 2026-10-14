// Package freezetest is test support for the freeze tools' parity checks against the bash tools
// they replace. It exists only until the bash tools are deleted at cut-over, with its callers.
package freezetest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// usageEcho is one `echo "..."` line of a bash tool's usage() function.
var usageEcho = regexp.MustCompile(`^    echo "(.*)"$`)

// RepoRoot is the repository root above the test's working directory, found by the bash tool
// that lives in it.
func RepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, statErr := os.Stat(filepath.Join(dir, "files/home/.local/bin/podfreeze")); statErr == nil {
			return dir
		}
	}
	t.Fatalf("repo root not found above %s", wd)
	return ""
}

// BashUsage is what the shipped bash tool prints for --help: the text of its usage() function (not
// its header comment), the echo lines with bash's \" escapes undone.
func BashUsage(t *testing.T, tool string) string {
	t.Helper()
	raw, err := fs.ReadFile(os.DirFS(filepath.Join(RepoRoot(t), "files/home/.local/bin")), tool)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := parseUsage(string(raw))
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return usage
}

func parseUsage(script string) (string, error) {
	var want strings.Builder
	inUsage := false
	for line := range strings.SplitSeq(script, "\n") {
		switch {
		case line == "usage() {":
			inUsage = true
		case inUsage && line == "}":
			return want.String(), nil
		case inUsage:
			m := usageEcho.FindStringSubmatch(line)
			if m == nil {
				return "", fmt.Errorf("usage() holds a line this parser does not understand: %q", line)
			}
			want.WriteString(strings.ReplaceAll(m[1], `\"`, `"`) + "\n")
		}
	}
	return "", errors.New("usage() not found")
}
