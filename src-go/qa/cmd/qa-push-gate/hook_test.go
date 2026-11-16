package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// hookUnderTest is a scratch scripts directory holding the tracked pre-push hook and, under the
// three gates' names, testdata/stub-gate.bash, each linked rather than copied so they stay
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
		"qa-gitleaks.bash":      stub,
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

// hookCase is one push through the hook: each gate's exit status, and which later gates must run.
type hookCase struct {
	name, private, leaks, gate string
	pushed, leaksRan, gateRan  bool
}

// A real git push through the hook: git refuses the push when any gate refuses, a refusal stops
// the gates after it, and each gate that runs is handed git's arguments and the same ref lines.
func TestThePrePushHookRunsEveryGateAndGitHonoursTheVerdict(t *testing.T) {
	for _, tc := range []hookCase{
		{"all pass", "0", "0", "0", true, true, true},
		{"verdict refuses", "0", "0", "1", false, true, true},
		{"gitleaks refuses before the verdict", "0", "1", "0", false, true, false},
		{"private names refuse first", "1", "0", "0", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) { pushThroughHook(t, tc) })
	}
}

func pushThroughHook(t *testing.T, tc hookCase) {
	t.Helper()
	scripts := hookUnderTest(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	git(t, t.TempDir(), "init", "-q", "--bare", remote)
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "F44")
	git(t, repo, "remote", "add", "origin", remote)
	git(t, repo, "config", "core.hooksPath", filepath.Join(scripts, "git-hooks"))
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "one")
	head := git(t, repo, "rev-parse", "HEAD")
	env := append([]string{
		"STUB_EXIT_qa_private_names=" + tc.private, "STUB_EXIT_qa_gitleaks=" + tc.leaks, "STUB_EXIT_qa_push_gate=" + tc.gate,
	}, gitEnv...)
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "-C", repo, "push", "-q", "origin", "F44"}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if pushed := res.Code == 0; pushed != tc.pushed {
		t.Fatalf("push exit %d, want pushed=%t: %s", res.Code, tc.pushed, res.Stderr)
	}
	refLine := "refs/heads/F44 " + head + " refs/heads/F44 0000000000000000000000000000000000000000\n"
	if got := read(t, filepath.Join(scripts, "qa-private-names.stdin")); got != refLine {
		t.Fatalf("private names got stdin %q", got)
	}
	if got := read(t, filepath.Join(scripts, "qa-private-names.args")); got != "push origin "+remote+"\n" {
		t.Fatalf("private names got args %q", got)
	}
	ranAsWanted(t, scripts, "qa-gitleaks", tc.leaksRan, refLine, "push origin "+remote+"\n")
	ranAsWanted(t, scripts, "qa-push-gate", tc.gateRan, refLine, "origin "+remote+"\n")
}

// ranAsWanted checks that the stub named gate ran only when it should, with these stdin and args.
func ranAsWanted(t *testing.T, scripts, gate string, wantRan bool, stdin, args string) {
	t.Helper()
	gotStdin, gotArgs := read(t, filepath.Join(scripts, gate+".stdin")), read(t, filepath.Join(scripts, gate+".args"))
	switch {
	case !wantRan && gotStdin != "<none>":
		t.Fatalf("%s ran after an earlier gate refused", gate)
	case wantRan && (gotStdin != stdin || gotArgs != args):
		t.Fatalf("%s got stdin %q args %q", gate, gotStdin, gotArgs)
	}
}
