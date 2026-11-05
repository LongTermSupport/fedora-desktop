package changed

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func passingReply() proc.Result { return proc.Result{Stdout: "passed: 1 failed: 0\n"} }

// The class: qa-changed ran a gate's script without the arguments its qa-gates.tsv row gives it,
// so a script that needs them printed usage and failed, while qa-all ran the row correctly.
func TestASuiteRunsWithTheArgumentsOfItsRow(t *testing.T) {
	w := newRunWorld(t)
	writeFile(t, w.scripts, "qa-gates.tsv", "gate\tone\tscripts/test-one.bash tree HEAD\tone failed\tcount\ngate\ttwo\tscripts/test-two.bash\ttwo failed\tcount\n")
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-one.bash")] = passingReply()
	w.changeKept()
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d, out %q", code, w.out.String())
	}
	base := "bash " + filepath.Join(w.scripts, "test-one.bash")
	if !slices.Contains(w.runner.argv(), base+" tree HEAD") {
		t.Fatalf("want a call %q, got %q", base+" tree HEAD", w.runner.argv())
	}
	if slices.Contains(w.runner.argv(), base) {
		t.Fatalf("the bare script ran without its row's arguments: %q", w.runner.argv())
	}
}

// A script with several rows is run once per row, each with its own arguments.
func TestAScriptWithSeveralRowsRunsOncePerRow(t *testing.T) {
	w := newRunWorld(t)
	writeFile(t, w.scripts, "qa-gates.tsv", "gate\tone\tscripts/test-one.bash a\tone failed\tcount\ngate\tuno\tscripts/test-one.bash b\tone failed\tcount\n")
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-one.bash")] = passingReply()
	w.changeKept()
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d, out %q", code, w.out.String())
	}
	base := "bash " + filepath.Join(w.scripts, "test-one.bash")
	for _, want := range []string{base + " a", base + " b"} {
		if !slices.Contains(w.runner.argv(), want) {
			t.Fatalf("want a call %q, got %q", want, w.runner.argv())
		}
	}
}

// Every row of the real table with arguments is run with them, by the real selection.
func TestEveryRealGateRowWithArgumentsIsRunWithThem(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	table, err := gates.Load(filepath.Join(root, "scripts", "qa-gates.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	w := newRunWorld(t)
	writeFile(t, w.scripts, "qa-gates.tsv", readReal(t, "qa-gates.tsv"))
	var withArgs []gates.Gate
	for _, g := range table {
		if name, ok := g.Script(); ok {
			if _, err := os.Stat(filepath.Join(w.scripts, name)); err != nil {
				writeFile(t, w.scripts, name, "#!/usr/bin/bash\nkept.bash\n")
			}
			if len(g.Argv) > 1 {
				withArgs = append(withArgs, g)
			}
		}
	}
	if len(withArgs) == 0 {
		t.Fatal("found no row with arguments: the test proves nothing")
	}
	writeFile(t, w.scripts, "go-binary-suites.tsv", "frz\tqa-go\n")
	w.changeKept()
	w.main()
	for _, g := range withArgs {
		want := strings.Replace(strings.Join(g.Command(root), " "), filepath.Join(root, "scripts"), w.scripts, 1)
		if !slices.Contains(w.runner.argv(), want) {
			t.Errorf("row %s: want a call %q", g.Name, want)
		}
	}
}
