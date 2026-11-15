package allowedhosts

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeRule puts an allowed-hostnames file with content in a fresh checkout and returns it.
func writeRule(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, Rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// H1: no file, or one that is not a regular file, is no restriction.
func TestNoFileIsNoRestriction(t *testing.T) {
	dir := t.TempDir()
	if v, err := Judge(dir, "box"); err != nil || !v.Allowed {
		t.Fatalf("absent: %+v %v", v, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, Rel), 0o750); err != nil {
		t.Fatal(err)
	}
	if v, err := Judge(dir, "box"); err != nil || !v.Allowed {
		t.Fatalf("a directory: %+v %v", v, err)
	}
}

// H2: comments (whole-line and inline) and every space are stripped; blank lines are dropped;
// a last line with no newline is read, as the bash reads it (fedora-desktop#293: a `never`
// written without one was dropped, lifting the restriction).
func TestPatternsAreReadAsTheBashReadsThem(t *testing.T) {
	dir := writeRule(t, "# a comment\n\n  lab-* # the lab\n\tbox 1\nlast-unterminated")
	got, present, err := Read(dir)
	if err != nil || !present {
		t.Fatalf("%v %v", present, err)
	}
	if want := []string{"lab-*", "box1", "last-unterminated"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if v, err := Judge(writeRule(t, "never"), "box"); err != nil || !v.Never {
		t.Fatalf("an unterminated never: %+v %v", v, err)
	}
}

// H3: a file with no pattern after the stripping is no restriction.
func TestAFileOfCommentsIsNoRestriction(t *testing.T) {
	dir := writeRule(t, "# nothing here\n   \n")
	v, err := Judge(dir, "box")
	if err != nil || !v.Allowed {
		t.Fatalf("%+v %v", v, err)
	}
}

// H4: the patterns are tried in order; never refuses when it is reached before a match, and
// a host no pattern matches is refused with the patterns.
func TestNeverAndOtherHosts(t *testing.T) {
	for _, c := range []struct {
		content, host  string
		allowed, never bool
	}{
		{"never\n", "box", false, true},
		{"box\nnever\n", "box", true, false},
		{"other\nnever\n", "box", false, true},
		{"other\nlab-*\n", "box", false, false},
		{"other\nlab-*\n", "lab-7", true, false},
		{"*\n", "anything", true, false},
	} {
		v, err := Judge(writeRule(t, c.content), c.host)
		if err != nil || v.Allowed != c.allowed || v.Never != c.never {
			t.Errorf("%q on %s: %+v %v", c.content, c.host, v, err)
		}
		if !c.allowed && !c.never && len(v.Patterns) == 0 {
			t.Errorf("%q: a refusal names the patterns", c.content)
		}
	}
}

// H5: an allowed-hostnames file that cannot be read is an error, never an allowance (the bash
// read nothing from it and let the launch go on).
func TestAnUnreadableFileIsAnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode")
	}
	dir := writeRule(t, "box\n")
	if err := os.Chmod(filepath.Join(dir, Rel), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := Judge(dir, "box"); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("an unreadable file: %v, want ErrUnreadable", err)
	}
}

// H5: a stat that fails for another reason than absence is an error too.
func TestAnUnstattableFileIsAnError(t *testing.T) {
	dir := writeRule(t, "box\n")
	path := filepath.Join(dir, Rel)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(dir); err == nil || errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrUnreadable) {
		t.Fatalf("a symlink loop: %v, want a stat error that is not ErrUnreadable", err)
	}
}

// H6: a pattern matches as bash's case matches an unquoted pattern: * and ?, bracket
// expressions with ranges, ! or ^ negation, a leading ] and POSIX classes, backslash escapes,
// and an unclosed [ as itself.
func TestMatchIsBashCaseMatching(t *testing.T) {
	for _, c := range []struct {
		pattern, name string
		want          bool
	}{
		{"box", "box", true},
		{"box", "box2", false},
		{"*", "", true},
		{"lab-*", "lab-", true},
		{"lab-*", "xlab-1", false},
		{"*.local", "a.b.local", true},
		{"prod-??", "prod-01", true},
		{"prod-??", "prod-1", false},
		{"node[0-9]", "node7", true},
		{"node[0-9]", "nodex", false},
		{"node[!0-9]", "nodex", true},
		{"node[^0-9]", "node7", false},
		{"[]a]", "]", true},
		{"[!]a]", "b", true},
		{"[[:alpha:]]x", "ax", true},
		{"[[:digit:]]x", "ax", false},
		{"[[:bogus:]]", "a", false},
		{`a\*`, "a*", true},
		{`a\*`, "ab", false},
		{`a\`, `a\`, true},
		{"a[b", "a[b", true},
		{"a[b", "ab", false},
		{"[a-]", "-", true},
		{`[\]]`, "]", true},
		{"*a*b", "xaybzb", true},
		{"ü?", "üx", true},
	} {
		if got := Match(c.pattern, c.name); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}
