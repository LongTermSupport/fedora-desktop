package proof

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Watcher says when a transcript may have changed. Changes coalesces: one value pending
// stands for every change since it was sent. It is closed when the watcher ends, by Close or by
// a failure Close then returns.
type Watcher interface {
	Changes() <-chan struct{}
	Close() error
}

const (
	// fileMask is growth, a truncation and the file moved or removed from under its name.
	fileMask = unix.IN_MODIFY | unix.IN_MOVE_SELF | unix.IN_DELETE_SELF | unix.IN_DONT_FOLLOW
	// dirMask is a name made, moved or removed in the directory: a rotation, a /clear's new
	// file, the transcript made after the watch.
	dirMask = unix.IN_CREATE | unix.IN_MOVED_TO | unix.IN_MOVED_FROM | unix.IN_DELETE |
		unix.IN_DELETE_SELF | unix.IN_MOVE_SELF | unix.IN_ONLYDIR | unix.IN_DONT_FOLLOW
	// madeHere is a name that now stands for a new inode, which the file's watch must follow.
	madeHere  = unix.IN_CREATE | unix.IN_MOVED_TO
	readBatch = 64 * (unix.SizeofInotifyEvent + unix.NAME_MAX + 1)
)

// inotifyEvent is the fixed part of an inotify record; the name follows it.
type inotifyEvent struct {
	Wd     int32
	Mask   uint32
	Cookie uint32
	Len    uint32
}

type watcher struct {
	f         *os.File
	path      string
	base      string
	file, dir int
	changes   chan struct{}
	done      chan struct{}
	err       error
}

// Watch holds an inotify watch on the transcript at path and on its directory (for a new path
// or a rotation), never following a link. Growth, a truncation, a rotation, any name made or
// removed in the directory, and an overflowed event queue (IN_Q_OVERFLOW) are each a change.
// The file need not exist yet; its directory must.
func Watch(path string) (Watcher, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("%w: %s is not an absolute, clean path", ErrRefused, path)
	}
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("proof: inotify: %w", err)
	}
	w := &watcher{path: path, base: filepath.Base(path), file: -1, changes: make(chan struct{}, 1), done: make(chan struct{})}
	dir, err := unix.InotifyAddWatch(fd, filepath.Dir(path), dirMask)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("proof: watch %s: %w", filepath.Dir(path), err), unix.Close(fd))
	}
	w.dir = dir
	if err := w.watchFile(fd); err != nil {
		return nil, errors.Join(err, unix.Close(fd))
	}
	w.f = os.NewFile(uintptr(fd), "inotify")
	go w.run()
	return w, nil
}

func (w *watcher) Changes() <-chan struct{} { return w.changes }

// Close ends the watch and returns the failure that ended it first, if one did.
func (w *watcher) Close() error {
	err := w.f.Close()
	<-w.done
	return errors.Join(err, w.err)
}

func (w *watcher) run() {
	defer close(w.done)
	defer close(w.changes)
	buf := make([]byte, readBatch)
	for {
		n, err := w.f.Read(buf)
		if errors.Is(err, os.ErrClosed) {
			return
		}
		var change, rewatch bool
		if err == nil {
			change, rewatch, err = w.judge(buf[:n])
		}
		if err == nil && rewatch {
			err = w.control(w.watchFile)
		}
		if err != nil {
			w.err = fmt.Errorf("proof: watch %s: %w", w.path, err)
			return
		}
		if change {
			select {
			case w.changes <- struct{}{}:
			default:
			}
		}
	}
}

// control runs f with the inotify fd, held open meanwhile.
func (w *watcher) control(f func(fd int) error) error {
	conn, err := w.f.SyscallConn()
	if err != nil {
		return err
	}
	var ferr error
	if err := conn.Control(func(fd uintptr) { ferr = f(int(fd)) }); err != nil {
		return err
	}
	return ferr
}

// watchFile watches the inode now at the path; no file there yet is no watch, until one is made.
func (w *watcher) watchFile(fd int) error {
	wd, err := unix.InotifyAddWatch(fd, w.path, fileMask)
	switch {
	case errors.Is(err, unix.ENOENT):
		w.file = -1
	case err != nil:
		return fmt.Errorf("proof: watch %s: %w", w.path, err)
	default:
		w.file = wd
	}
	return nil
}

// judge reads the events in buf: whether any is a change, and whether a new inode now has the
// transcript's name.
func (w *watcher) judge(buf []byte) (bool, bool, error) {
	var change, rewatch bool
	for len(buf) > 0 {
		if len(buf) < unix.SizeofInotifyEvent {
			return false, false, fmt.Errorf("a short inotify event of %d bytes", len(buf))
		}
		var ev inotifyEvent
		if _, err := binary.Decode(buf, binary.NativeEndian, &ev); err != nil {
			return false, false, err
		}
		wd, mask := int(ev.Wd), ev.Mask
		end := unix.SizeofInotifyEvent + int(ev.Len)
		if len(buf) < end {
			return false, false, fmt.Errorf("an inotify event's name cut at %d of %d bytes", len(buf), end)
		}
		name := string(bytesBeforeNUL(buf[unix.SizeofInotifyEvent:end]))
		buf = buf[end:]
		switch {
		case mask&unix.IN_Q_OVERFLOW != 0:
			change = true
		case wd == w.file && mask&unix.IN_IGNORED == 0:
			change = true
		case wd == w.dir && mask&dirMask != 0:
			change = true
			rewatch = rewatch || (name == w.base && mask&madeHere != 0)
		}
	}
	return change, rewatch, nil
}

func bytesBeforeNUL(b []byte) []byte {
	for i, c := range b {
		if c == 0 {
			return b[:i]
		}
	}
	return b
}
