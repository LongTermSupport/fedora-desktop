package guestroot

import (
	"context"
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// sys is every system call a Root makes on a descriptor or a path, the one opener seam TB1's
// tests name: the real one is linuxSys, and the tests' recorder wraps it.
type sys interface {
	pathCalls
	fdCalls
}

// pathCalls are the calls that name a path below a directory descriptor.
type pathCalls interface {
	openat2(dirfd int, path string, how *unix.OpenHow) (int, error)
	openat(dirfd int, path string, flags int, mode uint32) (int, error)
	mkdirat(dirfd int, path string, mode uint32) error
	renameat2(olddirfd int, oldpath string, newdirfd int, newpath string, flags uint) error
	symlinkat(target string, dirfd int, path string) error
	readlinkat(dirfd int, path string, buf []byte) (int, error)
	unlinkat(dirfd int, path string, flags int) error
	dial(ctx context.Context, name string) (*net.UnixConn, error)
}

// fdCalls are the calls on an open descriptor.
type fdCalls interface {
	close(fd int) error
	fstat(fd int, st *unix.Stat_t) error
	read(fd int, p []byte) (int, error)
	write(fd int, p []byte) (int, error)
	getdents(fd int, buf []byte) (int, error)
	flock(fd, how int) error
	fchmod(fd int, mode uint32) error
	fchown(fd, uid, gid int) error
	fsync(fd int) error
}

// linuxSys is the running kernel.
type linuxSys struct{}

func (linuxSys) openat2(dirfd int, path string, how *unix.OpenHow) (int, error) {
	return unix.Openat2(dirfd, path, how)
}

func (linuxSys) openat(dirfd int, path string, flags int, mode uint32) (int, error) {
	return unix.Openat(dirfd, path, flags, mode)
}

func (linuxSys) mkdirat(dirfd int, path string, mode uint32) error {
	return unix.Mkdirat(dirfd, path, mode)
}

func (linuxSys) renameat2(olddirfd int, oldpath string, newdirfd int, newpath string, flags uint) error {
	return unix.Renameat2(olddirfd, oldpath, newdirfd, newpath, flags)
}

func (linuxSys) symlinkat(target string, dirfd int, path string) error {
	return unix.Symlinkat(target, dirfd, path)
}

func (linuxSys) readlinkat(dirfd int, path string, buf []byte) (int, error) {
	return unix.Readlinkat(dirfd, path, buf)
}

func (linuxSys) unlinkat(dirfd int, path string, flags int) error {
	return unix.Unlinkat(dirfd, path, flags)
}

// dial connects to the unix socket name: for a guest socket, its own /proc/self/fd/<n>, so no
// path is resolved in the guest.
func (linuxSys) dial(ctx context.Context, name string) (*net.UnixConn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", name)
	if err != nil {
		return nil, err
	}
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, fmt.Errorf("guestroot: %s is not a unix connection: %w", name, conn.Close())
	}
	return uc, nil
}

func (linuxSys) close(fd int) error { return unix.Close(fd) }

func (linuxSys) fstat(fd int, st *unix.Stat_t) error { return unix.Fstat(fd, st) }

func (linuxSys) read(fd int, p []byte) (int, error) { return unix.Read(fd, p) }

func (linuxSys) write(fd int, p []byte) (int, error) { return unix.Write(fd, p) }

func (linuxSys) getdents(fd int, buf []byte) (int, error) { return unix.Getdents(fd, buf) }

func (linuxSys) flock(fd, how int) error { return unix.Flock(fd, how) }

func (linuxSys) fchmod(fd int, mode uint32) error { return unix.Fchmod(fd, mode) }

func (linuxSys) fchown(fd, uid, gid int) error { return unix.Fchown(fd, uid, gid) }

func (linuxSys) fsync(fd int) error { return unix.Fsync(fd) }
