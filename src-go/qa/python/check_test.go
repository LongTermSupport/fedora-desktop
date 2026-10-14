package python

import (
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// ruffJSON is one ruff diagnostic as ruff prints it.
func ruffJSON(file, code string, row int, msg string) string {
	codeJSON := "null"
	if code != "" {
		codeJSON = `"` + code + `"`
	}
	n := strconv.Itoa(row)
	return `{"cell":null,"code":` + codeJSON + `,"end_location":{"column":9,"row":` + n + `},"filename":"` + file +
		`","fix":null,"location":{"column":1,"row":` + n + `},"message":"` + msg + `","noqa_row":` + n + `,"url":"https://example.invalid/r"}`
}

func lintReturns(diagnostics ...string) func([]string) proc.Result {
	return func([]string) proc.Result {
		return proc.Result{Code: 1, Stdout: "[" + strings.Join(diagnostics, ",") + "]"}
	}
}

// Y11, Y13: a template is substituted and compiled, counted apart from the files.
func TestATemplateIsSubstitutedThenCompiled(t *testing.T) {
	w := newWorld(t, map[string]string{
		"tpl/helper.j2": "#!/usr/bin/env python3\nx = {{ a_var }}\ny = '{{ other\n  }}'\n",
		"tpl/notpy.j2":  "{{ nothing }}\n",
	})
	got := w.main()
	wantExit(t, got, ExitOK)
	if got.stdout != "✓ python: 1 files OK; 1 rendered template(s) compile (syntax only, not linted)\n" {
		t.Fatalf("stdout %q", got.stdout)
	}
	rep := w.report()
	if rep.Summary != (summary{Total: 2, Passed: 2, Failed: 0}) {
		t.Fatalf("summary %+v", rep.Summary)
	}
	if last := rep.Results[1]; last != (entry{File: "tpl/helper.j2", Type: "python-template", Status: "pass"}) {
		t.Fatalf("template result %+v", last)
	}
	for _, c := range w.run.argv("ruff") {
		if slices.ContainsFunc(c, func(a string) bool { return strings.Contains(a, "helper") }) {
			t.Fatalf("the template went to ruff: %v", c)
		}
	}
}

// Y13: the substitution is None, whatever the expression, and the text is the checker's input.
func TestTheSubstitutedTextIsWhatIsCompiled(t *testing.T) {
	w := newWorld(t, map[string]string{"tpl/h.j2": "#!/usr/bin/env python3\nx = {{ a }} + {{ b\n}}\n"})
	var seen string
	w.run.compile = func(files []string) proc.Result {
		for _, f := range files {
			if strings.HasSuffix(f, "tool.py") {
				continue
			}
			data, err := os.ReadFile(f)
			if err != nil {
				t.Error(err)
			}
			seen = string(data)
		}
		return w.run.compileAnswer(files)
	}
	wantExit(t, w.main(), ExitOK)
	if seen != "#!/usr/bin/env python3\nx = None + None\n" {
		t.Fatalf("compiled %q", seen)
	}
}

// Y12: a Jinja statement cannot be checked, so it stops the run before anything is compiled.
func TestAJinjaStatementInATemplateIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{"tpl/h.j2": "#!/usr/bin/env python3\n{% if x %}\ny = 1\n{% endif %}\n"})
	wantStderr(t, w.main(), "tpl/h.j2", "{%")
	w.noReport()
	if got := w.run.argv("python3"); len(got) != 1 {
		t.Fatalf("compiled anyway: %v", w.run.calls)
	}
}

// Y11: templates in an excluded tree are not looked at.
func TestATemplateInAnExcludedTreeIsIgnored(t *testing.T) {
	w := newWorld(t, map[string]string{".venv/h.j2": "#!/usr/bin/env python3\n{% if x %}\n"})
	got := w.main()
	wantExit(t, got, ExitOK)
	if !strings.Contains(got.stdout, "0 rendered template(s)") {
		t.Fatalf("stdout %q", got.stdout)
	}
}

// Y14: a compile failure names the file and carries Python's message, with no checkout path.
func TestACompileFailureIsReportedWithoutTheCheckoutPath(t *testing.T) {
	w := newWorld(t, map[string]string{
		"bad.py":       "SYNTAX_ERROR\n",
		"tpl/bad.j2":   "#!/usr/bin/env python3\nSYNTAX_ERROR {{ x }}\n",
		"tpl/other.j2": "#!/usr/bin/env python3\ny = {{ x }}\n",
	})
	got := w.main()
	wantExit(t, got, ExitFailed)
	if strings.Contains(got.stdout, w.root) || strings.Contains(got.stdout, w.tmp) {
		t.Fatalf("a path of this machine is in stdout:\n%s", got.stdout)
	}
	for _, want := range []string{
		"✗ python: bad.py:   File \"bad.py\", line 1\n    SYNTAX_ERROR\nSyntaxError: invalid syntax\n",
		"✗ python: tpl/bad.j2 (rendered):   File \"tpl/bad.j2\", line 1\n",
		"✗ python: failed → " + w.out + "\n",
	} {
		if !strings.Contains(got.stdout, want) {
			t.Fatalf("stdout lacks %q:\n%s", want, got.stdout)
		}
	}
	rep := w.report()
	if rep.Status != "fail" || rep.Summary != (summary{Total: 4, Passed: 2, Failed: 2}) {
		t.Fatalf("report %+v", rep)
	}
	if len(rep.Failures) != 2 || rep.Failures[0].File != "bad.py" || rep.Failures[1].File != "tpl/bad.j2" {
		t.Fatalf("failures %+v", rep.Failures)
	}
	if strings.Contains(rep.Failures[0].Error, w.root) {
		t.Fatalf("checkout path in the error %q", rep.Failures[0].Error)
	}
}

// Y14: python3 must answer for every file, in the expected form.
func TestACheckerThatDoesNotAnswerForAFileIsExitTwo(t *testing.T) {
	cases := map[string]func(files []string) proc.Result{
		"silent":    func([]string) proc.Result { return proc.Result{} },
		"garbage":   func([]string) proc.Result { return proc.Result{Stdout: "not json\n"} },
		"exit 1":    func([]string) proc.Result { return proc.Result{Code: 1, Stderr: "Traceback: boom\n"} },
		"one short": func(f []string) proc.Result { return proc.Result{Stdout: answer(f[0], "") + "\n"} },
		"unknown": func(f []string) proc.Result {
			return proc.Result{Stdout: answer(f[0], "") + "\n" + answer("/elsewhere.py", "") + "\n"}
		},
		"twice": func(f []string) proc.Result {
			return proc.Result{Stdout: answer(f[0], "") + "\n" + answer(f[0], "") + "\n"}
		},
		"timed out":  func([]string) proc.Result { return proc.Result{TimedOut: true} },
		"left child": func([]string) proc.Result { return proc.Result{Leftover: true} },
	}
	for name, compile := range cases {
		w := newWorld(t, map[string]string{"second.py": "y = 2\n"})
		w.run.compile = compile
		got := w.main()
		wantStderr(t, got, "python3")
		w.noReport()
		if name == "exit 1" && !strings.Contains(got.stderr, "Traceback: boom") {
			t.Fatalf("%s: python3's own stderr is missing: %s", name, got.stderr)
		}
	}
}

// Y15: ruff must have listed every file it was given.
func TestRuffMustSelectEveryFile(t *testing.T) {
	w := newWorld(t, map[string]string{"second.py": "y = 2\n", "third.py": "z = 3\n"})
	w.run.showFiles = func(given []string) []string {
		return slices.DeleteFunc(slices.Clone(given), func(g string) bool {
			return strings.HasSuffix(g, "second.py") || strings.HasSuffix(g, "third.py")
		})
	}
	got := w.main()
	wantStderr(t, got, "ruff did not select 2 of the Python file(s) it was given:", "\n    second.py\n    third.py\n")
	if strings.Contains(got.stderr, w.root) {
		t.Fatalf("checkout path in stderr: %s", got.stderr)
	}
	w.noReport()
	for _, c := range w.run.argv("ruff") {
		if slices.Contains(c, "json") {
			t.Fatalf("linted after the coverage proof failed: %v", c)
		}
	}
}

// Y15, Y16: both ruff runs take the same files, read-only; the files are absolute paths and the
// templates are not among them.
func TestRuffIsGivenTheSameFilesBothTimes(t *testing.T) {
	w := newWorld(t, map[string]string{"a.py": "x = 1\n", "tpl/t.j2": "#!/usr/bin/env python3\ny = {{ q }}\n"})
	wantExit(t, w.main(), ExitOK)
	var show, lint []string
	for _, c := range w.run.argv("ruff") {
		switch {
		case slices.Contains(c, "--show-files"):
			if !slices.Equal(c[:4], []string{"ruff", "check", "--show-files", "--no-cache"}) {
				t.Fatalf("show-files argv %v", c)
			}
			show = c[4:]
		case slices.Contains(c, "json"):
			if !slices.Equal(c[:5], []string{"ruff", "check", "--no-cache", "--output-format", "json"}) {
				t.Fatalf("lint argv %v", c)
			}
			lint = c[5:]
		}
	}
	want := []string{w.root + "/a.py", w.root + "/tool.py"}
	if !slices.Equal(show, want) || !slices.Equal(lint, want) {
		t.Fatalf("show %v lint %v want %v", show, lint, want)
	}
}

// Y16
func TestRuffCrashesAreExitTwo(t *testing.T) {
	cases := map[string]proc.Result{
		"status 2":       {Code: 2, Stderr: "ruff: invalid config\n"},
		"killed":         {Code: -1, Stderr: "killed\n"},
		"timed out":      {TimedOut: true},
		"left child":     {Leftover: true, Stdout: "[]"},
		"empty":          {Stdout: ""},
		"not json":       {Code: 1, Stdout: "oops\n"},
		"not an array":   {Stdout: `{"a":1}`},
		"bad diagnostic": {Code: 1, Stdout: `[1]`},
	}
	for name, res := range cases {
		w := newWorld(t, nil)
		w.run.lint = func([]string) proc.Result { return res }
		got := w.main()
		wantStderr(t, got, "ruff")
		if name == "status 2" && !strings.Contains(got.stderr, "invalid config") {
			t.Fatalf("ruff's own message is missing: %s", got.stderr)
		}
		w.noReport()
	}
}

// Y16: status 1 is data.
func TestRuffFindingsAreDataNotACrash(t *testing.T) {
	w := newWorld(t, map[string]string{"second.py": "import os\n"})
	w.run.lint = lintReturns(ruffJSON(w.root+"/second.py", "F401", 1, "`os` imported but unused"))
	wantExit(t, w.main(), ExitFailed)
}

// Y17, Y18, Y19: the diagnostics keep their fields, lose the checkout path, and fail their file.
func TestDiagnosticsKeepTheirFieldsAndLoseTheCheckoutPath(t *testing.T) {
	w := newWorld(t, map[string]string{"second.py": "import os\nimport sys\n"})
	w.run.lint = lintReturns(ruffJSON(w.root+"/second.py", "F401", 2, "`sys` imported but unused"),
		ruffJSON(w.root+"/second.py", "F401", 1, "`os` imported but unused"),
		ruffJSON(w.root+"/tool.py", "", 1, "SyntaxError: nope"),
	)
	got := w.main()
	wantExit(t, got, ExitFailed)
	wantOut := "✗ python: ruff: 3 issues in 2 file(s) (see " + w.out + " .ruff_diagnostics)\n✗ python: failed → " + w.out + "\n"
	if got.stdout != wantOut {
		t.Fatalf("stdout %q\nwant   %q", got.stdout, wantOut)
	}
	raw, err := os.ReadFile(w.out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), w.root) {
		t.Fatalf("checkout path in the report:\n%s", raw)
	}
	diags := w.report().RuffDiagnostics
	if len(diags) != 3 {
		t.Fatalf("diagnostics %v", diags)
	}
	first := diags[0]
	if first["filename"] != "second.py" || first["code"] != "F401" || first["url"] != "https://example.invalid/r" || first["noqa_row"] != 2.0 {
		t.Fatalf("first diagnostic %v", first)
	}
}

// Y18, Y21: a file with findings fails with its findings in line order; the counts follow.
func TestAFileWithFindingsFailsWithThemInLineOrder(t *testing.T) {
	w := newWorld(t, map[string]string{"second.py": "import os\nimport sys\n"})
	w.run.lint = lintReturns(ruffJSON(w.root+"/second.py", "F401", 2, "`sys` imported but unused"),
		ruffJSON(w.root+"/second.py", "F401", 1, "`os` imported but unused"),
		ruffJSON(w.root+"/tool.py", "", 1, "SyntaxError: nope"),
	)
	wantExit(t, w.main(), ExitFailed)
	rep := w.report()
	byFile := map[string]string{}
	for _, f := range rep.Failures {
		byFile[f.File] = f.Error
	}
	if byFile["second.py"] != "F401:1 `os` imported but unused; F401:2 `sys` imported but unused" {
		t.Fatalf("second.py error %q", byFile["second.py"])
	}
	if byFile["tool.py"] != "syntax-error:1 SyntaxError: nope" {
		t.Fatalf("tool.py error %q", byFile["tool.py"])
	}
	if rep.Summary != (summary{Total: 2, Passed: 0, Failed: 2}) || rep.Status != "fail" {
		t.Fatalf("report %+v", rep)
	}
}

// Y18: a compile error and a finding on one file are one failure with both texts.
func TestACompileErrorAndAFindingShareOneFailure(t *testing.T) {
	w := newWorld(t, map[string]string{"bad.py": "SYNTAX_ERROR\n"})
	w.run.lint = lintReturns(ruffJSON(w.root+"/bad.py", "E999", 1, "boom"))
	wantExit(t, w.main(), ExitFailed)
	fails := w.report().Failures
	if len(fails) != 1 {
		t.Fatalf("failures %+v", fails)
	}
	if e := fails[0].Error; !strings.Contains(e, "SyntaxError: invalid syntax") || !strings.HasSuffix(e, "; E999:1 boom") {
		t.Fatalf("error %q", e)
	}
}

// Y17: a diagnostic on a file ruff was not given means ruff and the gate disagree.
func TestADiagnosticOnAFileNotGivenIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.run.lint = lintReturns(ruffJSON("/elsewhere/x.py", "F401", 1, "m"))
	wantStderr(t, w.main(), "/elsewhere/x.py")
	w.noReport()
}

// Y19, Y20, Y21: order, the pass line and the report of a clean run.
func TestACleanRunPrintsThePassLineAndWritesTheReport(t *testing.T) {
	w := newWorld(t, map[string]string{"b.py": "x = 1\n", "a.py": "x = 1\n", "tpl/z.j2": "#!/usr/bin/env python3\n", "tpl/y.j2": "#!/usr/bin/env python3\n"})
	got := w.main()
	wantExit(t, got, ExitOK)
	if got.stdout != "✓ python: 3 files OK; 2 rendered template(s) compile (syntax only, not linted)\n" || got.stderr != "" {
		t.Fatalf("stdout %q stderr %q", got.stdout, got.stderr)
	}
	rep := w.report()
	if rep.Type != "python" || rep.Status != "pass" || len(rep.Failures) != 0 || rep.RuffDiagnostics == nil || len(rep.RuffDiagnostics) != 0 {
		t.Fatalf("report %+v", rep)
	}
	order := make([]string, 0, len(rep.Results))
	for _, r := range rep.Results {
		order = append(order, r.File)
		if r.Error != "" {
			t.Fatalf("a passing result carries an error: %+v", r)
		}
	}
	if want := []string{"a.py", "b.py", "tool.py", "tpl/y.j2", "tpl/z.j2"}; !slices.Equal(order, want) {
		t.Fatalf("order %v", order)
	}
	raw, err := os.ReadFile(w.out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "{\n  \"type\"") || !json.Valid(raw) {
		t.Fatalf("report is not two-space JSON:\n%s", raw)
	}
}

// Y20: the template count is stated even when it is nought.
func TestTheTemplateCountIsStatedWhenNought(t *testing.T) {
	w := newWorld(t, nil)
	got := w.main()
	wantExit(t, got, ExitOK)
	if got.stdout != "✓ python: 1 files OK; 0 rendered template(s) compile (syntax only, not linted)\n" {
		t.Fatalf("stdout %q", got.stdout)
	}
}

// The run leaves no temporary file behind, on a pass or on an abort.
func TestNoTemporaryFileIsLeftBehind(t *testing.T) {
	w := newWorld(t, map[string]string{"tpl/h.j2": "#!/usr/bin/env python3\ny = {{ q }}\n"})
	wantExit(t, w.main(), ExitOK)
	w.run.compile = func([]string) proc.Result { return proc.Result{Stdout: "junk\n"} }
	wantStderr(t, w.main(), "python3")
	entries, err := os.ReadDir(w.tmp)
	if err != nil || len(entries) != 0 {
		t.Fatalf("left in the temporary directory: %v %v", entries, err)
	}
}
