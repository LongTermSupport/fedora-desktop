package playlock

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// repoRoot walks up from the test's directory to the checkout holding the real lock.py.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "helpers", "play_lock", "lock.py")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no helpers/play_lock/lock.py above the test directory")
		}
		dir = parent
	}
}

// lockPy runs the real helper as run.bash does, for this test's user and runtime directory.
func lockPy(t *testing.T, r *rig, sub string, files []*os.File, env []string) (execx.Result, error) {
	t.Helper()
	root := repoRoot(t)
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	return execx.OS{}.Run(context.Background(), execx.Cmd{
		Name: "python3", Args: []string{"-P", "-m", "helpers.play_lock.lock", sub, "--user", me.Username},
		Env: append([]string{"PYTHONPATH=" + root, "XDG_RUNTIME_DIR=" + r.dir}, env...), Unset: []string{FDEnv},
		Dir: root, ExtraFiles: files,
	})
}

// The lock is lock.py's: the same path, and a hold delegated from Go is one run.bash's
// `lock.py held` proves, so the play it runs goes on inside the hold instead of exiting 75.
func TestTheHelperRunBashAsksFindsTheSameLockAndAcceptsTheDelegatedHold(t *testing.T) {
	r := newRig(t)
	res, err := lockPy(t, r, "path", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(res.Stdout)); got != filepath.Join(r.dir, Name) {
		t.Errorf("lock.py path %q, Go uses %q", got, filepath.Join(r.dir, Name))
	}
	lock, err := r.w.Take(t.Context(), "ccy-teams apply alpha", time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if relErr := lock.Release(); relErr != nil {
			t.Error(relErr)
		}
	}()
	files, env := lock.Delegate()
	if _, err = lockPy(t, r, "held", files, env); err != nil {
		t.Fatalf("lock.py held refused the delegated hold: %v", err)
	}
	_, err = lockPy(t, r, "held", nil, []string{FDEnv + "=3"})
	if ee, ok := errors.AsType[*execx.ExitError](err); !ok || ee.Code != 3 {
		t.Errorf("with no descriptor handed down, lock.py held gave %v, want exit 3", err)
	}
}
