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
	for _, id := range []string{"bash-syntax", "shellcheck", "shell-directives", "semgrep"} {
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

func TestThePatternsGateRunsWhenItsRulesFixtureOrToolChange(t *testing.T) {
	w := newWorld(t)
	for _, f := range []string{".semgrep/bash-conventions.yml", ".semgrep/bash-conventions.bash", "src-go/qa/patterns/run.go", "src-go/qa/cmd/qa-patterns/main.go"} {
		w.touch(f, "// x")
		p := w.classify([]string{f})
		if !p.has("patterns") || len(p.uncovered) != 0 || len(p.skipped) != 0 {
			t.Errorf("%s: checks %v uncovered %v skipped %v", f, keys(p), p.uncovered, p.skipped)
		}
	}
	// The wrapper, the library it runs and the table it reads change what the gate does.
	for _, f := range []string{"scripts/qa-patterns.bash", "scripts/lib/go-tool.bash", "scripts/qa-trees.tsv"} {
		if f != "scripts/qa-trees.tsv" {
			w.touch(f, "#!/usr/bin/bash")
		}
		if p := w.classify([]string{f}); !p.has("patterns") {
			t.Errorf("%s: checks %v", f, keys(p))
		}
	}
	w.touch(".semgrep/gh-scopes.yml", "# x")
	if p := w.classify([]string{".semgrep/gh-scopes.yml"}); p.has("patterns") {
		t.Fatalf("another rule file: %v", keys(p))
	}
}

func TestTheShellGateRunsWhenItsToolDiscoveryWrapperOrTableChange(t *testing.T) {
	w := newWorld(t)
	for _, f := range []string{"src-go/qa/bash/run.go", "src-go/qa/cmd/qa-bash/main.go", "src-go/qa/discovery/list.go"} {
		w.touch(f, "// x")
		p := w.classify([]string{f})
		if !p.has("bash") || len(p.uncovered) != 0 || len(p.skipped) != 0 {
			t.Errorf("%s: checks %v uncovered %v skipped %v", f, keys(p), p.uncovered, p.skipped)
		}
	}
	for _, f := range []string{"scripts/qa-bash.bash", "scripts/lib/go-tool.bash", "scripts/qa-trees.tsv"} {
		if f != "scripts/qa-trees.tsv" {
			w.touch(f, "#!/usr/bin/bash")
		}
		if p := w.classify([]string{f}); !p.has("bash") {
			t.Errorf("%s: checks %v", f, keys(p))
		}
	}
	w.touch("scripts/other.bash", "#!/usr/bin/bash")
	if p := w.classify([]string{"scripts/other.bash"}); p.has("bash") {
		t.Fatalf("an ordinary shell script runs the whole shell gate: %v", keys(p))
	}
}

func TestThePythonGateRunsWhenItsToolPinConfigOrTableChange(t *testing.T) {
	w := newWorld(t)
	for _, f := range []string{"src-go/qa/python/run.go", "src-go/qa/cmd/qa-python/main.go", "src-go/qa/discovery/list.go", "ruff.toml"} {
		w.touch(f, "// x")
		p := w.classify([]string{f})
		if !p.has("python") || len(p.uncovered) != 0 || len(p.skipped) != 0 {
			t.Errorf("%s: checks %v uncovered %v skipped %v", f, keys(p), p.uncovered, p.skipped)
		}
	}
	// The wrapper, the library it runs, the pin and the table it reads change what the gate does.
	for _, f := range []string{"scripts/qa-python.bash", "scripts/lib/go-tool.bash", "scripts/qa-trees.tsv", ".qa-versions"} {
		switch f {
		case "scripts/qa-python.bash", "scripts/lib/go-tool.bash":
			w.touch(f, "#!/usr/bin/bash")
		case ".qa-versions":
			w.touch(f, "RUFF=1\n")
		}
		if p := w.classify([]string{f}); !p.has("python") {
			t.Errorf("%s: checks %v", f, keys(p))
		}
	}
	w.touch("scripts/other.py", "# x")
	if p := w.classify([]string{"scripts/other.py"}); p.has("python") {
		t.Fatalf("an ordinary Python file runs the whole gate: %v", keys(p))
	}
}

// J: the js gate runs whole when its tool, its wrapper, the shared discovery, the lint config or
// the table it reads change; an ordinary extension file is still only parse-checked and linted.
func TestTheJSGateRunsWhenItsToolWrapperConfigOrTableChange(t *testing.T) {
	w := newWorld(t)
	for _, f := range []string{"src-go/qa/js/run.go", "src-go/qa/cmd/qa-js/main.go", "src-go/qa/discovery/list.go", "extensions/.eslintrc.json"} {
		w.touch(f, "// x")
		p := w.classify([]string{f})
		if !p.has("js") || len(p.uncovered) != 0 || len(p.skipped) != 0 {
			t.Errorf("%s: checks %v uncovered %v skipped %v", f, keys(p), p.uncovered, p.skipped)
		}
	}
	for _, f := range []string{"scripts/qa-js.bash", "scripts/lib/go-tool.bash", "scripts/qa-trees.tsv"} {
		if f != "scripts/qa-trees.tsv" {
			w.touch(f, "#!/usr/bin/bash")
		}
		if p := w.classify([]string{f}); !p.has("js") {
			t.Errorf("%s: checks %v", f, keys(p))
		}
	}
	w.touch("extensions/ext@x/extension.js", "// x")
	if p := w.classify([]string{"extensions/ext@x/extension.js"}); p.has("js") || !p.has("eslint") {
		t.Fatalf("an ordinary extension file runs only the per-file checks: %v", keys(p))
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

// A Python template is compiled by the python gate, so an edit to one runs it; another template
// still only asks the cheap gate.
func TestAPythonTemplateRunsThePythonGate(t *testing.T) {
	w := newWorld(t)
	w.touch("files/home/tool.j2", "#!/usr/bin/env python3")
	p := w.classify([]string{"files/home/tool.j2"})
	if !p.has("python") || p.has("ruff") || p.covered["files/home/tool.j2"] {
		t.Fatalf("a Python template: %v", keys(p))
	}
	w.touch("files/home/other.j2", "#!/usr/bin/bash")
	if p := w.classify([]string{"files/home/other.j2"}); p.has("python") {
		t.Fatalf("a shell template runs the python gate: %v", keys(p))
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
	p := w.classify(nil, "scripts/gone.bash", "src-go/internal/old/old.go", "docs/gone.md", "playbooks/old.yml", "scripts/gone.py", "files/home/.local/bin/listed-tool", "untracked/gone.js", ".semgrep/gh-scopes.fixture", ".qa-versions")
	if !p.has("bash-limits") || !p.has("go") || !p.has("docs") || !p.has("gh-scopes") || !p.has("toolchain") {
		t.Fatalf("whole gates: %v", keys(p))
	}
	for _, id := range []string{"bash-syntax", "shellcheck", "shell-directives", "semgrep", "ruff", "ansible-syntax", "ansible-patterns"} {
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
	if !p.has("ansible-pause") {
		t.Fatalf("a deleted playbook runs the pause gate over what remains: %v", keys(p))
	}
	want := []string{"scripts/gone.py"}
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

// A .semgrep/ file that no gate reads is reported uncovered, not called covered and not hidden
// as an excluded tree (SPEC-qa-changed B33).
func TestASemgrepFileNothingReadsIsUncovered(t *testing.T) {
	w := newWorld(t)
	w.touch(".semgrep/unread.yml", "rules: []")
	w.touch(".semgrep/unread.py", "# x")
	for _, f := range []string{".semgrep/unread.yml", ".semgrep/unread.py"} {
		p := w.classify([]string{f})
		if !reflect.DeepEqual(p.uncovered, []string{f}) || len(p.skipped) != 0 || len(keys(p)) != 0 {
			t.Errorf("%s: uncovered %v skipped %v checks %v", f, p.uncovered, p.skipped, keys(p))
		}
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

// Review nit 8: the whole ansible-syntax and deployed-drift gates rerun when their Go tool, their
// wrapper or (for the first) the shared discovery change, as the other ports' gates do.
func TestAnEditToAGateWithNoPerFileCheckRunsItWhole(t *testing.T) {
	for check, files := range map[string][]string{
		"ansible-syntax-gate": {
			"src-go/qa/ansiblesyntax/run.go", "src-go/qa/cmd/qa-ansible-syntax/main.go", "scripts/qa-ansible-syntax.bash",
			"scripts/lib/go-tool.bash", "scripts/qa-trees.tsv", "src-go/qa/discovery/run.go",
		},
		"deployed-drift": {"src-go/qa/deployeddrift/run.go", "src-go/qa/cmd/qa-deployed-drift/main.go", "scripts/qa-deployed-drift.bash"},
	} {
		for _, f := range files {
			w := newWorld(t)
			if f != "scripts/qa-trees.tsv" { // the world already holds the real table, which must stay valid
				w.touch(f, "// x")
			}
			if p := w.classify([]string{f}); !p.has(check) || len(p.uncovered) != 0 {
				t.Errorf("%s does not run %s: %v uncovered %v", f, check, keys(p), p.uncovered)
			}
		}
	}
	w := newWorld(t)
	w.touch("src-go/qa/bash/run.go", "// x")
	if p := w.classify([]string{"src-go/qa/bash/run.go"}); p.has("ansible-syntax-gate") || p.has("deployed-drift") {
		t.Errorf("an unrelated port runs the wrong gate: %v", keys(p))
	}
}

// An edit to a playbook or task file, to the pause gate's own sources or to its fixtures runs the
// whole pause gate, and the fixtures count as covered.
func TestAPauseGateEditRunsTheGate(t *testing.T) {
	for _, f := range []string{
		"playbooks/imports/play-x.yml", "tasks/deep/t.yml", "tasks/t.yaml",
		"src-go/qa/ansiblepause/scan.go", "src-go/qa/cmd/qa-ansible-pause/main.go", "scripts/qa-ansible-pause.bash",
	} {
		w := newWorld(t)
		w.touch(f, "- name: x")
		p := w.classify([]string{f})
		if !p.has("ansible-pause") || len(p.uncovered) != 0 {
			t.Errorf("%s: %v uncovered %v", f, keys(p), p.uncovered)
		}
	}
	// The fixtures are read by the Go tests of the rule (E17), so they run the go check, which
	// covers them; the gate itself never reads them.
	for _, f := range []string{"tests/fixtures/ansible-pause/flagged.yml", "tests/fixtures/ansible-pause/clean.yml"} {
		w := newWorld(t)
		w.touch(f, "- name: x")
		p := w.classify([]string{f})
		if !p.has("go") || p.has("ansible-pause") || len(p.uncovered) != 0 {
			t.Errorf("%s: %v uncovered %v", f, keys(p), p.uncovered)
		}
	}
	w := newWorld(t)
	w.touch("vars/v.yml", "a: 1")
	if p := w.classify([]string{"vars/v.yml"}); p.has("ansible-pause") {
		t.Errorf("a vars file must not run the pause gate: %v", keys(p))
	}
}

// An edit to the pins, the Go floor, the toolchain gate's wrapper, Go package or command, or the
// wrapper library runs the whole toolchain gate; .qa-versions counts as covered by it. A deleted
// pin file still owes the gate (it is a whole check), and a vars file does not run it.
func TestAToolchainInputEditRunsTheGate(t *testing.T) {
	for _, f := range []string{
		".qa-versions", "src-go/go.mod", "src-go/qa/toolchain/versions.go", "src-go/qa/cmd/qa-toolchain/main.go",
		"scripts/qa-toolchain.bash", "scripts/lib/go-tool.bash",
	} {
		w := newWorld(t)
		w.touch(f, "RUFF=0.16.8")
		p := w.classify([]string{f})
		if !p.has("toolchain") || len(p.uncovered) != 0 {
			t.Errorf("%s: %v uncovered %v", f, keys(p), p.uncovered)
		}
	}
	w := newWorld(t)
	if p := w.classify(nil, ".qa-versions"); !p.has("toolchain") || len(p.uncovered) != 0 {
		t.Errorf("a deleted .qa-versions: %v uncovered %v", keys(p), p.uncovered)
	}
	w = newWorld(t)
	w.touch("vars/v.yml", "a: 1")
	if p := w.classify([]string{"vars/v.yml"}); p.has("toolchain") {
		t.Errorf("a vars file must not run the toolchain gate: %v", keys(p))
	}
}

// A changed Python or shell file (by name or by shebang) runs the ready-wait gate over that file
// only; other files do not, and a deleted file has nothing to judge.
func TestAChangedSourceFileRunsTheReadyWaitGateOnThatFile(t *testing.T) {
	w := newWorld(t)
	w.touch("helpers/one.py", "x = 1")
	w.touch("scripts/two.bash", "#!/usr/bin/bash")
	w.touch("files/home/.local/bin/py-tool", "#!/usr/bin/env python3")
	w.touch("files/home/.local/bin/sh-tool", "#!/usr/bin/bash")
	for _, f := range []string{"helpers/one.py", "scripts/two.bash", "files/home/.local/bin/py-tool", "files/home/.local/bin/sh-tool"} {
		p := w.classify([]string{f})
		if got := p.items("ready-wait-files"); !reflect.DeepEqual(got, []string{f}) {
			t.Errorf("%s: ready-wait-files items %v", f, got)
		}
		if p.has("ready-wait") {
			t.Errorf("%s ran the whole ready-wait gate: %v", f, keys(p))
		}
	}
	p := w.classify([]string{"helpers/one.py", "scripts/two.bash"})
	if got := p.items("ready-wait-files"); !reflect.DeepEqual(got, []string{"helpers/one.py", "scripts/two.bash"}) {
		t.Errorf("both files: %v", got)
	}
	w.touch("docs/a.md", "# a")
	if p = w.classify([]string{"docs/a.md"}); p.has("ready-wait-files") {
		t.Errorf("a markdown file ran the ready-wait gate: %v", keys(p))
	}
	if p = w.classify(nil, "helpers/gone.py", "scripts/gone.bash"); p.has("ready-wait-files") {
		t.Errorf("a deleted file has nothing for ready-wait-files: %v", keys(p))
	}
}

// An edit to the ready-wait rule, its fixtures, its script, Go package or command runs the whole
// ready-wait gate, the .semgrep files count as covered, and the script is not also picked as a
// suite. An ordinary Python or shell file does not run it (the gate scans every tracked file).
func TestAReadyWaitGateEditRunsTheGate(t *testing.T) {
	for _, f := range []string{
		".semgrep/ready-wait.yml", ".semgrep/ready-wait.py", ".semgrep/ready-wait.bash",
		"src-go/qa/readywait/rule.go", "src-go/qa/cmd/qa-ready-wait-rules/main.go", "scripts/qa-ready-wait-rules.bash",
	} {
		w := newWorld(t)
		w.touch(f, "x")
		p := w.classify([]string{f})
		if !p.has("ready-wait") || len(p.uncovered) != 0 {
			t.Errorf("%s: %v uncovered %v", f, keys(p), p.uncovered)
		}
		if f == "scripts/qa-ready-wait-rules.bash" || f == ".semgrep/ready-wait.yml" {
			if len(p.suites) != 0 {
				t.Errorf("%s was also picked as a suite: %+v", f, p.suites)
			}
		}
	}
	w := newWorld(t)
	w.touch("helpers/other.py", "x = 1\n")
	if p := w.classify([]string{"helpers/other.py"}); p.has("ready-wait") {
		t.Errorf("an ordinary Python file must not run the ready-wait gate: %v", keys(p))
	}
}

// A change to an active plan's shell script or to a file under a plan's logs/ directory, or to the
// plan-script-logging gate's own script, Go package, command or the shared discovery, runs the whole
// gate; so does the deletion of such a script (the gate judges what remains). The glob is
// deliberately wider than the gate's scope, so a shell file under Completed/ or the plan tooling
// runs it too (pinned below). A plan's Markdown file, a playbook and an ordinary Python file do not.
func TestAPlanScriptLoggingEditRunsTheGate(t *testing.T) {
	for _, f := range []string{
		"CLAUDE/Plan/00130-x/run.bash", "CLAUDE/Plan/00130-x/sub/deep.sh", "CLAUDE/Plan/00130-x/logs/run.txt",
		"scripts/qa-plan-script-logging.bash", "src-go/qa/planscriptlogging/scan.go",
		"src-go/qa/cmd/qa-plan-script-logging/main.go", "src-go/qa/discovery/shebang.go",
	} {
		w := newWorld(t)
		w.touch(f, "#!/usr/bin/bash\n")
		p := w.classify([]string{f})
		if !p.has("plan-script-logging") || len(p.uncovered) != 0 {
			t.Errorf("%s: %v uncovered %v", f, keys(p), p.uncovered)
		}
		if f == "scripts/qa-plan-script-logging.bash" && len(p.suites) != 0 {
			t.Errorf("%s was also picked as a suite: %+v", f, p.suites)
		}
	}
	for _, f := range []string{"CLAUDE/Plan/Completed/00099-x/run.bash", "CLAUDE/Plan/_planlib.inc.bash", "CLAUDE/Plan/mkplan.bash"} {
		w := newWorld(t)
		w.touch(f, "#!/usr/bin/bash\n")
		if p := w.classify([]string{f}); !p.has("plan-script-logging") {
			t.Errorf("%s: the over-match is pinned, it must run the gate: %v", f, keys(p))
		}
	}
	w := newWorld(t)
	if p := w.classify(nil, "CLAUDE/Plan/00130-x/gone.bash"); !p.has("plan-script-logging") {
		t.Errorf("a deleted plan script owes the gate: %v", keys(p))
	}
	for _, f := range []string{"CLAUDE/Plan/00130-x/PLAN.md", "playbooks/imports/play-x.yml", "helpers/other.py"} {
		w = newWorld(t)
		w.touch(f, "x")
		if p := w.classify([]string{f}); p.has("plan-script-logging") {
			t.Errorf("%s must not run the plan-script-logging gate: %v", f, keys(p))
		}
	}
}

// Review F9: an edit to the container-watch watchdog or its two panel files runs the no-kill gate
// before the commit, not only at the push; a deletion does too (the gate judges what remains).
func TestAContainerWatchEditRunsTheNoKillGate(t *testing.T) {
	for _, f := range []string{
		"helpers/containerwatch/core.py",
		"helpers/containerwatch/sub/nested.py",
		"extensions/fedora-desktop@fedora-desktop/containerReport.js",
		"extensions/fedora-desktop@fedora-desktop/sections/containers.js",
	} {
		w := newWorld(t)
		w.touch(f, "// x")
		if p := w.classify([]string{f}); !p.has("nokill-containerwatch") {
			t.Errorf("%s: %v", f, keys(p))
		}
	}
	for _, f := range []string{"scripts/qa-nokill-containerwatch.bash", "src-go/qa/nokill/rules.go", "src-go/qa/cmd/qa-nokill-containerwatch/main.go"} {
		w := newWorld(t)
		w.touch(f, "// x")
		if p := w.classify([]string{f}); !p.has("nokill-containerwatch") {
			t.Errorf("the gate's own file %s: %v", f, keys(p))
		}
	}
	w := newWorld(t)
	if p := w.classify(nil, "helpers/containerwatch/gone.py"); !p.has("nokill-containerwatch") {
		t.Errorf("a deleted watchdog file: %v", keys(p))
	}
	w = newWorld(t)
	w.touch("helpers/other/tool.py", "# x")
	if p := w.classify([]string{"helpers/other/tool.py"}); p.has("nokill-containerwatch") {
		t.Errorf("an unrelated helper must not run the gate: %v", keys(p))
	}
}
