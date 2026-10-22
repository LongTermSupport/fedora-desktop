// Package guestroot is the one way host code reaches below a running LXC guest's root (Plan
// 00209 SPEC-n3 LX3, TB1): the guest's init pinned by a pidfd and its /proc/<pid>/root held
// open as a directory fd, and every path below it opened from that fd by openat2 with
// RESOLVE_IN_ROOT|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS (RESOLVE_NO_XDEV added for a write),
// so no link in the guest, and no mount a write would cross, is ever followed.
//
// A read opens O_PATH first, checks the inode by fstat, and only a regular file is reopened,
// through its own /proc/self/fd/<n>, for reading; so no host code opens a guest FIFO, socket or
// device. A guest socket is connected through /proc/self/fd/<n> of its O_PATH descriptor. A
// write is made below directories each checked root-owned and not group or other writable, by
// an exclusive temporary renamed into place. A path is absolute and clean (n3-D14), an error
// names the path and what it is, never a byte read from the guest.
//
// A root whose init dies answers ErrGone from every call, whatever the torn-down mount said.
// Whether that makes the member offline is the caller's decision (n3b-D1).
package guestroot

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var (
	// ErrMoved is a pin that did not hold: the init died, or the container's init pid changed,
	// between the pidfd and the second look (n3-D7).
	ErrMoved = errors.New("guestroot: the container's init changed while it was pinned")
	// ErrGone is a call on a root whose pinned init has since died.
	ErrGone = errors.New("guestroot: the container's init has gone")
	// ErrRefused is a path TB1 refuses, wrapped with the path and what it is.
	ErrRefused = errors.New("guestroot: refused")
	// ErrTooLarge is a file, a directory or a tree over its cap.
	ErrTooLarge = errors.New("guestroot: over its cap")

	errClosed = errors.New("guestroot: the root is closed")
)

const (
	readResolve  = unix.RESOLVE_IN_ROOT | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS
	writeResolve = readResolve | unix.RESOLVE_NO_XDEV
	// reopenFlags is TB1's reopen of an O_PATH descriptor through /proc/self/fd/<n>: the one
	// open exempt from TB4's O_NOFOLLOW, and not made by openat2, because either would fail with
	// ELOOP on the magic link it must follow.
	reopenFlags = unix.O_RDONLY | unix.O_NONBLOCK | unix.O_CLOEXEC
	// rootFlags opens <Proc>/<pid>/root and <Proc>/<pid>/exe, host paths whose magic links must
	// be followed: the guest's root directory, and the init's binary as the host sees it.
	rootFlags = unix.O_PATH | unix.O_CLOEXEC
	// openTries bounds the retries of an openat2 that a concurrent rename made return EAGAIN.
	openTries = 8
	statCap   = 4 << 10
	lockCap   = 4 << 10
	// sharedWrite is the group and other write bits no directory a write passes may have.
	sharedWrite = 0o022
)

// Kernel is the pidfd system calls, a seam so a test can kill the init at a chosen moment.
type Kernel interface {
	PidfdOpen(pid int) (int, error)
	// PidfdAlive is nil while the process lives, and ESRCH once it has exited.
	PidfdAlive(fd int) error
}

// LinuxKernel is the running kernel.
type LinuxKernel struct{}

// PidfdOpen opens a pidfd for pid; it is close-on-exec.
func (LinuxKernel) PidfdOpen(pid int) (int, error) { return unix.PidfdOpen(pid, 0) }

// PidfdAlive polls the pidfd, which is readable once the process has exited, a zombie
// included: ESRCH then.
func (LinuxKernel) PidfdAlive(fd int) error {
	if fd < 0 || fd > maxFD {
		return unix.EBADF
	}
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		_, err := unix.Poll(fds, 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		switch {
		case err != nil:
			return err
		case fds[0].Revents&unix.POLLNVAL != 0:
			return unix.EBADF
		case fds[0].Revents&unix.POLLIN != 0:
			return unix.ESRCH
		}
		return nil
	}
}

// maxFD is the largest descriptor poll(2) takes in its 32-bit field.
const maxFD = 1<<31 - 1

// Pinner opens a guest's root. Proc is "/proc" on a host.
type Pinner struct {
	Proc   string
	Kernel Kernel
}

// Root is one running guest's root directory, held open with its init pinned (LX3, n3-D7).
// Every read and write below it keeps TB1, and each read is bounded (B5). Safe for one
// goroutine at a time; a read abandoned at its bound ends on its own.
type Root struct {
	sys    sys
	kernel Kernel
	// fd and pidfd are set once, before the root is handed out, and released once.
	fd    int
	pidfd int
	pid   int
	proc  string
	bound time.Duration

	mu       sync.Mutex
	closed   bool
	inflight int
	lateErr  error
}

// Info is a guest inode by fstat.
type Info struct {
	Mode     fs.FileMode
	UID, GID uint32
	Dev, Ino uint64
	Size     int64
}

// Pin is the guest root of the container whose init pid answers: the pid asked, a pidfd
// opened on it, <Proc>/<pid>/root opened as a directory, then the pidfd checked alive and the
// pid asked again. A dead or different pid is ErrMoved.
func (p Pinner) Pin(ctx context.Context, pid func(context.Context) (int, error)) (*Root, error) {
	return p.pinWith(ctx, linuxSys{}, pid)
}

func (p Pinner) pinWith(ctx context.Context, s sys, pid func(context.Context) (int, error)) (*Root, error) {
	if !filepath.IsAbs(p.Proc) || filepath.Clean(p.Proc) != p.Proc {
		return nil, fmt.Errorf("guestroot: proc %q is not an absolute, clean path", p.Proc)
	}
	if p.Kernel == nil {
		return nil, errors.New("guestroot: a pinner needs a kernel")
	}
	first, err := pid(ctx)
	if err != nil {
		return nil, fmt.Errorf("guestroot: the init pid: %w", err)
	}
	if first <= 1 {
		return nil, fmt.Errorf("guestroot: pid %d is not a container's init", first)
	}
	pidfd, err := p.Kernel.PidfdOpen(first)
	if errors.Is(err, unix.ESRCH) {
		return nil, fmt.Errorf("%w: pid %d is not running", ErrMoved, first)
	}
	if err != nil {
		return nil, fmt.Errorf("guestroot: pidfd of %d: %w", first, err)
	}
	r := &Root{sys: s, kernel: p.Kernel, fd: -1, pidfd: pidfd, pid: first, proc: p.Proc, bound: readBound}
	rootPath := p.Proc + "/" + strconv.Itoa(first) + "/root"
	fd, err := s.openat(unix.AT_FDCWD, rootPath, rootFlags|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("guestroot: %s: %w", rootPath, err), r.Close())
	}
	r.fd = fd
	if aerr := r.Alive(); aerr != nil {
		if errors.Is(aerr, ErrGone) {
			aerr = fmt.Errorf("%w: pid %d died while it was pinned", ErrMoved, first)
		}
		return nil, errors.Join(aerr, r.Close())
	}
	second, err := pid(ctx)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("guestroot: the init pid, asked again: %w", err), r.Close())
	}
	if second != first {
		return nil, errors.Join(fmt.Errorf("%w: pid %d became %d", ErrMoved, first, second), r.Close())
	}
	return r, nil
}

// OpenDir is a Root over a plain directory, with no init: the fixture trees' constructor (PID
// 0, Alive always nil). It is for tests, P4's included.
func OpenDir(dir string) (*Root, error) { return openDirWith(linuxSys{}, dir) }

func openDirWith(s sys, dir string) (*Root, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, fmt.Errorf("guestroot: %q is not an absolute, clean path", dir)
	}
	fd, err := s.openat(unix.AT_FDCWD, dir, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("guestroot: %s: %w", dir, err)
	}
	r := &Root{sys: s, fd: fd, pidfd: -1, bound: readBound}
	info, err := r.statFD(fd, dir)
	if err == nil && !info.Mode.IsDir() {
		err = fmt.Errorf("%w: %s is %s", ErrRefused, dir, kind(info.Mode))
	}
	if err != nil {
		return nil, errors.Join(err, r.Close())
	}
	return r, nil
}

// PID is the pinned init's pid, 0 for a fixture root.
func (r *Root) PID() int { return r.pid }

// Alive is nil while the pinned init lives (always, for a fixture root), ErrGone once it has
// died.
func (r *Root) Alive() error {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed || r.fd < 0 {
		return errClosed
	}
	if r.kernel == nil {
		return nil
	}
	err := r.kernel.PidfdAlive(r.pidfd)
	switch {
	case errors.Is(err, unix.ESRCH):
		return fmt.Errorf("%w: pid %d", ErrGone, r.pid)
	case err != nil:
		return fmt.Errorf("guestroot: pidfd of %d: %w", r.pid, err)
	}
	return nil
}

// Close releases the root's descriptors, or, while a read abandoned at its bound is still in
// flight, leaves the last such read to release them, so no descriptor number it may still use
// is reused under it. Its error holds any abandoned read's error kept since the last Close.
func (r *Root) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errClosed
	}
	r.closed = true
	err := r.lateErr
	r.lateErr = nil
	if r.inflight > 0 {
		return err
	}
	return errors.Join(err, r.release())
}

// InitExe is the pinned init's binary as the host sees it: the host's /proc/<pid>/exe opened
// O_PATH, a host path, so no magic link inside the guest is resolved (n3-D17). Its device and
// inode are compared with the guest's systemd, read by Stat.
func (r *Root) InitExe() (Info, error) {
	var info Info
	err := r.live(func() error {
		if r.kernel == nil {
			return errors.New("guestroot: a fixture root has no init")
		}
		exe := r.proc + "/" + strconv.Itoa(r.pid) + "/exe"
		fd, err := r.sys.openat(unix.AT_FDCWD, exe, rootFlags, 0)
		if err != nil {
			return fmt.Errorf("guestroot: %s: %w", exe, err)
		}
		info, err = r.statFD(fd, exe)
		return errors.Join(err, r.closeFD(fd, exe))
	})
	return info, err
}

// release closes the root's descriptors, under mu, once.
func (r *Root) release() error {
	var err error
	if r.fd >= 0 {
		err = r.sys.close(r.fd)
	}
	if r.pidfd >= 0 {
		err = errors.Join(err, r.sys.close(r.pidfd))
	}
	return err
}

// live runs op between two looks at the init, counted in flight: a root whose init died
// before or during it answers ErrGone. A write and InitExe, a host path, run so, unbounded.
func (r *Root) live(op func() error) error {
	if err := r.Alive(); err != nil {
		return err
	}
	if err := r.enter(); err != nil {
		return err
	}
	err := errors.Join(op(), r.leave())
	if aerr := r.Alive(); aerr != nil {
		return aerr
	}
	return err
}

func (r *Root) closeFD(fd int, path string) error {
	if err := r.sys.close(fd); err != nil {
		return fmt.Errorf("guestroot: close %s: %w", path, err)
	}
	return nil
}

func (r *Root) statFD(fd int, path string) (Info, error) {
	var st unix.Stat_t
	if err := r.sys.fstat(fd, &st); err != nil {
		return Info{}, fmt.Errorf("guestroot: %s: %w", path, err)
	}
	return Info{Mode: fileMode(st.Mode), UID: st.Uid, GID: st.Gid, Dev: st.Dev, Ino: st.Ino, Size: st.Size}, nil
}

// fileMode is a stat mode as an fs.FileMode.
func fileMode(raw uint32) fs.FileMode {
	m := fs.FileMode(raw & 0o777)
	switch raw & unix.S_IFMT {
	case unix.S_IFREG:
	case unix.S_IFDIR:
		m |= fs.ModeDir
	case unix.S_IFLNK:
		m |= fs.ModeSymlink
	case unix.S_IFIFO:
		m |= fs.ModeNamedPipe
	case unix.S_IFSOCK:
		m |= fs.ModeSocket
	case unix.S_IFCHR:
		m |= fs.ModeDevice | fs.ModeCharDevice
	case unix.S_IFBLK:
		m |= fs.ModeDevice
	default:
		m |= fs.ModeIrregular
	}
	for bit, mode := range map[uint32]fs.FileMode{unix.S_ISUID: fs.ModeSetuid, unix.S_ISGID: fs.ModeSetgid, unix.S_ISVTX: fs.ModeSticky} {
		if raw&bit != 0 {
			m |= mode
		}
	}
	return m
}

// kind is what an inode is, for a refusal.
func kind(m fs.FileMode) string {
	switch t := m.Type(); {
	case t == 0:
		return "a regular file"
	case t&fs.ModeDir != 0:
		return "a directory"
	case t&fs.ModeSymlink != 0:
		return "a symbolic link"
	case t&fs.ModeNamedPipe != 0:
		return "a FIFO"
	case t&fs.ModeSocket != 0:
		return "a socket"
	case t&fs.ModeDevice != 0:
		return "a device"
	}
	return "an irregular file"
}
