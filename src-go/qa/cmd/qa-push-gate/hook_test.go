package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// hookUnderTest is a scratch scripts directory holding the tracked pre-push hook and, under the
// two gates' names, testdata/stub-gate.bash, each linked rather than copied so they stay
// executable: each stub records its arguments and stdin beside itself.
func hookUnderTest(t *testing.T) string {
	t.Helper()
	hook, err := filepath.Abs("../../../../scripts/git-hooks/pre-push")
	if err != nil {
		t.Fatal(err)
	}
	stub, err := filepath.Abs("testdata/stub-gate.bash")
	if err != nil {
		t.Fatal(err)
	}
	scripts := filepath.Join(t.TempDir(), "scripts")
	if err := os.MkdirAll(filepath.Join(scripts, "git-hooks"), 0o750); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		"git-hooks/pre-push":    hook,
		"qa-private-names.bash": stub,
		"qa-push-gate.bash":     stub,
	} {
		if err := os.Symlink(target, filepath.Join(scripts, link)); err != nil {
			t.Fatal(err)
		}
	}
	return scripts
}

func read(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "<none>"
	}
	return string(body)
}

// A real git push through the hook: git refuses the push when the verdict gate refuses, and both
// gates are handed git's arguments and the same ref lines.
func TestThePrePushHookRunsBothGatesAndGitHonoursTheVerdict(t *testing.T) {
	for _, tc := range []struct {
		name, private, gate string
		pushed, gateRan     bool
	}{
		{"both pass", "0", "0", true, true},
		{"verdict refuses", "0", "1", false, true},
		{"private names refuse first", "1", "0", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) { pushThroughHook(t, tc.private, tc.gate, tc.pushed, tc.gateRan) })
	}
}

func pushThroughHook(t *testing.T, private, gate string, wantPushed, wantGateRan bool) {
	t.Helper()
	scripts := hookUnderTest(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	git(t, t.TempDir(), nil, "init", "-q", "--bare", remote)
	repo := t.TempDir()
	git(t, repo, nil, "init", "-q", "-b", "F44")
	git(t, repo, nil, "remote", "add", "origin", remote)
	git(t, repo, nil, "config", "core.hooksPath", filepath.Join(scripts, "git-hooks"))
	git(t, repo, nil, "commit", "-q", "--allow-empty", "-m", "one")
	head := git(t, repo, nil, "rev-parse", "HEAD")
	env := append([]string{"STUB_EXIT_qa_private_names=" + private, "STUB_EXIT_qa_push_gate=" + gate}, gitEnv...)
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "-C", repo, "push", "-q", "origin", "F44"}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if pushed := res.Code == 0; pushed != wantPushed {
		t.Fatalf("push exit %d, want pushed=%t: %s", res.Code, wantPushed, res.Stderr)
	}
	refLine := "refs/heads/F44 " + head + " refs/heads/F44 0000000000000000000000000000000000000000\n"
	if got := read(t, filepath.Join(scripts, "qa-private-names.stdin")); got != refLine {
		t.Fatalf("private names got stdin %q", got)
	}
	if got := read(t, filepath.Join(scripts, "qa-private-names.args")); got != "push origin "+remote+"\n" {
		t.Fatalf("private names got args %q", got)
	}
	gateStdin, gateArgs := read(t, filepath.Join(scripts, "qa-push-gate.stdin")), read(t, filepath.Join(scripts, "qa-push-gate.args"))
	switch {
	case !wantGateRan && gateStdin != "<none>":
		t.Fatal("the verdict gate ran after the private-names scan refused")
	case wantGateRan && (gateStdin != refLine || gateArgs != "origin "+remote+"\n"):
		t.Fatalf("verdict gate got stdin %q args %q", gateStdin, gateArgs)
	}
}
