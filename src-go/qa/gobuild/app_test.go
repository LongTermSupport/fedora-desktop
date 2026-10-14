package gobuild

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func runMain(args ...string) verdict {
	var out, errOut bytes.Buffer
	code := Main(context.Background(), proc.OS{}, args, &out, &errOut)
	return verdict{code, out.String(), errOut.String()}
}

func TestDigestPrintsTheDigestAlone(t *testing.T) {
	root := toyTree(t)
	v := runMain("digest", "--source", root, "--command", "alpha")
	if want := mustDigest(t, root, "alpha") + "\n"; v.code != ExitOK || v.out != want || v.err != "" {
		t.Fatalf("%+v, want output %q", v, want)
	}
}

func TestDigestNamesAFailureOnStderrAndPrintsNothing(t *testing.T) {
	v := runMain("digest", "--source", t.TempDir(), "--command", "alpha")
	if v.code != ExitFailure || v.out != "" || !strings.Contains(v.err, "go.mod") {
		t.Fatalf("%+v", v)
	}
}

func TestDriftRunsTheCheckAgainstTheSource(t *testing.T) {
	w := newDriftWorld(t)
	w.rows(t, w.row("alpha"))
	v := runMain("drift", "--source", w.root, "--home", "/nonexistent-home")
	if v.code != ExitOK || !strings.Contains(v.out, "0 deployed Go command(s) match the checkout; 1 not installed") {
		t.Fatalf("%+v", v)
	}
}

func TestAMissingSourceFlagOrSubcommandIsUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"nope"}, {"digest"}, {"drift"}, {"digest", "--bogus"}} {
		if v := runMain(args...); v.code != ExitCannotRun || v.out != "" || v.err == "" {
			t.Errorf("%v: %+v", args, v)
		}
	}
}
