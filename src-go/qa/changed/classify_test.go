package changed

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// realScripts is the repository's own scripts directory, so the tests run the real tables.
const realScripts = "../../../scripts"

func readReal(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(realScripts, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// world is a scratch repository root whose scripts directory carries the real routing and
// tree tables, an allowlist naming one extensionless tool, and whatever else a test writes.
type world struct {
	t      *testing.T
	root   string
	runner *scriptedRunner
}

func newWorld(t *testing.T) *world {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "scripts/qa-routing.tsv", readReal(t, "qa-routing.tsv"))
	writeFile(t, root, "scripts/qa-trees.tsv", readReal(t, "qa-trees.tsv"))
	writeFile(t, root, "scripts/bash-limits.allowlist", "# header\nfiles/home/.local/bin/listed-tool\t10\t1\t10\t-\n")
	return &world{t: t, root: root, runner: &scriptedRunner{replies: map[string]proc.Result{}}}
}

// touch writes a file whose first line is the given text.
func (w *world) touch(rel, first string) {
	w.t.Helper()
	writeFile(w.t, w.root, rel, first+"\n:\n")
}

func (w *world) session() *session {
	w.t.Helper()
	s := &session{
		env:     Env{Runner: w.runner, Stderr: &strings.Builder{}, Stdout: &strings.Builder{}},
		root:    w.root,
		scripts: filepath.Join(w.root, "scripts"),
		opts:    options{base: "HEAD"},
	}
	if err := s.load(); err != nil {
		w.t.Fatal(err)
	}
	return s
}

// classify classifies the files (existing ones must have been touched) and returns the plan.
func (w *world) classify(existing []string, deleted ...string) *plan {
	w.t.Helper()
	s := w.session()
	if err := s.classify(context.Background(), collected{existing: existing, deleted: deleted}); err != nil {
		w.t.Fatal(err)
	}
	s.settleUncovered()
	return s.plan
}

func (p *plan) items(id string) []string {
	if cp := p.checks[id]; cp != nil {
		return cp.items
	}
	return nil
}

func (p *plan) has(id string) bool { return p.checks[id] != nil }

func TestShellFileGetsTheShellChecks(t *testing.T) {
	w := newWorld(t)
	w.touch("scripts/qa-bash.bash", "#!/usr/bin/bash")
	p := w.classify([]string{"scripts/qa-bash.bash"})
	for _, id := range []string{"bash-syntax", "shellcheck", "semgrep"} {
		if !reflect.DeepEqual(p.items(id), []string{"scripts/qa-bash.bash"}) {
			t.Errorf("%s: %v", id, p.items(id))
		}
	}
	if !p.has("toolchain") || !p.has("bash-limits") || p.has("ruff") || p.has("docs") || p.has("go") {
		t.Fatalf("checks: %v", keys(p))
	}
	if len(p.uncovered) != 0 {
		t.Fatalf("uncovered: %v", p.uncovered)
	}
}

func keys(p *plan) []string {
	out := make([]string, 0, len(p.checks))
	for id := range p.checks {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

func TestExtensionlessShebangIsShellOrPythonByContent(t *testing.T) {
	w := newWorld(t)
	w.touch("files/home/.local/bin/tmate-share", "#!/usr/bin/bash")
	w.touch("files/home/.local/bin/clip-scan", "#!/usr/bin/env python3")
	p := w.classify([]string{"files/home/.local/bin/tmate-share"})
	if !p.has("shellcheck") || !p.has("bash-limits") {
		t.Fatalf("a bash tool off the allowlist: %v", keys(p))
	}
	p = w.classify([]string{"files/home/.local/bin/clip-scan"})
	if !p.has("ruff") || p.has("bash-limits") || p.has("shellcheck") || !p.has("toolchain") {
		t.Fatalf("a python tool: %v", keys(p))
	}
}

func TestAllowlistedPathRunsTheBashLimitsGateWhateverItIs(t *testing.T) {
	w := newWorld(t)
	w.touch("files/home/.local/bin/listed-tool", "#!/usr/bin/env python3")
	p := w.classify([]string{"files/home/.local/bin/listed-tool"})
	if !p.has("bash-limits") {
		t.Fatalf("a listed path: %v", keys(p))
	}
	writeFile(t, w.root, "scripts/bash-limits.allowlist", "# x\n")
	p = w.classify([]string{"scripts/bash-limits.allowlist"})
	if !p.has("bash-limits") || len(p.uncovered) != 0 {
		t.Fatalf("the allowlist itself: %v %v", keys(p), p.uncovered)
	}
}

func TestPythonGetsRuffAndItsMirroredUnitTest(t *testing.T) {
	w := newWorld(t)
	w.touch("helpers/gnome/check_panel_contract.py", "# x")
	w.touch("tests/helpers/gnome/test_check_panel_contract.py", "# x")
	w.touch("helpers/gnome/lonely.py", "# x")
	w.touch("helpers/gnome/__init__.py", "# x")
	w.touch("helpers/top.py", "# x")
	w.touch("tests/helpers/test_top.py", "# x")
	w.touch("tools/other.py", "# x")
	p := w.classify([]string{
		"helpers/gnome/check_panel_contract.py", "tests/helpers/gnome/test_check_panel_contract.py",
		"helpers/gnome/lonely.py", "helpers/gnome/__init__.py", "helpers/top.py", "tools/other.py",
	})
	wantMods := []string{"tests.helpers.gnome.test_check_panel_contract", "tests.helpers.test_top"}
	if !reflect.DeepEqual(p.items("unit-tests"), wantMods) {
		t.Fatalf("modules (de-duplicated, first seen): %v", p.items("unit-tests"))
	}
	if len(p.items("ruff")) != 6 || p.has("shellcheck") || !p.has("toolchain") {
		t.Fatalf("ruff %v / %v", p.items("ruff"), keys(p))
	}
	if !reflect.DeepEqual(p.notes, []string{"helpers/gnome/lonely.py: no mirrored unit test (tests/helpers/gnome/test_lonely.py)"}) {
		t.Fatalf("notes: %v", p.notes)
	}
}

func TestPlaybookAndTaskAndVarsYAML(t *testing.T) {
	w := newWorld(t)
	w.touch("playbooks/imports/play-x.yml", "- hosts: all")
	w.touch("untracked/pb.yml", "- hosts: all")
	p := w.classify([]string{"playbooks/imports/play-x.yml"})
	if !reflect.DeepEqual(p.items("ansible-syntax"), []string{"playbooks/imports/play-x.yml"}) || !p.has("ansible-patterns") || !p.has("docs") {
		t.Fatalf("a playbook: %v", keys(p))
	}
	p = w.classify([]string{"untracked/pb.yml"})
	if !reflect.DeepEqual(p.skipped, []string{"untracked/pb.yml"}) || len(p.uncovered) != 0 || len(p.checks) != 0 {
		t.Fatalf("an excluded yml: %v", p.skipped)
	}
}

func TestTaskAndVarsYAMLMapToThePlaybooksThatNameThem(t *testing.T) {
	w := newWorld(t)
	w.touch("playbooks/imports/play-x.yml", "- hosts: all")
	w.touch("tasks/do-it.yml", "- name: step")
	w.touch("vars/unnamed.yml", "k: v")
	w.touch("other/thing.yml", "k: v")
	w.touch("untracked/pb.yml", "- hosts: all")
	w.touch("playbooks/imports/play-y.yml", "- hosts: all\n  tasks:\n    - include_tasks: ../../tasks/do-it.yml")
	w.runner.replies["git ls-files -z -- *.yml *.yaml"] = proc.Result{Stdout: "playbooks/imports/play-x.yml\x00playbooks/imports/play-y.yml\x00tasks/do-it.yml\x00untracked/pb.yml\x00"}
	p := w.classify([]string{"tasks/do-it.yml"})
	if !reflect.DeepEqual(p.items("ansible-syntax"), []string{"playbooks/imports/play-y.yml"}) || !p.has("ansible-patterns") || p.has("docs") {
		t.Fatalf("a task file: %v %v", p.items("ansible-syntax"), keys(p))
	}
	p = w.classify([]string{"vars/unnamed.yml"})
	if p.has("ansible-syntax") || !p.has("ansible-patterns") || len(p.uncovered) != 0 {
		t.Fatalf("an unnamed vars file: %v", keys(p))
	}
	if !reflect.DeepEqual(p.notes, []string{"vars/unnamed.yml: no playbook names 'unnamed.yml', so no ansible-syntax target (patterns gate only)"}) {
		t.Fatalf("notes: %v", p.notes)
	}
	p = w.classify([]string{"other/thing.yml"})
	if len(p.checks) != 0 || !reflect.DeepEqual(p.uncovered, []string{"other/thing.yml"}) {
		t.Fatalf("an unmapped yml is named, not dropped: %v %v", keys(p), p.uncovered)
	}
}

func TestMarkdownAndJavaScript(t *testing.T) {
	w := newWorld(t)
	w.touch("CLAUDE/QA.md", "# x")
	w.touch("extensions/speech@x/extension.js", "// x")
	w.touch("tools/plain.mjs", "// x")
	w.touch("extensions/node_modules/m/i.js", "// x")
	p := w.classify([]string{"CLAUDE/QA.md"})
	if !p.has("docs") || p.has("ansible-patterns") || len(p.uncovered) != 0 {
		t.Fatalf("markdown: %v", keys(p))
	}
	p = w.classify([]string{"extensions/speech@x/extension.js", "tools/plain.mjs"})
	if !reflect.DeepEqual(p.items("node-check"), []string{"extensions/speech@x/extension.js", "tools/plain.mjs"}) {
		t.Fatalf("node-check: %v", p.items("node-check"))
	}
	if !reflect.DeepEqual(p.items("eslint"), []string{"extensions/speech@x/extension.js"}) || p.checks["eslint"].rel != "extensions" {
		t.Fatalf("eslint only under extensions: %v", p.items("eslint"))
	}
	p = w.classify([]string{"extensions/node_modules/m/i.js"})
	if p.has("node-check") || p.has("eslint") || !reflect.DeepEqual(p.skipped, []string{"extensions/node_modules/m/i.js"}) {
		t.Fatalf("an excluded js: %v %v", keys(p), p.skipped)
	}
}

func TestSrcGoRoutingIncludingTheLintConfigAndTheNestedModule(t *testing.T) {
	w := newWorld(t)
	for _, f := range []string{"src-go/internal/ui/ui.go", "src-go/go.mod", "src-go/.golangci.yml", "src-go/qa/rules/seamexec.go", "src-go/qa/go.mod", "src-go/notes.md"} {
		w.touch(f, "// x")
	}
	p := w.classify([]string{"src-go/internal/ui/ui.go", "src-go/go.mod", "src-go/.golangci.yml", "src-go/qa/rules/seamexec.go", "src-go/qa/go.mod"})
	if !p.has("go") || len(p.uncovered) != 0 || p.has("bash-limits") || p.has("ansible-patterns") {
		t.Fatalf("checks %v uncovered %v", keys(p), p.uncovered)
	}
	if len(p.goFiles) != 5 {
		t.Fatalf("the closure list: %v", p.goFiles)
	}
	p = w.classify([]string{"src-go/notes.md"})
	if p.has("go") || !p.has("docs") {
		t.Fatalf("markdown under src-go is docs: %v", keys(p))
	}
	w.touch("src-go/qa/bashlimits/measure.go", "// x")
	w.touch("src-go/qa/cmd/qa-bash-limits/main.go", "// x")
	w.touch("src-go/qa/internal/proc/proc.go", "// x")
	for _, f := range []string{"src-go/qa/bashlimits/measure.go", "src-go/qa/cmd/qa-bash-limits/main.go", "src-go/qa/internal/proc/proc.go"} {
		if p := w.classify([]string{f}); !p.has("bash-limits") || !p.has("go") {
			t.Errorf("%s: %v", f, keys(p))
		}
	}
	w.touch("src-go/internal/ui/ui_test.go", "// x")
	if p := w.classify([]string{"src-go/internal/ui/ui_test.go"}); p.has("bash-limits") {
		t.Fatalf("an unrelated Go file: %v", keys(p))
	}
}

func TestParityReferencesRunTheGoGateAndStillClassify(t *testing.T) {
	w := newWorld(t)
	w.touch("files/var/local/claude-yolo/lib/common-pure.bash", "#!/usr/bin/bash")
	w.touch("helpers/agent_bus/declare.py", "# x")
	p := w.classify([]string{"files/var/local/claude-yolo/lib/common-pure.bash"})
	if !p.has("go") || !p.has("shellcheck") {
		t.Fatalf("shell parity reference: %v", keys(p))
	}
	p = w.classify([]string{"helpers/agent_bus/declare.py"})
	if !p.has("go") || !p.has("ruff") {
		t.Fatalf("python parity reference: %v", keys(p))
	}
}

func TestTemplatesRunTheCheapGateButAreNotCovered(t *testing.T) {
	w := newWorld(t)
	w.touch("files/home/x.j2", "#!/usr/bin/bash")
	p := w.classify([]string{"files/home/x.j2"})
	if !p.has("bash-limits") || p.has("shellcheck") || p.has("ruff") || p.covered["files/home/x.j2"] {
		t.Fatalf("a template: %v", keys(p))
	}
}

func TestExcludedShellIsSkippedButStillAsksTheBashLimitsGateLikeBefore(t *testing.T) {
	w := newWorld(t)
	w.touch("untracked/x.bash", "#!/usr/bin/bash")
	p := w.classify([]string{"untracked/x.bash"})
	if !reflect.DeepEqual(p.skipped, []string{"untracked/x.bash"}) || p.has("shellcheck") || p.has("toolchain") || len(p.uncovered) != 0 {
		t.Fatalf("skipped %v checks %v", p.skipped, keys(p))
	}
}

func TestDeletedFilesOweOnlyTheWholeGates(t *testing.T) {
	w := newWorld(t)
	w.touch("scripts/keep.bash", "#!/usr/bin/bash")
	p := w.classify(nil, "scripts/gone.bash", "src-go/internal/old/old.go", "docs/gone.md", "playbooks/old.yml", "scripts/gone.py", "files/home/.local/bin/listed-tool", "untracked/gone.js")
	if !p.has("bash-limits") || !p.has("go") || !p.has("docs") {
		t.Fatalf("whole gates: %v", keys(p))
	}
	for _, id := range []string{"bash-syntax", "shellcheck", "semgrep", "ruff", "toolchain", "ansible-syntax", "ansible-patterns"} {
		if p.has(id) {
			t.Errorf("a deleted file has nothing for %s", id)
		}
	}
	if !reflect.DeepEqual(p.goFiles, []string{"src-go/internal/old/old.go"}) {
		t.Fatalf("a deleted Go file is in the closure list: %v", p.goFiles)
	}
	if !p.covered["src-go/internal/old/old.go"] || !p.covered["docs/gone.md"] {
		t.Fatalf("covered: %v", p.covered)
	}
	want := []string{"playbooks/old.yml", "scripts/gone.py"}
	slices.Sort(want)
	got := slices.Clone(p.uncovered)
	slices.Sort(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a deleted file nothing covers is named: %v", got)
	}
	if !reflect.DeepEqual(p.skipped, []string{"untracked/gone.js"}) {
		t.Fatalf("skipped: %v", p.skipped)
	}
}

func TestUnreadableFileStopsClassification(t *testing.T) {
	w := newWorld(t)
	s := w.session()
	if err := s.classify(context.Background(), collected{existing: []string{"no-such-file"}}); err == nil {
		t.Fatal("a file that cannot be opened must not be classified as 'not a script'")
	}
}

func TestUnmatchedFileIsUncovered(t *testing.T) {
	w := newWorld(t)
	w.touch(".github/workflows/ci.txt", "name: x")
	p := w.classify([]string{".github/workflows/ci.txt"})
	if !reflect.DeepEqual(p.uncovered, []string{".github/workflows/ci.txt"}) {
		t.Fatalf("uncovered: %v", p.uncovered)
	}
}

func TestPlaybookListFailureIsAnError(t *testing.T) {
	w := newWorld(t)
	w.touch("tasks/t.yml", "- name: x")
	w.runner.replies["git ls-files -z -- *.yml *.yaml"] = proc.Result{Stderr: "fatal", Code: 128}
	s := w.session()
	if err := s.classify(context.Background(), collected{existing: []string{"tasks/t.yml"}}); err == nil {
		t.Fatal("a failed playbook listing must stop the run")
	}
}
