package toolchain

import (
	"context"
	"fmt"
	"go/version"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// pinnedTool is a tool a gate runs, the key that pins it in .qa-versions, and how its --version
// output names the version. None of them agree on where the number sits.
type pinnedTool struct {
	name, key string
	parse     func(stdout string) string
}

// pinnedTools is the assertion order (SPEC T8).
var pinnedTools = []pinnedTool{
	{"ruff", "RUFF", func(s string) string { return field(firstLine(s), 1) }},
	{"semgrep", "SEMGREP", func(s string) string { return field(firstLine(s), 0) }},
	{"shellcheck", "SHELLCHECK", shellcheckVersion},
	{"golangci-lint", "GOLANGCI_LINT", func(s string) string { return field(firstLine(s), 3) }},
}

func firstLine(s string) string {
	return strings.SplitN(s, "\n", 2)[0]
}

// field is the i-th whitespace-separated word of a line, "" when it has none.
func field(line string, i int) string {
	words := strings.Fields(line)
	if i < len(words) {
		return words[i]
	}
	return ""
}

// shellcheckVersion is the word after "version:" on the line that starts with it.
func shellcheckVersion(stdout string) string {
	for line := range strings.SplitSeq(stdout, "\n") {
		if strings.HasPrefix(line, "version:") {
			return field(line, 1)
		}
	}
	return ""
}

// assertion carries the run's counters and where its diagnostics go.
type assertion struct {
	env        Env
	root       string
	errOut     *emitter
	checked    int
	mismatches int
}

// readOut is how a version run ended: its stdout, or the evidence of why there is none.
type readOut struct {
	stdout, stderr, evidence string
	ok                       bool
}

// read runs a tool; when that fails, the evidence is what stderr the run left and the reason.
func (a *assertion) read(path string, args ...string) readOut {
	timeout := a.env.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	res, err := a.env.Runner.Run(ctx, proc.Cmd{Argv: append([]string{path}, args...), Dir: a.root})
	var reason string
	switch {
	case err != nil:
		reason = err.Error()
	case res.TimedOut:
		reason = fmt.Sprintf("timed out after %s", timeout)
	case res.Leftover:
		reason = "left a process running after it exited"
	case res.Code != 0:
		reason = fmt.Sprintf("exited with status %d", res.Code)
	default:
		return readOut{stdout: res.Stdout, stderr: res.Stderr, ok: true}
	}
	return readOut{evidence: unreadableEvidence(res.Stderr, reason)}
}

func unreadableEvidence(stderr, reason string) string {
	shown := strings.TrimSpace(stderr)
	if shown == "" {
		shown = "(nothing)"
	}
	return shown + "\n  reason: " + reason
}

func (a *assertion) fail(lines ...string) {
	a.mismatches++
	for _, l := range lines {
		a.errOut.line(l)
	}
}

func (a *assertion) unreadable(tool, expectation, evidence string) {
	a.fail(fmt.Sprintf("✗ toolchain: %s is installed but its version could not be read (%s)", tool, expectation),
		"  its stderr was:", evidence)
}

// pinned asserts one tool against its pin.
func (a *assertion) pinned(t pinnedTool, expected string) {
	a.checked++
	path, err := a.env.LookPath(t.name)
	if err != nil {
		a.fail(fmt.Sprintf("✗ toolchain: %s is not installed (expected %s)", t.name, expected))
		return
	}
	got := a.read(path, "--version")
	found := ""
	if got.ok {
		found = t.parse(got.stdout)
		if found == "" {
			got.evidence = unreadableEvidence(got.stderr, "its --version output holds no version")
		}
	}
	if found == "" {
		a.unreadable(t.name, "expected "+expected, got.evidence)
		return
	}
	if found != expected {
		a.fail(fmt.Sprintf("✗ toolchain: %s version mismatch", t.name),
			fmt.Sprintf("    expected : %s  (%s)", expected, pinFile),
			fmt.Sprintf("    found    : %s  (%s)", found, path))
	}
}

// goFloor asserts go is present and not older than the floor.
func (a *assertion) goFloor(floor string) {
	a.checked++
	path, err := a.env.LookPath("go")
	if err != nil {
		a.fail(fmt.Sprintf("✗ toolchain: go is not installed (need >= %s, %s)", floor, goModFile))
		return
	}
	got := a.read(path, "env", "GOVERSION")
	found := strings.TrimPrefix(field(got.stdout, 0), "go")
	if got.ok && !version.IsValid("go"+found) {
		got.evidence = unreadableEvidence(got.stderr, fmt.Sprintf("go env GOVERSION printed %q, not a Go version", strings.TrimSpace(got.stdout)))
		got.ok = false
	}
	if !got.ok {
		a.unreadable("go", "need >= "+floor, got.evidence)
		return
	}
	if version.Compare("go"+floor, "go"+found) > 0 {
		a.fail("✗ toolchain: go is older than "+goModFile+" requires",
			fmt.Sprintf("    need >= : %s  (%s)", floor, goModFile),
			fmt.Sprintf("    found   : %s  (%s)", found, path))
	}
}
