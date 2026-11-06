package v1names_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/v1names"
)

// This package's tests are external: units_test.go imports the packages that import v1names.

// d42 is the list of DESIGN-v2-D-migration.md section 4.2, written out here (this package is a
// named home of v1 names, D 4.1) so that a change to InaccessiblePaths is a change to its pin.
var d42 = []string{
	"-/var/lib/agent-bus",
	"-/var/lib/agent-bus-install",
	"-/var/lib/agent-bus-applied",
	"-/etc/agent-bus",
	"-/usr/local/lib/agent-bus",
	"-/usr/local/share/agent-bus",
}

const directiveKey = "InaccessiblePaths="

// pathsOf is the paths a unit line lists: its value, split on the spaces systemd splits on.
func pathsOf(t *testing.T, line string) []string {
	t.Helper()
	value, ok := strings.CutPrefix(line, directiveKey)
	if !ok {
		t.Fatalf("%q is not an %s line", line, directiveKey)
	}
	return strings.Fields(value)
}

// D 4.2: the line holds the six v1 paths, in the order D gives them, and no other.
func TestInaccessiblePathsAreD42sSix(t *testing.T) {
	if got := pathsOf(t, v1names.InaccessiblePaths); !slices.Equal(got, d42) {
		t.Fatalf("InaccessiblePaths lists\n%q\nwant D 4.2's\n%q", got, d42)
	}
}

// D 4.2: each path leads with the - that lets the unit start on a host without v1, and is an
// absolute path of one word. The line is one line, with no specifier systemd would expand.
func TestEveryPathIsAbsoluteAndOptional(t *testing.T) {
	line := v1names.InaccessiblePaths
	if strings.ContainsAny(line, "\n\r\t%") || strings.Count(line, directiveKey) != 1 {
		t.Fatalf("%q is not one plain unit line", line)
	}
	paths := pathsOf(t, line)
	for _, p := range paths {
		if !strings.HasPrefix(p, "-/") || strings.Contains(p[1:], "-/") || strings.HasSuffix(p, "/") {
			t.Errorf("%q is not - and an absolute path without a trailing slash", p)
		}
	}
	for i, p := range paths {
		if slices.Contains(paths[i+1:], p) {
			t.Errorf("%q is listed twice", p)
		}
	}
}
