package main

import (
	"bytes"
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ansiblesyntax"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(ansiblesyntax.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(ansiblesyntax.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

func TestWriteReportWritesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	if err := writeReport(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "{}" {
		t.Fatalf("%q %v", data, err)
	}
	if err := writeReport(filepath.Join(t.TempDir(), "no", "r.json"), nil); err == nil {
		t.Fatal("a report in a missing directory was written")
	}
}

// S3: a closed stdout cannot deliver the verdict, so the command refuses to run.
func TestClosedStdoutIsExitTwo(t *testing.T) {
	if os.Getenv(closedStdoutChild) != "" {
		os.Exit(run([]string{"--help"}))
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	res, runErr := proc.OS{}.Run(context.Background(), proc.Cmd{
		Argv: []string{"bash", "-c", `exec "$0" -test.run='^TestClosedStdoutIsExitTwo$' >&-`, self},
		Env:  []string{closedStdoutChild + "=1"},
	})
	if runErr != nil || res.Code != int(ansiblesyntax.ExitCannotRun) || !strings.Contains(res.Stderr, "ERROR: stdout is closed") {
		t.Fatalf("closed stdout: %v exit %d\nstderr:\n%s", runErr, res.Code, res.Stderr)
	}
}

// closedStdoutChild names the environment variable that turns this test binary into the command.
const closedStdoutChild = "QA_ANSIBLE_SYNTAX_CLOSED_STDOUT_CHILD"

// outcome is what one run over a real work tree produced.
type outcome struct {
	code   ansiblesyntax.ExitCode
	stdout string
	stderr string
	report string
}

// realRun runs the gate with the real git and ansible over a work tree holding files, an
// ansible.cfg and the trees table. A machine without ansible fails here with the gate's message.
func realRun(t *testing.T, files map[string]string) outcome {
	t.Helper()
	root := t.TempDir()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	all := map[string]string{
		"ansible.cfg":          "[defaults]\nnocows = 1\n",
		"scripts/qa-trees.tsv": "shell\t.git\npython\t.venv\n",
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
	if res, err := (proc.OS{}).Run(context.Background(), proc.Cmd{Argv: []string{"git", "add", "-A"}, Dir: root}); err != nil || res.Code != 0 {
		t.Fatalf("git add: %v %+v", err, res)
	}
	reportPath := filepath.Join(t.TempDir(), "report.json")
	var stdout, stderr bytes.Buffer
	env := ansiblesyntax.Env{
		Getenv: func(k string) string {
			if k == "QA_JSON_OUT" {
				return reportPath
			}
			return ""
		},
		Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}, WriteFile: writeReport,
	}
	code := ansiblesyntax.Main([]string{"--root", root}, env)
	got := outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
	if data, readErr := os.ReadFile(reportPath); readErr == nil {
		got.report = string(data)
	}
	return got
}

const goodPlay = "---\n- hosts: localhost\n  gather_facts: false\n  tasks:\n    - ansible.builtin.debug:\n        msg: hi\n"

// S10, S14: the real engine passes a good playbook and fails a malformed one, naming it.
func TestRealEngineJudgesPlaybooks(t *testing.T) {
	got := realRun(t, map[string]string{"playbooks/good.yml": goodPlay})
	if got.code != ansiblesyntax.ExitOK || got.stdout != "✓ ansible-syntax: 1 playbooks OK (0 under playbooks/imports/, 1 elsewhere)\n" {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
	got = realRun(t, map[string]string{
		"playbooks/good.yml": goodPlay,
		"playbooks/bad.yml":  "- hosts: localhost\n  tasks: [unclosed\n",
	})
	if got.code != ansiblesyntax.ExitFailed || !strings.Contains(got.stdout, "✗ ansible-syntax: playbooks/bad.yml\n") ||
		strings.Contains(got.stdout, "✗ ansible-syntax: playbooks/good.yml") || !strings.Contains(got.stdout, "1/2 playbooks failed") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
}

// S6: the password file the engine will look for, however it is set (here the environment
// variable that overrides the config), must exist; the real engine's own resolution says which.
func TestRealMissingPasswordFileIsExitTwoAndPresentOneIsEnough(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	t.Setenv("ANSIBLE_VAULT_PASSWORD_FILE", missing)
	got := realRun(t, map[string]string{"playbooks/good.yml": goodPlay})
	if got.code != ansiblesyntax.ExitCannotRun || !strings.Contains(got.stderr, missing) || got.stdout != "" || got.report != "" {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
	present := filepath.Join(t.TempDir(), "present")
	if err := os.WriteFile(present, []byte("any text\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANSIBLE_VAULT_PASSWORD_FILE", present)
	got = realRun(t, map[string]string{"playbooks/good.yml": goodPlay})
	if got.code != ansiblesyntax.ExitOK {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
}

// S5: a config nested next to the playbooks cannot replace the pinned one.
func TestRealNestedConfigCannotReplaceThePinnedOne(t *testing.T) {
	got := realRun(t, map[string]string{
		"playbooks/good.yml":    goodPlay,
		"playbooks/ansible.cfg": "[defaults]\ninventory = /nonexistent-inventory\nroles_path = /nonexistent-roles\n",
	})
	if got.code != ansiblesyntax.ExitOK {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
}
