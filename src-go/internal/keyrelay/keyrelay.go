// Package keyrelay is the stdin seam for a relay of the person's terminal to a child (issue
// #260; project_stdin_seam holds every read of stdin to its seams): the keys typed are copied,
// through a filter, to the child's side for as long as the child runs. It reads stdin without a
// bound, and is a seam because it never waits on a read alone:
//
//   - it is a relay of the terminal to a child, not a read of input a command waits for;
//   - it ends on a stop signal its owner controls, the stop pipe (Stop), whether or not a key
//     is ever typed;
//   - on a terminal it reads only once poll finds input there; a read of a terminal blocks until
//     the next key, and closing the terminal does not end it (os.Stdin is in blocking mode), so
//     the wait is in poll, which the stop pipe ends.
//
// A stdin with no terminal (inFd -1, a test's reader) is read directly, and must end by itself;
// its owner (ccc's guard) only runs on a terminal.
package keyrelay

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"golang.org/x/sys/unix"
)

// Relay is the copy of the person's keys to the child, on a goroutine of its own.
type Relay struct {
	// pipe stops the copy: once its write end is closed, its read end polls as hung up.
	pipe [2]int
	done chan struct{}
	// err is why the copy could not wait for a key, read once done is closed.
	err error
}

// Start starts the copy of stdin (whose terminal is inFd, -1 for none) to master, each read
// passed through filter (nil: as it is); Stop then Wait end it.
func Start(master *os.File, stdin io.Reader, inFd int, filter func([]byte) []byte) (*Relay, error) {
	k := &Relay{done: make(chan struct{})}
	if err := unix.Pipe2(k.pipe[:], unix.O_CLOEXEC); err != nil {
		return nil, fmt.Errorf("make the pipe that stops the key copy: %w", err)
	}
	if filter == nil {
		filter = func(p []byte) []byte { return p }
	}
	go k.copy(master, stdin, inFd, filter)
	return k, nil
}

// Stop ends a wait for a key at once; a key being copied is still written.
func (k *Relay) Stop() error {
	if err := unix.Close(k.pipe[1]); err != nil {
		return fmt.Errorf("stop the key copy: %w", err)
	}
	return nil
}

// Wait is the copy's end, and closes the pipe that stopped it.
func (k *Relay) Wait() error {
	<-k.done
	err := k.err
	if cerr := unix.Close(k.pipe[0]); cerr != nil {
		err = errors.Join(err, fmt.Errorf("close the pipe that stopped the key copy: %w", cerr))
	}
	return err
}

// copy copies stdin to master through filter until either side ends or the copy is stopped.
// The terminal is read only once poll finds a key on it (see the package doc).
func (k *Relay) copy(master *os.File, stdin io.Reader, inFd int, filter func([]byte) []byte) {
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
		n, err := stdin.Read(buf)
		if n > 0 {
			if _, werr := master.Write(filter(buf[:n])); werr != nil {
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
func (k *Relay) keyWaiting(fd int) (bool, error) {
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
