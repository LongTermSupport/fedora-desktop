package teamsync

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// SeatWatcher is LN9's inotify watch on a seat directory: a change is seat.json or token
// created, written, renamed or removed, or an overflow, coalesced to one pending change.
type SeatWatcher struct {
	f     *os.File
	names map[string]bool
	ch    chan struct{}
	done  chan struct{}
}

// WatchSeat watches s's directory, which must exist; the link is not followed.
func WatchSeat(s paths.Seat) (*SeatWatcher, error) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, fmt.Errorf("teamsync: inotify: %w", err)
	}
	const mask = unix.IN_CLOSE_WRITE | unix.IN_MOVED_TO | unix.IN_MOVED_FROM | unix.IN_CREATE | unix.IN_DELETE |
		unix.IN_ONLYDIR | unix.IN_DONT_FOLLOW
	if _, err := unix.InotifyAddWatch(fd, s.Dir(), mask); err != nil {
		if cerr := unix.Close(fd); cerr != nil {
			return nil, fmt.Errorf("teamsync: watch the seat directory: %w (and its close: %w)", err, cerr)
		}
		return nil, fmt.Errorf("teamsync: watch the seat directory: %w", err)
	}
	// A non-blocking descriptor goes through the runtime's poller, so Close ends a Read.
	w := &SeatWatcher{
		f:     os.NewFile(uintptr(fd), "inotify"),
		names: map[string]bool{filepath.Base(s.Bundle()): true, filepath.Base(s.Token()): true},
		ch:    make(chan struct{}, 1),
		done:  make(chan struct{}),
	}
	go w.read()
	return w, nil
}

// Changes receives once per burst of changes.
func (w *SeatWatcher) Changes() <-chan struct{} { return w.ch }

// Close ends the watch and waits for its reader.
func (w *SeatWatcher) Close() error {
	err := w.f.Close()
	<-w.done
	return err
}

func (w *SeatWatcher) read() {
	defer close(w.done)
	buf := make([]byte, 64*1024)
	for {
		n, err := w.f.Read(buf)
		if err != nil {
			return
		}
		if relevant(buf[:n], w.names) {
			select {
			case w.ch <- struct{}{}:
			default:
			}
		}
	}
}

// relevant is whether a read of inotify events holds one for a watched name, or an overflow.
func relevant(buf []byte, names map[string]bool) bool {
	for len(buf) >= unix.SizeofInotifyEvent {
		mask := binary.NativeEndian.Uint32(buf[4:8])
		n := int(binary.NativeEndian.Uint32(buf[12:16]))
		if mask&unix.IN_Q_OVERFLOW != 0 {
			return true
		}
		end := unix.SizeofInotifyEvent + n
		if end > len(buf) {
			return false
		}
		name := buf[unix.SizeofInotifyEvent:end]
		for len(name) > 0 && name[len(name)-1] == 0 {
			name = name[:len(name)-1]
		}
		if names[string(name)] {
			return true
		}
		buf = buf[end:]
	}
	return false
}
