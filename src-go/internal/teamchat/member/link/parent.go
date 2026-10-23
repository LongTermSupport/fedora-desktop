package link

import (
	"errors"
	"fmt"
	"math"
	"os"

	"golang.org/x/sys/unix"
)

// errParentGone is a keeper that was not the parent once its pidfd was open: it had already
// exited, and the link was handed to another process.
var errParentGone = errors.New("link: the keeper is no longer this process's parent")

// pidfdParent is the keeper watched by a pidfd (LN1), held by the runtime's poller: Exited
// closes when it is readable, which is when the process has exited. Close stops the watch.
type pidfdParent struct {
	pid    int
	file   *os.File
	exited chan struct{}
	done   chan struct{}
}

// OpenParent watches this process's parent, the keeper that started the link.
func OpenParent() (Parent, error) { return watchParent(os.Getppid) }

// watchParent opens the parent's pidfd, then reads the parent again: a keeper that died before
// its pidfd was open has left the link to another parent, whose pidfd watches the wrong
// process.
func watchParent(getppid func() int) (Parent, error) {
	pid := getppid()
	if pid <= 1 {
		return nil, fmt.Errorf("%w (its parent is %d)", errParentGone, pid)
	}
	p, err := watch(pid)
	if err != nil {
		return nil, err
	}
	if now := getppid(); now != pid {
		return nil, errors.Join(fmt.Errorf("%w (it was %d, it is %d)", errParentGone, pid, now), p.Close())
	}
	return p, nil
}

// watch is pid watched by its pidfd until Close.
func watch(pid int) (*pidfdParent, error) {
	f, err := openPidfd(pid)
	if err != nil {
		return nil, err
	}
	return watchFile(pid, f)
}

// openPidfd is pid's pidfd, held by the runtime's poller.
func openPidfd(pid int) (*os.File, error) {
	fd, err := unix.PidfdOpen(pid, unix.PIDFD_NONBLOCK)
	if err != nil {
		return nil, fmt.Errorf("link: open the keeper's pidfd: %w", err)
	}
	return os.NewFile(uintptr(fd), "pidfd"), nil
}

// watchFile watches pid through its open pidfd f, which Close closes.
func watchFile(pid int, f *os.File) (*pidfdParent, error) {
	rc, err := f.SyscallConn()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("link: watch the keeper's pidfd: %w", err), f.Close())
	}
	p := &pidfdParent{pid: pid, file: f, exited: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(p.done)
		// Read resets readiness the poller has already seen before its first call, so
		// every call tests the pidfd; the wait ends with an error when Close closes it.
		if err := rc.Read(pidfdReadable); err == nil {
			close(p.exited)
		}
	}()
	return p, nil
}

// pidfdReadable is whether the pidfd fd is readable now, which is its process's end; a poll
// that fails, or a descriptor no poll can name, ends the wait rather than parking it.
func pidfdReadable(fd uintptr) bool {
	if fd > math.MaxInt32 {
		return true
	}
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, 0)
	return err != nil || n > 0
}

func (p *pidfdParent) PID() int { return p.pid }

func (p *pidfdParent) Exited() <-chan struct{} { return p.exited }

// Close stops the watch and closes the pidfd.
func (p *pidfdParent) Close() error {
	err := p.file.Close()
	<-p.done
	return err
}
