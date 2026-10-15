package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/patterns"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(patterns.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(patterns.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

// TestRunOnARealGitTree runs the command over a real work tree. semgrep is the tool the gate
// needs; a machine without it fails here with the gate's own message, as the gate itself would.
func TestRunOnARealGitTree(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: append([]string{"git"}, args...), Dir: root})
		if err != nil || res.Code != 0 {
			t.Fatalf("git %v: %v %+v", args, err, res)
		}
	}
	git("init", "-q")
	files := map[string]string{
		"scripts/good.bash":                  "#!/usr/bin/bash\nset -e\necho hi\n",
		"scripts/qa-trees.tsv":               "shell\t.semgrep\n",
		"scripts/patterns-partial.allowlist": "# none\n",
		".semgrep/bash-conventions.yml":      realRules,
		".semgrep/bash-conventions.bash":     realFixture,
	}
	for rel, text := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("QA_JSON_OUT", filepath.Join(t.TempDir(), "report.json"))
	if code := run([]string{"--root", root}); code != int(patterns.ExitOK) {
		t.Fatalf("exit %d", code)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts/bad.bash"), []byte("#!/usr/bin/bash\nfalse || echo oops\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--root", root}); code != int(patterns.ExitFailed) {
		t.Fatalf("a '|| echo' script: exit %d", code)
	}
}

// A one-rule copy of the project's ruleset and its fixture: the same rule id and the same
// annotation grammar semgrep --test reads.
const realRules = `rules:
  - id: bash-error-hiding-pipe-echo
    pattern-regex: '\|\|[ \t]*echo\b'
    message: Error hiding.
    languages: [bash]
    severity: ERROR
`

const realFixture = `#!/usr/bin/bash
# ruleid: bash-error-hiding-pipe-echo
false || echo oops
# ok: bash-error-hiding-pipe-echo
false || exit 1
`
