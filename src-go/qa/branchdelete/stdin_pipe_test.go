package branchdelete

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/stdinread"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/stdinread/stdinreadtest"
)

// Issue 260: the candidate list read from an open pipe nothing writes to (an agent's shell's
// stdin for a command given no input) says it is waiting, then cannot judge, naming the bound.
func TestTheCandidateListOnAnOpenPipeIsBounded(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := errors.Join(pw.Close(), pr.Close()); err != nil {
			t.Error(err)
		}
	})
	c := stdinreadtest.NewClock()
	var out, errOut bytes.Buffer
	env := Env{
		Stdin: pr, Stdout: &out, Stderr: &errOut, Cwd: "/work", Clock: c,
		Eval: func(p string) (string, error) { return p, nil },
		Git:  &fakeGit{stdout: lsRemote},
		ReadFile: func(p string) ([]byte, error) {
			if p == "/repo/scripts/branch-policy.toml" {
				return []byte(goodPolicy), nil
			}
			return nil, errors.New("open " + p + ": no such file or directory")
		},
	}
	done := make(chan ExitCode, 1)
	go func() { done <- Main([]string{"--root", "/repo"}, env) }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := c.Await(ctx, stdinread.NoticeAfter); err != nil {
		t.Fatal(err)
	}
	c.Fire(stdinread.NoticeAfter)
	c.Fire(stdinread.PipedGiveUp)
	if code := <-done; code != ExitCannotRun || out.Len() != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
	for _, want := range []string{"branch-delete-list: waiting for input on stdin", "no input arrived on stdin within 60 s"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr lacks %q: %q", want, errOut.String())
		}
	}
}
