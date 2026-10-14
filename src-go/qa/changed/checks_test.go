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
}

func (r *scriptedRunner) Run(ctx context.Context, c proc.Cmd) (proc.Result, error) {
	r.calls = append(r.calls, c)
	if r.semgrepReport && c.Argv[0] == "semgrep" {
		if err := os.WriteFile(c.Argv[slices.Index(c.Argv, "--json-output")+1], []byte(`{"results":[]}`), 0o600); err != nil {
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
		"toolchain", "bash-syntax", "shellcheck", "semgrep", "ruff", "unit-tests", "ansible-syntax",
		"ansible-patterns", "patterns", "python", "docs", "go", "bash-limits", "nokill-containerwatch", "node-check", "eslint",
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
		"toolchain":        "QA tool versions match .qa-versions",
		"bash-syntax":      "2 file(s)",
		"unit-tests":       "2 module(s)",
		"ansible-syntax":   "2 playbook(s)",
		"ansible-patterns": "fail-fast patterns, whole gate",
		"patterns":         "bash-convention rules, whole gate",
		"python":           "compile and ruff, whole gate",
		"docs":             "whole gate",
		"go":               "whole gate (interim qa-go.bash)",
		"bash-limits":      "whole gate (tooling is Go; allowlist ratchet)",
		"eslint":           "2 file(s)",

		"nokill-containerwatch": "whole gate (the container-watch watchdog never signals a process)",
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
	return &session{env: Env{Runner: runner}, root: root, scripts: scripts}
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
		"shellcheck --external-sources --severity=warning a.bash b.bash",
		"ruff check x.py",
		"python3 -m unittest tests.helpers.test_a",
	}
	got := runner.argv()
	slices.Sort(got)
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	for _, c := range runner.calls {
		if c.Argv[0] == "python3" && !reflect.DeepEqual(c.Env, []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}) {
			t.Fatalf("unit tests must not read the git configuration: %v", c.Env)
		}
	}
}

func TestEslintRunsInExtensionsWithPrefixStripped(t *testing.T) {
	runner := &scriptedRunner{}
	s := newTestSession(t, runner)
	_, err := lookupCheck("eslint").run(context.Background(), s, &checkPlan{items: []string{"extensions/a@b/x.js"}, rel: "extensions"})
	if err != nil {
		t.Fatal(err)
	}
	c := runner.calls[0]
	if c.Dir != filepath.Join(s.root, "extensions") || !reflect.DeepEqual(c.Argv, []string{"./node_modules/.bin/eslint", "a@b/x.js"}) {
		t.Fatalf("got %+v", c)
	}
}

func TestAnsibleSyntaxTriesEveryPlaybookAndNamesTheFailures(t *testing.T) {
	runner := &scriptedRunner{replies: map[string]proc.Result{"ansible-playbook --syntax-check b.yml": {Stdout: "noise", Stderr: "bad yaml\n", Code: 4}}}
	s := newTestSession(t, runner)
	res, err := lookupCheck("ansible-syntax").run(context.Background(), s, &checkPlan{items: []string{"a.yml", "b.yml", "c.yml"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.code == 0 || !strings.Contains(res.output, "^^ b.yml") || !strings.Contains(res.output, "bad yaml") || strings.Contains(res.output, "noise") {
		t.Fatalf("got %+v", res)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("every playbook is tried: %v", runner.argv())
	}
	for _, c := range runner.calls {
		if !reflect.DeepEqual(c.Env, []string{"ANSIBLE_CONFIG=" + filepath.Join(s.root, "ansible.cfg")}) {
			t.Fatalf("env: %v", c.Env)
		}
	}
}

func TestGatesRunTheirScriptFromTheScriptsDirectory(t *testing.T) {
	runner := &scriptedRunner{}
	s := newTestSession(t, runner)
	ctx := context.Background()
	for _, id := range []string{"toolchain", "go", "bash-limits", "ansible-patterns", "docs", "nokill-containerwatch"} {
		if _, err := lookupCheck(id).run(ctx, s, &checkPlan{}); err != nil {
			t.Fatal(err)
		}
	}
	for i, name := range []string{"qa-toolchain.bash", "qa-go.bash", "qa-bash-limits.bash", "qa-ansible.bash", "qa-docs.bash", "qa-nokill-containerwatch.bash"} {
		argv := runner.calls[i].Argv
		if argv[0] != "bash" || argv[1] != filepath.Join(s.scripts, name) {
			t.Fatalf("call %d: %v", i, argv)
		}
		if !runner.calls[i].Merge {
			t.Fatalf("call %d: output must be merged", i)
		}
	}
	for _, i := range []int{3, 4} {
		env := runner.calls[i].Env
		if len(env) != 1 || !strings.HasPrefix(env[0], "QA_JSON_OUT=") {
			t.Fatalf("the JSON-report gates need QA_JSON_OUT: %v", env)
		}
	}
	s.cleanup()
	if _, err := os.Stat(strings.TrimPrefix(runner.calls[3].Env[0], "QA_JSON_OUT=")); !os.IsNotExist(err) {
		t.Fatalf("the report file must be removed on cleanup: %v", err)
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
		body := `{"results":[{"path":"tool.bash","start":{"line":3},"check_id":"rule.x","extra":{"message":"do not"}}]}`
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
	s.cleanup()
	if _, err := os.Stat(mirror); !os.IsNotExist(err) {
		t.Fatalf("the mirror must be removed on cleanup: %v", err)
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
		if err := os.WriteFile(cmd.Argv[slices.Index(cmd.Argv, "--json-output")+1], []byte(`{"results":[]}`), 0o600); err != nil {
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

type failingRunner struct{}

func (failingRunner) Run(context.Context, proc.Cmd) (proc.Result, error) {
	return proc.Result{}, os.ErrNotExist
}
