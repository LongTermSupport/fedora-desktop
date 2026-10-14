package ghscopes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// rulesText is a rule file shaped like the real one; DSTAR and STAR stand for the glob stars.
var rulesText = strings.NewReplacer("DSTAR", "**", "STAR", "*").Replace(`rules:
  - id: gh-scope-outside-ssot
    message: x
    paths:
      exclude:
        # a comment between entries
        - 'vars/github-required-scopes.yml'
        - 'helpers/github_scopes/DSTAR'
        - '.semgrep/gh-scopes.STAR'
        - 'CLAUDE/Plan/DSTAR/STAR.md'
        - "untracked/DSTAR"
    languages: [generic]
`)

// world is a repository on disk plus the fake git and semgrep that look at it.
type world struct {
	t    *testing.T
	root string
	run  *fakeRun
}

// newWorld writes files (relative path to text) under a fresh root, with the rule and its
// fixture unless the map overrides them (an empty text removes the file).
func newWorld(t *testing.T, files map[string]string) *world {
	t.Helper()
	all := map[string]string{
		ruleFile:    rulesText,
		fixtureFile: "# fixture\n# ruleid: gh-scope-outside-ssot\n",
		"a.txt":     "nothing\n",
	}
	for k, v := range files {
		if v == "" {
			delete(all, k)
		} else {
			all[k] = v
		}
	}
	w := &world{t: t, root: t.TempDir()}
	listed := make([]string, 0, len(all))
	for rel, text := range all {
		w.write(rel, text)
		listed = append(listed, rel)
	}
	slices.Sort(listed)
	w.run = &fakeRun{listed: listed}
	return w
}

func (w *world) write(rel, text string) {
	w.t.Helper()
	path := filepath.Join(w.root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) link(rel, target string) {
	w.t.Helper()
	if err := os.Symlink(target, filepath.Join(w.root, rel)); err != nil {
		w.t.Fatal(err)
	}
	w.run.listed = append(w.run.listed, rel)
}

type result struct {
	code           ExitCode
	stdout, stderr string
}

func (w *world) main(args ...string) result {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	code := Main(args, Env{Stdout: &stdout, Stderr: &stderr, Cwd: w.root, Runner: w.run})
	return result{code, stdout.String(), stderr.String()}
}

func wantExit(t *testing.T, got result, code ExitCode) {
	t.Helper()
	if got.code != code {
		t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", got.code, code, got.stdout, got.stderr)
	}
}

func wantIn(t *testing.T, stream, text string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(text, p) {
			t.Fatalf("%s lacks %q:\n%s", stream, p, text)
		}
	}
}

// fakeRun stands in for git and semgrep.
type fakeRun struct {
	listed []string

	gitCode int
	gitErr  string

	noSemgrep   bool
	versionCode int
	versionErr  string
	testCode    int
	testOut     string
	scanCode    int
	scanStderr  string
	timeout     string
	// scan returns the JSON semgrep prints, given the files it was handed; nil reads them all
	// except the rule's own (which its excludes drop).
	scan func(files []string) string

	calls    [][]string
	dirs     []string
	scanArgv []string
}

func (f *fakeRun) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	f.calls = append(f.calls, c.Argv)
	f.dirs = append(f.dirs, c.Dir)
	switch c.Argv[0] {
	case "git":
		if f.gitCode != 0 {
			return proc.Result{Code: f.gitCode, Stderr: f.gitErr}, nil
		}
		return proc.Result{Stdout: strings.Join(f.listed, "\x00") + "\x00"}, nil
	case "semgrep":
		return f.semgrep(c)
	}
	return proc.Result{}, fmt.Errorf("unexpected program %q", c.Argv[0])
}

func (f *fakeRun) semgrep(c proc.Cmd) (proc.Result, error) {
	if f.noSemgrep {
		return proc.Result{}, errors.New("executable file not found in $PATH")
	}
	switch {
	case slices.Contains(c.Argv, "--version"):
		return proc.Result{Code: f.versionCode, Stdout: "1.0.0\n", Stderr: f.versionErr}, nil
	case slices.Contains(c.Argv, "--test"):
		if f.timeout == "test" {
			return proc.Result{TimedOut: true}, nil
		}
		return proc.Result{Code: f.testCode, Stdout: f.testOut}, nil
	}
	f.scanArgv = c.Argv
	if f.timeout == "scan" {
		return proc.Result{TimedOut: true}, nil
	}
	body := report(scannable(handed(c.Argv)), nil, nil)
	if f.scan != nil {
		body = f.scan(handed(c.Argv))
	}
	return proc.Result{Code: f.scanCode, Stdout: body, Stderr: f.scanStderr}, nil
}

// handed is the files an argv names after the flags.
func handed(argv []string) []string {
	return argv[slices.Index(argv, "--max-target-bytes=0")+1:]
}

// scannable drops the files the test rule excludes by its .semgrep glob.
func scannable(files []string) []string {
	return slices.DeleteFunc(slices.Clone(files), func(f string) bool { return strings.HasPrefix(f, ".semgrep/") })
}

func (f *fakeRun) called(prefix ...string) bool {
	return slices.ContainsFunc(f.calls, func(c []string) bool {
		return len(c) >= len(prefix) && slices.Equal(c[:len(prefix)], prefix)
	})
}

// hit is one semgrep match.
type hit struct {
	path string
	line int
}

// report is the JSON semgrep prints: what it scanned, what it skipped, what matched.
func report(scanned []string, results []hit, skipped []string, errs ...string) string {
	res := make([]string, 0, len(results))
	for _, r := range results {
		res = append(res, fmt.Sprintf(`{"check_id":"tmp.rules.gh-scope-outside-ssot","path":%q,"start":{"line":%d}}`, r.path, r.line))
	}
	sk := make([]string, 0, len(skipped))
	for _, s := range skipped {
		sk = append(sk, fmt.Sprintf(`{"path":%q,"reason":"too big"}`, s))
	}
	sc, err := json.Marshal(scanned)
	if err != nil {
		panic(err)
	}
	if scanned == nil {
		sc = []byte("[]")
	}
	return fmt.Sprintf(`{"results":[%s],"errors":[%s],"paths":{"scanned":%s,"skipped":[%s]}}`, strings.Join(res, ","), strings.Join(errs, ","), sc, strings.Join(sk, ","))
}
