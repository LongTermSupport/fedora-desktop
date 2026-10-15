package changed

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// A gate whose row reads a detail line (not a case count) is judged by that reader when its
// script is picked as a suite: it answers its own line, not "passed: <n>".
func TestADetailGateIsJudgedByItsOwnReaderWhenPickedAsASuite(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	writeFile(t, w.scripts, "qa-gates.tsv", "gate\tone\tscripts/test-one.bash\tone failed\tcount\ngate\ttwo\tscripts/test-two.bash\ttwo failed\tdetail:^done [0-9]+ files$\n")
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-two.bash")] = proc.Result{Stdout: "noise\ndone 7 files\n"}
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d, stdout %q stderr %q", code, w.out.String(), w.errOut.String())
	}
	if want := "✓ suite test-two.bash: done 7 files (names kept.bash)\n"; !strings.Contains(w.out.String(), want) {
		t.Fatalf("stdout lacks %q:\n%s", want, w.out.String())
	}
}

func TestADetailGateWhoseReaderFindsNothingCouldNotRun(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	writeFile(t, w.scripts, "qa-gates.tsv", "gate\tone\tscripts/test-one.bash\tone failed\tcount\ngate\ttwo\tscripts/test-two.bash\ttwo failed\tdetail:^done [0-9]+ files$\n")
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-two.bash")] = proc.Result{Stdout: "nothing relevant\n"}
	if code := w.main(); code != ExitCannotRun {
		t.Fatalf("a detail reader that finds nothing must not pass: exit %d", code)
	}
	if !strings.Contains(w.errOut.String(), "summary line was not found") {
		t.Fatalf("stderr must say why: %q", w.errOut.String())
	}
}

const threeReaders = "gate\tone\tscripts/test-one.bash\tone failed\tcount\n" +
	"gate\ttwo\tscripts/test-two.bash\ttwo failed\tdetail:^alpha [0-9]+$\tdetail:^beta [0-9]+$\tdetail:^gamma [0-9]+$\n"

// Review F1: qa-all requires every reader of a row, so qa-changed must too; the last reader
// alone used to decide.
func TestAGateWithSeveralReadersNeedsEveryOneWhenPickedAsASuite(t *testing.T) {
	for name, stdout := range map[string]string{"first missing": "beta 2\ngamma 3\n", "middle missing": "alpha 1\ngamma 3\n", "last missing": "alpha 1\nbeta 2\n"} {
		t.Run(name, func(t *testing.T) {
			w := newRunWorld(t)
			w.changeKept()
			writeFile(t, w.scripts, "qa-gates.tsv", threeReaders)
			w.runner.replies["bash "+filepath.Join(w.scripts, "test-two.bash")] = proc.Result{Stdout: stdout}
			if code := w.main(); code != ExitCannotRun {
				t.Fatalf("exit %d: a suite missing one reader line passed qa-changed and fails qa-all\n%s", code, w.out.String())
			}
			if !strings.Contains(w.errOut.String(), "summary line was not found") {
				t.Fatalf("stderr must say why: %q", w.errOut.String())
			}
		})
	}
}

func TestAGateWithSeveralReadersPassesWithAllOfThem(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	writeFile(t, w.scripts, "qa-gates.tsv", threeReaders)
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-two.bash")] = proc.Result{Stdout: "alpha 1\nbeta 2\ngamma 3\n"}
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.errOut.String())
	}
	if want := "✓ suite test-two.bash: alpha 1; beta 2; gamma 3 (names kept.bash)\n"; !strings.Contains(w.out.String(), want) {
		t.Fatalf("stdout lacks %q:\n%s", want, w.out.String())
	}
}

// A count and a detail reader on one row are both required.
func TestARowMixingCountAndDetailNeedsBoth(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	writeFile(t, w.scripts, "qa-gates.tsv", "gate\tone\tscripts/test-one.bash\tone failed\tcount\ngate\ttwo\tscripts/test-two.bash\ttwo failed\tcount\tdetail:^done [0-9]+$\n")
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-two.bash")] = proc.Result{Stdout: "done 7\n"}
	if code := w.main(); code != ExitCannotRun {
		t.Fatalf("a missing count line must not pass: exit %d", code)
	}
}

// Review F10: a drift row is judged by its own stage line, a helpers row cannot be judged from
// output at all; neither is held to "passed: <n>".
func TestADriftRowIsJudgedByItsOwnStageLine(t *testing.T) {
	table := "gate\tone\tscripts/test-one.bash\tone failed\tcount\ndrift\tdeployed\tscripts/test-two.bash\tdrifted\n"
	w := newRunWorld(t)
	w.changeKept()
	writeFile(t, w.scripts, "qa-gates.tsv", table)
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-two.bash")] = proc.Result{Stdout: "✓ deployed: 3 files match\n"}
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d: %s%s", code, w.out.String(), w.errOut.String())
	}
	if want := "✓ suite test-two.bash: 3 files match (names kept.bash)\n"; !strings.Contains(w.out.String(), want) {
		t.Fatalf("stdout lacks %q:\n%s", want, w.out.String())
	}
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-two.bash")] = proc.Result{Stdout: "nothing\n"}
	if code := w.main(); code != ExitCannotRun || !strings.Contains(w.errOut.String(), "printed no deployed stage line") {
		t.Fatalf("a drift gate without its stage line proved nothing: exit %d %q", code, w.errOut.String())
	}
}

func TestAHelpersRowCannotBeJudgedFromItsOutput(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	writeFile(t, w.scripts, "qa-gates.tsv", "gate\tone\tscripts/test-one.bash\tone failed\tcount\nhelpers\thelper-tests\tscripts/test-two.bash\tfailed\n")
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-two.bash")] = proc.Result{Stdout: "passed: 9\n"}
	if code := w.main(); code != ExitCannotRun {
		t.Fatalf("exit %d: its counts travel in a file, so output cannot vouch for it", code)
	}
}
