package all

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qaverdict"
)

// CI runs the table in parallel jobs: --only for each job that takes named gates, --except for
// the job that takes the rest. --only runs the named gates alone, in table order.
func TestOnlyRunsTheNamedGatesInTableOrder(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	if code := w.main(context.Background(), "--only", "suite-b,toolchain"); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	if want := []string{"qa-toolchain.bash", "test-b.bash"}; !reflect.DeepEqual(w.runner.log, want) {
		t.Fatalf("ran %v, want %v", w.runner.log, want)
	}
	if !strings.Contains(w.errOut.String(), "2 of 9 gates") {
		t.Fatalf("stderr does not say the run is a subset: %q", w.errOut.String())
	}
}

// --except runs every gate but the named ones, so --only A and --except A together run each
// gate exactly once.
func TestExceptAndOnlyOfOneListRunEveryGateExactlyOnce(t *testing.T) {
	only := newWorld(t, smallTable)
	passing(only)
	if code := only.main(context.Background(), "--only", "helper-tests,suite-a"); code != ExitOK {
		t.Fatalf("--only: exit %d, stderr %q", code, only.errOut.String())
	}
	rest := newWorld(t, smallTable)
	passing(rest)
	if code := rest.main(context.Background(), "--except", "helper-tests,suite-a"); code != ExitOK {
		t.Fatalf("--except: exit %d, stderr %q", code, rest.errOut.String())
	}
	whole := newWorld(t, smallTable)
	passing(whole)
	whole.main(context.Background())
	got := append(append([]string{}, only.runner.log...), rest.runner.log...)
	if len(got) != len(whole.runner.log) {
		t.Fatalf("the two halves ran %v, the whole run %v", got, whole.runner.log)
	}
	for _, ran := range whole.runner.log {
		if n := countOf(got, ran); n != 1 {
			t.Fatalf("%s ran %d times across the two halves", ran, n)
		}
	}
}

func countOf(list []string, item string) int {
	n := 0
	for _, s := range list {
		if s == item {
			n++
		}
	}
	return n
}

// A failing gate inside the subset still fails the run.
func TestASubsetFailsOnAFailingGate(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.runner.replies["test-a.bash"] = reply{code: 1}
	if code := w.main(context.Background(), "--only", "suite-a"); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
}

// A name the table does not hold is refused before anything runs: a renamed gate must not drop
// out of every CI job unnoticed.
func TestASubsetNamingNoGateIsRefused(t *testing.T) {
	for _, args := range [][]string{
		{"--only", "suite-a,nosuch"},
		{"--except", "nosuch"},
		{"--only", "suite-a", "--except", "suite-b"},
		{"--only", ""},
		{"--only", "suite-a,,suite-b"},
		{"--except"},
	} {
		w := newWorld(t, smallTable)
		passing(w)
		if code := w.main(context.Background(), args...); code != ExitCannotRun {
			t.Fatalf("%v: exit %d", args, code)
		}
		if len(w.runner.log) != 0 {
			t.Fatalf("%v: ran %v", args, w.runner.log)
		}
	}
}

// --except of every gate leaves nothing to run, which proves nothing: refused.
func TestASubsetThatRunsNothingIsRefused(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	all := "toolchain,bash,docs,deployed-drift,helper-tests,suite-a,suite-b,manifest,compat"
	if code := w.main(context.Background(), "--except", all); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
}

// A subset is not the full run: it writes no verdict and leaves the last full run's alone.
func TestASubsetLeavesTheVerdictAlone(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	old := qaverdict.Record{Status: qaverdict.StatusFail, Commit: worldHead, Started: worldClock[0], Finished: worldClock[0]}
	if err := qaverdict.Write(w.verdictPath(), old); err != nil {
		t.Fatal(err)
	}
	if code := w.main(context.Background(), "--only", "suite-a"); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	got, err := qaverdict.Read(w.verdictPath())
	if err != nil {
		t.Fatal(err)
	}
	if got != old {
		t.Fatalf("verdict %+v, want the old %+v untouched", got, old)
	}
	fresh := newWorld(t, smallTable)
	passing(fresh)
	fresh.main(context.Background(), "--except", "suite-a")
	if _, err := os.Stat(fresh.verdictPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a subset wrote a verdict: %v", err)
	}
}
