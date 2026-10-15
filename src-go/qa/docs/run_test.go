package docs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const brokenLink = "[x](./nope.md)"

// D1
func TestD1HelpUnknownArgumentsAndMissingValues(t *testing.T) {
	w := newWorld(t, nil)
	got := w.main("-h")
	wantExit(t, got, ExitOK)
	wantStdout(t, got, "usage: qa-docs")
	for name, args := range map[string][]string{
		"unknown flag":     {"--nope"},
		"positional":       {"docs"},
		"root no value":    {"--root"},
		"scripts no value": {"--scripts"},
	} {
		bad := w.main(args...)
		wantExit(t, bad, ExitCannotRun)
		if bad.stdout != "" {
			t.Fatalf("%s: stdout %q", name, bad.stdout)
		}
		if !strings.HasPrefix(bad.stderr, "ERROR: ") {
			t.Fatalf("%s: stderr %q", name, bad.stderr)
		}
	}
	got = w.main("--nope")
	wantStderr(t, got, "ERROR: unknown argument '--nope'")
	wantStderr(t, got, "usage: qa-docs")
	got = w.main("docs")
	wantStderr(t, got, "ERROR: unknown argument 'docs'")
}

func TestD1RootAndScriptsSelectTheTree(t *testing.T) {
	w := newWorld(t, nil)
	elsewhere := t.TempDir()
	var stdout, stderr bytes.Buffer
	env := w.environment(&stdout, &stderr)
	env.Cwd = elsewhere
	if code := Main([]string{"--root", w.root}, env); code != ExitOK {
		t.Fatalf("--root: exit %d\n%s%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := Main([]string{"--scripts", filepath.Join(w.root, "scripts")}, env); code != ExitOK {
		t.Fatalf("--scripts: exit %d\n%s%s", code, stdout.String(), stderr.String())
	}
}

func TestD1AnUnresolvableRootIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	got := w.main("--root", filepath.Join(w.root, "missing"))
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: cannot resolve the repository root")
	w.noReport()
}

// D2
func TestD2CleanIsZeroAFindingIsOneAndErrorsAreOnStderr(t *testing.T) {
	w := newWorld(t, nil)
	got := w.check()
	wantExit(t, got, ExitOK)
	if got.stderr != "" {
		t.Fatalf("stderr %q", got.stderr)
	}
	w.doc("a.md", brokenLink)
	got = w.check()
	wantExit(t, got, ExitFailed)
	if got.stderr != "" {
		t.Fatalf("stderr %q", got.stderr)
	}
}

func TestD2AStdoutThatCannotBeWrittenIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	var stderr bytes.Buffer
	env := w.environment(&bytes.Buffer{}, &stderr)
	env.Stdout = failingWriter{}
	if code := Main(nil, env); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stderr.String(), "ERROR: cannot write to stdout: broken pipe") {
		t.Fatalf("stderr %q", stderr.String())
	}
	stderr.Reset()
	if code := Main([]string{"-h"}, env); code != ExitCannotRun || !strings.Contains(stderr.String(), "cannot write to stdout") {
		t.Fatalf("help: exit %d %q", code, stderr.String())
	}
}

// D3
func TestD3TheReportIsWrittenOnPassAndFailAndNotOnExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	wantExit(t, w.check(), ExitOK)
	if rep := w.report(); rep.Status != "pass" || rep.Type != "docs" {
		t.Fatalf("%+v", rep)
	}
	if err := os.Remove(w.out); err != nil {
		t.Fatal(err)
	}
	w.doc("a.md", brokenLink)
	wantExit(t, w.check(), ExitFailed)
	if rep := w.report(); rep.Status != "fail" {
		t.Fatalf("%+v", rep)
	}
	if err := os.Remove(w.out); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(w.root, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	wantExit(t, w.check(), ExitCannotRun)
	w.noReport()
}

func TestD3TheReportGoesWhereQAJSONOutSaysElseTheDefault(t *testing.T) {
	w := newWorld(t, nil)
	w.env["QA_JSON_OUT"] = filepath.Join(t.TempDir(), "elsewhere.json")
	wantExit(t, w.check(), ExitOK)
	if len(w.wrote) != 1 || w.wrote[0] != w.env["QA_JSON_OUT"] {
		t.Fatalf("wrote %v", w.wrote)
	}
	w = newWorld(t, nil)
	w.env["QA_JSON_OUT"] = ""
	var stdout, stderr bytes.Buffer
	env := w.environment(&stdout, &stderr)
	env.Getenv = func(string) string { return "" }
	if code := Main(nil, env); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if len(w.wrote) != 1 || w.wrote[0] != "/tmp/qa-docs-results.json" {
		t.Fatalf("wrote %v", w.wrote)
	}
}

func TestD3AReportThatCannotBeWrittenIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	var stdout, stderr bytes.Buffer
	env := w.environment(&stdout, &stderr)
	env.WriteFile = func(string, []byte) error { return errors.New("disk full") }
	if code := Main(nil, env); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stderr.String(), "ERROR: cannot write the report") || !strings.Contains(stderr.String(), "disk full") {
		t.Fatalf("stderr %q", stderr.String())
	}
	if strings.Contains(stdout.String(), "✓") {
		t.Fatalf("a verdict was printed for a run whose report was lost:\n%s", stdout.String())
	}
}

// D4
func TestD4ScopeIsTheCoreDocumentsGitWouldPublish(t *testing.T) {
	cases := []struct {
		rel     string
		scanned bool
	}{
		{"README.md", true},
		{"docs/deep/a.md", true},
		{".claude/rules/r.md", true},
		{"CLAUDE/Topic.md", true},
		{"CLAUDE/sub/Deeper.md", true},
		{"helpers/CLAUDE.md", true},
		{"helpers/other.md", false},
		{"notes.md", false},
		{"CLAUDE/Plan/00001-x/PLAN.md", false},
		{"CLAUDE/Plan/Completed/00001-x/PLAN.md", false},
		{".claude/agents/a.md", false},
		{".claude/skills/s/CLAUDE.md", false},
		{"untracked/CLAUDE.md", false},
		{"roles/vendor/r/CLAUDE.md", false},
		{"docs/node_modules/x/README.md", false},
	}
	for _, c := range cases {
		t.Run(c.rel, func(t *testing.T) {
			files := map[string]string{c.rel: brokenLink}
			w := newWorld(t, files)
			got := w.check()
			if c.scanned {
				wantExit(t, got, ExitFailed)
				wantStdout(t, got, c.rel+":1  ./nope.md")
			} else {
				wantExit(t, got, ExitOK)
			}
		})
	}
}

func TestD4AnIgnoredDocumentIsNotScannedAndANewOneIs(t *testing.T) {
	w := newWorld(t, nil)
	w.ignored("docs/ignored.md", brokenLink)
	wantExit(t, w.check(), ExitOK)
	w.fresh("docs/new.md", brokenLink)
	got := w.check()
	wantExit(t, got, ExitFailed)
	wantStdout(t, got, "docs/new.md:1  ./nope.md")
	if strings.Contains(got.stdout, "ignored.md") {
		t.Fatal("an ignored document was scanned")
	}
}

func TestD4ATrackedDocumentGoneFromTheTreeIsNotScanned(t *testing.T) {
	w := newWorld(t, nil)
	w.run.tracked = append(w.run.tracked, "docs/gone.md")
	w.run.deleted = []string{"docs/gone.md"}
	wantExit(t, w.check(), ExitOK)
}

// D5
func TestD5NoInScopeDocumentIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{
		"CLAUDE.md": "", "CLAUDE/QA.md": "", "docs/playbooks.md": "", "docs/architecture.md": "", "docs/README.md": "",
		".claude/hooks-daemon/README.md": "",
	})
	w.add("helpers/loose.md", "# not core\n")
	got := w.check()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: found 0 in-scope markdown files")
	w.noReport()
	if got.stdout != "" {
		t.Fatalf("stdout %q", got.stdout)
	}
}

// D6
func TestD6AnUnreadableDocumentIsExitTwoNamingIt(t *testing.T) {
	w := newWorld(t, nil)
	w.doc("a.md", "# A")
	w.readFail["a.md"] = errors.New("permission denied")
	got := w.check()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: cannot read docs/a.md: permission denied")
	w.noReport()
}

func TestD6AnUnreadableAnchorTargetIsExitTwoNamingIt(t *testing.T) {
	w := newWorld(t, nil)
	w.add("other/target.md", "# T\n")
	w.doc("a.md", "[x](../other/target.md#t)")
	w.readFail["target.md"] = errors.New("permission denied")
	got := w.check()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: cannot read other/target.md: permission denied")
}

// D7
func TestD7TheStageLineStatesHowManyOfTheScannedFilesAreTracked(t *testing.T) {
	w := newWorld(t, nil)
	got := w.check()
	wantExit(t, got, ExitOK)
	wantStdout(t, got, "✓ docs: 5 files (5 tracked) OK")
	w.fresh("docs/seven.md", "# New\n")
	got = w.check()
	wantExit(t, got, ExitOK)
	wantStdout(t, got, "✓ docs: 6 files (5 tracked) OK")
}

// D14
func TestD14ARepositoryThatTracksNothingIsExitTwoNotAVerdict(t *testing.T) {
	w := newWorld(t, nil)
	w.fresh("docs/new.md", "# New\n")
	w.run.tracked = nil
	got := w.check()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: ")
	w.noReport()
}

// D22
func TestD22TheCleanLineIsExact(t *testing.T) {
	w := newWorld(t, nil)
	got := w.check()
	want := "✓ docs: 5 files (5 tracked) OK (links, anchors, playbook catalogue, topic index, gate inventory) — VENDORED: 0 verified, 0 unverifiable (repo absent), 0 broken\n"
	if got.stdout != want {
		t.Fatalf("stdout\n got  %q\n want %q", got.stdout, want)
	}
}

func TestD22FindingsAreListedAndTheFailLineCountsThem(t *testing.T) {
	w := newWorld(t, nil)
	w.doc("a.md", "", brokenLink, "", "[y](./gone.md)")
	got := w.check()
	wantExit(t, got, ExitFailed)
	want := "✗ docs: 2 finding(s) across 6 files (6 tracked) — VENDORED: 0 verified, 0 unverifiable (repo absent), 0 broken\n" +
		"  docs/a.md:2  ./nope.md  — target does not exist\n" +
		"  docs/a.md:4  ./gone.md  — target does not exist\n" +
		"  Details: jq '.failures[]' " + w.out + "\n"
	if got.stdout != want {
		t.Fatalf("stdout\n got  %q\n want %q", got.stdout, want)
	}
}

func TestD22FindingsComeInPathThenLineOrderThenTheOtherChecks(t *testing.T) {
	w := newWorld(t, nil)
	w.doc("z.md", brokenLink)
	w.doc("a.md", "", "", "[a](./nope.md)", "[b](./nope2.md)")
	w.add("CLAUDE/Topic.md", "# T\n")
	w.add("docs/playbooks.md", "# no names\n")
	w.wantFindings(
		"docs/a.md:3  ./nope.md  — target does not exist",
		"docs/a.md:4  ./nope2.md  — target does not exist",
		"docs/z.md:1  ./nope.md  — target does not exist",
		"docs/playbooks.md:0  play-a.yml  — core playbook is absent from this document",
		"CLAUDE.md:0  CLAUDE/Topic.md  — topic file has no row in the index",
	)
}

// D23
func TestD23ACleanReportHasTheShapeQAAllMerges(t *testing.T) {
	w := newWorld(t, nil)
	wantExit(t, w.check(), ExitOK)
	rep := w.report()
	if rep.Summary.Total != 5 || rep.Summary.Passed != 5 || rep.Summary.Failed != 0 {
		t.Fatalf("%+v", rep.Summary)
	}
	if len(rep.Results) != 0 || len(rep.Failures) != 0 {
		t.Fatalf("%+v", rep)
	}
}

func TestD23AFailingReportCountsFilesAndListsFailures(t *testing.T) {
	w := newWorld(t, nil)
	w.doc("a.md", brokenLink, brokenLink)
	w.doc("b.md", brokenLink)
	wantExit(t, w.check(), ExitFailed)
	rep := w.report()
	if rep.Status != "fail" || rep.Summary.Total != 7 || rep.Summary.Failed != 2 || rep.Summary.Passed != 5 {
		t.Fatalf("%+v", rep.Summary)
	}
	if len(rep.Results) != 3 || len(rep.Failures) != 3 {
		t.Fatalf("%d results, %d failures", len(rep.Results), len(rep.Failures))
	}
	f := rep.Failures[0]
	if f.File != "docs/a.md" || f.Type != "docs" || f.Status != "fail" || f.Error != "./nope.md — target does not exist" {
		t.Fatalf("%+v", f)
	}
}

func TestD23TheCountsNeverGoNegativeWhenAFindingSitsOutsideThePopulation(t *testing.T) {
	population := []string{"docs/a.md", "docs/b.md"}
	got := summaryOf(population, []finding{{File: "docs/a.md"}, {File: "docs/a.md"}, {File: "scripts/qa-gates.tsv"}})
	if got.Total != 3 || got.Failed != 2 || got.Passed != 1 {
		t.Fatalf("%+v", got)
	}
	if got := summaryOf(population, nil); got.Total != 2 || got.Passed != 2 || got.Failed != 0 {
		t.Fatalf("%+v", got)
	}
}

// D24
func TestD24ASymlinkedRootNeverLeaksTheCheckoutPath(t *testing.T) {
	w := newWorld(t, nil)
	w.doc("a.md", brokenLink, "[v](../.claude/hooks-daemon/gone.md)")
	link := filepath.Join(t.TempDir(), "view")
	if err := os.Symlink(w.root, link); err != nil {
		t.Fatal(err)
	}
	got := w.main("--root", link)
	wantExit(t, got, ExitFailed)
	raw, err := os.ReadFile(w.out)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{w.root, link} {
		if strings.Contains(got.stdout, leak) || strings.Contains(got.stderr, leak) || strings.Contains(string(raw), leak) {
			t.Fatalf("the checkout path %s leaked:\nstdout:\n%s\nreport:\n%s", leak, got.stdout, raw)
		}
	}
	wantStdout(t, got, "docs/a.md:1  ./nope.md")
}

func TestD24ErrorsDoNotNameTheCheckout(t *testing.T) {
	w := newWorld(t, nil)
	w.run.code = 128
	got := w.check()
	wantExit(t, got, ExitCannotRun)
	if strings.Contains(got.stderr, w.root) {
		t.Fatalf("stderr names the checkout:\n%s", got.stderr)
	}
	wantStderr(t, got, "not a git repository")
}

// D25
func TestD25AGitThatCannotRunTimesOutOrLeavesAChildIsExitTwo(t *testing.T) {
	for name, set := range map[string]func(*fakeRun){
		"exit":       func(f *fakeRun) { f.code = 128 },
		"timeout":    func(f *fakeRun) { f.timesOut = true },
		"leftover":   func(f *fakeRun) { f.leftover = true },
		"cannot run": func(f *fakeRun) { f.startFails = true },
	} {
		w := newWorld(t, nil)
		set(w.run)
		got := w.check()
		wantExit(t, got, ExitCannotRun)
		if !strings.HasPrefix(got.stderr, "ERROR: ") || got.stdout != "" {
			t.Fatalf("%s: %+v", name, got)
		}
		w.noReport()
	}
}
