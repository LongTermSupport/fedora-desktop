package archpin

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

// rulesText is a rule file shaped like the real one; DSTAR stands for the glob stars.
var rulesText = strings.ReplaceAll(`rules:
  - id: arch-pinned-artefact
    message: x
    paths:
      exclude:
        - '.semgrep/arch-pin.DSTAR'
        - 'scripts/arch-pin.allowlist'
        - 'docs/DSTAR'
    languages: [generic]
`, "DSTAR", "**")

// world is a repository on disk plus the fake git and semgrep that look at it.
type world struct {
	t    *testing.T
	root string
	run  *fakeRun
}

// newWorld writes files (relative path to text) under a fresh root, with the rule, its fixture
// and an empty allowlist unless the map overrides them (an empty text removes the file).
func newWorld(t *testing.T, files map[string]string) *world {
	t.Helper()
	all := map[string]string{
		ruleFile:      rulesText,
		fixtureFile:   "# fixture\n# ruleid: arch-pinned-artefact\n",
		allowlistFile: "# ratchet\n",
		"a.txt":       "nothing\n",
		"docs/x.md":   "prose\n",
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

// hit is one semgrep match.
type hit struct {
	path string
	line int
}

// fakeRun stands in for git and semgrep.
type fakeRun struct {
	listed []string

	gitCode     int
	noSemgrep   bool
	versionCode int
	testCode    int
	scanCode    int
	timeout     bool
	// hits are what the scan reports; drop are handed files it leaves out of paths.scanned.
	hits    []hit
	drop    []string
	errs    []string
	garbage bool

	handed []string
}

func (f *fakeRun) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	switch c.Argv[0] {
	case "git":
		if f.gitCode != 0 {
			return proc.Result{Code: f.gitCode, Stderr: "fatal: not a git repository"}, nil
		}
		return proc.Result{Stdout: strings.Join(f.listed, "\x00") + "\x00"}, nil
	case "semgrep":
		return f.semgrep(c)
	}
	return proc.Result{}, fmt.Errorf("unexpected program %q", c.Argv[0])
}

func (f *fakeRun) semgrep(c proc.Cmd) (proc.Result, error) {
	switch {
	case f.noSemgrep:
		return proc.Result{}, errors.New("executable file not found in $PATH")
	case slices.Contains(c.Argv, "--version"):
		return proc.Result{Code: f.versionCode, Stdout: "1.0.0\n"}, nil
	case slices.Contains(c.Argv, "--test"):
		return proc.Result{Code: f.testCode, Stdout: "1/1: test output\n"}, nil
	case f.timeout:
		return proc.Result{TimedOut: true}, nil
	}
	f.handed = c.Argv[slices.Index(c.Argv, "--max-target-bytes=0")+1:]
	if f.garbage {
		return proc.Result{Code: f.scanCode, Stdout: "not json"}, nil
	}
	scanned := slices.DeleteFunc(slices.Clone(f.handed), func(p string) bool { return slices.Contains(f.drop, p) })
	res := make([]string, 0, len(f.hits))
	for _, h := range f.hits {
		res = append(res, fmt.Sprintf(`{"check_id":"tmp.arch-pinned-artefact","path":%q,"start":{"line":%d}}`, h.path, h.line))
	}
	sc, err := json.Marshal(scanned)
	if err != nil {
		return proc.Result{}, err
	}
	body := fmt.Sprintf(`{"results":[%s],"errors":[%s],"paths":{"scanned":%s}}`, strings.Join(res, ","), strings.Join(f.errs, ","), sc)
	return proc.Result{Code: f.scanCode, Stdout: body}, nil
}
