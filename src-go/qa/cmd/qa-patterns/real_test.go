package main

import (
	"bytes"
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/patterns"
)

// projectFile reads a file of the real repository (the rules and their fixture) by its path from
// this package.
func projectFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// realOutcome is what one run over a real work tree produced.
type realOutcome struct {
	code   patterns.ExitCode
	stdout string
	report string
}

// realRun runs the gate with the real git and semgrep over a work tree holding the project's own
// rules and fixture plus files.
func realRun(t *testing.T, files map[string]string) realOutcome {
	t.Helper()
	root := t.TempDir()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	all := map[string]string{
		"scripts/qa-trees.tsv":               "shell\t.semgrep\n",
		"scripts/patterns-partial.allowlist": "# none\n",
		".semgrep/bash-conventions.yml":      projectFile(t, ".semgrep/bash-conventions.yml"),
		".semgrep/bash-conventions.bash":     projectFile(t, ".semgrep/bash-conventions.bash"),
	}
	maps.Copy(all, files)
	for rel, text := range all {
		path := filepath.Join(root, rel)
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o750); mkErr != nil {
			t.Fatal(mkErr)
		}
		if wrErr := os.WriteFile(path, []byte(text), 0o600); wrErr != nil {
			t.Fatal(wrErr)
		}
	}
	reportPath := filepath.Join(t.TempDir(), "report.json")
	var stdout, stderr bytes.Buffer
	env := patterns.Env{
		Getenv: func(k string) string {
			if k == "QA_JSON_OUT" {
				return reportPath
			}
			return ""
		},
		Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}, TempDir: t.TempDir(), WriteFile: cmdmain.WriteReportPrivate,
	}
	code := patterns.Main([]string{"--root", root}, env)
	if code == patterns.ExitCannotRun {
		t.Fatalf("the run could not complete:\n%s", stderr.String())
	}
	report, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), root) || strings.Contains(string(report), root) {
		t.Fatalf("the checkout path is in the output or the report:\n%s\n%s", stdout.String(), report)
	}
	return realOutcome{code: code, stdout: stdout.String(), report: string(report)}
}

// P11, P15: the rule id printed and reported is the bare id from the rules file.
func TestRealSemgrepReportsBareRuleIds(t *testing.T) {
	got := realRun(t, map[string]string{"scripts/bad.bash": "#!/usr/bin/bash\nfalse || echo oops\n"})
	if got.code != patterns.ExitFailed {
		t.Fatalf("exit %d\n%s", got.code, got.stdout)
	}
	if !strings.Contains(got.stdout, "\n  ✗ scripts/bad.bash\n    bash-error-hiding-pipe-echo:2 Error hiding: '|| echo' swallows") {
		t.Fatalf("stdout:\n%s", got.stdout)
	}
	if !strings.Contains(got.report, `"error": "bash-error-hiding-pipe-echo:2 Error hiding: `) {
		t.Fatalf("report:\n%s", got.report)
	}
}

// P10: a CRLF script is matched by the rules that anchor on the end of the line.
func TestRealSemgrepMatchesDollarAnchoredRulesInACRLFScript(t *testing.T) {
	script := "#!/usr/bin/bash\r\ncmd || true\r\nx=$(cmd 2>/dev/null)\r\n"
	got := realRun(t, map[string]string{"scripts/crlf.bash": script})
	if got.code != patterns.ExitFailed {
		t.Fatalf("exit %d\n%s", got.code, got.stdout)
	}
	for _, want := range []string{"    bash-error-hiding-or-true:2 ", "bash-capture-discards-status:3 "} {
		if !strings.Contains(got.stdout, want) {
			t.Fatalf("stdout lacks %q:\n%s", want, got.stdout)
		}
	}
}

// semgrepDefaultMaxTargetBytes is the size above which semgrep skips a file unless told otherwise
// (`--max-target-bytes`, absent=1000000).
const semgrepDefaultMaxTargetBytes = 1_000_000

// P10, P11: semgrep's own ignore list and size limit do not drop a script the gate discovered.
//
// The large script is padded with comment lines, just over semgrep's default size limit: that is
// all the size filter looks at, and a comment is the cheapest bash to parse. Semgrep counts the
// parse against its per-rule timeout on the file (5 s); padding of commands took 2 to 4.5 s to
// parse on a loaded host and timed out, comments take under 0.5 s.
func TestRealSemgrepScansTestsDirectoriesAndLargeScripts(t *testing.T) {
	big := "#!/usr/bin/bash\n" + strings.Repeat("# padding to make this script larger than semgrep's default size limit\n", 14100) + "cmd || true\n"
	if len(big) <= semgrepDefaultMaxTargetBytes {
		t.Fatalf("the large script is %d bytes, not over semgrep's default limit of %d", len(big), semgrepDefaultMaxTargetBytes)
	}
	got := realRun(t, map[string]string{
		"tests/clean.bash":        "#!/usr/bin/bash\nset -e\n",
		"scripts/tests/bad.bash":  "#!/usr/bin/bash\ncmd || true\n",
		"scripts/vendor/bad.bash": "#!/usr/bin/bash\ncmd || true\n",
		"scripts/big.bash":        big,
	})
	if got.code != patterns.ExitFailed {
		t.Fatalf("exit %d\n%s", got.code, got.stdout)
	}
	for _, file := range []string{"scripts/tests/bad.bash", "scripts/vendor/bad.bash", "scripts/big.bash"} {
		if !strings.Contains(got.stdout, "  ✗ "+file+"\n") {
			t.Fatalf("%s was not scanned:\n%s", file, got.stdout)
		}
	}
}
