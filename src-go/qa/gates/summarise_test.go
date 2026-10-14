package gates

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

func detailGate(patterns ...string) Gate {
	g := Gate{Name: "g", Kind: KindGate, Argv: []string{"scripts/g.bash"}}
	for _, p := range patterns {
		g.Readers = append(g.Readers, Reader{Detail: regexp.MustCompilePOSIX(p)})
	}
	return g
}

// Every reader of a row must find its line: qa-all and qa-changed share this one judge.
func TestSummariseRequiresEveryReaderOfTheRow(t *testing.T) {
	g := detailGate("^alpha [0-9]+$", "^beta [0-9]+$", "^gamma [0-9]+$")
	got, err := Summarise(g, "alpha 1\nbeta 2\ngamma 3\n")
	if err != nil || got != "alpha 1; beta 2; gamma 3" {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, capture := range []string{"gamma 3\n", "alpha 1\ngamma 3\n", "alpha 1\nbeta 2\n"} {
		if _, err := Summarise(g, capture); err == nil || !strings.Contains(err.Error(), "summary line was not found") {
			t.Fatalf("%q: a missing reader line must be refused, got %v", capture, err)
		}
	}
}

func TestSummariseMixesCountAndDetailReaders(t *testing.T) {
	g := detailGate("^done [0-9]+$")
	g.Readers = append([]Reader{{}}, g.Readers...)
	got, err := Summarise(g, "passed: 4 failed: 0\ndone 9\n")
	if err != nil || got != "passed: 4; done 9" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := Summarise(g, "done 9\n"); !errors.Is(err, ErrNoCount) {
		t.Fatalf("the count reader must still be required: %v", err)
	}
	if _, err := Summarise(g, "passed: 4 failed: 2\ndone 9\n"); !errors.Is(err, ErrFailedCases) {
		t.Fatalf("failed cases must stay distinguishable: %v", err)
	}
}

func TestSummariseDriftNeedsItsOwnStageLine(t *testing.T) {
	g := Gate{Name: "deployed-drift", Kind: KindDrift, Argv: []string{"scripts/d.bash"}}
	if got, err := Summarise(g, "noise\n✓ deployed-drift: 3 files match\n"); err != nil || got != "3 files match" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := Summarise(g, "⚠ deployed-drift: 1 drifted\n"); err != nil {
		t.Fatalf("a warning stage line is the drift gate's own verdict: %v", err)
	}
	if _, err := Summarise(g, "all fine\n"); err == nil || !strings.Contains(err.Error(), "printed no deployed-drift stage line") {
		t.Fatalf("no stage line is no pass: %v", err)
	}
}

func TestSummariseRefusesKindsWhoseVerdictIsNotInTheirOutput(t *testing.T) {
	for _, kind := range []Kind{KindHelpers, KindStage} {
		g := Gate{Name: "x", Kind: kind, Argv: []string{"scripts/x.bash"}}
		if _, err := Summarise(g, "passed: 5\n"); err == nil {
			t.Fatalf("%s: its verdict is not in its output, so it cannot be judged from it", kind)
		}
	}
}
