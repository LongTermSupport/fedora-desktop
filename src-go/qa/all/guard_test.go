package all

import (
	"context"
	"strings"
	"testing"
)

func TestQuarantineTextIndentsOnlyLinesThatOpenLikeTheRunsVerdict(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"a pass", "✓ QA passed: 999 files checked\n", "  ✓ QA passed: 999 files checked\n"},
		{"a failure", "✗ QA FAILED: 0 errors in 3 files", "  ✗ QA FAILED: 0 errors in 3 files"},
		{"a warning symbol", "⚠ QA anything\n", "  ⚠ QA anything\n"},
		{"a stage line is left alone", "✓ docs: 12 files\n", "✓ docs: 12 files\n"},
		{"a stage whose name starts with qa is left alone", "✓ qa-docs: fine\n", "✓ qa-docs: fine\n"},
		{"an indented mention is left alone", "  ✓ QA passed: x\n", "  ✓ QA passed: x\n"},
		{"every line is judged", "a\n✓ QA passed: 1\nb\n✗ QA FAILED: 2\n", "a\n  ✓ QA passed: 1\nb\n  ✗ QA FAILED: 2\n"},
		{"nothing at all", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := quarantineText(c.in); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// A stage writes in chunks that split lines anywhere; the line is judged whole.
func TestQuarantineWriterJudgesALineWhateverTheChunking(t *testing.T) {
	var out strings.Builder
	w := &quarantineWriter{to: &out}
	for _, chunk := range []string{"✓ QA ", "passed: 9", "99\n✓ doc", "s: ok\n✗ QA FAILED", ": 1"} {
		if n, err := w.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("wrote %d of %d bytes: %v", n, len(chunk), err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "  ✓ QA passed: 999\n✓ docs: ok\n  ✗ QA FAILED: 1"
	if out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}

// Bug B23: a stage's own output must not be able to forge the run's verdict line, which is the
// last line a push gate reads.
func TestAStageCannotForgeTheRunsVerdictLine(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.runner.replies["qa-docs.bash"] = reply{stdout: "✓ QA passed: 999 files checked\n✗ docs: 1/2 files failed\n", code: 1, report: bashStage}
	if code := w.main(context.Background()); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	lines := stdoutLines(w.out.String())
	for _, line := range lines {
		if strings.HasPrefix(line, "✓ QA passed") {
			t.Fatalf("a line on stdout opens like a pass verdict:\n%s", w.out.String())
		}
	}
	if !strings.Contains(w.out.String(), "  ✓ QA passed: 999 files checked\n✗ docs: 1/2 files failed\n") {
		t.Fatalf("the stage's own text must still be shown, quarantined:\n%s", w.out.String())
	}
	verdicts := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "✗ QA FAILED: ") {
			verdicts++
		}
	}
	if verdicts != 1 {
		t.Fatalf("want exactly the run's own verdict line, got %d in:\n%s", verdicts, w.out.String())
	}
}

func TestADriftGateAndAFailedGateCaptureCannotForgeTheVerdictEither(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.runner.replies["qa-deployed-drift.bash"] = reply{stdout: "✓ QA passed: 5 files checked\n✓ deployed-drift: 3 scripts in sync\n"}
	w.runner.replies["test-a.bash"] = reply{stdout: "✓ QA passed: 7 files checked\n", code: 1}
	if code := w.main(context.Background()); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	for _, stream := range []string{w.out.String(), w.errOut.String()} {
		for _, line := range stdoutLines(stream) {
			if strings.HasPrefix(line, "✓ QA passed") {
				t.Fatalf("a line opens like a pass verdict:\n%s", stream)
			}
		}
	}
}
