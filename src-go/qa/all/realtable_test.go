package all

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// The real wording of every detail gate is testdata/wording/<gate>.txt: the gate's own output
// (stdout and stderr merged, the checkout path written as <ROOT>), captured by running the real
// gate. The runner reads each pattern against the gate's output at run time and fails the gate
// if they stop agreeing (bug B3); these tests keep the table's patterns honest against that
// captured wording, so a pattern changed without looking at its gate fails here before it
// fails a full run. A gate that is reworded is caught by recapturing:
//
//	go test ./all -run TestCaptureTheRealWordingOfTheDetailGates -capture-wording -timeout 30m
//
// which runs every detail gate for real (the Go gate among them, so it takes minutes), refuses
// a gate that fails or whose output its pattern does not match, and rewrites the files.
var captureWording = flag.Bool("capture-wording", false, "run every detail gate for real and rewrite testdata/wording")

const wordingDir = "testdata/wording"

func wordingPath(gate string) string { return filepath.Join(wordingDir, gate+".txt") }

// realWording is the captured output of a detail gate; the test fails if it is missing.
func realWording(t *testing.T, gate string) string {
	t.Helper()
	body, err := os.ReadFile(wordingPath(gate))
	if err != nil {
		t.Fatalf("gate %s has detail patterns and no captured wording (%v): run the -capture-wording command described above captureWording", gate, err)
	}
	return string(body)
}

func detailGates(gs []gates.Gate) []gates.Gate {
	var out []gates.Gate
	for _, g := range gs {
		if slices.ContainsFunc(g.Readers, func(r gates.Reader) bool { return r.Detail != nil }) {
			out = append(out, g)
		}
	}
	return out
}

func TestCaptureTheRealWordingOfTheDetailGates(t *testing.T) {
	if !*captureWording {
		t.Skip("recaptures the real wording; run with -capture-wording")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	_, gs := realTable(t)
	for _, g := range detailGates(gs) {
		t.Run(g.Name, func(t *testing.T) {
			res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: g.Command(root), Dir: root, Merge: true})
			if err != nil || res.Code != 0 {
				t.Fatalf("the gate must pass to have wording worth capturing: %v, status %d\n%s", err, res.Code, res.Stdout)
			}
			text := strings.ReplaceAll(res.Stdout, root, "<ROOT>")
			if _, summary := summarise(g, text); summary != "" {
				t.Fatalf("the table's patterns do not read the gate's real output: %s\n%s", summary, text)
			}
			if err := os.MkdirAll(wordingDir, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(wordingPath(g.Name), []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func realTable(t *testing.T) (string, []gates.Gate) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "scripts", "qa-gates.tsv")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	gs, err := gates.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body), gs
}

func TestEveryDetailPatternReadsTheCapturedWordingOfItsGateAndNoCaptureIsOrphaned(t *testing.T) {
	_, gs := realTable(t)
	wanted := map[string]bool{}
	for _, g := range detailGates(gs) {
		wanted[g.Name+".txt"] = true
		if _, summary := summarise(g, realWording(t, g.Name)); summary != "" {
			t.Errorf("gate %s: the captured wording no longer satisfies the table: %s", g.Name, summary)
		}
	}
	entries, err := os.ReadDir(wordingDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !wanted[e.Name()] {
			t.Errorf("%s/%s belongs to no gate with a detail pattern", wordingDir, e.Name())
		}
	}
}

// newRealWorld is the real gate table over stub scripts, every gate answering as a passing one.
func newRealWorld(t *testing.T) *world {
	t.Helper()
	body, gs := realTable(t)
	w := newWorld(t, body)
	for _, g := range gs {
		key := keyOf(g.Command(w.root))
		switch g.Kind {
		case gates.KindDrift:
			w.runner.replies[key] = reply{stdout: "✓ deployed-drift: 4 scripts in sync\n"}
		case gates.KindGate:
			if slices.ContainsFunc(g.Readers, func(r gates.Reader) bool { return r.Detail != nil }) {
				w.runner.replies[key] = reply{stdout: realWording(t, g.Name)}
			}
		case gates.KindStage, gates.KindHelpers:
		}
	}
	return w
}

// tableOrder is the key of every gate of the real table, in order.
func tableOrder(t *testing.T) []string {
	t.Helper()
	_, gs := realTable(t)
	order := make([]string, len(gs))
	for i, g := range gs {
		order[i] = keyOf(g.Command("/r"))
	}
	return order
}

// A stage that tells the user where the merged report is must name the path the runner
// writes it to: QA_RESULTS_JSON (SPEC-qa-all.md B7), not a variable nothing sets.
func TestQaBashPointsAtTheReportPathTheRunnerWrites(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "qa-bash.bash"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `MERGED_JSON_OUT="${QA_RESULTS_JSON:-/tmp/qa-results.json}"`) {
		t.Error("qa-bash.bash must take the merged report path from QA_RESULTS_JSON, as qa-all does")
	}
	if strings.Contains(text, "QA_JSON_OUT_MERGED") {
		t.Error("QA_JSON_OUT_MERGED is set by nothing; qa-bash.bash must not read it")
	}
}

func TestTheRealTablePassesEndToEndInOrder(t *testing.T) {
	w := newRealWorld(t)
	if code := w.main(context.Background()); code != ExitOK {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, w.out.String(), w.errOut.String())
	}
	if order := tableOrder(t); !slices.Equal(w.runner.log, order) {
		t.Fatalf("ran %v, want %v", w.runner.log, order)
	}
	if !strings.Contains(w.out.String(), "✓ QA passed: ") {
		t.Fatalf("no verdict:\n%s", w.out.String())
	}
}

// The property the bash suite pinned by counting `exit 1` in qa-all.bash's source: no gate aborts
// the run. Here it is run: every gate of the real table fails in turn, and every other gate still
// runs, in order, with the failure named in the verdict.
func TestEveryGateOfTheRealTableRunsWhenAnyOneOfThemFails(t *testing.T) {
	order := tableOrder(t)
	_, gs := realTable(t)
	for i, g := range gs {
		t.Run(g.Name, func(t *testing.T) {
			w := newRealWorld(t)
			w.runner.replies[order[i]] = reply{stdout: "boom\n", code: 1, report: bashStage}
			if code := w.main(context.Background()); code != ExitFailed {
				t.Fatalf("exit %d", code)
			}
			if !slices.Equal(w.runner.log, order) {
				t.Fatalf("a failing %s disabled the gates after it: ran %d of %d", g.Name, len(w.runner.log), len(order))
			}
			verdict := w.out.String()
			if !strings.Contains(verdict, "✗ QA FAILED: ") || !strings.Contains(verdict, g.Name) {
				t.Fatalf("the verdict does not name %s:\n%s", g.Name, verdict)
			}
		})
	}
}
