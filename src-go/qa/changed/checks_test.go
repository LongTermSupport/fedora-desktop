package changed

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/semgrepreport"
)

// scriptedRunner is a proc.Runner that answers by the joined argv and records every call.
type scriptedRunner struct {
	replies map[string]proc.Result
	calls   []proc.Cmd
	// onRun, when set, runs for every call (to block until the context ends, or to write files).
	onRun func(ctx context.Context, c proc.Cmd)
	// semgrepReport makes a semgrep call write an empty JSON report, as a clean run does.
	semgrepReport bool
	// vcs sends the version-control commands no reply matches to the real program.
	vcs bool
	// mask is the process's umask as the run left it, and masks the one each call ran under.
	mask  int
	masks []int
}

// machineUmask is the umask of the machine a fake run starts on: a GitHub runner's, not
// gates.SuiteUmask.
const machineUmask = 0o022

// keepUmask is an Env.Umask that changes nothing, for a test that does not judge the umask.
func keepUmask(int) int { return machineUmask }

func (r *scriptedRunner) Run(ctx context.Context, c proc.Cmd) (proc.Result, error) {
	r.calls = append(r.calls, c)
	r.masks = append(r.masks, r.mask)
	if r.semgrepReport && c.Argv[0] == "semgrep" {
		if err := os.WriteFile(c.Argv[slices.Index(c.Argv, "--json-output")+1], []byte(mirrorReport(c.Dir, "", "")), 0o600); err != nil {
			panic(err)
		}
	}
	if r.onRun != nil {
		r.onRun(ctx, c)
	}
	key := strings.Join(c.Argv, " ")
	for prefix, res := range r.replies {
		if strings.HasPrefix(key, prefix) {
			return res, nil
		}
	}
	if r.vcs && c.Argv[0] == "git" {
		return proc.OS{}.Run(ctx, c)
	}
	return proc.Result{}, nil
}

// setUmask is Env.Umask over the fake: the process umask the next calls inherit.
func (r *scriptedRunner) setUmask(mask int) int {
	old := r.mask
	r.mask = mask
	return old
}

func lookupCheck(id string) checkDef {
	for _, d := range checkDefs {
		if d.id == id {
			return d
		}
	}
	panic("no such check: " + id)
}

func (r *scriptedRunner) argv() []string {
	out := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		out = append(out, strings.Join(c.Argv, " "))
	}
	return out
}

func TestEveryRegisteredCheckIsKnownAndOrdered(t *testing.T) {
	want := []string{
		"toolchain", "bash-syntax", "shellcheck", "shell-directives", "semgrep", "ready-wait-files", "stt-rules-files", "hook-handler-rules-files", "walk-open-files", "stdin-wait-files", "ruff", "unit-tests", "ansible-syntax",
		"ansible-patterns", "ansible-syntax-gate", "patterns", "bash", "python", "js", "docs", "go", "bash-limits", "nokill-containerwatch", "teamchat-keeps-off-v1", "seat-number-text", "ansible-pause", "gh-scopes", "find-race", "arch-pin", "argv-leak", "place-issued-token", "boot-hold", "gh-poll", "secret-screen", "temp-exclusive", "ready-wait", "hidden-bindings", "lxc-attach-streams", "plan-script-logging", "stt-rules", "walk-open", "stdin-wait", "hook-handler-rules", "version-pins", "go-deps", "go-vuln", "hook-handlers", "plan-tests", "vmtest-manifest", "gitleaks", "ccy-version-bump", "deployed-drift", "node-check", "eslint",
	}
	got := make([]string, 0, len(checkDefs))
	for _, d := range checkDefs {
		got = append(got, d.id)
		if !isCheckID(d.id) {
			t.Errorf("%s not known", d.id)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order: %v", got)
	}
	if isCheckID("suite") || isCheckID("") {
		t.Fatal("only registered checks are ids")
	}
}

func TestSummaries(t *testing.T) {
	two := &checkPlan{items: []string{"a", "b"}}
	for id, want := range map[string]string{
		"toolchain":                "QA tool versions match .qa-versions",
		"bash-syntax":              "2 file(s)",
		"shell-directives":         "2 file(s)",
		"ready-wait-files":         "2 file(s)",
		"stt-rules-files":          "2 file(s)",
		"hook-handler-rules-files": "2 file(s)",
		"walk-open-files":          "2 file(s)",
		"stdin-wait-files":         "2 file(s)",
		"unit-tests":               "2 module(s)",
		"ansible-syntax":           "2 playbook(s)",
		"ansible-patterns":         "fail-fast patterns, whole gate",
		"ansible-syntax-gate":      "every playbook, whole gate",
		"deployed-drift":           "repo-owned scripts against their deployed copies, whole gate",
		"patterns":                 "bash-convention rules, whole gate",
		"bash":                     "bash -n and shellcheck, whole gate",
		"python":                   "compile and ruff, whole gate",
		"js":                       "node --check and ESLint, whole gate",
		"docs":                     "whole gate",
		"go":                       "go-qa, both modules (diff mode on a git change)",
		"bash-limits":              "whole gate (tooling is Go; allowlist ratchet)",
		"eslint":                   "2 file(s)",

		"nokill-containerwatch": "whole gate (the container-watch watchdog never signals a process)",
		"teamchat-keeps-off-v1": "whole gate (no file v2 of the agent-team bus owns holds a v1 name)",
		"seat-number-text":      "whole gate (no doc, help or launcher message asks a person for a seat number)",
		"ansible-pause":         "whole gate (every pause registers its answer or says why it waits)",
		"gh-scopes":             "gh-scope-outside-ssot rule, whole gate",
		"walk-open":             "walk-opens-non-regular-file rule, whole gate",
		"stdin-wait":            "stdin-read-unbounded rule, whole gate",
		"gitleaks":              "whole gate (no secret gitleaks finds in what a commit would carry)",
		"ccy-version-bump":      "the change from the base by the pre-commit ccy version gates",
		"find-race":             "find-race-negated-stat-test rule, whole gate",
		"arch-pin":              "arch-pinned-artefact rule, whole gate",
		"argv-leak":             "secret-in-argv rules, whole gate",
		"place-issued-token":    "token-bundle-into-place-issued rule, whole gate",
		"boot-hold":             "unit-holds-boot-target rule, whole gate",
		"gh-poll":               "github-poll-too-often rule, whole gate",
		"secret-screen":         "secret-on-normal-screen rules, whole gate",
		"temp-exclusive":        "temp-not-exclusive rules, whole gate",
		"ready-wait":            "whole gate (no wait ignores the exit of the process it waits for)",
		"plan-script-logging":   "whole gate (no active plan script uses the un-waitable run-log pattern; no plan-local logs/ dir)",
		"stt-rules":             "whole gate (no model judged present without its weights, no dropdown label that explains itself, no job end published before its cleanup)",
		"hook-handler-rules":    "whole gate (no code the hooks daemon loads resolves a path against the daemon's working directory, matches text against the raw Bash command line outside the sanctioned judge _raw_command_judge, or reuses a name that judge's exemption trusts)",
		"version-pins":          "whole gate (the version-pin manifest is well formed and every pin still resolves to a declared var)",
		"vmtest-manifest":       "whole gate (the VM-test scenario manifest is valid and agrees with its guest scripts)",
		"go-deps":               "whole gate (src-go's dependencies allowlisted, pinned and vendored in step)",
		"go-vuln":               "whole gate (govulncheck finds no vulnerability src-go reaches)",
		"hook-handlers":         "whole gate (the project hook handlers pass their tests under the daemon's interpreter)",
		"plan-tests":            "whole gate (every active plan folder's own Python tests pass, each run from its folder)",
		"lxc-attach-streams":    "whole gate (no command reaching lxc-attach has a standard stream on a file)",
	} {
		if got := lookupCheck(id).summaryFor(two); got != want {
			t.Errorf("%s: %q, want %q", id, got, want)
		}
	}
}

func newTestSession(t *testing.T, runner proc.Runner) *session {
	t.Helper()
	root := t.TempDir()
	scripts := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scripts, 0o700); err != nil {
		t.Fatal(err)
	}
	return &session{
		env: Env{Getenv: noEnv, Umask: keepUmask, Runner: runner}, root: root, scripts: scripts,
		plan: &plan{checks: map[string]*checkPlan{}, covered: map[string]bool{}}, narrowed: map[string]int{},
	}
}

func TestPerFileChecksRunEachFileAndFailIfAnyDoes(t *testing.T) {
	runner := &scriptedRunner{replies: map[string]proc.Result{"bash -n bad.bash": {Stdout: "syntax error\n", Code: 2}}}
	s := newTestSession(t, runner)
	res, err := lookupCheck("bash-syntax").run(context.Background(), s, &checkPlan{items: []string{"ok.bash", "bad.bash", "also.bash"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.code == 0 || !strings.Contains(res.output, "syntax error") {
		t.Fatalf("got %+v", res)
	}
	if got := runner.argv(); !reflect.DeepEqual(got, []string{"bash -n ok.bash", "bash -n bad.bash", "bash -n also.bash"}) {
		t.Fatalf("every file is tried: %v", got)
	}
	if runner.calls[0].Dir != s.root {
		t.Fatalf("cwd %q", runner.calls[0].Dir)
	}
	node := &scriptedRunner{}
	if res, err := lookupCheck("node-check").run(context.Background(), newTestSession(t, node), &checkPlan{items: []string{"a.js"}}); err != nil || res.code != 0 {
		t.Fatalf("node-check: %+v %v", res, err)
	}
	if got := node.argv(); !reflect.DeepEqual(got, []string{"node --check a.js"}) {
		t.Fatalf("node-check argv: %v", got)
	}
}

func TestToolChecksPassTheirFilesOnOneCommandLine(t *testing.T) {
	runner := &scriptedRunner{}
	s := newTestSession(t, runner)
	ctx := context.Background()
	for id, items := range map[string][]string{"shellcheck": {"a.bash", "b.bash"}, "ruff": {"x.py"}, "unit-tests": {"tests.helpers.test_a"}} {
		if _, err := lookupCheck(id).run(ctx, s, &checkPlan{items: items}); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		"shellcheck --norc --external-sources --severity=warning a.bash b.bash",
		"ruff check --config " + filepath.Join(s.root, "ruff.toml") + " --no-cache x.py",
		"python3 -m unittest tests.helpers.test_a",
	}
	got := runner.argv()
	slices.Sort(got)
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	for _, c := range runner.calls {
		if c.Argv[0] == "shellcheck" && !slices.Contains(c.Env, "SHELLCHECK_OPTS=") {
			t.Fatalf("shellcheck must not read options from the environment: %v", c.Env)
		}
		if c.Argv[0] == "python3" && !reflect.DeepEqual(ownEnv(c.Env), []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}) {
			t.Fatalf("unit tests must not read the git configuration: %v", c.Env)
		}
	}
}

// SPEC-qa-bash.md S20: a changed shell file gets the suppression verdict the gate gives it, by the
// gate's own --directives mode, in one process over all the files.
func TestShellDirectivesRunsTheGatesDirectivesMode(t *testing.T) {
	runner := &scriptedRunner{}
	s := newTestSession(t, runner)
	if _, err := lookupCheck("shell-directives").run(context.Background(), s, &checkPlan{items: []string{"a.bash", "b.bash"}}); err != nil {
		t.Fatal(err)
	}
	want := []string{"bash " + filepath.Join(s.scripts, "qa-bash.bash") + " --directives a.bash b.bash"}
	if got := runner.argv(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// The per-file ready-wait check runs the gate over the changed files by name (SPEC-qa-ready-wait-rules R9).
func TestReadyWaitFilesRunsTheGateOnTheNamedFiles(t *testing.T) {
	runner := &scriptedRunner{}
	s := newTestSession(t, runner)
	if _, err := lookupCheck("ready-wait-files").run(context.Background(), s, &checkPlan{items: []string{"a.py", "b.bash"}}); err != nil {
		t.Fatal(err)
	}
	want := []string{"bash " + filepath.Join(s.scripts, "qa-ready-wait-rules.bash") + " a.py b.bash"}
	if got := runner.argv(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// The per-file speech-to-text check runs the gate over the changed files by name (SPEC-qa-speech-to-text-rules S10).
func TestSttRulesFilesRunsTheGateOnTheNamedFiles(t *testing.T) {
	runner := &scriptedRunner{}
	s := newTestSession(t, runner)
	if _, err := lookupCheck("stt-rules-files").run(context.Background(), s, &checkPlan{items: []string{"a.py", "b.js"}}); err != nil {
		t.Fatal(err)
	}
	want := []string{"bash " + filepath.Join(s.scripts, "qa-speech-to-text-rules.bash") + " a.py b.js"}
	if got := runner.argv(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// The per-file hook handler rules check runs its gate over the changed Python files by name.
func TestHookHandlerRulesFilesRunsTheGateOnTheNamedFiles(t *testing.T) {
	runner := &scriptedRunner{}
	s := newTestSession(t, runner)
	if _, err := lookupCheck("hook-handler-rules-files").run(context.Background(), s, &checkPlan{items: []string{"a.py", "b.py"}}); err != nil {
		t.Fatal(err)
	}
	want := []string{"bash " + filepath.Join(s.scripts, "qa-hook-handler-rules.bash") + " a.py b.py"}
	if got := runner.argv(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// The per-file walk-open and stdin-wait checks run their gates over the changed Python files by
// name.
func TestPerFilePythonRuleChecksRunTheGateOnTheNamedFiles(t *testing.T) {
	for id, script := range map[string]string{"walk-open-files": "qa-walk-open.bash", "stdin-wait-files": "qa-stdin-wait.bash"} {
		runner := &scriptedRunner{}
		s := newTestSession(t, runner)
		if _, err := lookupCheck(id).run(context.Background(), s, &checkPlan{items: []string{"a.py", "b.py"}}); err != nil {
			t.Fatal(err)
		}
		want := []string{"bash " + filepath.Join(s.scripts, script) + " a.py b.py"}
		if got := runner.argv(); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: got %v, want %v", id, got, want)
		}
	}
}

// eslintReply is the JSON ESLint prints for one file with the given messages.
func eslintReply(t *testing.T, file string, messages ...map[string]any) string {
	t.Helper()
	if messages == nil {
		messages = []map[string]any{}
	}
	data, err := json.Marshal([]map[string]any{{"filePath": file, "messages": messages}})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// runEslintCheck runs the eslint check over one changed file whose ESLint answer is reply.
func runEslintCheck(t *testing.T, reply proc.Result, file string) checkResult {
	t.Helper()
	runner := &scriptedRunner{replies: map[string]proc.Result{"": reply}}
	s := newTestSession(t, runner)
	res, err := lookupCheck("eslint").run(context.Background(), s, &checkPlan{items: []string{file}, rel: "extensions"})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// J9, J8: the per-file lint is given the gate's pins, so a nested config or an eslint-disable
// comment cannot make it pass what the gate fails; and it runs from the root on absolute paths,
// as the gate does, so an ignore file is read from the same directory.
func TestEslintRunsFromTheRootOnAbsolutePathsWithTheGatesPins(t *testing.T) {
	rel := "extensions/a@b/x.js"
	runner := &scriptedRunner{}
	s := newTestSession(t, runner)
	file := filepath.Join(s.root, rel)
	runner.replies = map[string]proc.Result{"": {Stdout: eslintReply(t, file)}}
	res, err := lookupCheck("eslint").run(context.Background(), s, &checkPlan{items: []string{rel}, rel: "extensions"})
	if err != nil {
		t.Fatal(err)
	}
	c := runner.calls[0]
	want := []string{filepath.Join(s.root, "extensions", "node_modules", ".bin", "eslint"), "--no-eslintrc", "--config", filepath.Join(s.root, "extensions", ".eslintrc.json"), "--no-inline-config", "--format", "json", file}
	if c.Dir != s.root || !reflect.DeepEqual(c.Argv, want) || res.code != 0 {
		t.Fatalf("got %+v, result %+v", c, res)
	}
}

// B37: a file ESLint names but does not read (an ignore pattern drops it, with exit 0) is a
// failure, as it is in the gate (J11).
func TestEslintFailsAFileItIgnoredInsteadOfVouchingForIt(t *testing.T) {
	ignored := map[string]any{"ruleId": nil, "severity": 1, "message": "File ignored by default.  Use a negated ignore pattern to override.", "line": 0, "column": 0}
	runner := &scriptedRunner{}
	s := newTestSession(t, runner)
	file := filepath.Join(s.root, "extensions", "a@b", ".hidden.js")
	runner.replies = map[string]proc.Result{"": {Stdout: eslintReply(t, file, ignored)}}
	res, err := lookupCheck("eslint").run(context.Background(), s, &checkPlan{items: []string{"extensions/a@b/.hidden.js"}, rel: "extensions"})
	if err != nil {
		t.Fatal(err)
	}
	if res.code == 0 || !strings.Contains(res.output, "ignored") || !strings.Contains(res.output, "extensions/a@b/.hidden.js") {
		t.Fatalf("an ignored file was vouched for: %+v", res)
	}
}

// B37: a file ESLint does not answer for at all is a failure too.
func TestEslintFailsWhenItAnswersForNoFile(t *testing.T) {
	res := runEslintCheck(t, proc.Result{Stdout: "[]"}, "extensions/a@b/x.js")
	if res.code == 0 || !strings.Contains(res.output, "did not report on 1 of the 1 file(s)") {
		t.Fatalf("got %+v", res)
	}
}

// J12, J13: a finding is a line naming the file relative to the root, with its place and rule.
func TestEslintReportsEachFindingAsAFileLine(t *testing.T) {
	runner := &scriptedRunner{}
	s := newTestSession(t, runner)
	file := filepath.Join(s.root, "extensions", "a@b", "x.js")
	finding := map[string]any{"ruleId": "no-undef", "severity": 2, "message": "'w' is not defined.", "line": 1, "column": 8}
	runner.replies = map[string]proc.Result{"": {Stdout: eslintReply(t, file, finding), Code: 1}}
	res, err := lookupCheck("eslint").run(context.Background(), s, &checkPlan{items: []string{"extensions/a@b/x.js"}, rel: "extensions"})
	if err != nil {
		t.Fatal(err)
	}
	if res.code == 0 || !strings.Contains(res.output, "extensions/a@b/x.js:1:8: 'w' is not defined. (no-undef)") || strings.Contains(res.output, s.root) {
		t.Fatalf("got %+v", res)
	}
}

// J10: an ESLint that fails to run fails the check with its stderr.
func TestEslintThatFailsToRunFailsTheCheckWithItsStderr(t *testing.T) {
	res := runEslintCheck(t, proc.Result{Stderr: "Oops! Something went wrong\n", Code: 2}, "extensions/a@b/x.js")
	if res.code == 0 || !strings.Contains(res.output, "status 2") || !strings.Contains(res.output, "Oops! Something went wrong") {
		t.Fatalf("got %+v", res)
	}
}

// B57: the playbooks a change selects are checked by the gate itself, named on its command line
// (issue #222), so they get its prerequisites (a missing password file is exit 2, not every
// playbook failing) and its report; the gate's verdict is the check's.
func TestAnsibleSyntaxRunsTheGateOverThePlaybooksItNames(t *testing.T) {
	runner := &scriptedRunner{replies: map[string]proc.Result{}}
	s := newTestSession(t, runner)
	gate := "bash " + filepath.Join(s.scripts, "qa-ansible-syntax.bash")
	runner.replies[gate] = proc.Result{Stdout: "✗ ansible-syntax: b.yml\nbad yaml\n", Code: 1}
	res, err := lookupCheck("ansible-syntax").run(context.Background(), s, &checkPlan{items: []string{"a.yml", "b.yml", "c.yml"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.code != 1 || !strings.Contains(res.output, "✗ ansible-syntax: b.yml\nbad yaml") {
		t.Fatalf("got %+v", res)
	}
	if !reflect.DeepEqual(runner.argv(), []string{gate + " a.yml b.yml c.yml"}) {
		t.Fatalf("one gate run over the named playbooks: %v", runner.argv())
	}
	if env := ownEnv(runner.calls[0].Env); len(env) != 1 || !strings.HasPrefix(env[0], "QA_JSON_OUT=") {
		t.Fatalf("the gate needs QA_JSON_OUT: %v", env)
	}
}

func TestGatesRunTheirScriptFromTheScriptsDirectory(t *testing.T) {
	runner := &scriptedRunner{}
	s := newTestSession(t, runner)
	ctx := context.Background()
	for _, id := range []string{"toolchain", "go", "bash-limits", "ansible-patterns", "docs", "nokill-containerwatch", "ansible-syntax-gate", "deployed-drift", "ansible-pause", "gh-scopes", "find-race", "arch-pin", "argv-leak", "place-issued-token", "boot-hold", "gh-poll", "secret-screen", "temp-exclusive", "ready-wait", "plan-script-logging", "stt-rules", "walk-open", "stdin-wait", "hook-handler-rules", "version-pins", "vmtest-manifest", "go-deps", "go-vuln", "teamchat-keeps-off-v1", "seat-number-text", "hook-handlers", "plan-tests", "lxc-attach-streams", "gitleaks"} {
		if _, err := lookupCheck(id).run(ctx, s, &checkPlan{}); err != nil {
			t.Fatal(err)
		}
	}
	for i, name := range []string{"qa-toolchain.bash", "qa-go-qa.bash", "qa-bash-limits.bash", "qa-ansible.bash", "qa-docs.bash", "qa-nokill-containerwatch.bash", "qa-ansible-syntax.bash", "qa-deployed-drift.bash", "qa-ansible-pause.bash", "qa-gh-scopes.bash", "qa-find-race.bash", "qa-arch-pin.bash", "qa-argv-leak.bash", "qa-place-issued-token.bash", "qa-boot-hold.bash", "qa-gh-poll.bash", "qa-secret-screen.bash", "qa-temp-exclusive.bash", "qa-ready-wait-rules.bash", "qa-plan-script-logging.bash", "qa-speech-to-text-rules.bash", "qa-walk-open.bash", "qa-stdin-wait.bash", "qa-hook-handler-rules.bash", "qa-version-pins.bash", "qa-vmtest-manifest.bash", "qa-go-deps.bash", "qa-go-vuln.bash", "qa-teamchat-separate.bash", "qa-seat-text.bash", "qa-hook-handlers.bash", "qa-plan-tests.bash", "qa-lxc-attach-streams.bash", "qa-gitleaks.bash"} {
		argv := runner.calls[i].Argv
		if argv[0] != "bash" || argv[1] != filepath.Join(s.scripts, name) {
			t.Fatalf("call %d: %v", i, argv)
		}
		if !runner.calls[i].Merge {
			t.Fatalf("call %d: output must be merged", i)
		}
	}
	for _, i := range []int{3, 4, 6} {
		env := ownEnv(runner.calls[i].Env)
		if len(env) != 1 || !strings.HasPrefix(env[0], "QA_JSON_OUT=") {
			t.Fatalf("the JSON-report gates need QA_JSON_OUT: %v", env)
		}
	}
	s.cleanup()
	if _, err := os.Stat(strings.TrimPrefix(runner.calls[3].Env[0], "QA_JSON_OUT=")); !os.IsNotExist(err) {
		t.Fatalf("the report file must be removed on cleanup: %v", err)
	}
}

// B68 (issue #428): the ccy version check judges the change from the same base qa-changed diffed
// against (CI's diff mode: the protected line's tip), so the pre-commit version gates hold in CI
// where no hook ran; its verdict is the gate's.
func TestCcyVersionBumpJudgesTheChangeFromTheBase(t *testing.T) {
	runner := &scriptedRunner{replies: map[string]proc.Result{}}
	s := newTestSession(t, runner)
	s.opts.base = "origin/F44"
	gate := "bash " + filepath.Join(s.scripts, "qa-ccy-version-bump.bash") + " origin/F44"
	runner.replies[gate] = proc.Result{Stdout: "COMMIT REJECTED [CCY-IMAGE-INPUT]\n", Code: 1}
	res, err := lookupCheck("ccy-version-bump").run(context.Background(), s, &checkPlan{})
	if err != nil {
		t.Fatal(err)
	}
	if res.code != 1 || !strings.Contains(res.output, "CCY-IMAGE-INPUT") {
		t.Fatalf("got %+v", res)
	}
	if !reflect.DeepEqual(runner.argv(), []string{gate}) || !runner.calls[0].Merge {
		t.Fatalf("one gate run against the base, output merged: %+v", runner.calls)
	}
}

// The Go gate is scoped to the same change qa-changed collected: go-qa's diff mode against the
// same base, which sees only src-go/. A file outside it that routes to the go check (a parity
// reference or a table the Go tests read) adds the packages its row of the real routing table
// names (--also-packages); a file whose row says @full (the routing tables, the gate's wrapper,
// .qa-versions) runs the whole gate, as does an explicit file list, which is no diff. Each row is
// classified by the real table, so a row that lost its packages or its @full fails here.
func TestTheGoGateRunsGoQaOverTheSameChange(t *testing.T) {
	diff := []string{"--changed", "--base", "origin/F44"}
	also := func(pkgs string) []string { return append(slices.Clone(diff), "--also-packages", pkgs) }
	launcher := "internal/ccclaude,internal/ccylaunch/...,internal/ccylimits,internal/registry,internal/restore,internal/screen,internal/sessions,internal/transcript"
	for _, c := range []struct {
		name     string
		explicit bool
		files    []string
		want     []string
	}{
		{"src-go only: diff mode, nothing added", false, []string{"src-go/internal/ui/ui.go", "src-go/qa/go.mod"}, diff},
		{"a mapped outside file: diff mode with its packages", false, []string{"files/var/local/claude-yolo/lib/common-pure.bash"}, also(launcher)},
		{"a mapped outside file beside src-go", false, []string{"src-go/internal/ui/ui.go", "helpers/version_pins/manifest.py"}, also("qa/cmd/qa-version-pins")},
		{"three mapped rows: their packages once each, sorted", false, []string{"tests/fixtures/ansible-sudoers/x.yml", "vars/golang.yml", ".github/workflows/qa.yml"}, also("qa/ansible,qa/pushgate,qa/toolchain")},
		{"a routing table: the whole gate", false, []string{"src-go/internal/ui/ui.go", "scripts/qa-gates.tsv"}, nil},
		{"the gate's own pins: the whole gate", false, []string{"vars/golang.yml", ".qa-versions"}, nil},
		{"the gate's wrapper: the whole gate", false, []string{"scripts/qa-go-qa.bash"}, nil},
		{"an explicit list is no diff: the whole gate", true, []string{"files/var/local/claude-yolo/lib/common-pure.bash"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			for _, f := range c.files {
				w.touch(f, "# x")
			}
			s := w.session()
			s.opts = options{base: "origin/F44", explicit: c.explicit, files: c.files}
			if err := s.classify(context.Background(), collected{existing: c.files}); err != nil {
				t.Fatal(err)
			}
			cp := s.plan.checks["go"]
			if cp == nil {
				t.Fatalf("%v: no go check planned", c.files)
			}
			if _, err := lookupCheck("go").run(context.Background(), s, cp); err != nil {
				t.Fatal(err)
			}
			calls := w.runner.calls
			want := append([]string{"bash", filepath.Join(s.scripts, "qa-go-qa.bash")}, c.want...)
			if got := calls[len(calls)-1].Argv; !reflect.DeepEqual(got, want) {
				t.Fatalf("argv %v, want %v", got, want)
			}
		})
	}
}

// An outside file no routing row named packages for (a plan built some other way) is still run:
// the whole gate, never a diff that cannot see it.
func TestTheGoGateRunsWholeForAnOutsideFileNoRowNamed(t *testing.T) {
	runner := &scriptedRunner{}
	s := newTestSession(t, runner)
	s.opts = options{base: "HEAD"}
	if _, err := lookupCheck("go").run(context.Background(), s, &checkPlan{items: []string{"src-go/x.go", "helpers/unnamed.py"}}); err != nil {
		t.Fatal(err)
	}
	if got, want := runner.calls[0].Argv, []string{"bash", filepath.Join(s.scripts, "qa-go-qa.bash")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("argv %v, want %v", got, want)
	}
}

func TestSemgrepMirrorsFilesWithABashExtensionAndReadsItsJSON(t *testing.T) {
	runner := &scriptedRunner{}
	s := newTestSession(t, runner)
	for name, body := range map[string]string{"tool": "#!/usr/bin/bash\n:\n", "dir/x.bash": ":\n"} {
		writeFile(t, s.root, name, body)
	}
	var mirror string
	runner.onRun = func(_ context.Context, c proc.Cmd) {
		mirror = c.Dir
		for _, name := range []string{"tool.bash", "dir/x.bash"} {
			if _, err := os.Stat(filepath.Join(c.Dir, name)); err != nil {
				t.Errorf("mirror lacks %s: %v", name, err)
			}
		}
		out := c.Argv[slices.Index(c.Argv, "--json-output")+1]
		body := mirrorReport(c.Dir, `{"path":"tool.bash","start":{"line":3},"check_id":"rule.x","extra":{"message":"do not"}}`, "")
		if err := os.WriteFile(out, []byte(body), 0o600); err != nil {
			t.Error(err)
		}
	}
	res, err := lookupCheck("semgrep").run(context.Background(), s, &checkPlan{items: []string{"tool", "dir/x.bash"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.code != 1 || !strings.Contains(res.output, "tool.bash:3 rule.x do not") {
		t.Fatalf("a finding must fail the check: %+v", res)
	}
	argv := runner.calls[0].Argv
	if argv[0] != "semgrep" || argv[2] != filepath.Join(s.root, ".semgrep", "bash-conventions.yml") || argv[len(argv)-1] != "." {
		t.Fatalf("argv: %v", argv)
	}
	if !slices.Contains(argv, semgrepreport.TimeoutFlag) {
		t.Fatalf("argv lacks %s, so a loaded host turns a clean file into a timeout: %v", semgrepreport.TimeoutFlag, argv)
	}
	s.cleanup()
	if _, err := os.Stat(mirror); !os.IsNotExist(err) {
		t.Fatalf("the mirror must be removed on cleanup: %v", err)
	}
}

// mirrorReport is semgrep's report on the mirror at dir: every file in it scanned (but the ignore
// file), with the results and errors given as JSON list bodies.
func mirrorReport(dir, results, errs string) string {
	var scanned []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || d.Name() == ".semgrepignore" {
			return walkErr
		}
		rel, relErr := filepath.Rel(dir, path)
		scanned = append(scanned, rel)
		return relErr
	})
	if err != nil {
		panic(err)
	}
	paths, err := json.Marshal(scanned)
	if err != nil {
		panic(err)
	}
	return `{"results":[` + results + `],"errors":[` + errs + `],"paths":{"scanned":` + string(paths) + `}}`
}

// semgrepOn runs the semgrep check over a.bash and b.bash with semgrep's report made by report
// from the mirror's directory.
func semgrepOn(t *testing.T, report func(mirror string) string) checkResult {
	t.Helper()
	runner := &scriptedRunner{}
	s := newTestSession(t, runner)
	defer s.cleanup()
	writeFile(t, s.root, "a.bash", ":\n")
	writeFile(t, s.root, "tests/b.bash", ":\n")
	runner.onRun = func(_ context.Context, cmd proc.Cmd) {
		if _, err := os.Stat(filepath.Join(cmd.Dir, ".semgrepignore")); err != nil {
			t.Errorf("the mirror has no .semgrepignore, so semgrep's default ignores (tests/ among them) apply: %v", err)
		}
		if err := os.WriteFile(cmd.Argv[slices.Index(cmd.Argv, "--json-output")+1], []byte(report(cmd.Dir)), 0o600); err != nil {
			t.Error(err)
		}
	}
	res, err := lookupCheck("semgrep").run(context.Background(), s, &checkPlan{items: []string{"a.bash", "tests/b.bash"}})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// semgrep exits 0 on a file it could not parse; its errors say no rule ran there, so the check
// fails, naming the file.
func TestSemgrepErrorsFailTheCheck(t *testing.T) {
	for name, errs := range map[string]string{
		"syntax error": `{"type":"Syntax error","path":"a.bash","message":"cannot parse","spans":[{"start":{"line":4}}]}`,
		"timeout":      `{"type":"Timeout","path":"a.bash","message":"rule x timed out"}`,
	} {
		res := semgrepOn(t, func(dir string) string { return mirrorReport(dir, "", errs) })
		if res.code == 0 || !strings.Contains(res.output, "a.bash") {
			t.Errorf("%s: a file semgrep did not read passed: %+v", name, res)
		}
	}
}

func TestAFileSemgrepDidNotScanFailsTheCheck(t *testing.T) {
	res := semgrepOn(t, func(string) string { return `{"results":[],"errors":[],"paths":{"scanned":["a.bash"]}}` })
	if res.code == 0 || !strings.Contains(res.output, "tests/b.bash") || !strings.Contains(res.output, "did not scan") {
		t.Fatalf("an unscanned file passed: %+v", res)
	}
}

func TestAReportWithoutPathsFailsTheCheck(t *testing.T) {
	res := semgrepOn(t, func(string) string { return `{"results":[]}` })
	if res.code == 0 || !strings.Contains(res.output, "cannot read semgrep's report") {
		t.Fatalf("a report that cannot say what was read passed: %+v", res)
	}
}

// A partial parse is judged by the full patterns gate against its allowlist, so here it is shown,
// not failed: failing it would refuse a file the full gate accepts.
func TestAPartialParseIsShownAndLeftToThePatternsGate(t *testing.T) {
	partial := `{"type":["PartialParsing",[{"path":"a.bash","start":{"line":2},"end":{"line":3}}]],"path":"a.bash","message":"Syntax error at line a.bash:2","spans":[{"start":{"line":2}}]}`
	res := semgrepOn(t, func(dir string) string { return mirrorReport(dir, "", partial) })
	if res.code != 0 || !strings.Contains(res.output, "a.bash:2") || !strings.Contains(res.output, "patterns-partial.allowlist") {
		t.Fatalf("a partial parse must pass with a note: %+v", res)
	}
}

func TestSemgrepFailuresAreFailuresNotPasses(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		reply proc.Result
		json  string
		want  string
	}{
		"semgrep itself failed": {proc.Result{Stdout: "boom", Code: 2}, "", "semgrep failed (rc=2)"},
		"malformed json":        {proc.Result{}, "{not json", "cannot read semgrep's report"},
		"no report written":     {proc.Result{}, "", "cannot read semgrep's report"},
	} {
		runner := &scriptedRunner{replies: map[string]proc.Result{"semgrep": c.reply}}
		s := newTestSession(t, runner)
		writeFile(t, s.root, "a.bash", ":\n")
		runner.onRun = func(_ context.Context, cmd proc.Cmd) {
			if c.json != "" {
				if err := os.WriteFile(cmd.Argv[slices.Index(cmd.Argv, "--json-output")+1], []byte(c.json), 0o600); err != nil {
					t.Error(err)
				}
			}
		}
		res, err := lookupCheck("semgrep").run(ctx, s, &checkPlan{items: []string{"a.bash"}})
		if err != nil || res.code == 0 || !strings.Contains(res.output, c.want) {
			t.Errorf("%s: %+v %v", name, res, err)
		}
		s.cleanup()
	}
	clean := &scriptedRunner{}
	s := newTestSession(t, clean)
	writeFile(t, s.root, "a.bash", ":\n")
	clean.onRun = func(_ context.Context, cmd proc.Cmd) {
		if err := os.WriteFile(cmd.Argv[slices.Index(cmd.Argv, "--json-output")+1], []byte(mirrorReport(cmd.Dir, "", "")), 0o600); err != nil {
			t.Error(err)
		}
	}
	if res, err := lookupCheck("semgrep").run(ctx, s, &checkPlan{items: []string{"a.bash"}}); err != nil || res.code != 0 {
		t.Fatalf("no findings is a pass: %+v %v", res, err)
	}
	s.cleanup()
	if _, err := lookupCheck("semgrep").run(ctx, newTestSession(t, clean), &checkPlan{items: []string{"missing.bash"}}); err == nil {
		t.Fatal("a file that cannot be mirrored is an error")
	}
}

func TestRunnerStartFailuresSurface(t *testing.T) {
	s := newTestSession(t, failingRunner{})
	for _, id := range []string{"toolchain", "bash-syntax", "shellcheck", "ansible-syntax", "eslint", "ruff", "unit-tests"} {
		if _, err := lookupCheck(id).run(context.Background(), s, &checkPlan{items: []string{"x"}}); err == nil {
			t.Errorf("%s swallowed a start failure", id)
		}
	}
}

// The toolchain check is a whole gate (a deleted pin file may ask for it) and needs bash and go,
// because its wrapper builds the Go command.
func TestTheToolchainCheckIsAWholeGateNeedingGo(t *testing.T) {
	d := lookupCheck("toolchain")
	if !d.gate || !d.whole {
		t.Errorf("toolchain: gate %v whole %v", d.gate, d.whole)
	}
	if !slices.Contains(d.tools, toolBash) || !slices.Contains(d.tools, toolGo) {
		t.Errorf("toolchain needs bash and go, has %v", d.tools)
	}
}

// The VM-test manifest check is a whole gate (a deleted guest script may owe it) and needs bash,
// go (its wrapper builds the Go command) and python3 (the host's validator it runs).
func TestTheVmtestManifestCheckIsAWholeGateNeedingGoAndPython(t *testing.T) {
	d := lookupCheck("vmtest-manifest")
	if !d.gate || !d.whole {
		t.Errorf("vmtest-manifest: gate %v whole %v", d.gate, d.whole)
	}
	for _, tool := range []string{toolBash, toolGo, toolPython} {
		if !slices.Contains(d.tools, tool) {
			t.Errorf("vmtest-manifest needs %s, has %v", tool, d.tools)
		}
	}
}

// The helper-tests gate is not a qa-changed check (SPEC-qa-helper-tests B9): qa-changed runs the
// mirrored unit-tests module of each changed helper and leaves the whole suite to qa-all, whose
// table (qa-gates.tsv, kind helpers) owns it. Adding a check is a decision that comes with routing
// rows, a golden deviation and a SPEC-qa-changed B-row, not a side effect of the port.
func TestTheHelperTestsGateIsNotAChangedCheck(t *testing.T) {
	for _, d := range checkDefs {
		if strings.Contains(d.id, "helper") {
			t.Errorf("check %q: the helper-tests gate is not routed by qa-changed", d.id)
		}
	}
}

// The docs check is a whole gate (a deleted document or playbook may ask for it) and needs bash and
// go, because its wrapper builds the Go command; it is run through the wrapper with the report.
func TestTheDocsCheckIsAWholeGateNeedingGoAndRunsItsWrapper(t *testing.T) {
	d := lookupCheck("docs")
	if !d.gate || !d.whole {
		t.Errorf("docs: gate %v whole %v", d.gate, d.whole)
	}
	if !slices.Contains(d.tools, toolBash) || !slices.Contains(d.tools, toolGo) {
		t.Errorf("docs needs bash and go, has %v", d.tools)
	}
	runner := &scriptedRunner{}
	s := newTestSession(t, runner)
	if _, err := d.run(context.Background(), s, &checkPlan{}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 || runner.calls[0].Argv[0] != "bash" || runner.calls[0].Argv[1] != filepath.Join(s.scripts, "qa-docs.bash") {
		t.Fatalf("calls: %+v", runner.calls)
	}
	if env := ownEnv(runner.calls[0].Env); len(env) != 1 || !strings.HasPrefix(env[0], "QA_JSON_OUT=") {
		t.Fatalf("the docs gate writes a report: %v", env)
	}
}

type failingRunner struct{}

func (failingRunner) Run(context.Context, proc.Cmd) (proc.Result, error) {
	return proc.Result{}, os.ErrNotExist
}

// A missing tool is reported with how to get it, so every tool a check needs has a fix line
// (eslint is a path in the checkout, which its preflight names itself).
func TestEveryToolACheckNeedsSaysHowToGetIt(t *testing.T) {
	for _, d := range checkDefs {
		for _, tool := range d.tools {
			if tool == eslintTool {
				continue
			}
			if toolFix[tool] == "" {
				t.Errorf("check %s needs %s, which toolFix gives no fix for", d.id, tool)
			}
		}
	}
}
