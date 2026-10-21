package main

import (
	"bytes"
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/bash"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func TestMain(m *testing.M) { os.Exit(cmdmaintest.Main(m)) }

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(bash.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(bash.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

// outcome is what one run over a real work tree produced.
type outcome struct {
	code   bash.ExitCode
	root   string
	stdout string
	stderr string
	report string
}

// realRun runs the gate with the real git, bash and shellcheck over a work tree holding files.
func realRun(t *testing.T, files map[string]string) outcome {
	t.Helper()
	return realRunWith(t, files, func(root string) []string { return []string{"--root", root} })
}

// realRunWith is realRun with the command line chosen by the test from the root.
func realRunWith(t *testing.T, files map[string]string, args func(root string) []string) outcome {
	t.Helper()
	root := t.TempDir()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	all := map[string]string{"scripts/qa-trees.tsv": "shell\t.git\nshell\tuntracked\npython\t.venv\n"}
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
	// The coverage yardstick is the tracked scripts, so the tree is staged as a checkout is.
	if res, err = (proc.OS{}).Run(context.Background(), proc.Cmd{Argv: []string{"git", "add", "-A"}, Dir: root}); err != nil || res.Code != 0 {
		t.Fatalf("git add: %v %+v", err, res)
	}
	reportPath := filepath.Join(t.TempDir(), "report.json")
	var stdout, stderr bytes.Buffer
	env := bash.Env{
		Getenv: func(k string) string {
			if k == "QA_JSON_OUT" {
				return reportPath
			}
			return ""
		},
		Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}, WriteFile: cmdmain.WriteReport,
	}
	code := bash.Main(args(root), env)
	got := outcome{code: code, root: root, stdout: stdout.String(), stderr: stderr.String()}
	if data, readErr := os.ReadFile(reportPath); readErr == nil {
		got.report = string(data)
	}
	return got
}

// S7, S14: a clean tree passes with the real tools.
func TestRealToolsPassACleanTree(t *testing.T) {
	got := realRun(t, map[string]string{
		"good.bash":      "#!/usr/bin/env bash\nset -euo pipefail\necho \"$HOME\"\n",
		"bin/prog":       "#!/bin/sh\n:\n",
		"untracked/x.sh": "if then\n",
	})
	if got.code != bash.ExitOK || got.stdout != "✓ bash: 2 files OK\n" {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
}

// S7, S10, S12, S13: a syntax error and a gating finding fail with the real tools, without the
// checkout's path.
func TestRealToolsFailASyntaxErrorAndAFinding(t *testing.T) {
	got := realRun(t, map[string]string{
		"bad.bash":   "#!/usr/bin/env bash\nif true; then\n",
		"unq.bash":   "#!/usr/bin/env bash\necho $1\nx=$(date)\n",
		"fine.bash":  "#!/usr/bin/env bash\n:\n",
		"style.bash": "#!/usr/bin/env bash\nx='$HOME'\necho \"$x\"\n",
	})
	if got.code != bash.ExitFailed {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
	for _, want := range []string{"✗ bash: bad.bash: bad.bash: line ", "unq.bash:3 SC2034 (warning)", "⚠ shellcheck: ", "✗ bash: 2/4 files failed → "} {
		if !strings.Contains(got.stdout, want) {
			t.Fatalf("stdout lacks %q:\n%s", want, got.stdout)
		}
	}
	if strings.Contains(got.stdout, got.root) || strings.Contains(got.report, got.root) {
		t.Fatalf("the checkout path is in the output or the report:\n%s\n%s", got.stdout, got.report)
	}
	if !strings.Contains(got.report, `"file": "bad.bash"`) || !strings.Contains(got.report, `"shellcheck_diagnostics"`) {
		t.Fatalf("report:\n%s", got.report)
	}
}

// S8: a warning shellcheck gives by default fails, and no .shellcheckrc beside the script (nor
// the environment) can disable it.
func TestRealNestedRcFileAndEnvironmentCannotHideAFinding(t *testing.T) {
	t.Setenv("SHELLCHECK_OPTS", "--exclude=SC2155")
	got := realRun(t, map[string]string{
		"sub/.shellcheckrc": "disable=SC2155\n",
		"sub/warn.bash":     "#!/usr/bin/env bash\nf() {\n  local x=$(date)\n  echo \"$x\"\n}\nf\n",
	})
	if got.code != bash.ExitFailed || !strings.Contains(got.stdout, "sub/warn.bash:3 SC2155 (warning)") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
}

// S9: a source directive is followed, so a variable a library defines is not reported unassigned,
// even when the library is not among the files checked (it is in an excluded tree).
func TestRealSourcedLibraryIsFollowed(t *testing.T) {
	got := realRun(t, map[string]string{
		"untracked/lib.bash": "export from_lib=1\n",
		"main.bash":          "#!/usr/bin/env bash\n# shellcheck source=untracked/lib.bash\nsource ./untracked/lib.bash\necho \"$from_lib\"\n",
	})
	if got.code != bash.ExitOK {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
}

// S1: the real tools with a relative root and with a trailing slash.
func TestRealToolsAcceptARelativeAndATrailingSlashRoot(t *testing.T) {
	for name, args := range map[string]func(string) []string{
		"relative":       func(string) []string { return []string{"--root", "."} },
		"trailing slash": func(root string) []string { return []string{"--root", root + "/"} },
	} {
		got := realRunWith(t, map[string]string{"good.bash": "#!/usr/bin/env bash\n:\n"}, args)
		if got.code != bash.ExitOK {
			t.Fatalf("%s: exit %d\nstdout:\n%s\nstderr:\n%s", name, got.code, got.stdout, got.stderr)
		}
	}
}

// S3: with stdout closed (>&-) the run is exit 2, it does not report a verdict nobody can read.
// The test re-runs its own binary as the command over a real tree, with fd 1 closed by the shell.
func TestAClosedStdoutIsExitTwo(t *testing.T) {
	t.Setenv("QA_JSON_OUT", filepath.Join(t.TempDir(), "r.json"))
	root := t.TempDir()
	if res, err := (proc.OS{}).Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root}); err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	for rel, text := range map[string]string{"scripts/qa-trees.tsv": "shell\t.git\npython\t.venv\n", "good.bash": "#!/usr/bin/env bash\n:\n"} {
		path := filepath.Join(root, rel)
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o750); mkErr != nil {
			t.Fatal(mkErr)
		}
		if wrErr := os.WriteFile(path, []byte(text), 0o600); wrErr != nil {
			t.Fatal(wrErr)
		}
	}
	if res, err := (proc.OS{}).Run(context.Background(), proc.Cmd{Argv: []string{"git", "add", "-A"}, Dir: root}); err != nil || res.Code != 0 {
		t.Fatalf("git add: %v %+v", err, res)
	}
	cmdmaintest.RefusesClosedStdout(t, func() int { return run([]string{"--root", root}) }, int(bash.ExitCannotRun))
}
