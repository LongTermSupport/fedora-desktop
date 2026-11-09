package ccc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// suspendByte is ctrl+z. Claude Code's terminal UI stops itself with SIGSTOP when it reads it,
// and a container has no shell to resume it from, so the session would freeze for good.
const suspendByte = 0x1a

// drainWait bounds the wait for the last of the session's output once it has exited, in case a
// process it left behind still holds the terminal.
const drainWait = 2 * time.Second

// Guard runs a session on the person's terminal.
type Guard interface {
	Run(c execx.Cmd) (execx.Exit, error)
}

// PtyGuard is the ctrl+z guard: the session runs on a pseudo-terminal of ccc's own, and the
// person's keystrokes are copied to it with the suspend byte removed, as the hooks-daemon
// supervisor's strip_suspend does inside a ccy container. A folder with no hooks daemon has no
// supervisor, so the guard is here instead.
type PtyGuard struct {
	Foreground execx.Foregrounder
	In         io.Reader
	Out        io.Writer
	// InFd is In's terminal, put in raw mode and followed for its size; -1 when In is not one.
	InFd int
}

// stripSuspend is p without its suspend bytes.
func stripSuspend(p []byte) []byte {
	return bytes.ReplaceAll(p, []byte{suspendByte}, nil)
}

// Run implements Guard.
func (g *PtyGuard) Run(c execx.Cmd) (execx.Exit, error) {
	master, slave, err := openPty()
	if err != nil {
		return execx.Exit{}, err
	}
	restore, err := g.adopt(master)
	if err != nil {
		return execx.Exit{}, errors.Join(err, slave.Close(), master.Close())
	}
	keys, err := g.copyKeys(master)
	if err != nil {
		return execx.Exit{}, errors.Join(err, restore(), slave.Close(), master.Close())
	}
	stopResize := g.followSize(master)
	c.Stdin, c.Stdout, c.Stderr = slave, slave, slave
	drained := make(chan error, 1)
	go func() { drained <- copyOut(g.Out, master) }()
	exit, runErr := g.Foreground.Foreground(c)
	stopResize()
	closeErr := slave.Close()
	var drainErr error
	select {
	case drainErr = <-drained:
	case <-time.After(drainWait):
	}
	// The key copy is stopped, then the pseudo-terminal closed, which ends a write to it the
	// session left unread; only then is the copy waited for.
	stopErr := keys.stop()
	masterErr := master.Close()
	keysErr := keys.wait()
	return exit, errors.Join(runErr, closeErr, drainErr, stopErr, masterErr, keysErr, restore())
}

// adopt puts the person's terminal in raw mode, so every key reaches the session as a byte,
// and gives the pseudo-terminal its size. The returned function puts the terminal back.
func (g *PtyGuard) adopt(master *os.File) (func() error, error) {
	if g.InFd < 0 {
		return func() error { return nil }, nil
	}
	if err := copySize(g.InFd, master); err != nil {
		return nil, err
	}
	saved, err := unix.IoctlGetTermios(g.InFd, unix.TCGETS)
	if err != nil {
		return nil, fmt.Errorf("read the terminal's mode: %w", err)
	}
	raw := *saved
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN], raw.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(g.InFd, unix.TCSETS, &raw); err != nil {
		return nil, fmt.Errorf("put the terminal in raw mode: %w", err)
	}
	signal.Ignore(syscall.SIGTSTP)
	return func() error {
		if err := unix.IoctlSetTermios(g.InFd, unix.TCSETS, saved); err != nil {
			return fmt.Errorf("put the terminal back: %w", err)
		}
		return nil
	}, nil
}

// followSize passes the person's window size on to the session whenever it changes. The
// session's pseudo-terminal is not its controlling terminal, so the kernel tells no one of a
// change: the size is set and SIGWINCH sent to ccc's process group, which the session is in.
// ccc receives its own signal too, finds the size unchanged and sends nothing more. The returned
// stop ends the follower and waits for it.
func (g *PtyGuard) followSize(master *os.File) func() {
	if g.InFd < 0 {
		return func() {}
	}
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	done := make(chan struct{})
	var follower sync.WaitGroup
	follower.Go(func() {
		var last *unix.Winsize
		for {
			select {
			case <-done:
				return
			case <-winch:
				ws, err := unix.IoctlGetWinsize(g.InFd, unix.TIOCGWINSZ)
				if err != nil || (last != nil && *ws == *last) {
					continue
				}
				last = ws
				if control(master, func(fd int) error { return unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, ws) }) != nil {
					continue
				}
				if err := unix.Kill(0, syscall.SIGWINCH); err != nil {
					continue
				}
			}
		}
	})
	return func() {
		signal.Stop(winch)
		close(done)
		follower.Wait()
	}
}

func copySize(from int, master *os.File) error {
	ws, err := unix.IoctlGetWinsize(from, unix.TIOCGWINSZ)
	if err != nil {
		return fmt.Errorf("read the terminal's size: %w", err)
	}
	return control(master, func(fd int) error { return unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, ws) })
}

// copyOut copies the session's output until the pseudo-terminal closes, which Linux reports as
// EIO once the session's side is gone.
func copyOut(w io.Writer, master *os.File) error {
	_, err := io.Copy(w, master)
	if err == nil || errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed) {
		return nil
	}
	return fmt.Errorf("copy the session's output: %w", err)
}

// keyCopy is the copy of the person's keys to the session, on a goroutine of its own.
type keyCopy struct {
	// pipe stops the copy: once its write end is closed, its read end polls as hung up.
	pipe [2]int
	done chan struct{}
	// err is why the copy could not wait for a key, read once done is closed.
	err error
}

// copyKeys starts the copy of In to master; stop then wait end it.
func (g *PtyGuard) copyKeys(master *os.File) (*keyCopy, error) {
	k := &keyCopy{done: make(chan struct{})}
	if err := unix.Pipe2(k.pipe[:], unix.O_CLOEXEC); err != nil {
		return nil, fmt.Errorf("make the pipe that stops the key copy: %w", err)
	}
	go k.copy(master, g.In, g.InFd)
	return k, nil
}

// copy copies in to master without the suspend byte until either side ends or the copy is
// stopped. A read of the person's terminal blocks until the next key, and closing the terminal
// does not end it (os.Stdin is in blocking mode), so the terminal is read only once poll finds a
// key on it. An in with no terminal (inFd -1, a test's reader) is read directly, and must end by
// itself.
func (k *keyCopy) copy(master *os.File, in io.Reader, inFd int) {
	defer close(k.done)
	buf := make([]byte, 4096)
	for {
		if inFd >= 0 {
			waiting, err := k.keyWaiting(inFd)
			if err != nil || !waiting {
				k.err = err
				return
			}
		}
		n, err := in.Read(buf)
		if n > 0 {
			if _, werr := master.Write(stripSuspend(buf[:n])); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// keyWaiting waits until the terminal at fd can be read (a key, or a hang-up the read then
// reports); false once the copy is stopped.
func (k *keyCopy) keyWaiting(fd int) (bool, error) {
	terminal, err := pollIn(fd)
	if err != nil {
		return false, err
	}
	stop, err := pollIn(k.pipe[0])
	if err != nil {
		return false, err
	}
	fds := []unix.PollFd{terminal, stop}
	for {
		_, err := unix.Poll(fds, -1)
		switch {
		case errors.Is(err, unix.EINTR):
			continue
		case err != nil:
			return false, fmt.Errorf("wait for a key: %w", err)
		}
		return fds[1].Revents == 0, nil
	}
}

// pollIn is poll's entry for fd becoming readable.
func pollIn(fd int) (unix.PollFd, error) {
	if fd < 0 || fd > math.MaxInt32 {
		return unix.PollFd{}, fmt.Errorf("wait for a key: descriptor %d is not one poll takes", fd)
	}
	return unix.PollFd{Fd: int32(fd), Events: unix.POLLIN}, nil
}

// stop ends a wait for a key at once; a key being copied is still written.
func (k *keyCopy) stop() error {
	if err := unix.Close(k.pipe[1]); err != nil {
		return fmt.Errorf("stop the key copy: %w", err)
	}
	return nil
}

// wait is the copy's end, and closes the pipe that stopped it.
func (k *keyCopy) wait() error {
	<-k.done
	err := k.err
	if cerr := unix.Close(k.pipe[0]); cerr != nil {
		err = errors.Join(err, fmt.Errorf("close the pipe that stopped the key copy: %w", cerr))
	}
	return err
}

// openPty is a new pseudo-terminal pair, neither side made ccc's controlling terminal.
func openPty() (*os.File, *os.File, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open a pseudo-terminal: %w", err)
	}
	var n int
	err = control(master, func(fd int) error {
		if lerr := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); lerr != nil {
			return lerr
		}
		var nerr error
		n, nerr = unix.IoctlGetInt(fd, unix.TIOCGPTN)
		return nerr
	})
	if err != nil {
		return nil, nil, errors.Join(fmt.Errorf("unlock the pseudo-terminal: %w", err), master.Close())
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, errors.Join(fmt.Errorf("open the pseudo-terminal's other side: %w", err), master.Close())
	}
	return master, slave, nil
}

// control runs fn on f's descriptor without taking it out of Go's poller, as f.Fd would.
func control(f *os.File, fn func(fd int) error) error {
	raw, err := f.SyscallConn()
	if err != nil {
		return fmt.Errorf("reach the pseudo-terminal: %w", err)
	}
	var fnErr error
	if err := raw.Control(func(fd uintptr) { fnErr = fn(int(fd)) }); err != nil {
		return fmt.Errorf("reach the pseudo-terminal: %w", err)
	}
	return fnErr
}
