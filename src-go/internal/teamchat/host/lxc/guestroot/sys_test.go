package guestroot

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

// call is one system call the recorder saw.
type call struct {
	op      string
	path    string
	flags   uint64
	resolve uint64
}

// recorder is the opener seam TB1's tests name: it records every call around the real one,
// checks that no open but an O_PATH one reaches an inode that is neither a regular file nor a
// directory, counts the descriptors it handed out, and can fail one call by its index.
type recorder struct {
	real sys

	mu       sync.Mutex
	calls    []call
	open     map[int]string
	badOpens []string
	n        int
	failAt   int
	failErr  error
	// failOp, when set, fails every call of that op with failErr instead of one by index.
	failOp string
	// swapReopen, when set, makes the reopen through /proc/self/fd open this file instead.
	swapReopen string
}

func newRecorder() *recorder {
	return &recorder{real: linuxSys{}, open: map[int]string{}, failAt: -1}
}

// arm fails the call numbered i from now on (counting from 0), -1 for none.
func (r *recorder) arm(i int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n, r.failAt, r.failErr, r.calls = 0, i, err, nil
}

// count is the calls made since the last arm.
func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func (r *recorder) step(c call) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, c)
	i := r.n
	r.n++
	if r.failOp != "" && r.failOp == c.op {
		return r.failErr
	}
	if i == r.failAt {
		return r.failErr
	}
	return nil
}

func (r *recorder) opened(fd int, path string, flags uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.open[fd] = path
	if flags&unix.O_PATH != 0 {
		return
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		r.badOpens = append(r.badOpens, path+": "+err.Error())
		return
	}
	if t := st.Mode & unix.S_IFMT; t != unix.S_IFREG && t != unix.S_IFDIR {
		r.badOpens = append(r.badOpens, path)
	}
}

// leaked is the descriptors handed out and not closed.
func (r *recorder) leaked() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for fd, p := range r.open {
		out = append(out, strconv.Itoa(fd)+" "+p)
	}
	return out
}

func (r *recorder) callsOf(op string) []call {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []call
	for _, c := range r.calls {
		if c.op == op {
			out = append(out, c)
		}
	}
	return out
}

func (r *recorder) openat2(dirfd int, path string, how *unix.OpenHow) (int, error) {
	if err := r.step(call{op: "openat2", path: path, flags: how.Flags, resolve: how.Resolve}); err != nil {
		return -1, err
	}
	fd, err := r.real.openat2(dirfd, path, how)
	if err == nil {
		r.opened(fd, path, how.Flags)
	}
	return fd, err
}

func (r *recorder) openat(dirfd int, path string, flags int, mode uint32) (int, error) {
	if err := r.step(call{op: "openat", path: path, flags: uint64(flags)}); err != nil {
		return -1, err
	}
	if r.swapReopen != "" && strings.HasPrefix(path, "/proc/self/fd/") {
		path = r.swapReopen
	}
	fd, err := r.real.openat(dirfd, path, flags, mode)
	if err == nil {
		r.opened(fd, path, uint64(flags))
	}
	return fd, err
}

func (r *recorder) close(fd int) error {
	stepErr := r.step(call{op: "close"})
	r.mu.Lock()
	delete(r.open, fd)
	r.mu.Unlock()
	return errors.Join(r.real.close(fd), stepErr)
}

func (r *recorder) fstat(fd int, st *unix.Stat_t) error {
	if err := r.step(call{op: "fstat"}); err != nil {
		return err
	}
	return r.real.fstat(fd, st)
}

func (r *recorder) read(fd int, p []byte) (int, error) {
	if err := r.step(call{op: "read"}); err != nil {
		return 0, err
	}
	return r.real.read(fd, p)
}

func (r *recorder) write(fd int, p []byte) (int, error) {
	if err := r.step(call{op: "write"}); err != nil {
		return 0, err
	}
	return r.real.write(fd, p)
}

func (r *recorder) getdents(fd int, buf []byte) (int, error) {
	if err := r.step(call{op: "getdents"}); err != nil {
		return 0, err
	}
	return r.real.getdents(fd, buf)
}

func (r *recorder) flock(fd, how int) error {
	if err := r.step(call{op: "flock", flags: uint64(how)}); err != nil {
		return err
	}
	return r.real.flock(fd, how)
}

func (r *recorder) mkdirat(dirfd int, path string, mode uint32) error {
	if err := r.step(call{op: "mkdirat", path: path}); err != nil {
		return err
	}
	return r.real.mkdirat(dirfd, path, mode)
}

func (r *recorder) fchmod(fd int, mode uint32) error {
	if err := r.step(call{op: "fchmod"}); err != nil {
		return err
	}
	return r.real.fchmod(fd, mode)
}

func (r *recorder) fchown(fd, uid, gid int) error {
	if err := r.step(call{op: "fchown"}); err != nil {
		return err
	}
	return r.real.fchown(fd, uid, gid)
}

func (r *recorder) fsync(fd int) error {
	if err := r.step(call{op: "fsync"}); err != nil {
		return err
	}
	return r.real.fsync(fd)
}

func (r *recorder) renameat2(olddirfd int, oldpath string, newdirfd int, newpath string, flags uint) error {
	if err := r.step(call{op: "renameat2", path: oldpath + " " + newpath, flags: uint64(flags)}); err != nil {
		return err
	}
	return r.real.renameat2(olddirfd, oldpath, newdirfd, newpath, flags)
}

func (r *recorder) symlinkat(target string, dirfd int, path string) error {
	if err := r.step(call{op: "symlinkat", path: path}); err != nil {
		return err
	}
	return r.real.symlinkat(target, dirfd, path)
}

func (r *recorder) readlinkat(dirfd int, path string, buf []byte) (int, error) {
	if err := r.step(call{op: "readlinkat", path: path}); err != nil {
		return 0, err
	}
	return r.real.readlinkat(dirfd, path, buf)
}

func (r *recorder) unlinkat(dirfd int, path string, flags int) error {
	if err := r.step(call{op: "unlinkat", path: path, flags: uint64(flags)}); err != nil {
		return err
	}
	return r.real.unlinkat(dirfd, path, flags)
}

func (r *recorder) dial(ctx context.Context, name string) (*net.UnixConn, error) {
	if err := r.step(call{op: "dial", path: name}); err != nil {
		return nil, err
	}
	return r.real.dial(ctx, name)
}

// tree is a fixture guest root in a temporary directory.
type tree struct {
	t   *testing.T
	dir string
}

func newTree(t *testing.T) tree {
	t.Helper()
	return tree{t: t, dir: t.TempDir()}
}

// host is the host path of a guest path.
func (g tree) host(p string) string { return filepath.Join(g.dir, p) }

func (g tree) mkdir(p string) {
	g.t.Helper()
	if err := os.MkdirAll(g.host(p), 0o755); err != nil {
		g.t.Fatal(err)
	}
	if err := os.Chmod(g.host(p), 0o755); err != nil {
		g.t.Fatal(err)
	}
}

func (g tree) file(p, content string, mode os.FileMode) {
	g.t.Helper()
	g.mkdir(filepath.Dir(p))
	if err := os.WriteFile(g.host(p), []byte(content), mode); err != nil {
		g.t.Fatal(err)
	}
	if err := os.Chmod(g.host(p), mode); err != nil {
		g.t.Fatal(err)
	}
}

func (g tree) fifo(p string) {
	g.t.Helper()
	g.mkdir(filepath.Dir(p))
	if err := unix.Mkfifo(g.host(p), 0o644); err != nil {
		g.t.Fatal(err)
	}
}

// socket listens on a unix socket at p for the test's life. It is bound at a short path and
// renamed into place, since a deep fixture path is longer than a socket address may be.
func (g tree) socket(p string) *net.UnixListener {
	g.t.Helper()
	g.mkdir(filepath.Dir(p))
	short := g.t.TempDir() + "/s"
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: short, Net: "unix"})
	if err != nil {
		g.t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	if err := os.Rename(short, g.host(p)); err != nil {
		g.t.Fatal(err)
	}
	g.t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			g.t.Error(err)
		}
	})
	return ln
}

func (g tree) link(p, target string) {
	g.t.Helper()
	g.mkdir(filepath.Dir(p))
	if err := os.Symlink(target, g.host(p)); err != nil {
		g.t.Fatal(err)
	}
}

// root opens the tree through rec.
func (g tree) root(rec *recorder) *Root {
	g.t.Helper()
	r, err := openDirWith(rec, g.dir)
	if err != nil {
		g.t.Fatal(err)
	}
	g.t.Cleanup(func() {
		if err := r.Close(); err != nil && !errors.Is(err, errClosed) {
			g.t.Error(err)
		}
	})
	return r
}

func TestLinuxSysDialsAndReads(t *testing.T) {
	g := newTree(t)
	ln := g.socket("/s")
	conn, err := linuxSys{}.dial(t.Context(), g.host("/s"))
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	accepted, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if err := accepted.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := (linuxSys{}).dial(t.Context(), g.host("/none")); err == nil {
		t.Fatal("dial of a missing socket succeeded")
	}
}

func TestOpenDirRefusesWhatIsNotADirectory(t *testing.T) {
	g := newTree(t)
	g.file("/f", "x", 0o644)
	g.link("/l", g.dir)
	for _, p := range []string{"relative", g.host("/f"), g.host("/l"), g.host("/missing")} {
		if r, err := OpenDir(p); err == nil {
			t.Errorf("OpenDir(%s) succeeded", p)
			if cerr := r.Close(); cerr != nil {
				t.Error(cerr)
			}
		}
	}
	r, err := OpenDir(g.dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.PID() != 0 || r.Alive() != nil {
		t.Errorf("a fixture root: pid %d, alive %v", r.PID(), r.Alive())
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); !errors.Is(err, errClosed) {
		t.Errorf("a second Close: %v", err)
	}
	if _, err := r.Stat("/"); !errors.Is(err, errClosed) {
		t.Errorf("a closed root's Stat: %v", err)
	}
}

func TestOpenDirClosesOnAFailedCheck(t *testing.T) {
	g := newTree(t)
	rec := newRecorder()
	rec.failOp, rec.failErr = "fstat", unix.EIO
	if _, err := openDirWith(rec, g.dir); !errors.Is(err, unix.EIO) {
		t.Fatalf("got %v", err)
	}
	if l := rec.leaked(); len(l) != 0 {
		t.Errorf("leaked %v", l)
	}
}
