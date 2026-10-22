package keeper

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// procStat is the production Proc: /proc/<pid>/stat.
type procStat struct{}

func (procStat) StartTicks(pid int) (int64, error) {
	if pid <= 0 {
		return 0, fmt.Errorf("keeper: pid %d is no process", pid)
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, err
	}
	return parseStartTicks(string(data))
}

// startField is field 22's index among the fields after the command name, which start at
// field 3 and follow the last ")" (the name may hold one).
const startField = 22 - 3

func parseStartTicks(stat string) (int64, error) {
	i := strings.LastIndexByte(stat, ')')
	if i < 0 {
		return 0, errors.New("keeper: no command name in a process status")
	}
	fields := strings.Fields(stat[i+1:])
	if len(fields) <= startField {
		return 0, errors.New("keeper: no start time in a process status")
	}
	return strconv.ParseInt(fields[startField], 10, 64)
}

// osPidfds is the production Pidfds: pidfd_open, non-blocking, so the runtime's poller waits
// for the exit and Close ends the wait.
type osPidfds struct{}

func (osPidfds) Open(pid int) (Pidfd, error) {
	fd, err := unix.PidfdOpen(pid, unix.PIDFD_NONBLOCK)
	if err != nil {
		return nil, fmt.Errorf("keeper: pidfd_open %d: %w", pid, err)
	}
	p := &pidfd{f: os.NewFile(uintptr(fd), "pidfd"), exited: make(chan struct{})}
	go p.wait()
	return p, nil
}

type pidfd struct {
	f      *os.File
	exited chan struct{}
}

// wait closes exited once the pidfd is readable, which is the process's end.
func (p *pidfd) wait() {
	rc, err := p.f.SyscallConn()
	if err != nil {
		return
	}
	err = rc.Read(func(fd uintptr) bool {
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, perr := unix.Poll(fds, 0)
		return perr != nil || n > 0
	})
	if err == nil {
		close(p.exited)
	}
}

func (p *pidfd) Signal(sig syscall.Signal) error {
	rc, err := p.f.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	if err := rc.Control(func(fd uintptr) { serr = unix.PidfdSendSignal(int(fd), sig, nil, 0) }); err != nil {
		return err
	}
	if errors.Is(serr, unix.ESRCH) {
		return nil
	}
	return serr
}

func (p *pidfd) Exited() <-chan struct{} { return p.exited }

func (p *pidfd) Close() error { return p.f.Close() }

// inotify is the production Watcher: one inotify instance, non-blocking, so a read waits in
// the runtime's poller and Close ends it.
type inotify struct {
	f    *os.File
	done chan struct{}
	once sync.Once
}

func newInotify() (*inotify, error) {
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("keeper: inotify_init1: %w", err)
	}
	return &inotify{f: os.NewFile(uintptr(fd), "inotify"), done: make(chan struct{})}, nil
}

// watchMask is every way desired.json can change in its directory.
const watchMask = unix.IN_CLOSE_WRITE | unix.IN_MOVED_TO | unix.IN_MOVED_FROM | unix.IN_CREATE |
	unix.IN_DELETE | unix.IN_DELETE_SELF | unix.IN_MOVE_SELF | unix.IN_ONLYDIR

func (w *inotify) Watch(dir string) (<-chan WatchEvent, error) {
	rc, err := w.f.SyscallConn()
	if err != nil {
		return nil, err
	}
	var werr error
	if err := rc.Control(func(fd uintptr) { _, werr = unix.InotifyAddWatch(int(fd), dir, watchMask) }); err != nil {
		return nil, err
	}
	if werr != nil {
		return nil, fmt.Errorf("keeper: watch %s: %w", dir, werr)
	}
	ch := make(chan WatchEvent, 16)
	go w.read(ch)
	return ch, nil
}

func (w *inotify) read(ch chan<- WatchEvent) {
	defer close(ch)
	buf := make([]byte, 64<<10)
	for {
		n, err := w.f.Read(buf)
		if err != nil {
			return
		}
		evs, err := parseEvents(buf[:n])
		if err != nil {
			return
		}
		for _, ev := range evs {
			select {
			case ch <- ev:
			case <-w.done:
				return
			}
		}
	}
}

func (w *inotify) Close() error {
	w.once.Do(func() { close(w.done) })
	return w.f.Close()
}

// parseEvents reads the kernel's inotify records.
func parseEvents(b []byte) ([]WatchEvent, error) {
	var out []WatchEvent
	for len(b) > 0 {
		if len(b) < unix.SizeofInotifyEvent {
			return nil, errors.New("keeper: a short inotify record")
		}
		mask := binary.NativeEndian.Uint32(b[4:])
		n := int(binary.NativeEndian.Uint32(b[12:]))
		end := unix.SizeofInotifyEvent + n
		if len(b) < end {
			return nil, errors.New("keeper: an inotify record cut short")
		}
		name := string(bytes.TrimRight(b[unix.SizeofInotifyEvent:end], "\x00"))
		out = append(out, WatchEvent{Name: name, Op: mask})
		b = b[end:]
	}
	return out, nil
}
