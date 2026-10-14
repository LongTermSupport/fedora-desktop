package fsx

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

type env struct {
	t    *testing.T
	dir  string
	out  *bytes.Buffer
	fs   *FS
	self Owner
}

func newEnv(t *testing.T, display string) *env {
	t.Helper()
	self, err := CurrentOwner()
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, dir: t.TempDir(), out: &bytes.Buffer{}, self: self}
	e.fs, err = Open(e.dir, display, e.out, 4242)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := e.fs.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	return e
}

// The helpers take the mode as a parameter: the tests need modes the gosec rules would
// reject as literals (they are the point of the test).

func chmod(t *testing.T, path string, mode fs.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, data string, mode fs.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
	chmod(t, path, mode)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func readlink(t *testing.T, path string) string {
	t.Helper()
	target, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func infoOf(t *testing.T, path string) fs.FileInfo {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func modeOf(t *testing.T, path string) fs.FileMode {
	t.Helper()
	return infoOf(t, path).Mode()
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func (e *env) path(name string) string { return filepath.Join(e.dir, name) }

func (e *env) write(name, data string, mode fs.FileMode) {
	e.t.Helper()
	writeFile(e.t, e.path(name), data, mode)
}

func (e *env) mkdir(name string) {
	e.t.Helper()
	if err := os.MkdirAll(e.path(name), 0o700); err != nil {
		e.t.Fatal(err)
	}
	// MkdirAll honours the umask; the tests compare exact modes.
	chmod(e.t, e.path(name), 0o700)
}

func (e *env) symlink(target, name string) {
	e.t.Helper()
	if err := os.Symlink(target, e.path(name)); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) changed() string {
	e.t.Helper()
	s := e.out.String()
	e.out.Reset()
	return s
}

func (e *env) read(name string) string {
	e.t.Helper()
	return readFile(e.t, e.path(name))
}

func (e *env) leftovers() []string {
	e.t.Helper()
	var found []string
	err := filepath.WalkDir(e.dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if strings.Contains(d.Name(), ".agent-bus-install.") {
			found = append(found, p)
		}
		return nil
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return found
}

func TestEnsureDirCreates(t *testing.T) {
	e := newEnv(t, "")
	e.mkdir("var/lib")
	changed, err := e.fs.EnsureDir("var/lib/agent-bus", 0o750, e.self)
	if err != nil || !changed {
		t.Fatalf("EnsureDir = %v, %v", changed, err)
	}
	if got := e.changed(); got != "CHANGED\tdirectory /var/lib/agent-bus\n" {
		t.Fatalf("output %q", got)
	}
	if m := modeOf(t, e.path("var/lib/agent-bus")); !m.IsDir() || m.Perm() != 0o750 {
		t.Fatalf("mode %v", m)
	}
	changed, err = e.fs.EnsureDir("var/lib/agent-bus", 0o750, e.self)
	if err != nil || changed || e.changed() != "" {
		t.Fatalf("second run: %v, %v", changed, err)
	}
}

// The mode is exact whatever the umask: a 0755 directory is 0755 under a closed umask.
func TestEnsureDirModeIsExactUnderAnyUmask(t *testing.T) {
	old := syscall.Umask(0o777)
	t.Cleanup(func() { syscall.Umask(old) })
	e := newEnv(t, "")
	e.mkdir("a")
	if _, err := e.fs.EnsureDir("a/x", 0o755, e.self); err != nil {
		t.Fatal(err)
	}
	if m := modeOf(t, e.path("a/x")); m.Perm() != 0o755 {
		t.Fatalf("mode %v under a closed umask", m)
	}
}

func TestEnsureDirFixesModeAndReportsIt(t *testing.T) {
	e := newEnv(t, "")
	e.mkdir("d")
	chmod(t, e.path("d"), 0o777)
	changed, err := e.fs.EnsureDir("d", 0o700, e.self)
	if err != nil || !changed {
		t.Fatalf("EnsureDir = %v, %v", changed, err)
	}
	if got := e.changed(); got != "CHANGED\tdirectory /d mode and owner\n" {
		t.Fatalf("output %q", got)
	}
	if m := modeOf(t, e.path("d")); m.Perm() != 0o700 {
		t.Fatalf("mode %v", m)
	}
}

func TestEnsureDirSpecialBitsAreCompared(t *testing.T) {
	e := newEnv(t, "")
	e.mkdir("d")
	chmod(t, e.path("d"), fs.ModeSticky|0o755)
	changed, err := e.fs.EnsureDir("d", 0o755, e.self)
	if err != nil || !changed {
		t.Fatalf("a sticky bit went unnoticed: %v, %v", changed, err)
	}
	if m := modeOf(t, e.path("d")); m&fs.ModeSticky != 0 {
		t.Fatalf("sticky bit kept: %v", m)
	}
}

func TestEnsureDirFixesOwner(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("changing the owner needs root")
	}
	e := newEnv(t, "")
	e.mkdir("d")
	other := Owner{UID: 4321, GID: 4322}
	if _, err := e.fs.EnsureDir("d", 0o755, other); err != nil {
		t.Fatal(err)
	}
	st, ok := infoOf(t, e.path("d")).Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != 4321 || int(st.Gid) != 4322 {
		t.Fatalf("owner %v", st)
	}
	if got := e.changed(); got != "CHANGED\tdirectory /d mode and owner\n" {
		t.Fatalf("output %q", got)
	}
}

func TestEnsureDirRefusesASymlinkToADirectory(t *testing.T) {
	e := newEnv(t, "")
	e.mkdir("real")
	e.symlink("real", "d")
	_, err := e.fs.EnsureDir("d", 0o755, e.self)
	if err == nil || err.Error() != "/d exists and is not a directory" {
		t.Fatalf("error %v", err)
	}
}

func TestEnsureDirRefusesAFile(t *testing.T) {
	e := newEnv(t, "")
	e.write("d", "x", 0o600)
	_, err := e.fs.EnsureDir("d", 0o755, e.self)
	if err == nil || err.Error() != "/d exists and is not a directory" {
		t.Fatalf("error %v", err)
	}
}

func TestEnsureDirRefusesAMissingParent(t *testing.T) {
	e := newEnv(t, "")
	if _, err := e.fs.EnsureDir("no/such/dir", 0o755, e.self); err == nil {
		t.Fatal("created a directory under a missing parent")
	}
}

func TestEnsureDirRefusesAPathThroughASymlinkOutOfTheRoot(t *testing.T) {
	outside := t.TempDir()
	e := newEnv(t, "")
	e.symlink(outside, "escape")
	if _, err := e.fs.EnsureDir("escape/x", 0o755, e.self); err == nil {
		t.Fatal("followed a symlink out of the root")
	}
	if exists(filepath.Join(outside, "x")) {
		t.Fatal("created a directory outside the root")
	}
}

func TestEnsureDirRefusesTraversal(t *testing.T) {
	e := newEnv(t, "")
	for _, name := range []string{"../x", "a/../../x", "/abs", ".", "", "a//b", "a/./b"} {
		if _, err := e.fs.EnsureDir(name, 0o755, e.self); err == nil {
			t.Fatalf("EnsureDir(%q) accepted", name)
		}
	}
}

func TestRequireSystemDir(t *testing.T) {
	e := newEnv(t, "")
	e.mkdir("usr/local/bin")
	e.symlink("bin", "usr/local/sbin")
	e.write("usr/local/file", "x", 0o600)
	if err := e.fs.RequireSystemDir("usr/local/bin"); err != nil {
		t.Fatal(err)
	}
	if err := e.fs.RequireSystemDir("usr/local/sbin"); err != nil {
		t.Fatalf("a symlink to a directory must count: %v", err)
	}
	if m := modeOf(t, e.path("usr/local/bin")); m.Perm() != 0o700 {
		t.Fatalf("mode touched: %v", m)
	}
	for _, name := range []string{"usr/local/file", "usr/local/absent"} {
		err := e.fs.RequireSystemDir(name)
		if err == nil || err.Error() != "/"+name+" is not a directory on this host" {
			t.Fatalf("RequireSystemDir(%q) = %v", name, err)
		}
	}
}

func TestPutFileCreatesAndIsIdempotent(t *testing.T) {
	e := newEnv(t, "")
	e.mkdir("usr/local/bin")
	changed, err := e.fs.PutBytes("tool", []byte("v1"), "usr/local/bin/tool", 0o755, e.self)
	if err != nil || !changed {
		t.Fatalf("PutBytes = %v, %v", changed, err)
	}
	if got := e.changed(); got != "CHANGED\ttool\n" {
		t.Fatalf("output %q", got)
	}
	if e.read("usr/local/bin/tool") != "v1" || modeOf(t, e.path("usr/local/bin/tool")).Perm() != 0o755 {
		t.Fatal("content or mode wrong")
	}
	changed, err = e.fs.PutBytes("tool", []byte("v1"), "usr/local/bin/tool", 0o755, e.self)
	if err != nil || changed || e.changed() != "" {
		t.Fatalf("identical put changed something: %v, %v", changed, err)
	}
	if left := e.leftovers(); len(left) != 0 {
		t.Fatalf("temp files left: %q", left)
	}
}

func TestPutFileUpdatesChangedBytesAndMode(t *testing.T) {
	e := newEnv(t, "")
	e.mkdir("bin")
	if _, err := e.fs.PutBytes("tool", []byte("v1"), "bin/tool", 0o755, e.self); err != nil {
		t.Fatal(err)
	}
	e.changed()
	changed, err := e.fs.PutBytes("tool", []byte("v2"), "bin/tool", 0o755, e.self)
	if err != nil || !changed || e.changed() != "CHANGED\ttool\n" || e.read("bin/tool") != "v2" {
		t.Fatalf("changed bytes: %v, %v", changed, err)
	}
	changed, err = e.fs.PutBytes("tool", []byte("v2"), "bin/tool", 0o644, e.self)
	if err != nil || !changed || modeOf(t, e.path("bin/tool")).Perm() != 0o644 {
		t.Fatalf("changed mode: %v, %v", changed, err)
	}
	if left := e.leftovers(); len(left) != 0 {
		t.Fatalf("temp files left: %q", left)
	}
}

func TestPutFileFromAPath(t *testing.T) {
	e := newEnv(t, "")
	e.mkdir("dst")
	src := filepath.Join(t.TempDir(), "src")
	writeFile(t, src, "payload", 0o600)
	changed, err := e.fs.PutFile("payload", src, "dst/f", 0o640, e.self)
	if err != nil || !changed || e.read("dst/f") != "payload" {
		t.Fatalf("PutFile = %v, %v", changed, err)
	}
	changed, err = e.fs.PutFile("payload", src, "dst/f", 0o640, e.self)
	if err != nil || changed {
		t.Fatalf("second PutFile = %v, %v", changed, err)
	}
	if _, err := e.fs.PutFile("payload", filepath.Join(t.TempDir(), "absent"), "dst/g", 0o640, e.self); err == nil {
		t.Fatal("a missing source was accepted")
	}
}

func TestPutFileAcrossTheCopyBufferSize(t *testing.T) {
	e := newEnv(t, "")
	e.mkdir("d")
	big := strings.Repeat("0123456789abcdef", 8<<10) // 128 KiB, two full compare buffers
	if _, err := e.fs.PutBytes("big", []byte(big), "d/big", 0o640, e.self); err != nil {
		t.Fatal(err)
	}
	if changed, err := e.fs.PutBytes("big", []byte(big), "d/big", 0o640, e.self); err != nil || changed {
		t.Fatalf("identical large file: %v, %v", changed, err)
	}
	other := big[:len(big)-1] + "X"
	if changed, err := e.fs.PutBytes("big", []byte(other), "d/big", 0o640, e.self); err != nil || !changed || e.read("d/big") != other {
		t.Fatalf("last byte changed: %v, %v", changed, err)
	}
	if changed, err := e.fs.PutBytes("big", []byte(other+"more"), "d/big", 0o640, e.self); err != nil || !changed {
		t.Fatalf("longer content: %v, %v", changed, err)
	}
	if changed, err := e.fs.PutBytes("big", []byte(big), "d/big", 0o640, e.self); err != nil || !changed || e.read("d/big") != big {
		t.Fatalf("shorter content: %v, %v", changed, err)
	}
}

func TestPutFileReplacesUnequalSizes(t *testing.T) {
	e := newEnv(t, "")
	e.write("f", "long content", 0o600)
	if changed, err := e.fs.PutBytes("f", []byte("short"), "f", 0o600, e.self); err != nil || !changed || e.read("f") != "short" {
		t.Fatalf("%v, %v", changed, err)
	}
	if changed, err := e.fs.PutBytes("f", []byte("short and then some"), "f", 0o600, e.self); err != nil || !changed {
		t.Fatalf("%v, %v", changed, err)
	}
	// A prefix of the existing content is still a change.
	e.write("g", "abcdef", 0o600)
	if changed, err := e.fs.PutBytes("g", []byte("abc"), "g", 0o600, e.self); err != nil || !changed || e.read("g") != "abc" {
		t.Fatalf("%v, %v", changed, err)
	}
}

func TestPutFileRefusesASymlinkToAFileOutsideTheRoot(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "victim")
	writeFile(t, outside, "keep", 0o600)
	e := newEnv(t, "")
	e.symlink(outside, "f")
	_, err := e.fs.PutBytes("f", []byte("evil"), "f", 0o644, e.self)
	if err == nil || err.Error() != "/f is a symlink; refusing to write through it" {
		t.Fatalf("error %v", err)
	}
	if got := readFile(t, outside); got != "keep" {
		t.Fatalf("victim overwritten: %q", got)
	}
}

func TestPutFileRefusesASymlinkToAFileInsideTheRoot(t *testing.T) {
	e := newEnv(t, "")
	e.write("real", "keep", 0o600)
	e.symlink("real", "f")
	if _, err := e.fs.PutBytes("f", []byte("evil"), "f", 0o644, e.self); err == nil {
		t.Fatal("wrote through a symlink")
	}
	if e.read("real") != "keep" {
		t.Fatal("the link target was overwritten")
	}
}

func TestPutFileRefusesADanglingSymlink(t *testing.T) {
	e := newEnv(t, "")
	e.symlink("nowhere", "f")
	if _, err := e.fs.PutBytes("f", []byte("evil"), "f", 0o644, e.self); err == nil {
		t.Fatal("wrote through a dangling symlink")
	}
	if exists(e.path("nowhere")) {
		t.Fatal("created the link target")
	}
}

func TestPutFileRefusesAParentSymlinkedOutOfTheRoot(t *testing.T) {
	outside := t.TempDir()
	e := newEnv(t, "")
	e.symlink(outside, "dir")
	if _, err := e.fs.PutBytes("f", []byte("evil"), "dir/f", 0o644, e.self); err == nil {
		t.Fatal("followed a parent symlink out of the root")
	}
	if exists(filepath.Join(outside, "f")) {
		t.Fatal("wrote outside the root")
	}
}

func TestPutFileFollowsAParentSymlinkInsideTheRoot(t *testing.T) {
	e := newEnv(t, "")
	e.mkdir("usr/local/bin")
	e.symlink("bin", "usr/local/sbin")
	if _, err := e.fs.PutBytes("f", []byte("ok"), "usr/local/sbin/tool", 0o755, e.self); err != nil {
		t.Fatal(err)
	}
	if e.read("usr/local/bin/tool") != "ok" {
		t.Fatal("not written through the OS symlink")
	}
}

func TestPutFileRefusesADirectory(t *testing.T) {
	e := newEnv(t, "")
	e.mkdir("f")
	_, err := e.fs.PutBytes("f", []byte("x"), "f", 0o644, e.self)
	if err == nil || err.Error() != "/f exists and is not a regular file" {
		t.Fatalf("error %v", err)
	}
}

func TestPutFileReplacesAStaleTempWithoutFollowingIt(t *testing.T) {
	victim := filepath.Join(t.TempDir(), "victim")
	writeFile(t, victim, "keep", 0o600)
	e := newEnv(t, "")
	e.symlink(victim, ".f.agent-bus-install.4242")
	if _, err := e.fs.PutBytes("f", []byte("new"), "f", 0o644, e.self); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, victim); got != "keep" {
		t.Fatalf("a planted temp symlink redirected the write: %q", got)
	}
	if e.read("f") != "new" {
		t.Fatal("content not written")
	}
	if left := e.leftovers(); len(left) != 0 {
		t.Fatalf("temp left: %q", left)
	}
}

// B9: a failed write leaves no temp file behind.
func TestPutFileRemovesItsTempOnFailure(t *testing.T) {
	e := newEnv(t, "")
	e.mkdir("dst")
	srcDir := t.TempDir() // reading a directory fails part-way
	_, err := e.fs.PutFile("x", srcDir, "dst/f", 0o644, e.self)
	if err == nil {
		t.Fatal("copied a directory")
	}
	if left := e.leftovers(); len(left) != 0 {
		t.Fatalf("temp files left after a failure: %q", left)
	}
	if exists(e.path("dst/f")) {
		t.Fatal("a partial destination appeared")
	}
}

func TestPutFileMissingParent(t *testing.T) {
	e := newEnv(t, "")
	if _, err := e.fs.PutBytes("x", []byte("x"), "no/such/f", 0o644, e.self); err == nil {
		t.Fatal("wrote under a missing parent")
	}
}

func TestPutFileRefusesTraversal(t *testing.T) {
	e := newEnv(t, "")
	for _, name := range []string{"../x", "/abs", "a/../../x", ".", ""} {
		if _, err := e.fs.PutBytes("x", []byte("x"), name, 0o644, e.self); err == nil {
			t.Fatalf("PutBytes(%q) accepted", name)
		}
		if _, err := e.fs.PutSymlink("x", "t", name); err == nil {
			t.Fatalf("PutSymlink(%q) accepted", name)
		}
	}
}

func TestPutFileKeepsTheOldFileIfTheNewOneFails(t *testing.T) {
	e := newEnv(t, "")
	e.write("f", "old", 0o644)
	if _, err := e.fs.PutFile("x", t.TempDir(), "f", 0o644, e.self); err == nil {
		t.Fatal("copied a directory")
	}
	if e.read("f") != "old" {
		t.Fatal("the destination was damaged by a failed update")
	}
}

func TestPutSymlink(t *testing.T) {
	e := newEnv(t, "")
	e.mkdir("lib")
	changed, err := e.fs.PutSymlink("current v1", "tool-1", "lib/tool")
	if err != nil || !changed || e.changed() != "CHANGED\tcurrent v1\n" {
		t.Fatalf("create: %v, %v", changed, err)
	}
	if got := readlink(t, e.path("lib/tool")); got != "tool-1" {
		t.Fatalf("link = %q", got)
	}
	changed, err = e.fs.PutSymlink("current v1", "tool-1", "lib/tool")
	if err != nil || changed || e.changed() != "" {
		t.Fatalf("same target: %v, %v", changed, err)
	}
	changed, err = e.fs.PutSymlink("current v2", "tool-2", "lib/tool")
	if err != nil || !changed || e.changed() != "CHANGED\tcurrent v2\n" {
		t.Fatalf("new target: %v, %v", changed, err)
	}
	if got := readlink(t, e.path("lib/tool")); got != "tool-2" {
		t.Fatalf("link = %q", got)
	}
	if left := e.leftovers(); len(left) != 0 {
		t.Fatalf("temp left: %q", left)
	}
}

func TestPutSymlinkRefusesAnythingElseInTheWay(t *testing.T) {
	e := newEnv(t, "")
	e.write("file", "x", 0o600)
	e.mkdir("dir")
	for _, name := range []string{"file", "dir"} {
		_, err := e.fs.PutSymlink("x", "t", name)
		if err == nil || err.Error() != "/"+name+" exists and is not a symlink" {
			t.Fatalf("PutSymlink over %s = %v", name, err)
		}
	}
	if e.read("file") != "x" {
		t.Fatal("file damaged")
	}
}

func TestPutSymlinkStoresTheTargetAsGiven(t *testing.T) {
	e := newEnv(t, "")
	changed, err := e.fs.PutSymlink("abs", "/etc/passwd", "l")
	if err != nil || !changed {
		t.Fatalf("%v, %v", changed, err)
	}
	if got := readlink(t, e.path("l")); got != "/etc/passwd" {
		t.Fatalf("link = %q", got)
	}
	// An existing symlink whose target differs is replaced, not written through.
	changed, err = e.fs.PutSymlink("rel", "t2", "l")
	if err != nil || !changed {
		t.Fatalf("%v, %v", changed, err)
	}
}

func TestPutSymlinkReplacesAStaleTemp(t *testing.T) {
	e := newEnv(t, "")
	e.symlink("elsewhere", ".l.agent-bus-install.4242")
	if _, err := e.fs.PutSymlink("l", "t", "l"); err != nil {
		t.Fatal(err)
	}
	if left := e.leftovers(); len(left) != 0 {
		t.Fatalf("temp left: %q", left)
	}
}

func TestDisplayPrefix(t *testing.T) {
	e := newEnv(t, "/var/lib/agent-bus/team")
	changed, err := e.fs.EnsureDir("secrets", 0o700, e.self)
	if err != nil || !changed {
		t.Fatalf("%v, %v", changed, err)
	}
	if got := e.changed(); got != "CHANGED\tdirectory /var/lib/agent-bus/team/secrets\n" {
		t.Fatalf("output %q", got)
	}
	_, err = e.fs.PutBytes("x", []byte("a"), "secrets", 0o600, e.self)
	if err == nil || !strings.Contains(err.Error(), "/var/lib/agent-bus/team/secrets") {
		t.Fatalf("error %v does not carry the display path", err)
	}
}

type failWriter struct{}

var errBoom = errors.New("boom")

func (failWriter) Write([]byte) (int, error) { return 0, errBoom }

func TestAFailedChangedLineIsAnError(t *testing.T) {
	dir := t.TempDir()
	self, err := CurrentOwner()
	if err != nil {
		t.Fatal(err)
	}
	f, err := Open(dir, "", failWriter{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	if _, err := f.EnsureDir("d", 0o755, self); !errors.Is(err, errBoom) {
		t.Fatalf("error %v", err)
	}
	if _, err := f.PutBytes("x", []byte("a"), "f", 0o644, self); !errors.Is(err, errBoom) {
		t.Fatalf("PutBytes error %v", err)
	}
	if _, err := f.PutSymlink("x", "t", "l"); !errors.Is(err, errBoom) {
		t.Fatalf("PutSymlink error %v", err)
	}
}

func TestOpenRefusesAMissingDirectory(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "absent"), "", &bytes.Buffer{}, 1); err == nil {
		t.Fatal("opened a missing directory")
	}
}

func TestShownRoot(t *testing.T) {
	e := newEnv(t, "")
	if got := e.fs.shown("."); got != "/" {
		t.Fatalf("shown(.) = %q", got)
	}
	e2 := newEnv(t, "/var/lib/x")
	if got := e2.fs.shown("."); got != "/var/lib/x" {
		t.Fatalf("shown(.) = %q", got)
	}
}

func TestUnixBits(t *testing.T) {
	cases := map[fs.FileMode]uint32{
		0o755:                                 0o755,
		fs.ModeSetuid | 0o755:                 0o4755,
		fs.ModeSetgid | 0o750:                 0o2750,
		fs.ModeSticky | 0o777:                 0o1777,
		fs.ModeSetuid | fs.ModeSetgid | 0o700: 0o6700,
		fs.ModeDir | 0o700:                    0o700,
	}
	for mode, want := range cases {
		if got := unixBits(mode); got != want {
			t.Errorf("unixBits(%v) = %o, want %o", mode, got, want)
		}
	}
}

type noOwnerInfo struct{ fs.FileInfo }

func (noOwnerInfo) Sys() any { return nil }

func TestMatchesNeedsAKnownOwner(t *testing.T) {
	dir := t.TempDir()
	info, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	self, err := CurrentOwner()
	if err != nil {
		t.Fatal(err)
	}
	if matches(noOwnerInfo{info}, info.Mode(), self) {
		t.Fatal("matched without being able to read the owner")
	}
}

func TestSameContentReportsReadErrors(t *testing.T) {
	if _, err := sameContent(errReader{}, strings.NewReader("x")); err == nil {
		t.Fatal("a read error in the first stream was ignored")
	}
	if _, err := sameContent(strings.NewReader("x"), errReader{}); err == nil {
		t.Fatal("a read error in the second stream was ignored")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errBoom }
