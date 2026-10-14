package js

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func wantExit(t *testing.T, got result, want ExitCode) {
	t.Helper()
	if got.code != want {
		t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", got.code, want, got.stdout, got.stderr)
	}
}

func wantStderr(t *testing.T, got result, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(got.stderr, p) {
			t.Fatalf("stderr lacks %q:\n%s", p, got.stderr)
		}
	}
}

// cannotRun is the shape every exit-2 test checks: nothing on stdout, no report.
func cannotRun(t *testing.T, w *world, got result, parts ...string) {
	t.Helper()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, parts...)
	if got.stdout != "" {
		t.Fatalf("stdout %q on a run that could not complete", got.stdout)
	}
	w.noReport()
}

// J1
func TestHelpIsStdoutExitZero(t *testing.T) {
	got := newWorld(t, nil).main("--help")
	wantExit(t, got, ExitOK)
	if !strings.Contains(got.stdout, "usage: qa-js") || got.stderr != "" {
		t.Fatalf("%+v", got)
	}
}

func TestBadCommandLinesAreUsageErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown":       {"--frob"},
		"positional":    {"extensions"},
		"root no value": {"--root"},
		"scripts no":    {"--scripts"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, nil)
			cannotRun(t, w, w.main(args...), "ERROR:", "usage: qa-js")
		})
	}
}

func TestTheRootComesFromTheFlagTheScriptsDirOrTheWorkingDirectory(t *testing.T) {
	w := newWorld(t, nil)
	for name, args := range map[string][]string{
		"cwd":            {},
		"root":           {"--root", w.root},
		"relative root":  {"--root", "."},
		"trailing slash": {"--root", w.root + "/"},
		"scripts":        {"--scripts", filepath.Join(w.root, "scripts")},
		"relative scrip": {"--scripts", "scripts"},
	} {
		got := w.main(args...)
		if got.code != ExitOK || !strings.Contains(got.stdout, "✓ js: 2 files OK (1 linted by eslint)") {
			t.Fatalf("%s: %+v", name, got)
		}
	}
}

// J3
func TestNodeMissingIsExitTwoNamingThePlay(t *testing.T) {
	w := newWorld(t, nil)
	w.run.nodeMissing = true
	cannotRun(t, w, w.main(), "node not installed", "play-nvm-install.yml")
}

// J4
func TestEslintNotInstalledIsExitTwoBeforeAnythingIsChecked(t *testing.T) {
	w := newWorld(t, map[string]string{"extensions/node_modules/.bin/eslint": ""})
	cannotRun(t, w, w.main(), "ERROR:", w.eslintPath(), "npm ci")
	for _, c := range w.run.argv("node") {
		if c[1] == "--check" {
			t.Fatalf("a file was checked although ESLint is missing: %v", w.run.calls)
		}
	}
}

func TestEslintOfAnotherMajorIsExitTwo(t *testing.T) {
	for _, version := range []string{"v9.1.0\n", "v7.32.0\n", "banana\n", ""} {
		w := newWorld(t, nil)
		w.run.eslintVersion = version
		cannotRun(t, w, w.main(), "ESLint 8", strings.TrimSpace(version))
	}
}

func TestMissingEslintConfigIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{"extensions/.eslintrc.json": ""})
	cannotRun(t, w, w.main(), filepath.Join(w.root, "extensions", ".eslintrc.json"))
}

// J5
func TestDiscoveryFailureIsExitTwoAndNothingIsChecked(t *testing.T) {
	w := newWorld(t, nil)
	w.run.gitFails = 128
	cannotRun(t, w, w.main(), "ERROR: discovery of js failed:", "not a git repository")
	if len(w.run.argv("node")) > 1 {
		t.Fatalf("files were checked: %v", w.run.calls)
	}
}

func TestOnlyThePublishedJsAndMjsFilesOutsideTheExcludedTreesAreChecked(t *testing.T) {
	w := newWorld(t, map[string]string{"notes.json": "{}\n", "tools/run.cjs": "x\n"})
	if got := w.main(); got.code != ExitOK {
		t.Fatalf("%+v", got)
	}
	var checked []string
	for _, c := range w.run.argv("node") {
		if c[1] == "--check" {
			checked = append(checked, strings.TrimPrefix(c[2], w.root+"/"))
		}
	}
	want := []string{"extensions/ext@x/extension.js", "tests/extensions/test-thing.mjs"}
	if !slices.Equal(checked, want) {
		t.Fatalf("checked %v, want %v", checked, want)
	}
}

// J6
func TestFilesOutsideExtensionsAreParseCheckedButNotLinted(t *testing.T) {
	w := newWorld(t, nil)
	if got := w.main(); got.code != ExitOK {
		t.Fatalf("%+v", got)
	}
	lints := w.run.argv(w.eslintPath())
	last := lints[len(lints)-1]
	if got := last[len(last)-1]; got != filepath.Join(w.root, "extensions/ext@x/extension.js") || strings.Contains(strings.Join(last, " "), "tests/extensions") {
		t.Fatalf("eslint was given %v", last)
	}
}

func TestNothingUnderExtensionsToLintIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{"extensions/ext@x/extension.js": "", "extensions/ext@x/node_modules/dep/dep.js": ""})
	cannotRun(t, w, w.main(), "ERROR: no JavaScript file under extensions/ to lint")
}

// J7
func TestEachFileIsParseCheckedByAnAbsolutePathFromTheRoot(t *testing.T) {
	w := newWorld(t, nil)
	wantExit(t, w.main(), ExitOK)
	n := 0
	for i, c := range w.run.calls {
		if c[0] == "node" && c[1] == "--check" {
			n++
			if !filepath.IsAbs(c[2]) || w.run.dirs[i] != w.root {
				t.Fatalf("%v in %q", c, w.run.dirs[i])
			}
		}
	}
	if n != 2 {
		t.Fatalf("%d parse checks", n)
	}
}

func TestAParseFailureNamesTheFileAndNeverTheCheckoutPath(t *testing.T) {
	w := newWorld(t, map[string]string{"tests/extensions/test-thing.mjs": "SYNTAX_ERROR\n"})
	got := w.main()
	wantExit(t, got, ExitFailed)
	if !strings.Contains(got.stdout, "✗ js: tests/extensions/test-thing.mjs: tests/extensions/test-thing.mjs:1\nSYNTAX_ERROR\n") ||
		!strings.Contains(got.stdout, "SyntaxError: Unexpected identifier") ||
		strings.Contains(got.stdout, "Node.js v") ||
		strings.Contains(got.stdout, w.root) || strings.Contains(w.rawReport(), w.root) {
		t.Fatalf("stdout:\n%s\nreport:\n%s", got.stdout, w.rawReport())
	}
	if !strings.Contains(got.stdout, "✗ js: 1 failing file(s) of 2 → "+w.out) {
		t.Fatalf("stdout:\n%s", got.stdout)
	}
	rep := w.report()
	if rep.Status != "fail" || rep.Summary != (summary{Total: 2, Passed: 1, Failed: 1}) || len(rep.Failures) != 1 || rep.Failures[0].File != "tests/extensions/test-thing.mjs" {
		t.Fatalf("%+v", rep)
	}
}

func TestANodeThatCannotRunAParseCheckIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.run.nodeCheck = func(string) (proc.Result, error) {
		return proc.Result{}, errors.New("fork: resource temporarily unavailable")
	}
	cannotRun(t, w, w.main(), "ERROR:", "resource temporarily unavailable")
}

func TestANodeThatTimesOutIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.run.nodeCheck = func(string) (proc.Result, error) { return proc.Result{TimedOut: true}, nil }
	cannotRun(t, w, w.main(), "node did not finish within")
}

// J8, J9
func TestEslintRunsOnceUnderThePinsFromTheRoot(t *testing.T) {
	w := newWorld(t, map[string]string{"extensions/other/b.js": "export const c = 3;\n"})
	wantExit(t, w.main(), ExitOK)
	var runs [][]string
	for i, c := range w.run.calls {
		if c[0] == w.eslintPath() && c[1] != "--version" {
			runs = append(runs, c)
			if w.run.dirs[i] != w.root {
				t.Fatalf("eslint ran in %q", w.run.dirs[i])
			}
		}
	}
	want := []string{
		w.eslintPath(), "--no-eslintrc", "--config", filepath.Join(w.root, "extensions", ".eslintrc.json"),
		"--no-inline-config", "--format", "json",
		filepath.Join(w.root, "extensions/ext@x/extension.js"), filepath.Join(w.root, "extensions/other/b.js"),
	}
	if len(runs) != 1 || !slices.Equal(runs[0], want) {
		t.Fatalf("eslint ran as %v, want once as %v", runs, want)
	}
}

// J9: the control. A tree that nests a config, whose file carries an eslint-disable comment, is
// still judged by the fake exactly as an ESLint under the pins would judge it: the finding stands.
func TestANestedConfigAndAnInlineDisableDoNotChangeTheRunTheGateAsks(t *testing.T) {
	w := newWorld(t, map[string]string{
		"extensions/ext@x/.eslintrc.json": "{\"rules\": {}}\n",
		"extensions/ext@x/extension.js":   "/* eslint-disable */\nLINT_ERROR\n",
	})
	got := w.main()
	wantExit(t, got, ExitFailed)
	if !strings.Contains(got.stdout, "✗ js: extensions/ext@x/extension.js:1:8: 'LINT_ERROR' is not defined. (no-undef)") {
		t.Fatalf("stdout:\n%s", got.stdout)
	}
	args := strings.Join(w.run.argv(w.eslintPath())[1], " ")
	for _, pin := range []string{"--no-eslintrc", "--no-inline-config"} {
		if !strings.Contains(args, pin) {
			t.Fatalf("pin %s missing: %s", pin, args)
		}
	}
}

// J10
func TestEslintFailingToRunIsExitTwoWithItsStderr(t *testing.T) {
	for name, res := range map[string]proc.Result{
		"status 2":  {Code: 2, Stderr: "Oops! Something went wrong! :(\n"},
		"no output": {Code: 0, Stdout: ""},
		"not json":  {Code: 0, Stdout: "{\"a\": 1}"},
		"timed out": {TimedOut: true},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, nil)
			w.run.lint = func([]string) (proc.Result, error) { return res, nil }
			cannotRun(t, w, w.main(), "ERROR:", "eslint")
		})
	}
}

func TestEslintStatusOneWithNoFindingIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.run.lint = func(files []string) (proc.Result, error) {
		return proc.Result{Code: 1, Stdout: mustJSON([]map[string]any{{"filePath": files[0], "messages": []any{}}})}, nil
	}
	cannotRun(t, w, w.main(), "eslint exited 1 but reported no finding")
}

// J11
func TestEveryGivenFileMustBeReportedOnceAndNothingElse(t *testing.T) {
	missing := func(files []string) []map[string]any {
		return []map[string]any{{"filePath": files[0], "messages": []any{}}}
	}
	for name, tc := range map[string]struct {
		files map[string]string
		lint  func(files []string) []map[string]any
		want  string
	}{
		"a file left out": {
			map[string]string{"extensions/b.js": "export const q = 1;\n"},
			missing, "eslint did not report on 1 of the 2 file(s) it was given",
		},
		"a file not given": {
			nil,
			func(files []string) []map[string]any {
				return []map[string]any{{"filePath": files[0], "messages": []any{}}, {"filePath": "/elsewhere/x.js", "messages": []any{}}}
			},
			"eslint reported on /elsewhere/x.js, which it was not given",
		},
		"a file twice": {
			nil,
			func(files []string) []map[string]any {
				return []map[string]any{{"filePath": files[0], "messages": []any{}}, {"filePath": files[0], "messages": []any{}}}
			},
			"eslint reported on extensions/ext@x/extension.js twice",
		},
		"an ignored file": {
			nil,
			func(files []string) []map[string]any {
				msg := map[string]any{"ruleId": nil, "severity": 1, "message": "File ignored because of a matching ignore pattern. Use \"--no-ignore\" to override."}
				return []map[string]any{{"filePath": files[0], "messages": []any{msg}}}
			},
			"eslint ignored 1 file(s)",
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, tc.files)
			w.run.lint = func(files []string) (proc.Result, error) {
				return proc.Result{Stdout: mustJSON(tc.lint(files))}, nil
			}
			cannotRun(t, w, w.main(), "ERROR:", tc.want)
		})
	}
}

// J12, J13
func TestEveryMessageFailsItsFileWarningsIncludedInLineOrder(t *testing.T) {
	w := newWorld(t, map[string]string{"extensions/b.js": "export const q = 1;\n"})
	w.run.lint = func(files []string) (proc.Result, error) {
		b, a := files[0], files[1]
		results := []map[string]any{
			{"filePath": b, "messages": []map[string]any{
				{"ruleId": "no-undef", "severity": 2, "message": "late", "line": 9, "column": 1},
				{"ruleId": "semi", "severity": 1, "message": "warned", "line": 2, "column": 7},
				{"ruleId": nil, "severity": 2, "fatal": true, "message": "Parsing error: Unexpected token", "line": 2, "column": 3},
			}},
			{"filePath": a, "messages": []any{}},
		}
		return proc.Result{Code: 1, Stdout: mustJSON(results)}, nil
	}
	got := w.main()
	wantExit(t, got, ExitFailed)
	want := "✗ js: extensions/b.js:2:3: Parsing error: Unexpected token\n" +
		"✗ js: extensions/b.js:2:7: warned (semi)\n" +
		"✗ js: extensions/b.js:9:1: late (no-undef)\n" +
		"✗ js: 1 failing file(s) of 3 → " + w.out + "\n"
	if got.stdout != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", got.stdout, want)
	}
	if strings.Contains(got.stdout, w.root) {
		t.Fatalf("checkout path in stdout:\n%s", got.stdout)
	}
}

// J14
func TestAPassingRunSaysHowManyFilesAndHowManyWereLinted(t *testing.T) {
	w := newWorld(t, nil)
	got := w.main()
	if got.code != ExitOK || got.stdout != "✓ js: 2 files OK (1 linted by eslint)\n" || got.stderr != "" {
		t.Fatalf("%+v", got)
	}
}

// J15
func TestTheReportCarriesTheStageReadersFieldsAndTheEslintBlock(t *testing.T) {
	w := newWorld(t, map[string]string{"extensions/ext@x/extension.js": "LINT_ERROR\n", "extensions/ext@x/more.js": "ok\n"})
	wantExit(t, w.main(), ExitFailed)
	rep := w.report()
	if rep.Type != "js" || rep.Status != "fail" || rep.Summary != (summary{Total: 3, Passed: 2, Failed: 1}) {
		t.Fatalf("%+v", rep)
	}
	files := make([]string, 0, len(rep.Results))
	for _, r := range rep.Results {
		files = append(files, r.File)
	}
	if !slices.Equal(files, []string{"extensions/ext@x/extension.js", "extensions/ext@x/more.js", "tests/extensions/test-thing.mjs"}) {
		t.Fatalf("results %v", files)
	}
	if len(rep.Failures) != 1 || rep.Failures[0].Status != "fail" || rep.Failures[0].Type != "js" ||
		rep.Failures[0].Error != "1:8 no-undef 'LINT_ERROR' is not defined." {
		t.Fatalf("%+v", rep.Failures)
	}
	if !rep.Eslint.Ran || rep.Eslint.Status != "fail" || rep.Eslint.Error != "1 message(s)" {
		t.Fatalf("%+v", rep.Eslint)
	}
}

func TestAPassingReportHasEmptyFailuresAndAPassingEslint(t *testing.T) {
	w := newWorld(t, nil)
	wantExit(t, w.main(), ExitOK)
	raw := w.rawReport()
	if !strings.Contains(raw, `"failures": []`) {
		t.Fatalf("failures must be an empty array:\n%s", raw)
	}
	rep := w.report()
	if rep.Status != "pass" || !rep.Eslint.Ran || rep.Eslint.Status != "pass" || rep.Eslint.Error != "" || rep.Summary.Failed != 0 {
		t.Fatalf("%+v", rep)
	}
}

func TestAParseFailureAndALintFindingOnOneFileAreJoined(t *testing.T) {
	w := newWorld(t, map[string]string{"extensions/ext@x/extension.js": "SYNTAX_ERROR LINT_ERROR\n"})
	w.run.nodeCheck = func(file string) (proc.Result, error) {
		if strings.HasSuffix(file, "extension.js") {
			return proc.Result{Code: 1, Stderr: "boom\n"}, nil
		}
		return proc.Result{}, nil
	}
	wantExit(t, w.main(), ExitFailed)
	rep := w.report()
	if len(rep.Failures) != 1 || rep.Failures[0].Error != "boom; 1:8 no-undef 'LINT_ERROR' is not defined." {
		t.Fatalf("%+v", rep.Failures)
	}
}

func TestAReportThatCannotBeWrittenIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.env["QA_JSON_OUT"] = filepath.Join(w.root, "no", "such", "dir", "r.json")
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "cannot write the report")
}
