package bashlimits

import (
	"fmt"
	"path"
	"strings"
)

// Exemption is a kind of bash file that is exempt from the limits, and why. A Pattern ending in
// "/" covers every file below that directory; any other is matched against the whole path.
// Either way "*" and "[...]" match within one path segment only (path.Match).
//
// Tests marks an entry that names test code (suites, fixtures, testdata): the limits are for
// tooling, so an allowlist row for such a file may be dropped where the file stands.
type Exemption struct {
	Pattern string
	Reason  string
	Tests   bool
}

// IsTestCode reports whether p is exempt as test code (an Exemption with Tests set).
func IsTestCode(p string) bool {
	for _, e := range Exemptions {
		if hit, err := matchPattern(e.Pattern, p); err == nil && hit && e.Tests {
			return true
		}
	}
	return false
}

// Exemptions is the one place a kind of file is exempted: test suites, test fixtures and
// testdata (test code is not tooling, so the limits do not judge it), plan-folder scripts and
// vendored trees. Everything else, user-shell snippets and run.bash included, is measured
// and, if over a limit, held on the allowlist at its size. A tool is never exempt for being
// named for tests (scripts/qa-helper-tests.bash) or sitting beside test files: each test
// entry names the files that ARE tests, by location or by the prefix the test files carry.
// Keep each entry narrow, its reason true, and delete one that matches no file.
var Exemptions = []Exemption{
	{"scripts/test-*.bash", "test suites drive a command or library; a Go port retires its suite rather than porting it", true},
	{"tests/", "test suites and fixtures drive a command or library; a Go port retires them", true},
	{".semgrep/", "semgrep rule fixtures: deliberately full of the patterns the rules flag, and the cases that must not be flagged", true},
	{"files/home/.local/share/vmtest/guest-acceptance-*.bash", "guest-side acceptance checkers, the test suites the VM lab runs inside a guest", true},
	{"files/home/.local/share/vmtest/guest-prepare-*.bash", "guest-side test fixtures that set up the state a guest acceptance checker then judges", true},
	{"CLAUDE/Plan/[0-9]*/", "plan-folder scripts are plan-local scaffolding that CLAUDE/PlanScriptStandards.md requires; shared plan tooling sits directly in CLAUDE/Plan/ and is not exempt", false},
	{"CLAUDE/Plan/Completed/", "completed plans' scaffolding, kept as history", false},
	{"CLAUDE/Plan/Cancelled/", "cancelled plans' scaffolding, kept as history", false},
	{"src-go/vendor/", "third-party Go modules as go mod vendor writes them (Plan 00204); never edited here, and qa-go-deps fails any byte that differs", false},
	{".claude/init.sh", "daemon-owned file, replaced on every hooks-daemon upgrade", false},
	{".claude/provision.sh", "daemon-owned file, replaced on every hooks-daemon upgrade", false},
	{".claude/skills/hooks-daemon/", "daemon-owned skill scripts, replaced on every hooks-daemon upgrade", false},
	{".claude/skills/docs-qa/", "daemon-owned skill scripts, replaced on every hooks-daemon upgrade", false},
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
