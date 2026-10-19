package bashlimits

import (
	"fmt"
	"path"
	"strings"
)

// Exemption is a kind of bash file that is exempt from the limits, and why. A Pattern ending in
// "/" covers every file below that directory; any other is matched against the whole path.
// Either way "*" and "[...]" match within one path segment only (path.Match).
type Exemption struct {
	Pattern string
	Reason  string
}

// Exemptions is the one place a kind of file is exempted: test suites, plan-folder scripts,
// vendored trees and the daemon-owned trees; single daemon-owned files are DaemonOwnedFiles,
// which ExemptionFor also consults. Everything else, user-shell snippets and run.bash included, is measured
// and, if over a limit, held on the allowlist at its size. Keep each entry narrow, its reason
// true, and delete one that matches no file.
var Exemptions = []Exemption{
	{"scripts/test-*.bash", "test suites drive a command or library; a Go port retires its suite rather than porting it"},
	{"tests/", "test suites and fixtures drive a command or library; a Go port retires them"},
	{"CLAUDE/Plan/[0-9]*/", "plan-folder scripts are plan-local scaffolding that CLAUDE/PlanScriptStandards.md requires; shared plan tooling sits directly in CLAUDE/Plan/ and is not exempt"},
	{"CLAUDE/Plan/Completed/", "completed plans' scaffolding, kept as history"},
	{"CLAUDE/Plan/Cancelled/", "cancelled plans' scaffolding, kept as history"},
	{"src-go/vendor/", "third-party Go modules as go mod vendor writes them (Plan 00204); never edited here, and qa-go-deps fails any byte that differs"},
	{".claude/init.sh", "daemon-owned file, replaced on every hooks-daemon upgrade"},
	{".claude/provision.sh", "daemon-owned file, replaced on every hooks-daemon upgrade"},
	{".claude/skills/hooks-daemon/", "daemon-owned skill scripts, replaced on every hooks-daemon upgrade"},
	{".claude/skills/docs-qa/", "daemon-owned skill scripts, replaced on every hooks-daemon upgrade"},
}

// ExemptionFor is the reason p is exempt, and whether it is.
func ExemptionFor(p string) (string, bool, error) {
	if reason := daemonOwnedReason(p); reason != "" {
		return reason, true, nil
	}
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
