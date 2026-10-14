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
	if !strings.Contains(w.errOut.String(), "its detail pattern matched no line") {
		t.Fatalf("stderr must say why: %q", w.errOut.String())
	}
}
