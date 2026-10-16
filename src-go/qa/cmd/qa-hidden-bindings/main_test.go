package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/hiddenbindings"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// A closed stdout cannot carry the verdict, so the command refuses to run.
func TestClosedStdoutIsRefused(t *testing.T) {
	cmdmaintest.RefusesClosedStdout(t, func() int { return run([]string{"--help"}) }, int(hiddenbindings.ExitCannotRun))
}

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(hiddenbindings.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(hiddenbindings.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

// The real fixtures, proven by the real shell rule and the real Python helper, then two real
// files that bind no key.
func TestTheRealFixturesProveBothHalves(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	env := hiddenbindings.Env{Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}}
	code := hiddenbindings.Main([]string{"--root", root, "scripts/qa-go.bash", "helpers/qa_environment/textual_bindings.py"}, env)
	if code != hiddenbindings.ExitOK {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, stdout.String(), stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "HIDDEN-BINDINGS-OK 1 shell file(s), 1 Python file(s)") {
		t.Errorf("stdout %q", stdout.String())
	}
}
