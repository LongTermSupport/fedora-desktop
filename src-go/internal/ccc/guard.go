package ccc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
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
	stopResize := g.followSize(master)
	c.Stdin, c.Stdout, c.Stderr = slave, slave, slave
	drained := make(chan error, 1)
	go func() { drained <- copyOut(g.Out, master) }()
	go copyIn(master, g.In)
	exit, runErr := g.Foreground.Foreground(c)
	stopResize()
	closeErr := slave.Close()
	var drainErr error
	select {
	case drainErr = <-drained:
	case <-time.After(drainWait):
	}
	return exit, errors.Join(runErr, closeErr, drainErr, restore(), master.Close())
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
// ccc receives its own signal too, finds the size unchanged and sends nothing more.
func (g *PtyGuard) followSize(master *os.File) func() {
	if g.InFd < 0 {
		return func() {}
	}
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
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
	}()
	return func() {
		signal.Stop(winch)
		close(done)
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

// copyIn copies the person's keys to the session without the suspend byte, until either side
// ends. It is not waited for: a read of the terminal blocks until the next key.
func copyIn(master *os.File, r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
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
