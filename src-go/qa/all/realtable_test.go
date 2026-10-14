package all

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
)

// realWording is what each detail gate actually prints, one sample per pattern. The runner
// reads these patterns at run time against the gate's real output and fails the gate if they
// stop agreeing (bug B3); this test keeps the samples honest against the table, so a pattern
// changed without its gate being looked at fails here before it fails a full run.
var realWording = map[string][]string{
	"toolchain":             {"TOOLCHAIN-OK 6 tools match .qa-versions"},
	"go":                    {"GO-OK 39 package(s), all tested: fmt, vet, test -race"},
	"bash-limits":           {"BASH-LIMITS-OK 446 bash files: 109 allowlisted"},
	"nokill-containerwatch": {"✓ no-kill gate: 3 container-watch file(s) clean — reporting-only confirmed"},
	"planlib-tests":         {"test-planlib: PASSED (library version 1.2.0)"},
	"ssh-suspend-guard":     {"VERDICT: PASS"},
	"extension-compat":      {"All 3 extension(s) cover the GNOME Shell that Fedora 44 ships"},
	"panel-contract":        {"7 constant(s) agree between the panel and the producer"},
	"plan-script-logging":   {"12 plan script(s) examined, no offences"},
	"version-pins":          {"vars/version-pins.yml: VERSION-PINS-OK 20 rows"},
	"vmtest-manifest": {
		"vars/vm-test-scenarios.yml: scenarios=8 runnable=8 bridge=6 host_only=2 bases=2",
		"8 scenario(s) agree with their guest checker",
		"3 scenario fixture(s) run before a declared reboot",
	},
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

func TestEverySampleOfARealGatesWordingMatchesItsPatternAndNoPatternIsUnsampled(t *testing.T) {
	_, gs := realTable(t)
	for _, g := range gs {
		samples, sampled := realWording[g.Name]
		var patterns []string
		for _, r := range g.Readers {
			if r.Detail != nil {
				patterns = append(patterns, r.Detail.String())
				if !slices.ContainsFunc(samples, r.Detail.MatchString) {
					t.Errorf("gate %s: no sample matches %s", g.Name, r.Detail)
				}
			}
		}
		if len(patterns) > 0 && !sampled {
			t.Errorf("gate %s has detail patterns %v and no sample", g.Name, patterns)
		}
		if len(patterns) == 0 && sampled {
			t.Errorf("gate %s has a sample but no detail pattern", g.Name)
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
			if samples, ok := realWording[g.Name]; ok {
				w.runner.replies[key] = reply{stdout: strings.Join(samples, "\n") + "\n"}
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
