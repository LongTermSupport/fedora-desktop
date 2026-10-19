package bashlimits

// Regression tests for the adversarial review of this gate (Plan 00192): each case reproduces
// a finding that was shown to get past, or to mis-measure, the first version.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

// m measures src and fails the test if it cannot be parsed.
func m(t *testing.T, src string) Measurement {
	t.Helper()
	got, err := Measure([]byte(src))
	if err != nil {
		t.Fatalf("measure %q: %v", src, err)
	}
	return got
}

func TestFunctionsInEveryForm(t *testing.T) {
	cases := []struct {
		name, src string
		want      int
	}{
		{"after a semicolon", "true; f1() { :; }\n", 1},
		{"ten on one line", strings.Repeat("f() { :; }; ", 10) + "\n", 10},
		{"keyword form after &&", "true && function f { :; }\n", 1},
		{"nested", "outer() {\n  inner() { :; }\n}\n", 2},
		{"inside a command substitution", "x=$(f() { :; }; f)\n", 1},
		{"function name in a string", "echo 'f() { :; }'\n", 0},
	}
	for _, c := range cases {
		if got := m(t, c.src).Funcs; got != c.want {
			t.Errorf("%s: funcs = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestAssociativeArraySpellings(t *testing.T) {
	yes := []string{
		"builtin declare -A m",
		`\declare -A n`,
		`declare "-A" o`,
		"command declare -A m",
		"builtin typeset -gA m",
		"declare -r -A m",
		"f() { local -A m; }",
		"x=$(declare -A m; echo 1)",
	}
	no := []string{"declare -a m", "echo declare -A m", "declare -x -- -A", "# declare -A"}
	for _, s := range yes {
		if !m(t, s+"\n").Assoc {
			t.Errorf("not detected: %q", s)
		}
	}
	for _, s := range no {
		if m(t, s+"\n").Assoc {
			t.Errorf("false positive: %q", s)
		}
	}
}

func TestInteractiveUISpellings(t *testing.T) {
	yes := []string{
		"/usr/bin/fzf",
		"~/.fzf/bin/fzf --multi",
		`\fzf`,
		`"fzf"`,
		"timeout 5 fzf",
		"xargs -0 fzf",
		"fzf-tmux -p",
		"env FOO=1 gum choose a b",
		"sudo -u bob dialog --msgbox x 3 3",
		"nice -n 5 whiptail --yesno x 3 3",
		"command fzf",
		"exec gum input",
		"x=$(timeout 9 /opt/bin/gum input)",
		"select o in a b; do break; done",
	}
	no := []string{
		"command -v fzf",
		"echo fzf",
		"type -P gum",
		"which dialog",
		"fzf_cmd=1",
		"case $x in\n  fzf) :;;\n  gum) :;;\nesac",
		"case $x in fzf | gum) :;; esac",
		"echo select",
	}
	for _, s := range yes {
		if !m(t, s+"\n").UI {
			t.Errorf("not detected: %q", s)
		}
	}
	for _, s := range no {
		if m(t, s+"\n").UI {
			t.Errorf("false positive: %q", s)
		}
	}
}

func TestHeredocLookalikesDoNotBlindTheMeasure(t *testing.T) {
	tail := strings.Repeat("g() { :; }\n", 6) + "declare -A m\nfzf\n"
	cases := map[string]string{
		"<< in a double-quoted string": ": \"<<EOF\"\n" + tail,
		"<< in an echoed usage line":   "echo \"usage: tool <<file\"\n" + tail,
		"<< in a single-quoted string": ": '<<EOF'\n" + tail,
		"hyphenated delimiter":         "cat <<EOF-1\nbody\nEOF-1\n" + tail,
		"quoted delimiter with space":  "cat <<'END X'\nbody\nEND X\n" + tail,
		"embedded in a bash -c string": "bash -c 'cat > /x << '\\''EOFCCY'\\''\nbody\nEOFCCY\n'\n" + tail,
	}
	for name, src := range cases {
		got := m(t, src)
		if got.Funcs != 6 || !got.Assoc || !got.UI {
			t.Errorf("%s: funcs=%d assoc=%v ui=%v, want 6 true true", name, got.Funcs, got.Assoc, got.UI)
		}
	}
}

func TestHashLinesInsideAStringAreCode(t *testing.T) {
	src := "x=\"\n" + strings.Repeat("# data\n", 100) + "\"\n"
	if got := m(t, src).Code; got != 102 {
		t.Fatalf("code = %d, want 102 (a # line inside a string is not a comment)", got)
	}
}

func TestSemicolonJoiningDoesNotHideStatements(t *testing.T) {
	src := "#!/usr/bin/bash\n" + strings.Repeat("echo x; ", 200) + "\n"
	got := m(t, src)
	if got.Code != 1 || got.Stmts != 200 {
		t.Fatalf("code=%d stmts=%d, want 1 and 200", got.Code, got.Stmts)
	}
	if got.WithinLimits() || !strings.Contains(strings.Join(got.Breaches(), "|"), "200 statements") {
		t.Fatalf("200 joined statements are within limits: %v", got.Breaches())
	}
	ok := m(t, "echo a; echo b\n"+strings.Repeat("echo x\n", 55))
	if !ok.WithinLimits() {
		t.Fatalf("an ordinary file with one joined line breaches: %v", ok.Breaches())
	}
}

func TestDataParsingClause(t *testing.T) {
	yes := []string{
		"jq -r .a f\njq -r .b f",
		"x=$(jq -r .a f | jq -r .b)",
		"python3 -c 'import json'",
		"python -c 'print(1)'",
		"timeout 5 python3.12 -c 'print(1)'",
		"python3 - <<'PY'\nprint(1)\nPY",
		"yq '.a' f",
		"cat f | yq -r .a",
	}
	no := []string{
		"jq -r .a f",
		"command -v jq",
		"echo jq jq",
		"python3 script.py",
		"echo python3 -c",
	}
	for _, s := range yes {
		if got := m(t, s+"\n"); !got.Data || got.WithinLimits() {
			t.Errorf("not flagged as data parsing: %q", s)
		}
	}
	for _, s := range no {
		if m(t, s+"\n").Data {
			t.Errorf("false positive: %q", s)
		}
	}
}

func TestUnparseableBashIsAnError(t *testing.T) {
	if _, err := Measure([]byte("if then fi ((\n")); err == nil {
		t.Fatal("a file that does not parse was measured")
	}
}

func TestJinjaTemplatesAreMeasuredAfterTheirTagsAreSetAside(t *testing.T) {
	src := "{{ ansible_managed | comment }}\n#!/usr/bin/bash\n{% if x %}\necho {{ y }}\n{% endif %}\nf() { :; }\n"
	got, err := MeasureTemplate([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got.Funcs != 1 {
		t.Fatalf("funcs = %d, want 1", got.Funcs)
	}
}

func TestCandidatesAreFoundByContentNotByName(t *testing.T) {
	bash := []byte("#!/usr/bin/env bash\necho\n")
	fsys := fstest.MapFS{
		"bin/com.example.tool":     {Data: bash},
		"bin/tool.txt":             {Data: bash},
		"bin/tool.cmd":             {Data: bash},
		"home/.envrc":              {Data: bash},
		"bin/tool.bats":            {Data: []byte("#!/usr/bin/env bats\n")},
		"bin/tool.zsh":             {Data: []byte("#!/usr/bin/zsh\n")},
		"bin/nothing":              {Data: []byte("set -e\necho\n")},
		"bin/inline":               {Data: []byte("#!/usr/bin/env -S bash -c 'exec x \"$0\"'\n")},
		"a.yml":                    {Data: []byte("#!/usr/bin/env -S bash -c 'p=1 && exec run.bash'\n- hosts: all\n")},
		"j/jinja-comment.j2":       {Data: []byte("{# managed #}\n#!/usr/bin/bash\necho\n")},
		"j/ansible-managed.j2":     {Data: []byte("{{ ansible_managed | comment }}\n#!/usr/bin/bash\necho\n")},
		"j/two-header-lines.j2":    {Data: []byte("{# a #}\n{% raw %}\n\n#!/bin/bash\n")},
		"j/tool.bash.j2":           {Data: []byte("echo {{ x }}\n")},
		"j/tool.sh.j2":             {Data: []byte("echo {{ x }}\n")},
		"j/python.j2":              {Data: []byte("{# a #}\n#!/usr/bin/python3\n")},
		"j/config.j2":              {Data: []byte("key = {{ v }}\n")},
		"j/shebang-late.j2":        {Data: []byte("a\nb\nc\nd\ne\nf\ng\nh\n#!/bin/bash\n")},
		"j/text-before-shebang.j2": {Data: []byte("not jinja\n#!/bin/bash\n")},
	}
	want := map[string]bool{
		"bin/com.example.tool": true, "bin/tool.txt": true, "bin/tool.cmd": true, "home/.envrc": true,
		"bin/tool.bats": false, "bin/tool.zsh": false, "bin/nothing": false, "bin/inline": false,
		"a.yml": false, "j/jinja-comment.j2": true, "j/ansible-managed.j2": true,
		"j/two-header-lines.j2": true, "j/tool.bash.j2": true, "j/tool.sh.j2": true,
		"j/python.j2": false, "j/config.j2": false, "j/shebang-late.j2": false,
		"j/text-before-shebang.j2": false,
	}
	for p, w := range want {
		got, err := IsCandidate(fsys, p)
		if err != nil || got != w {
			t.Errorf("%s: got %v, %v; want %v", p, got, err, w)
		}
	}
}

func TestOnlyTestSuitesPlansAndVendoredTreesAreExempt(t *testing.T) {
	exempt := []string{
		"scripts/test-ccy-lifecycle.bash",
		"tests/helpers/x.bash",
		"CLAUDE/Plan/00192-x/triage.bash",
		"CLAUDE/Plan/Completed/00001-y/a.bash",
		"CLAUDE/Plan/Cancelled/00066-z/triage.bash",
		".claude/init.sh",
		".claude/provision.sh",
		".claude/skills/hooks-daemon/scripts/x.sh",
		".claude/skills/docs-qa/scripts/x.sh",
		"src-go/vendor/golang.org/x/sys/unix/mkall.sh",
		"CLAUDE/Plan/mkplan.bash",
	}
	checked := []string{
		"run.bash",
		"files/home/bashrc-includes/history-search.bash",
		"files/etc/profile.d/zz_lts-fedora-desktop.bash",
		"files/var/local/ps1-prompt",
		"files/var/local/colours",
		"CLAUDE/Plan/_planlib.inc.bash",
		".claude/skills/other/scripts/x.sh",
		"scripts/qa-all.bash",
		"src-go/vendorish/x.sh",
	}
	for _, p := range exempt {
		if _, ok, err := ExemptionFor(p); err != nil || !ok {
			t.Errorf("%s: want exempt, got %v, %v", p, ok, err)
		}
	}
	for _, p := range checked {
		if _, ok, err := ExemptionFor(p); err != nil || ok {
			t.Errorf("%s: want checked, got exempt=%v, %v", p, ok, err)
		}
	}
}

func TestExemptionTableHasNoDeadOrDuplicateEntries(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range Exemptions {
		if seen[e.Pattern] {
			t.Errorf("%q is listed twice", e.Pattern)
		}
		seen[e.Pattern] = true
	}
	for _, dead := range []string{"files/home/bashrc-includes/", "files/etc/profile.d/", "run.bash", ".claude/hooks-daemon/"} {
		if seen[dead] {
			t.Errorf("%q is exempt: user-shell snippets and run.bash are ratcheted, and a gitignored clone is never listed", dead)
		}
	}
}

// --- the ratchet -----------------------------------------------------------------------------

func TestRenamingAnAllowlistedFileCannotLaunderIt(t *testing.T) {
	root := repo(t)
	gitIn(t, root, "mv", "files/bin/long", "files/bin/long.cmd")
	// The renamed file is still bash and still over the limit; --update must not drop the old
	// entry and leave the new name green.
	run(t, root, "--base", "HEAD", "--update")
	wantFail(t, "renamed with a dot", run(t, root, "--base", "HEAD"), "files/bin/long.cmd", "code lines")
}

func TestMovingAnAllowlistedFileIntoAnExemptPathCannotLaunderIt(t *testing.T) {
	for _, dest := range []string{"scripts/test-long.bash", "CLAUDE/Plan/00001-x/long", "tests/long"} {
		root := repo(t)
		if err := inRoot(t, root).MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			t.Fatal(err)
		}
		gitIn(t, root, "mv", "files/bin/long", dest)
		r := run(t, root, "--base", "HEAD", "--update")
		if r.code == ExitOK {
			t.Errorf("%s: --update accepted the move:\n%s", dest, r.err)
		}
		if !strings.Contains(readAllowlist(t, root), "files/bin/long\t") {
			t.Errorf("%s: the entry was dropped", dest)
		}
		wantFail(t, dest, run(t, root, "--base", "HEAD"), "files/bin/long")
	}
}

func TestUpdateRefusesToDropAnEntryWhoseFileStillExists(t *testing.T) {
	root := repo(t)
	put(t, root, "CLAUDE/Plan/001-x/a.bash", script(500))
	put(t, root, allowlistRel, Allowlist{"files/bin/long": {Code: 80, Stmts: 80}, "CLAUDE/Plan/001-x/a.bash": {Code: 500, Stmts: 500}}.Format())
	r := run(t, root, "--base", "HEAD", "--update")
	if r.code == ExitOK || !strings.Contains(r.err, "CLAUDE/Plan/001-x/a.bash") {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
	if !strings.Contains(readAllowlist(t, root), "CLAUDE/Plan/001-x/a.bash") {
		t.Fatal("the entry was dropped")
	}
}

func TestBaseWithoutAnAllowlistIsAnError(t *testing.T) {
	root := t.TempDir()
	gitIn(t, root, "init", "-q")
	put(t, root, "README.md", "x\n")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "first")
	put(t, root, allowlistRel, Allowlist{}.Format())
	r := run(t, root, "--base", "HEAD")
	if r.code != ExitCannotRun || !strings.Contains(r.err, "has no allowlist") {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
}

func TestWrittenAllowlistIsWorldReadable(t *testing.T) {
	root := repo(t)
	put(t, root, "files/bin/long", script(70))
	if r := run(t, root, "--base", "HEAD", "--update"); r.code != ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
	info, err := os.Stat(filepath.Join(root, allowlistRel))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v, want 0644", info.Mode().Perm())
	}
}

func TestSummaryLineSaysWhatItCounts(t *testing.T) {
	r := run(t, repo(t), "--base", "HEAD")
	// small + long are checked (long allowlisted), run.bash is checked too now, and
	// scripts/test-x.bash is exempt.
	want := "BASH-LIMITS-OK 4 bash files: 1 exempt, 3 checked (2 allowlisted, 1 within limits)"
	if !strings.Contains(r.out, want) {
		t.Fatalf("summary %q lacks %q", r.out, want)
	}
}

// --- the measurer version ---------------------------------------------------------------------

func TestPlantedRaiseWithTheSameMeasurerVersionFails(t *testing.T) {
	root := repo(t)
	put(t, root, "files/bin/long", script(90))
	put(t, root, allowlistRel, Allowlist{"files/bin/long": {Code: 90, Stmts: 90}, "run.bash": {Code: 500, Stmts: 500}}.Format())
	wantFail(t, "same version", run(t, root, "--base", "HEAD"), "files/bin/long", "raised")
}

func TestAllowlistFromAnotherMeasurerCannotRunUntilRemeasured(t *testing.T) {
	root := repo(t)
	old := strings.Replace(readAllowlist(t, root), "# measurer: "+itoa(MeasurerVersion), "# measurer: "+itoa(MeasurerVersion-1), 1)
	put(t, root, allowlistRel, old)
	r := run(t, root, "--base", "HEAD")
	if r.code != ExitCannotRun || !strings.Contains(r.err, "--remeasure") {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
}

func TestRemeasureRewritesEveryRecordAndStampsTheVersion(t *testing.T) {
	root := repo(t)
	put(t, root, allowlistRel, strings.Replace(readAllowlist(t, root), "# measurer: "+itoa(MeasurerVersion), "# measurer: "+itoa(MeasurerVersion-1), 1))
	put(t, root, "files/bin/long", script(95)) // the new measure sees more than the old record
	committed := readAllowlist(t, root)
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "old measurer")
	r := run(t, root, "--base", "HEAD", "--remeasure")
	if r.code != ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
	got := readAllowlist(t, root)
	if got == committed || !strings.Contains(got, "# measurer: "+itoa(MeasurerVersion)) || !strings.Contains(got, "files/bin/long\t95\t0\t95\t-") {
		t.Fatalf("remeasured allowlist:\n%s", got)
	}
}

func TestRemeasureNeedsANewerMeasurer(t *testing.T) {
	root := repo(t)
	put(t, root, "files/bin/long", script(95))
	r := run(t, root, "--base", "HEAD", "--remeasure")
	if r.code == ExitOK || !strings.Contains(r.err, "measurer") {
		t.Fatalf("a remeasure with an unchanged measurer was accepted (exit %d): %s", r.code, r.err)
	}
	if strings.Contains(readAllowlist(t, root), "\t95\t") {
		t.Fatal("the refused remeasure raised a record")
	}
}

// --- the base ---------------------------------------------------------------------------------

// clonedRepo is a repository with an origin whose default branch is the one checked out; HEAD
// can then be ahead of origin without being pushed.
func clonedRepo(t *testing.T) string {
	t.Helper()
	origin := repo(t)
	gitIn(t, origin, "branch", "-M", "main")
	clone := filepath.Join(t.TempDir(), "clone")
	gitIn(t, filepath.Dir(clone), "clone", "-q", origin, clone)
	gitIn(t, clone, "remote", "set-head", "origin", "main")
	return clone
}

func TestACommittedButUnpushedRaiseIsCaught(t *testing.T) {
	clone := clonedRepo(t)
	put(t, clone, "files/bin/long", script(90))
	put(t, clone, allowlistRel, Allowlist{"files/bin/long": {Code: 90, Stmts: 90}, "run.bash": {Code: 500, Stmts: 500}}.Format())
	gitIn(t, clone, "add", "-A")
	gitIn(t, clone, "commit", "-q", "-m", "raise the record", "--no-verify")
	// No --base: the default is the merge-base with the remote default branch, not HEAD.
	wantFail(t, "unpushed raise", runRaw(t, clone), "files/bin/long", "raised")
	// Against HEAD itself the same tree is clean: that was the hole.
	if r := run(t, clone, "--base", "HEAD"); r.code != ExitOK {
		t.Fatalf("control against HEAD: %s", r.err)
	}
}

func TestNoDerivableBaseIsAnError(t *testing.T) {
	root := repo(t) // no remote at all
	r := runRaw(t, root)
	if r.code != ExitCannotRun || !strings.Contains(r.err, "default branch") {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
