package golden

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// nowVar carries the step's clock to an external implementation.
const nowVar = "PINGBUS_ORACLE_NOW_MS"

// pythonPrelude fixes the zipapp's clock at the step's instant and runs it as `__main__`. The
// half millisecond makes int(time.time() * 1000) come out at now_ms exactly.
const pythonPrelude = `import os, runpy, sys, time
_now = (int(os.environ.pop("` + nowVar + `")) + 0.5) / 1000
time.time = lambda: _now
sys.argv = sys.argv[1:]
runpy.run_path(sys.argv[0], run_name="__main__")
`

// execRender runs an external command through execx, in the world, with exactly the step's
// environment and its clock in nowVar. The event is the last argument; for the Python, after
// `hook`.
func execRender(argv []string, python bool) renderer {
	return func(ctx context.Context, root, event string, env map[string]string, stdin []byte, nowMs int64) ([]byte, []byte, int, error) {
		add := make([]string, 0, len(env)+1)
		for k, v := range env {
			add = append(add, k+"="+v)
		}
		add = append(add, nowVar+"="+strconv.FormatInt(nowMs, 10))
		args := append(append([]string{}, argv[1:]...), event)
		if python {
			args = append(append([]string{}, argv[1:]...), "hook", event)
		}
		cmd := execx.Cmd{
			Name: argv[0], Args: args, Env: add, Unset: unsetFor(env), Dir: root,
			Stdin: bytes.NewReader(stdin),
		}
		res, err := execx.OS{}.Run(ctx, cmd)
		var exitErr *execx.ExitError
		switch {
		case err == nil:
			return res.Stdout, res.Stderr, 0, nil
		case errors.As(err, &exitErr) && exitErr.Signal == 0:
			return res.Stdout, res.Stderr, exitErr.Code, nil
		}
		return nil, nil, 0, err
	}
}

// unsetFor names the variables of this process the child must not inherit.
func unsetFor(env map[string]string) []string {
	var names []string
	for _, kv := range os.Environ() {
		name, _, found := strings.Cut(kv, "=")
		if _, ok := env[name]; found && !ok {
			names = append(names, name)
		}
	}
	return names
}

// pythonCommand builds the Python zipapp from this checkout with the bundle tool, as the play
// does, and returns the command that runs it under the prelude.
func pythonCommand(t *testing.T) []string {
	t.Helper()
	repo := repoRoot(t)
	out := filepath.Join(t.TempDir(), "pingbus.pyz")
	build := execx.Cmd{
		Name: "python3",
		Args: []string{"-m", "helpers.pingbus.bundle", "--source", repo, "--out", out, "pingbus"},
		Dir:  repo,
	}
	if _, err := (execx.OS{}).Run(context.Background(), build); err != nil {
		t.Fatalf("build the Python zipapp: %v", err)
	}
	return []string{"python3", "-c", pythonPrelude, out}
}

// repoRoot walks up from the package directory to the checkout that holds the bundle tool.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "helpers", "pingbus", "bundle.py")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no checkout with helpers/pingbus/bundle.py above the package")
		}
		dir = parent
	}
}
