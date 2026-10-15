package vmtestmanifest

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const (
	line1 = "vars/vm-test-scenarios.yml: scenarios=3 runnable=2 bridge=2 host_only=0 bases=2\n"
	line2 = "vars/vm-test-scenarios.yml: 2 scenario(s) agree with their guest checker\n"
	line3 = "vars/vm-test-scenarios.yml: 1 scenario fixture(s) run before a declared reboot\n"
)

// V1
func TestV1HelpIsStdoutExitZero(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		w := newWorld(t, nil)
		w.wantExit(ExitOK, w.run(flag))
		if !strings.Contains(w.stdout.String(), "usage: qa-vmtest-manifest") || w.stderr.Len() != 0 {
			t.Fatalf("%s: stdout %q stderr %q", flag, w.stdout.String(), w.stderr.String())
		}
		if len(w.fake.calls) != 0 {
			t.Fatalf("help ran %v", w.fake.calls)
		}
	}
}

// V1
func TestV1BadCommandLinesAreUsageErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown flag": {"--nope"},
		"positional":   {"vars/vm-test-scenarios.yml"},
		"root no val":  {"--root"},
		"scripts none": {"--scripts"},
		"flag as val":  {"--root", "--help"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, nil)
			w.wantExit(ExitCannotRun, Main(args, w.env()))
			w.wantNoStdout()
			w.wantStderr("ERROR: ")
		})
	}
	w := newWorld(t, nil)
	w.wantExit(ExitCannotRun, Main([]string{"--nope"}, w.env()))
	w.wantStderr("ERROR: unknown argument '--nope'", "usage: qa-vmtest-manifest")
}

// V1: the root is --root, else the parent of --scripts, else the working directory.
func TestV1TheRootComesFromRootScriptsOrCwd(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, Main([]string{"--scripts", filepath.Join(w.root, "scripts")}, w.env()))
	w = newWorld(t, nil)
	w.wantExit(ExitOK, Main(nil, w.env()))
}

// V1, V16: a root reached through a symlink is resolved, and the real path never reaches the output.
func TestV1ASymlinkedRootIsResolved(t *testing.T) {
	w := newWorld(t, nil)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(w.root, link); err != nil {
		t.Fatal(err)
	}
	w.wantExit(ExitOK, Main([]string{"--root", link}, w.env()))
	if strings.Contains(w.stdout.String()+w.stderr.String(), w.root) {
		t.Fatalf("a checkout path reached the output:\n%s%s", w.stdout.String(), w.stderr.String())
	}
	if w.fake.calls[0].dir != w.root {
		t.Fatalf("python ran in %q, want the resolved root %q", w.fake.calls[0].dir, w.root)
	}
}

// V1
func TestV1AnUnresolvableRootIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitCannotRun, Main([]string{"--root", filepath.Join(w.root, "nowhere")}, w.env()))
	w.wantStderr("ERROR: ")
}

// V2
func TestV2AFailedStdoutWriteIsExitTwoNamingWhy(t *testing.T) {
	w := newWorld(t, nil)
	env := w.env()
	env.Stdout = failWriter{}
	w.wantExit(ExitCannotRun, Main([]string{"--root", w.root}, env))
	w.wantStderr("ERROR: cannot write to stdout: ")
}

// V15
func TestV15ASuccessfulRunPrintsExactlyThreeLinesAndNothingOnStderr(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, w.run())
	if got := w.stdout.String(); got != line1+line2+line3 {
		t.Fatalf("stdout:\n%s", got)
	}
	if w.stderr.Len() != 0 {
		t.Fatalf("stderr: %s", w.stderr.String())
	}
}

// V3
func TestV3AMissingOrNonRegularInputIsExitTwoNamingIt(t *testing.T) {
	for _, rel := range []string{manifestFile, versionFile} {
		w := newWorld(t, map[string]string{rel: ""})
		w.wantExit(ExitCannotRun, w.run())
		w.wantStderr("ERROR: ", rel)
		w.wantNoStdout()
		if len(w.fake.calls) != 0 {
			t.Fatalf("%s: the validator ran without the input", rel)
		}
	}
	w := newWorld(t, map[string]string{manifestFile: ""})
	if err := os.MkdirAll(filepath.Join(w.root, manifestFile), 0o750); err != nil {
		t.Fatal(err)
	}
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr(manifestFile)
}

// V4
func TestV4TheManifestIsParsedStrictly(t *testing.T) {
	for name, text := range map[string]string{
		"syntax error":     "vm_test_bases: [unclosed\n",
		"empty file":       "",
		"only a comment":   "# nothing here\n",
		"two documents":    goodManifest + "---\nvm_test_bases: {}\n",
		"duplicate key":    goodManifest + "vm_test_ttl_upgrade_days: 8\n",
		"nested duplicate": strings.Replace(goodManifest, "    max_skipped: 0\n  desktop", "    max_skipped: 0\n    planned: 14\n  desktop", 1),
		"integer key":      "1: a\nvm_test_bases: {}\n",
		"nan":              goodManifest + "extra: .nan\n",
		"infinity":         goodManifest + "extra: .inf\n",
		"scalar top":       "just text\n",
		"list top":         "- a\n- b\n",
		"nested int key":   goodManifest + "extra:\n  1: a\n  b: c\n",
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, map[string]string{manifestFile: text})
			if text == "" {
				w.write(manifestFile, "")
			}
			w.wantExit(ExitCannotRun, w.run())
			w.wantStderr("ERROR: ", manifestFile)
			w.wantNoStdout()
			if len(w.fake.calls) != 0 {
				t.Fatalf("the validator ran on an unparsed manifest: %v", w.fake.calls)
			}
		})
	}
}

// V4: the version file is parsed as strictly.
func TestV4TheVersionFileIsParsedStrictly(t *testing.T) {
	for name, text := range map[string]string{
		"syntax error":  "fedora_version: [\n",
		"duplicate key": "fedora_version: 44\nfedora_version: 45\n",
		"two documents": "fedora_version: 44\n---\nfedora_version: 45\n",
		"empty":         "# only a comment\n",
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, map[string]string{versionFile: text})
			w.wantExit(ExitCannotRun, w.run())
			w.wantStderr("ERROR: ", versionFile)
		})
	}
}

// V5
func TestV5TheFedoraVersionIsAPositiveInteger(t *testing.T) {
	for name, text := range map[string]string{
		"quoted":     "fedora_version: \"44\"\n",
		"zero":       "fedora_version: 0\n",
		"negative":   "fedora_version: -3\n",
		"bool":       "fedora_version: true\n",
		"float":      "fedora_version: 44.5\n",
		"missing":    "other: 44\n",
		"null":       "fedora_version:\n",
		"list":       "- 44\n",
		"a sequence": "fedora_version: [44]\n",
		"commented":  "# fedora_version: 44\nother: 1\n",
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, map[string]string{versionFile: text})
			w.wantExit(ExitCannotRun, w.run())
			w.wantStderr("ERROR: ", versionFile)
			if len(w.fake.calls) != 0 {
				t.Fatal("the validator ran with no usable version")
			}
		})
	}
	w := newWorld(t, map[string]string{versionFile: "fedora_version: 45\n"})
	w.wantExit(ExitOK, w.run())
	if got := w.fake.calls[0].argv; got[len(got)-1] != "45" || got[len(got)-2] != "--fedora-version" {
		t.Fatalf("argv: %v", got)
	}
}

// V6: the validator is the host's own, fed the manifest as JSON.
func TestV6TheValidatorIsRunInTheRootOnTheManifestAsJSON(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, w.run())
	if len(w.fake.calls) != 1 {
		t.Fatalf("calls: %v", w.fake.calls)
	}
	c := w.fake.calls[0]
	want := []string{"python3", "-m", "helpers.vmtest.validate_manifest", "--fedora-version", "44"}
	if !reflect.DeepEqual(c.argv, want) {
		t.Fatalf("argv %v, want %v", c.argv, want)
	}
	if c.dir != w.root {
		t.Fatalf("dir %q, want %q", c.dir, w.root)
	}
	if !slices.Contains(c.env, "PYTHONDONTWRITEBYTECODE=1") {
		t.Fatalf("env %v must stop python writing bytecode into the tree", c.env)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(c.stdin), &doc); err != nil {
		t.Fatalf("stdin is not JSON: %v\n%s", err, c.stdin)
	}
	scenarios, ok := doc["vm_test_scenarios"].(map[string]any)
	if !ok || len(scenarios) != 3 || doc["vm_test_ttl_upgrade_days"] != float64(7) {
		t.Fatalf("the manifest did not reach the validator whole: %v", doc)
	}
	notYet, ok := scenarios["not-yet"].(map[string]any)
	if !ok || notYet["planned"] != nil {
		t.Fatalf("null must stay null: %v", scenarios["not-yet"])
	}
}

// V6: an invalid manifest is exit 1 with the validator's reason; the cross-checks do not run.
func TestV6AnInvalidManifestIsExitOneAndSkipsTheCrossChecks(t *testing.T) {
	w := newWorld(t, map[string]string{guestDir + "guest-acceptance-server.bash": script("99")})
	w.fake.reply = proc.Result{
		Stdout: "VMTEST-MANIFEST-INVALID\n", Stderr: "ERROR: scenario 'x': planned must be a positive integer\n", Code: 1,
	}
	w.wantExit(ExitFailed, w.run())
	w.wantNoStdout()
	w.wantStderr("ERROR: vars/vm-test-scenarios.yml: scenario 'x': planned must be a positive integer")
	if strings.Contains(w.stderr.String(), "PLANNED") {
		t.Fatalf("the cross-checks ran on an invalid manifest:\n%s", w.stderr.String())
	}
}

// V6: only the validator's ERROR lines are findings; a warning Python printed is not one.
func TestV6OnlyTheValidatorsErrorLinesAreFindings(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.reply = proc.Result{
		Stdout: "VMTEST-MANIFEST-INVALID\n", Code: 1,
		Stderr: "/x/y.py:3: DeprecationWarning: old thing\nERROR: scenario 'x': bad\n  warnings.warn(...)\n",
	}
	w.wantExit(ExitFailed, w.run())
	w.wantStderr("ERROR: vars/vm-test-scenarios.yml: scenario 'x': bad")
	if strings.Contains(w.stderr.String(), "DeprecationWarning") || strings.Contains(w.stderr.String(), "warnings.warn") {
		t.Fatalf("a warning became a finding:\n%s", w.stderr.String())
	}
	w = newWorld(t, nil)
	w.fake.reply = proc.Result{Stdout: "VMTEST-MANIFEST-INVALID\n", Code: 1, Stderr: "only a warning\n"}
	w.wantExit(ExitFailed, w.run())
	w.wantStderr("without saying why")
}

// V8: a path discovery names outside the root is an error, not a silent drop.
func TestV8APathOutsideTheRootIsRefused(t *testing.T) {
	if _, err := relativeTo("/repo", "/elsewhere/x.bash"); err == nil {
		t.Fatal("a path outside the root was accepted")
	}
	got, err := relativeTo("/repo", "/repo/files/x.bash")
	if err != nil || got != "files/x.bash" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// V6: everything else is the gate being unable to judge.
func TestV6AValidatorThatDidNotJudgeIsExitTwo(t *testing.T) {
	for name, setup := range map[string]func(*fakeRunner){
		"cannot start":              func(f *fakeRunner) { f.startErr = errors.New("executable file not found") },
		"timed out":                 func(f *fakeRunner) { f.reply = proc.Result{Stdout: okMarker, TimedOut: true} },
		"leftover child":            func(f *fakeRunner) { f.reply = proc.Result{Stdout: okMarker, Leftover: true} },
		"traceback":                 func(f *fakeRunner) { f.reply = proc.Result{Stderr: "Traceback (most recent call last):\n", Code: 1} },
		"exit 2":                    func(f *fakeRunner) { f.reply = proc.Result{Stderr: "usage: validate_manifest\n", Code: 2} },
		"killed":                    func(f *fakeRunner) { f.reply = proc.Result{Code: -1} },
		"exit 0 without a marker":   func(f *fakeRunner) { f.reply = proc.Result{Stdout: "all fine\n"} },
		"exit 0 and nothing":        func(f *fakeRunner) { f.reply = proc.Result{} },
		"invalid marker but exit 0": func(f *fakeRunner) { f.reply = proc.Result{Stdout: "VMTEST-MANIFEST-INVALID\n"} },
		"exit 1 with the OK marker": func(f *fakeRunner) { f.reply = proc.Result{Stdout: okMarker, Code: 1} },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, nil)
			setup(w.fake)
			w.wantExit(ExitCannotRun, w.run())
			w.wantNoStdout()
			w.wantStderr("ERROR: ", "validate_manifest")
		})
	}
}

// V6, V16: the validator's text reaches stderr without the checkout path.
func TestV6TheValidatorsStderrIsScrubbedOfTheCheckoutPath(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.reply = proc.Result{Stderr: "Traceback:\n  File \"" + w.root + "/helpers/vmtest/x.py\", line 1\n", Code: 1}
	w.wantExit(ExitCannotRun, w.run())
	if strings.Contains(w.stderr.String(), w.root) {
		t.Fatalf("a checkout path reached stderr:\n%s", w.stderr.String())
	}
	w.wantStderr("helpers/vmtest/x.py")
}

// V7
func TestV7TheOKMarkerGrammarIsExact(t *testing.T) {
	for name, out := range map[string]string{
		"two markers":    okMarker + okMarker,
		"extra line":     okMarker + "noise\n",
		"leading line":   "noise\n" + okMarker,
		"missing field":  "VMTEST-MANIFEST-OK scenarios=3 runnable=2 bridge=2 host_only=0\n",
		"non numeric":    "VMTEST-MANIFEST-OK scenarios=x runnable=2 bridge=2 host_only=0 bases=2\n",
		"swapped fields": "VMTEST-MANIFEST-OK runnable=2 scenarios=3 bridge=2 host_only=0 bases=2\n",
		"extra field":    "VMTEST-MANIFEST-OK scenarios=3 runnable=2 bridge=2 host_only=0 bases=2 more=1\n",
		"signed":         "VMTEST-MANIFEST-OK scenarios=-3 runnable=2 bridge=2 host_only=0 bases=2\n",
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, nil)
			w.fake.reply = proc.Result{Stdout: out}
			w.wantExit(ExitCannotRun, w.run())
			w.wantNoStdout()
			w.wantStderr("ERROR: ", "VMTEST-MANIFEST-OK")
		})
	}
	w := newWorld(t, nil)
	w.fake.reply = proc.Result{Stdout: "VMTEST-MANIFEST-OK scenarios=30 runnable=20 bridge=19 host_only=1 bases=4\n"}
	w.wantExit(ExitOK, w.run())
	if !strings.HasPrefix(w.stdout.String(), "vars/vm-test-scenarios.yml: scenarios=30 runnable=20 bridge=19 host_only=1 bases=4\n") {
		t.Fatalf("the validator's counts are passed through:\n%s", w.stdout.String())
	}
}

// V9, V10: planned against PLANNED.
func TestV10AMismatchIsAFindingAndTheRunStillReportsEveryOther(t *testing.T) {
	w := newWorld(t, map[string]string{
		guestDir + "guest-acceptance-server.bash":                script("12"),
		guestDir + "guest-acceptance-desktop-fresh-install.bash": script("17"),
	})
	w.wantExit(ExitFailed, w.run())
	w.wantNoStdout()
	w.wantStderr(
		"ERROR: server-fast-provision: manifest planned=13 but guest-acceptance-server.bash declares PLANNED=12",
		"ERROR: desktop-fresh-install: manifest planned=16 but guest-acceptance-desktop-fresh-install.bash declares PLANNED=17",
	)
}

// V9: the scenario's own checker wins over the profile's, and the profile's is the fallback.
func TestV9TheCheckerIsTheScenariosOwnElseTheProfiles(t *testing.T) {
	w := newWorld(t, map[string]string{
		guestDir + "guest-acceptance-desktop.bash":               script("99"),
		guestDir + "guest-acceptance-server-fast-provision.bash": script("13"),
		guestDir + "guest-acceptance-server.bash":                script("98"),
	})
	// desktop-fresh-install has its own checker (16) and ignores the profile's 99; server-fast-provision
	// has its own (13) and ignores the profile's 98. Both agree, so the run is clean.
	w.wantExit(ExitOK, w.run())
}

// V9: a profile checker serves a scenario with no checker of its own.
func TestV9TheProfileCheckerServesAScenarioWithoutItsOwn(t *testing.T) {
	w := newWorld(t, map[string]string{
		guestDir + "guest-acceptance-desktop-fresh-install.bash": "",
		guestDir + "guest-acceptance-desktop.bash":               script("16"),
	})
	w.wantExit(ExitOK, w.run())
}

// V10
func TestV10ANoCheckerAtAllNamesTheProfileScript(t *testing.T) {
	w := newWorld(t, map[string]string{guestDir + "guest-acceptance-server.bash": ""})
	w.wantExit(ExitFailed, w.run())
	w.wantStderr("ERROR: server-fast-provision: no guest checker at files/home/.local/share/vmtest/guest-acceptance-server.bash")
}

// V8: a checker git would not publish does not exist for the gate; a fresh unignored one does.
func TestV8OnlyAPublishedCheckerCounts(t *testing.T) {
	w := newWorld(t, nil)
	w.write(".gitignore", "files/home/.local/share/vmtest/guest-acceptance-server.bash\n")
	w.git("rm", "-q", "--cached", guestDir+"guest-acceptance-server.bash")
	w.wantExit(ExitFailed, w.run())
	w.wantStderr("server-fast-provision: no guest checker at")

	w = newWorld(t, map[string]string{guestDir + "guest-acceptance-server.bash": ""})
	w.write(guestDir+"guest-acceptance-server.bash", script("13")) // written after the tracking: untracked, not ignored
	w.wantExit(ExitOK, w.run())
}

// V8: discovery that cannot list is exit 2 with its message.
func TestV8ADiscoveryFailureIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/qa-trees.tsv": ""})
	w.wantExit(ExitCannotRun, w.run())
	w.wantNoStdout()
	w.wantStderr("ERROR: ", "qa-trees.tsv")
}

// V9: the count is read from the syntax tree.
func TestV9PlannedIsReadFromTheSyntaxTreeNotTheText(t *testing.T) {
	ok := map[string]string{
		"plain":                 "PLANNED=13\n",
		"readonly assignment":   "readonly PLANNED=13\n",
		"declare":               "declare -r PLANNED=13\n",
		"typeset":               "typeset -r PLANNED=13\n",
		"export":                "export PLANNED=13\n",
		"local at top level":    "local PLANNED=13\n",
		"declare -i":            "declare -i PLANNED=13\n",
		"indented":              "    PLANNED=13\n",
		"trailing comment":      "PLANNED=13   # the count\n",
		"after a comment about": "# PLANNED=99 is the old value\nPLANNED=13\n",
		"a string mentioning":   "echo 'PLANNED=99'\nPLANNED=13\n",
		"a heredoc mentioning":  "cat <<'EOF'\nPLANNED=99\nEOF\nPLANNED=13\n",
		"in a function body":    "f() {\n    local PLANNED=99\n}\nPLANNED=13\n",
		"leading zeros":         "PLANNED=013\n",
	}
	for name, body := range ok {
		t.Run("ok/"+name, func(t *testing.T) {
			w := newWorld(t, map[string]string{guestDir + "guest-acceptance-server.bash": "#!/usr/bin/bash\n" + body})
			w.wantExit(ExitOK, w.run())
		})
	}
	bad := map[string]struct{ body, want string }{
		"no assignment":      {"echo hi\n", "no PLANNED=<n> line in guest-acceptance-server.bash"},
		"only a comment":     {"# PLANNED=13\n", "no PLANNED=<n> line"},
		"only in a string":   {"echo 'PLANNED=13'\n", "no PLANNED=<n> line"},
		"only in a heredoc":  {"cat <<'EOF'\nPLANNED=13\nEOF\n", "no PLANNED=<n> line"},
		"only in a function": {"f() {\n    PLANNED=13\n}\n", "no PLANNED=<n> line"},
		"only as a prefix":   {"PLANNED=13 env\n", "no PLANNED=<n> line"},
		"twice":              {"PLANNED=13\nPLANNED=13\n", "assigns PLANNED more than once"},
		"arithmetic":         {"PLANNED=$((6 + 7))\n", "PLANNED in guest-acceptance-server.bash is not a plain integer"},
		"expansion":          {"PLANNED=\"${N}\"\n", "is not a plain integer"},
		"quoted":             {"PLANNED=\"13\"\n", "is not a plain integer"},
		"empty":              {"PLANNED=\n", "is not a plain integer"},
		"word":               {"PLANNED=thirteen\n", "is not a plain integer"},
		"append":             {"PLANNED+=1\n", "is not a plain integer"},
		"array":              {"PLANNED=(13)\n", "is not a plain integer"},
		"too large":          {"PLANNED=99999999999999999999999\n", "is not a plain integer"},
	}
	for name, c := range bad {
		t.Run("bad/"+name, func(t *testing.T) {
			w := newWorld(t, map[string]string{guestDir + "guest-acceptance-server.bash": "#!/usr/bin/bash\n" + c.body})
			w.wantExit(ExitFailed, w.run())
			w.wantStderr("ERROR: server-fast-provision: ", c.want)
			w.wantNoStdout()
		})
	}
}

// V9: a checker the shell parser rejects is exit 2, not a finding and not a pass.
func TestV9AnUnparseableCheckerIsExitTwoNamingIt(t *testing.T) {
	w := newWorld(t, map[string]string{guestDir + "guest-acceptance-server.bash": "#!/usr/bin/bash\nPLANNED=13\nif then fi done\n"})
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("ERROR: ", "guest-acceptance-server.bash")
	w.wantNoStdout()
}

// V10: only runnable scenarios are checked; an empty set is exit 2.
func TestV10ANullPlannedIsNotCheckedAndNoRunnableScenarioIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, w.run()) // not-yet has no checker and declares no count: fine
	allNull := strings.NewReplacer("planned: 13", "planned: null", "planned: 16", "planned: null").Replace(goodManifest)
	w = newWorld(t, map[string]string{manifestFile: allNull})
	w.wantExit(ExitCannotRun, w.run())
	w.wantNoStdout()
	w.wantStderr("ERROR: ", "no scenario declares planned")
}

// V11
func TestV11AFixtureWithoutADeclaredRebootIsAFinding(t *testing.T) {
	for name, repl := range map[string]string{
		"false":   "reboot_before_checks: false",
		"missing": "",
		"string":  "reboot_before_checks: \"true\"",
	} {
		t.Run(name, func(t *testing.T) {
			m := strings.Replace(goodManifest, "    reboot_before_checks: true\n", indent(repl), 1)
			w := newWorld(t, map[string]string{manifestFile: m})
			w.wantExit(ExitFailed, w.run())
			w.wantNoStdout()
			w.wantStderr("ERROR: desktop-fresh-install: guest-prepare-desktop-fresh-install.bash exists but reboot_before_checks is ", "so the fixture would never run")
		})
	}
}

func indent(line string) string {
	if line == "" {
		return ""
	}
	return "    " + line + "\n"
}

// V11: a fixture beside no scenario would be deployed and never run.
func TestV11AFixtureOfNoScenarioIsAFinding(t *testing.T) {
	w := newWorld(t, map[string]string{guestDir + "guest-prepare-ghost.bash": "#!/usr/bin/bash\n"})
	w.wantExit(ExitFailed, w.run())
	w.wantStderr("ERROR: guest-prepare-ghost.bash belongs to no scenario in the manifest, so it would be deployed and never run")
}

// V11: zero fixtures is an ordinary repository.
func TestV11NoFixtureAtAllIsNotAnEmptyScope(t *testing.T) {
	w := newWorld(t, map[string]string{guestDir + "guest-prepare-desktop-fresh-install.bash": ""})
	w.wantExit(ExitOK, w.run())
	if !strings.HasSuffix(w.stdout.String(), "vars/vm-test-scenarios.yml: 0 scenario fixture(s) run before a declared reboot\n") {
		t.Fatalf("stdout:\n%s", w.stdout.String())
	}
}

// V12
func TestV12AnAcceptanceScriptOfNothingIsAFinding(t *testing.T) {
	w := newWorld(t, map[string]string{
		guestDir + "guest-acceptance-ghost.bash":   script("1"),
		guestDir + "guest-acceptance-desktop.bash": script("16"), // a base profile: fine
	})
	w.wantExit(ExitFailed, w.run())
	w.wantStderr("ERROR: guest-acceptance-ghost.bash is the checker of no scenario and of no base profile, so it would be deployed and never run")
	if strings.Contains(w.stderr.String(), "guest-acceptance-desktop.bash") || strings.Contains(w.stderr.String(), "guest-acceptance-server.bash") {
		t.Fatalf("a profile or scenario checker is not an orphan:\n%s", w.stderr.String())
	}
}

// V12: other files in the directory are not guest scripts.
func TestV12OtherFilesInTheDirectoryAreNotJudged(t *testing.T) {
	w := newWorld(t, map[string]string{
		guestDir + "guest-cleanup.bash":                       "#!/usr/bin/bash\n",
		guestDir + "guest-acceptance-notes.txt":               "text\n",
		guestDir + "sub/guest-acceptance-deeper.bash":         "#!/usr/bin/bash\n",
		"elsewhere/guest-acceptance-nowhere.bash":             "#!/usr/bin/bash\n",
		"elsewhere/guest-prepare-nowhere.bash":                "#!/usr/bin/bash\n",
		guestDir + "guest-prepared-by-hand.bash":              "#!/usr/bin/bash\n",
		guestDir + "guest-acceptance.bash":                    "#!/usr/bin/bash\n",
		guestDir + "guest-prepare-desktop-fresh-install.bash": "#!/usr/bin/bash\n",
	})
	w.wantExit(ExitOK, w.run())
}

// V10, V15: several findings, every one reported, in scenario order.
func TestV2EveryFindingIsReportedNotTheFirst(t *testing.T) {
	m := strings.Replace(goodManifest, "    reboot_before_checks: true\n", "", 1)
	w := newWorld(t, map[string]string{
		manifestFile: m,
		guestDir + "guest-acceptance-server.bash": script("12"),
		guestDir + "guest-acceptance-ghost.bash":  script("1"),
	})
	w.wantExit(ExitFailed, w.run())
	got := w.stderr.String()
	for _, want := range []string{"server-fast-provision: manifest planned=13", "so the fixture would never run", "guest-acceptance-ghost.bash is the checker of no scenario"} {
		if !strings.Contains(got, want) {
			t.Errorf("stderr lacks %q:\n%s", want, got)
		}
	}
}

// V10: the findings of one check are in scenario-id order.
func TestV10FindingsAreInScenarioIdOrder(t *testing.T) {
	w := newWorld(t, map[string]string{
		guestDir + "guest-acceptance-server.bash":                script("12"),
		guestDir + "guest-acceptance-desktop-fresh-install.bash": script("17"),
	})
	w.wantExit(ExitFailed, w.run())
	got := w.stderr.String()
	a, b := strings.Index(got, "desktop-fresh-install: manifest"), strings.Index(got, "server-fast-provision: manifest")
	if a < 0 || b < 0 || a > b {
		t.Errorf("findings are in scenario-id order:\n%s", got)
	}
}

// V14: the validator accepted a shape the cross-checks cannot read.
func TestV14AShapeTheCrossChecksCannotReadIsExitTwo(t *testing.T) {
	for name, m := range map[string]string{
		"scenario not a mapping": strings.Replace(goodManifest, "  not-yet:\n    base: server-fast\n    planned: null\n    max_skipped: 0\n", "  not-yet: nope\n", 1),
		"planned a string":       strings.Replace(goodManifest, "planned: 13   # = PLANNED in guest-acceptance-server.bash", "planned: \"13\"", 1),
		"base unknown":           strings.Replace(goodManifest, "base: server-fast\n    planned: 13", "base: nowhere\n    planned: 13", 1),
		"bases not a mapping":    strings.Replace(goodManifest, "vm_test_bases:\n  server-fast:\n    kind: fast\n    profile: server\n  desktop:\n    kind: full\n    profile: desktop\n", "vm_test_bases: none\n", 1),
		"scenarios missing":      "vm_test_bases: {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, map[string]string{manifestFile: m})
			w.wantExit(ExitCannotRun, w.run())
			w.wantNoStdout()
			w.wantStderr("ERROR: ")
		})
	}
}

// V13: git failing under discovery is exit 2 (here, a root that is not a repository).
func TestV13GitFailingIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	if err := os.RemoveAll(filepath.Join(w.root, ".git")); err != nil {
		t.Fatal(err)
	}
	w.wantExit(ExitCannotRun, w.run())
	w.wantNoStdout()
	w.wantStderr("ERROR: ")
}

// V16: a failing run names files relative to the root and leaks nothing.
func TestV16NoCheckoutPathReachesTheOutputOnAFailingRun(t *testing.T) {
	w := newWorld(t, map[string]string{guestDir + "guest-acceptance-server.bash": script("12")})
	w.wantExit(ExitFailed, w.run())
	if strings.Contains(w.stdout.String()+w.stderr.String(), w.root) {
		t.Fatalf("a checkout path reached the output:\n%s", w.stderr.String())
	}
}
