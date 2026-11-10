package all

import (
	"context"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
)

// headline is the run's verdict line, the one a reader who reads only one line sees.
func headline(t *testing.T, stdout string) string {
	t.Helper()
	for line := range strings.SplitSeq(stdout, "\n") {
		if strings.HasPrefix(line, "✗ QA FAILED: ") || strings.HasPrefix(line, "✓ QA passed: ") {
			return line
		}
	}
	t.Fatalf("no verdict line:\n%s", stdout)
	return ""
}

// Rule summary-count-omits-failure-source (issue #183): the headline counts every source of
// failure it summarises. Every gate of the real table fails alone in turn; the headline must
// count that failure, whatever kind of gate it is, and never read as a failure of nothing.
func TestTheHeadlineCountsEveryKindOfFailure(t *testing.T) {
	order := tableOrder(t)
	_, gs := realTable(t)
	for i, g := range gs {
		t.Run(g.Name, func(t *testing.T) {
			w := newRealWorld(t)
			w.runner.replies[order[i]] = reply{stdout: "boom\n", code: 1, report: bashStage}
			if code := w.main(context.Background()); code != ExitFailed {
				t.Fatalf("exit %d", code)
			}
			line := headline(t, w.out.String())
			want := "1 gate(s) failed"
			if g.Kind == gates.KindStage {
				want = "1 stage(s) failed"
			}
			if !strings.Contains(line, want) {
				t.Fatalf("a failing %s (%v) is not counted in the headline %q", g.Name, g.Kind, line)
			}
		})
	}
}

// A hard gate failing alone, with every lint report clean: the headline says what failed first,
// and the lint numbers only as what they are.
func TestAHardGateFailureIsTheHeadlineNotZeroErrors(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.runner.replies["test-a.bash"] = reply{code: 1}
	if code := w.main(context.Background()); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	if got, want := headline(t, w.out.String()), "✗ QA FAILED: 1 gate(s) failed; 0 lint errors in 8 files"; got != want {
		t.Fatalf("headline %q, want %q", got, want)
	}
}
