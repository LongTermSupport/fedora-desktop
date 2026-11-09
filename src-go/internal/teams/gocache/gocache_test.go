package gocache

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const project = "parent-project"

type rig struct {
	*teamtest.World

	cache *Cache
}

func newRig(t *testing.T) rig {
	t.Helper()
	w := teamtest.New(t)
	return rig{World: w, cache: New(w.Host)}
}

// run is one launch's call: its exit code and its stdout.
func (r rig) run(t *testing.T, args ...string) (term.ExitCode, string) {
	t.Helper()
	r.Out.Reset()
	r.ErrOut.Reset()
	code := r.cache.Run(t.Context(), args)
	return code, r.Out.String()
}

// mustRun is a launch that must succeed.
func (r rig) mustRun(t *testing.T, args ...string) string {
	t.Helper()
	code, out := r.run(t, args...)
	if code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, r.ErrOut.String())
	}
	return out
}

// root is where the project's caches live on the host.
func (r rig) root() string { return filepath.Join(r.Env["HOME"], ".cache", "ccy", project, "go") }

// fill writes a file of n KiB under the named cache.
func (r rig) fill(t *testing.T, cache, name string, kib int) string {
	t.Helper()
	path := filepath.Join(r.root(), cache, name)
	teamtest.MustWrite(t, path, strings.Repeat("x", kib*1024))
	return path
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return err == nil
}

// readOnly takes write permission away from each path, as go leaves its module cache.
func readOnly(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := syscall.Chmod(p, 0o500); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPrintsTheRunArgumentsAndMakesTheDirectories(t *testing.T) {
	r := newRig(t)
	out := r.mustRun(t, project, "z")
	want := strings.Join([]string{
		"-v", r.root() + ":/var/cache/ccy-go:z",
		"-e", "GOCACHE=/var/cache/ccy-go/go-build",
		"-e", "GOLANGCI_LINT_CACHE=/var/cache/ccy-go/golangci-lint",
		"-e", "GOMODCACHE=/var/cache/ccy-go/mod",
		"-e", "GOQA_TOOLS_DIR=/var/cache/ccy-go/go-qa-tools",
	}, "\n") + "\n"
	if out != want {
		t.Fatalf("stdout\n%s\nwant\n%s", out, want)
	}
	for _, dir := range []string{"go-build", "golangci-lint", "mod", "go-qa-tools"} {
		info, err := os.Lstat(filepath.Join(r.root(), dir))
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("%s: mode %v", dir, info.Mode())
		}
	}
	if r.ErrOut.Len() != 0 {
		t.Fatalf("stderr %q on a launch under the bound", r.ErrOut.String())
	}
}

func TestNoRelabelLeavesTheMountUnlabelled(t *testing.T) {
	r := newRig(t)
	out := r.mustRun(t, project)
	if !strings.HasPrefix(out, "-v\n"+r.root()+":/var/cache/ccy-go\n-e\n") {
		t.Fatalf("stdout %q", out)
	}
}

func TestRefusesWhatTheLauncherCannotHaveMeant(t *testing.T) {
	for name, args := range map[string][]string{
		"no project":      {},
		"a path":          {"../etc"},
		"a dot":           {"a.b"},
		"private relabel": {project, "Z"},
		"other option":    {project, "ro"},
		"too many":        {project, "z", "z"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			code, out := r.run(t, args...)
			if code != term.ExitUsage || out != "" {
				t.Fatalf("exit %d stdout %q", code, out)
			}
			if !strings.Contains(r.ErrOut.String(), "ccy-teams: internal go-cache: ") {
				t.Fatalf("stderr %q", r.ErrOut.String())
			}
		})
	}
}

func TestAHomeTheMountCannotNameFails(t *testing.T) {
	for name, home := range map[string]string{
		"relative": "home",
		"colon":    "/tmp/a:b",
		"comma":    "/tmp/a,b",
		"newline":  "/tmp/a\nb",
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.Env["HOME"] = home
			code, out := r.run(t, project, "z")
			if code != term.ExitFailure || out != "" {
				t.Fatalf("exit %d stdout %q", code, out)
			}
			if !strings.Contains(r.ErrOut.String(), "HOME") {
				t.Fatalf("stderr %q", r.ErrOut.String())
			}
		})
	}
}

func TestASymlinkInPlaceOfTheCacheFails(t *testing.T) {
	r := newRig(t)
	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(r.root()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, r.root()); err != nil {
		t.Fatal(err)
	}
	code, out := r.run(t, project, "z")
	if code != term.ExitFailure || out != "" {
		t.Fatalf("exit %d stdout %q", code, out)
	}
	if !strings.Contains(r.ErrOut.String(), "not a directory") {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
}

func TestUnderTheBoundEverythingIsKept(t *testing.T) {
	r := newRig(t)
	r.cache.Limit = 1 << 20
	build := r.fill(t, "go-build", "ab/entry-d", 64)
	mod := r.fill(t, "mod", "example.com/m@v1/go.mod", 8)
	r.mustRun(t, project, "z")
	if !exists(t, build) || !exists(t, mod) {
		t.Fatal("a cache under the bound was pruned")
	}
}

func TestOverTheBoundTheBuildCachesGoFirst(t *testing.T) {
	r := newRig(t)
	r.cache.Limit = 256 << 10
	build := r.fill(t, "go-build", "ab/entry-d", 512)
	lint := r.fill(t, "golangci-lint", "cd/entry-a", 64)
	mod := r.fill(t, "mod", "example.com/m@v1/go.mod", 8)
	tool := r.fill(t, "go-qa-tools", "deadcode/v1/deadcode", 8)
	if out := r.mustRun(t, project, "z"); out == "" {
		t.Fatal("no run arguments")
	}
	if exists(t, build) || exists(t, lint) {
		t.Fatal("the build caches over the bound were kept")
	}
	if !exists(t, mod) || !exists(t, tool) {
		t.Fatal("the module cache and the tools went although the build caches were enough")
	}
	for _, dir := range []string{"go-build", "golangci-lint"} {
		if !exists(t, filepath.Join(r.root(), dir)) {
			t.Fatalf("%s was not made again", dir)
		}
	}
	if !strings.Contains(r.ErrOut.String(), "go-build, golangci-lint") {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
	leftovers, err := filepath.Glob(filepath.Join(r.root(), ".prune-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("left %v", leftovers)
	}
}

func TestStillOverTheBoundTheModulesAndToolsGoToo(t *testing.T) {
	r := newRig(t)
	r.cache.Limit = 256 << 10
	build := r.fill(t, "go-build", "ab/entry-d", 64)
	mod := r.fill(t, "mod", "example.com/m@v1/big.go", 512)
	tool := r.fill(t, "go-qa-tools", "deadcode/v1/deadcode", 8)
	// The module cache is read-only, as go leaves it.
	readOnly(t, mod, filepath.Dir(mod))
	r.mustRun(t, project, "z")
	if exists(t, build) || exists(t, mod) || exists(t, tool) {
		t.Fatal("a cache was kept although the total stayed over the bound")
	}
	if !strings.Contains(r.ErrOut.String(), "mod, go-qa-tools") {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
}

func TestAnInterruptedPruneIsFinished(t *testing.T) {
	r := newRig(t)
	left := filepath.Join(r.root(), ".prune-123", "mod", "example.com", "m@v1", "go.mod")
	teamtest.MustWrite(t, left, "module m\n")
	readOnly(t, filepath.Dir(left))
	r.mustRun(t, project, "z")
	if exists(t, filepath.Join(r.root(), ".prune-123")) {
		t.Fatal("an interrupted prune was left behind")
	}
}

func TestAFileInPlaceOfACacheFails(t *testing.T) {
	r := newRig(t)
	teamtest.MustWrite(t, filepath.Join(r.root(), "go-build"), "")
	code, out := r.run(t, project, "z")
	if code != term.ExitFailure || out != "" {
		t.Fatalf("exit %d stdout %q", code, out)
	}
	if !strings.Contains(r.ErrOut.String(), "go-build") {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
}

func TestAPruneOfACacheNotThereYetSucceeds(t *testing.T) {
	r := newRig(t)
	r.cache.Limit = 256 << 10
	mod := r.fill(t, "mod", "example.com/m@v1/big.go", 512)
	r.mustRun(t, project, "z")
	if exists(t, mod) {
		t.Fatal("the module cache over the bound was kept")
	}
	if !exists(t, filepath.Join(r.root(), "go-build")) {
		t.Fatal("go-build was not made")
	}
}

func TestTheBoundIsAppliedUnderTheProjectsLock(t *testing.T) {
	r := newRig(t)
	r.cache.Limit = 256 << 10
	build := r.fill(t, "go-build", "ab/entry-d", 512)
	var order []string
	r.cache.lock = func(path string) (func() error, error) {
		// Beside the bind, never in it: the container must not be able to hold, delete or
		// replace the lock.
		if path != filepath.Join(filepath.Dir(r.root()), "go.lock") {
			t.Fatalf("lock %s", path)
		}
		if !exists(t, build) {
			t.Fatal("pruned before the lock was taken")
		}
		order = append(order, "lock")
		return func() error {
			if exists(t, build) || !exists(t, filepath.Join(r.root(), "mod")) {
				t.Fatal("the lock was given up before the bound was applied")
			}
			order = append(order, "unlock")
			return nil
		}, nil
	}
	r.mustRun(t, project, "z")
	if strings.Join(order, " ") != "lock unlock" {
		t.Fatalf("order %v", order)
	}
}

func TestTheRealLockIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".lock")
	unlock, err := flock(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cerr := f.Close(); cerr != nil {
			t.Error(cerr)
		}
	})
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatalf("a second lock while held: %v", err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("after unlock: %v", err)
	}
}

// gone is a directory entry whose file was removed after it was listed.
type gone struct{ fs.DirEntry }

func (gone) Type() fs.FileMode          { return 0 }
func (gone) Info() (fs.FileInfo, error) { return nil, fs.ErrNotExist }

func TestAFileRemovedDuringTheWalkCountsAsNothing(t *testing.T) {
	// Another seat's go trims its cache, removes a download's temporary or an install's
	// scratch directory at any moment: a launch measuring the shared caches must not fail.
	n, err := blocks(gone{}, nil)
	if n != 0 || err != nil {
		t.Fatalf("listed then removed: %d %v", n, err)
	}
	n, err = blocks(nil, &fs.PathError{Op: "lstat", Path: "x", Err: fs.ErrNotExist})
	if n != 0 || err != nil {
		t.Fatalf("removed before its stat: %d %v", n, err)
	}
	if err := skipGone(&fs.PathError{Op: "open", Path: "x", Err: fs.ErrNotExist}); err != nil {
		t.Fatalf("skipGone kept %v", err)
	}
	if err := skipGone(fs.ErrPermission); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("skipGone dropped %v", err)
	}
}

func TestUnderDockerTheSameBoundedBindHoldsTheCaches(t *testing.T) {
	// The launcher refuses a rootful engine before it asks for the caches, so docker's
	// container root is the user, as podman's is: the same bind, relabel and bound apply.
	r := newRig(t)
	r.cache.Limit = 256 << 10
	build := r.fill(t, "go-build", "ab/entry-d", 512)
	r.Env["CONTAINER_ENGINE"] = "docker"
	out := r.mustRun(t, project, "z")
	if !strings.HasPrefix(out, "-v\n"+r.root()+":/var/cache/ccy-go:z\n-e\nGOCACHE=/var/cache/ccy-go/go-build\n") {
		t.Fatalf("stdout %q", out)
	}
	if exists(t, build) {
		t.Fatal("the bound was not applied under docker")
	}
}

func TestAnEngineOtherThanPodmanOrDockerFails(t *testing.T) {
	r := newRig(t)
	r.Env["CONTAINER_ENGINE"] = "lxc"
	code, out := r.run(t, project, "z")
	if code != term.ExitFailure || out != "" || !strings.Contains(r.ErrOut.String(), "lxc") {
		t.Fatalf("exit %d stdout %q stderr %q", code, out, r.ErrOut.String())
	}
}

// outside is a directory beyond the cache, with a read-only subdirectory, that a container
// writing the bind must never get the host to change.
func outside(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	teamtest.MustWrite(t, filepath.Join(sub, "keep"), "keep\n")
	readOnly(t, sub)
	t.Cleanup(func() {
		if err := syscall.Chmod(sub, 0o700); err != nil {
			t.Error(err)
		}
	})
	return dir
}

func unchanged(t *testing.T, dir string) {
	t.Helper()
	info, err := os.Stat(filepath.Join(dir, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o500 {
		t.Fatalf("the host changed %s outside the cache to %v", dir, info.Mode().Perm())
	}
	if !exists(t, filepath.Join(dir, "sub", "keep")) {
		t.Fatalf("the host removed a file outside the cache in %s", dir)
	}
}

func TestALeftoverThatIsASymlinkIsOnlyUnlinked(t *testing.T) {
	r := newRig(t)
	target := outside(t)
	if err := os.MkdirAll(r.root(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(r.root(), ".prune-planted")); err != nil {
		t.Fatal(err)
	}
	r.mustRun(t, project, "z")
	unchanged(t, target)
	if exists(t, filepath.Join(r.root(), ".prune-planted")) {
		t.Fatal("the planted link was left")
	}
}

func TestASymlinkInsideAPrunedCacheIsNotFollowed(t *testing.T) {
	r := newRig(t)
	r.cache.Limit = 256 << 10
	target := outside(t)
	r.fill(t, "go-build", "ab/entry-d", 512)
	if err := os.Symlink(target, filepath.Join(r.root(), "go-build", "escape")); err != nil {
		t.Fatal(err)
	}
	r.mustRun(t, project, "z")
	unchanged(t, target)
}

func TestACacheThatIsASymlinkIsMovedNotFollowed(t *testing.T) {
	r := newRig(t)
	r.cache.Limit = 256 << 10
	target := outside(t)
	r.fill(t, "mod", "example.com/m@v1/big.go", 512)
	if err := os.Symlink(target, filepath.Join(r.root(), "go-build")); err != nil {
		t.Fatal(err)
	}
	r.mustRun(t, project, "z")
	unchanged(t, target)
	info, err := os.Lstat(filepath.Join(r.root(), "go-build"))
	if err != nil || !info.IsDir() {
		t.Fatalf("go-build is %v (%v), want a real directory again", info, err)
	}
}

func TestALockThatCannotBeTakenFails(t *testing.T) {
	r := newRig(t)
	r.cache.lock = func(string) (func() error, error) { return nil, errors.New("no lock for you") }
	code, out := r.run(t, project, "z")
	if code != term.ExitFailure || out != "" || !strings.Contains(r.ErrOut.String(), "no lock for you") {
		t.Fatalf("exit %d stdout %q stderr %q", code, out, r.ErrOut.String())
	}
}

func TestALockReleaseThatFailsFails(t *testing.T) {
	r := newRig(t)
	r.cache.lock = func(string) (func() error, error) {
		return func() error { return errors.New("stuck") }, nil
	}
	code, out := r.run(t, project, "z")
	if code != term.ExitFailure || out != "" || !strings.Contains(r.ErrOut.String(), "stuck") {
		t.Fatalf("exit %d stdout %q stderr %q", code, out, r.ErrOut.String())
	}
}

func TestTheRealLockNeedsItsDirectory(t *testing.T) {
	if _, err := flock(filepath.Join(t.TempDir(), "missing", ".lock")); err == nil ||
		!strings.Contains(err.Error(), "open the lock") {
		t.Fatalf("err %v", err)
	}
}

func TestACacheRootThatCannotBeMadeFails(t *testing.T) {
	r := newRig(t)
	teamtest.MustWrite(t, filepath.Join(r.Env["HOME"], ".cache"), "a file\n")
	code, out := r.run(t, project, "z")
	if code != term.ExitFailure || out != "" || !strings.Contains(r.ErrOut.String(), "make the Go cache directory") {
		t.Fatalf("exit %d stdout %q stderr %q", code, out, r.ErrOut.String())
	}
}

var errInjected = errors.New("injected fault")

// faulty is the cache root with one operation failing: op names the method, and for FS an
// Open of anything but the root itself.
type faulty struct {
	fsRoot

	op string
}

func (f faulty) Mkdir(name string, perm fs.FileMode) error {
	return errors.Join(f.fail("Mkdir"), f.fsRoot.Mkdir(name, perm))
}

func (f faulty) Lstat(name string) (fs.FileInfo, error) {
	if err := f.fail("Lstat"); err != nil {
		return nil, err
	}
	return f.fsRoot.Lstat(name)
}

func (f faulty) Rename(a, b string) error {
	if err := f.fail("Rename"); err != nil {
		return err
	}
	return f.fsRoot.Rename(a, b)
}

func (f faulty) Chmod(name string, mode fs.FileMode) error {
	if err := f.fail("Chmod"); err != nil {
		return err
	}
	return f.fsRoot.Chmod(name, mode)
}

func (f faulty) Remove(name string) error {
	if err := f.fail("Remove"); err != nil {
		return err
	}
	return f.fsRoot.Remove(name)
}

func (f faulty) RemoveAll(name string) error {
	if err := f.fail("RemoveAll"); err != nil {
		return err
	}
	return f.fsRoot.RemoveAll(name)
}

func (f faulty) FS() fs.FS { return faultyFS{FS: f.fsRoot.FS(), fail: f.op == "FS"} }

func (f faulty) fail(op string) error {
	if f.op == op {
		return errInjected
	}
	return nil
}

// faultyFS fails every Open below the root when fail is set.
type faultyFS struct {
	fs.FS

	fail bool
}

func (f faultyFS) Open(name string) (fs.File, error) {
	if f.fail && name != "." {
		return nil, errInjected
	}
	return f.FS.Open(name)
}

func TestAFileSystemFaultFailsTheLaunch(t *testing.T) {
	for name, tc := range map[string]struct {
		op    string
		setup func(t *testing.T, r rig)
	}{
		"making a cache":              {op: "Mkdir"},
		"reading a cache":             {op: "Lstat"},
		"making the trash":            {op: "Mkdir", setup: overBound},
		"moving a cache aside":        {op: "Rename", setup: overBound},
		"opening a cache to clear":    {op: "Lstat", setup: overBound},
		"making a directory writable": {op: "Chmod", setup: overBound},
		"removing the trash":          {op: "RemoveAll", setup: overBound},
		"walking the caches":          {op: "FS", setup: overBound},
		"unlinking a planted leftover": {op: "Remove", setup: func(t *testing.T, r rig) {
			t.Helper()
			teamtest.MustWrite(t, filepath.Join(r.root(), ".prune-file"), "")
		}},
		"listing the leftovers": {op: "FS", setup: func(t *testing.T, r rig) {
			t.Helper()
			teamtest.MustWrite(t, filepath.Join(r.root(), ".prune-old", "x"), "")
		}},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			if tc.setup != nil {
				tc.setup(t, r)
			}
			r.cache.wrap = func(root fsRoot) fsRoot { return faulty{fsRoot: root, op: tc.op} }
			code, out := r.run(t, project, "z")
			if code != term.ExitFailure || out != "" || !strings.Contains(r.ErrOut.String(), "injected fault") {
				t.Fatalf("exit %d stdout %q stderr %q", code, out, r.ErrOut.String())
			}
		})
	}
}

// overBound fills go-build past a small bound, so the launch prunes.
func overBound(t *testing.T, r rig) {
	t.Helper()
	r.cache.Limit = 256 << 10
	r.fill(t, "go-build", "ab/entry-d", 512)
}

func TestStillOverAfterEveryCacheIsClearedFails(t *testing.T) {
	r := newRig(t)
	r.cache.Limit = 256 << 10
	r.fill(t, "go-build", "ab/entry-d", 64)
	stray := r.fill(t, ".", "planted", 512)
	code, out := r.run(t, project, "z")
	if code != term.ExitFailure || out != "" {
		t.Fatalf("exit %d stdout %q", code, out)
	}
	if !strings.Contains(r.ErrOut.String(), "not a cache ccy keeps") || !exists(t, stray) {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
}

func TestASparseFileCountsOnlyWhatItHoldsOnDisk(t *testing.T) {
	r := newRig(t)
	r.cache.Limit = 256 << 10
	mod := r.fill(t, "mod", "example.com/m@v1/go.mod", 8)
	sparse := filepath.Join(r.root(), "go-build", "ab", "sparse-d")
	teamtest.MustWrite(t, sparse, "")
	if err := os.Truncate(sparse, 9<<30); err != nil {
		t.Fatal(err)
	}
	r.mustRun(t, project, "z")
	if !exists(t, sparse) || !exists(t, mod) {
		t.Fatal("a sparse file holding nothing on disk set off a prune")
	}
}

func TestTwoLaunchesAgree(t *testing.T) {
	r := newRig(t)
	first := r.mustRun(t, project, "z")
	if second := r.mustRun(t, project, "z"); first != second {
		t.Fatalf("first %q second %q", first, second)
	}
}
