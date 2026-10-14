package bashlimits

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"strings"
)

// shebangRe is a first line that runs bash or sh, directly or through env.
var shebangRe = regexp.MustCompile(`^#!\s*(?:\S*/)?(?:env\s+(?:-\S+\s+)*)?(?:\S*/)?(?:ba)?sh(?:\s|$)`)

// shebangBytes is how much of a file is read to see its first line.
const shebangBytes = 256

// IsCandidate reports whether path is a bash file the gate looks at: a *.bash or *.sh file, or
// an extensionless file or *.j2 template whose first line is a bash or sh shebang.
func IsCandidate(fsys fs.FS, p string) (bool, error) {
	base := path.Base(p)
	ext := path.Ext(base)
	if ext == ".bash" || ext == ".sh" {
		return true, nil
	}
	if ext != ".j2" && strings.Contains(base, ".") {
		return false, nil
	}
	first, err := firstLine(fsys, p)
	if err != nil {
		return false, err
	}
	return shebangRe.MatchString(first), nil
}

func firstLine(fsys fs.FS, p string) (string, error) {
	f, err := fsys.Open(p)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", p, err)
	}
	head, err := io.ReadAll(io.LimitReader(f, shebangBytes))
	if closeErr := f.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p, err)
	}
	text := string(head)
	if end := strings.IndexByte(text, '\n'); end >= 0 {
		text = text[:end]
	}
	return text, nil
}

// Exemption is a kind of bash file that is exempt from the limits, and why. A Pattern ending in
// "/" covers every file below that directory; any other is matched against the whole path.
// Either way "*" and "[...]" match within one path segment only (path.Match).
type Exemption struct {
	Pattern string
	Reason  string
}

// Exemptions is the one place a kind of file is exempted. Keep each narrow and keep its reason
// true: an exemption is a statement that bash is the right tool for that file.
var Exemptions = []Exemption{
	{"files/home/bashrc-includes/", "snippets sourced into the user's interactive shell: they must be shell, and heavy logic they call belongs in a Go binary"},
	{"files/etc/profile.d/", "snippets the login shell sources: they must be shell"},
	{"files/var/local/ps1-prompt", "sourced by the profile.d snippet to draw the prompt: shell-environment code"},
	{"files/var/local/colours", "sourced colour-variable definitions: shell-environment code"},
	{"run.bash", "the bootstrap that runs before Go exists on a fresh host"},
	{"scripts/test-*.bash", "test suites drive a command or library; a Go port retires its suite rather than porting it"},
	{"tests/", "test suites and fixtures drive a command or library; a Go port retires them"},
	{"CLAUDE/Plan/[0-9]*/", "plan-local transient scaffolding that travels with its plan; shared plan tooling sits directly in CLAUDE/Plan/ and is not exempt"},
	{"CLAUDE/Plan/Completed/", "completed plans' transient scaffolding, kept as history"},
	{".claude/hooks-daemon/", "vendored upstream hooks-daemon clone"},
	{".claude/init.sh", "daemon-owned file, replaced on every hooks-daemon upgrade"},
	{".claude/provision.sh", "daemon-owned file, replaced on every hooks-daemon upgrade"},
	{".claude/skills/hooks-daemon/", "daemon-owned skill scripts, replaced on every hooks-daemon upgrade"},
	{".claude/skills/docs-qa/", "daemon-owned skill scripts, replaced on every hooks-daemon upgrade"},
}

// ExemptionFor is the reason p is exempt, and whether it is.
func ExemptionFor(p string) (string, bool, error) {
	for _, e := range Exemptions {
		hit, err := matchPattern(e.Pattern, p)
		if err != nil {
			return "", false, err
		}
		if hit {
			return e.Reason, true, nil
		}
	}
	return "", false, nil
}

func matchPattern(pattern, p string) (bool, error) {
	dir, isDir := strings.CutSuffix(pattern, "/")
	if !isDir {
		hit, err := path.Match(pattern, p)
		if err != nil {
			return false, fmt.Errorf("exemption pattern %q: %w", pattern, err)
		}
		return hit, nil
	}
	want := strings.Split(dir, "/")
	got := strings.Split(p, "/")
	if len(got) <= len(want) {
		return false, nil
	}
	for i, seg := range want {
		hit, err := path.Match(seg, got[i])
		if err != nil {
			return false, fmt.Errorf("exemption pattern %q: %w", pattern, err)
		}
		if !hit {
			return false, nil
		}
	}
	return true, nil
}
