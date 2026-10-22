// Package lock is the member's three locks (Plan 00209 B 2.5; SPEC-n3 LK1-LK4): keeper.lock,
// link.lock and each seat's seat.lock, every one an flock(2) on a file opened through homefs
// (O_NOFOLLOW|O_CLOEXEC, made 0600), each acquire on its own open file description.
//
// Every acquire is LOCK_EX|LOCK_NB, tried three times 50 ms apart, because the status line, a
// client and the host's probe hold a momentary LOCK_SH on these files (B 2.5 L1, L3); still held
// after the third try is a *BusyError (errors.Is ErrBusy), and nothing waits longer.
//
// The order is enforced by type (LK2): a SeatLock comes only from a held KeeperLock's TakeSeat,
// or from AdoptSeat of a description a keeper passed the link. A seat lock's description is
// shared by the keeper and its link, so it is released by close alone, never LOCK_UN, which
// would free it under the other holder; keeper.lock and link.lock are released by LOCK_UN then
// close. A SeatHold is the seat lock taken by a removal (seat set-aside, guest remove, a guest
// bundle's identity change): it can only be released, and no store writer can be made from it.
//
// No code here unlinks, renames or truncates-by-open a lock file (LK3): a fresh inode at the
// same path would let a second process hold "the" lock.
package lock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const (
	// tries is how many times an acquire is tried before the lock is Busy (B 2.5).
	tries = 3
	// gap is the wait between two tries, longer than any probe holds its shared lock.
	gap = 50 * time.Millisecond
)

// ErrBusy is a lock another description holds (exit 75 where a verb reports it).
var ErrBusy = errors.New("lock: held by another process")

// ErrReleased is a lock used after it was released.
var ErrReleased = errors.New("lock: already released")

// BusyError is a lock still held after every try.
type BusyError struct {
	Path string
}

func (e *BusyError) Error() string { return "lock: " + e.Path + " is held by another process" }

// Is makes errors.Is(err, ErrBusy) match.
func (e *BusyError) Is(target error) bool { return target == ErrBusy }

// Clock is what the wait between tries sleeps on: the system's in production, a fake in tests.
type Clock interface {
	Sleep(ctx context.Context, d time.Duration) error
}

// SystemClock sleeps for real.
func SystemClock() Clock { return systemClock{} }

type systemClock struct{}

func (systemClock) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// held is one held description, closed once.
type held struct {
	mu   sync.Mutex
	file *os.File
}

func (h *held) take() (*os.File, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.file == nil {
		return nil, ErrReleased
	}
	return h.file, nil
}

// end closes the description, after LOCK_UN when unlock is set.
func (h *held) end(unlock bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.file == nil {
		return ErrReleased
	}
	f := h.file
	h.file = nil
	var err error
	if unlock {
		err = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return fmt.Errorf("lock: release %s: %w", f.Name(), err)
	}
	return nil
}

// KeeperLock is keeper.lock, held for the session's life: one keeper per runtime directory.
type KeeperLock struct {
	held

	clock Clock
}

// TakeKeeper takes run's keeper.lock; root must hold run's directory.
func TakeKeeper(ctx context.Context, clock Clock, root *homefs.Root, run paths.Run) (*KeeperLock, error) {
	f, err := acquire(ctx, clock, root, run.KeeperLock())
	if err != nil {
		return nil, err
	}
	return &KeeperLock{held: held{file: f}, clock: clock}, nil
}

// Release releases the keeper lock: LOCK_UN, then close.
func (k *KeeperLock) Release() error { return k.end(true) }

// TakeSeat takes seat's seat.lock for this keeper's session (LK2); root must hold the seat's
// directory. The keeper must still hold its own lock.
func (k *KeeperLock) TakeSeat(ctx context.Context, root *homefs.Root, seat paths.Seat) (*SeatLock, error) {
	if _, err := k.take(); err != nil {
		return nil, err
	}
	f, err := acquire(ctx, k.clock, root, seat.Lock())
	if err != nil {
		return nil, err
	}
	return &SeatLock{held: held{file: f}, seat: seat}, nil
}

// LinkLock is link.lock, held while the link runs: one link, so one server of link.sock.
type LinkLock struct {
	held
}

// TakeLink takes run's link.lock; root must hold run's directory.
func TakeLink(ctx context.Context, clock Clock, root *homefs.Root, run paths.Run) (*LinkLock, error) {
	f, err := acquire(ctx, clock, root, run.LinkLock())
	if err != nil {
		return nil, err
	}
	return &LinkLock{held: held{file: f}}, nil
}

// Release releases the link lock: LOCK_UN, then close.
func (l *LinkLock) Release() error { return l.end(true) }

// SeatLock is a session's hold of one seat, the store writer's only key (MS2).
type SeatLock struct {
	held

	seat paths.Seat
}

// AdoptSeat is the link's SeatLock from the description its keeper passed it: f must already
// hold the seat's lock, so a description another holds is refused (ErrBusy) and closed.
func AdoptSeat(f *os.File, seat paths.Seat) (*SeatLock, error) {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		err = fmt.Errorf("lock: adopt %s: %w", seat.Lock(), err)
		if errors.Is(err, unix.EWOULDBLOCK) {
			err = &BusyError{Path: seat.Lock()}
		}
		return nil, errors.Join(err, f.Close())
	}
	return &SeatLock{held: held{file: f}, seat: seat}, nil
}

// Seat is the seat this lock holds.
func (s *SeatLock) Seat() paths.Seat { return s.seat }

// Dup is a second descriptor of the same held description, for the link (B 2.5): the seat
// stays held until it and this lock are both closed.
func (s *SeatLock) Dup() (*os.File, error) {
	f, err := s.take()
	if err != nil {
		return nil, err
	}
	fd, err := unix.FcntlInt(f.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("lock: duplicate %s: %w", f.Name(), err)
	}
	return os.NewFile(uintptr(fd), f.Name()), nil
}

// WriteRecord replaces the lock file's content with data through the held description (LK4):
// written at the start, cut to its length and synced, so the file is never empty between.
func (s *SeatLock) WriteRecord(data []byte) error {
	f, err := s.take()
	if err != nil {
		return err
	}
	if _, err := f.WriteAt(data, 0); err != nil {
		return fmt.Errorf("lock: write the record of %s: %w", f.Name(), err)
	}
	if err := f.Truncate(int64(len(data))); err != nil {
		return fmt.Errorf("lock: write the record of %s: %w", f.Name(), err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("lock: write the record of %s: %w", f.Name(), err)
	}
	return nil
}

// Close closes this descriptor of the seat lock; never LOCK_UN, which would free the seat
// under the link's copy.
func (s *SeatLock) Close() error { return s.end(false) }

// SeatHold is a removal's hold of a seat: it proves no session holds the seat while files
// beside seat.lock are removed or renamed, and is good for nothing else.
type SeatHold struct {
	held
}

// HoldSeat takes seat's seat.lock for a removal; root must hold the seat's directory.
func HoldSeat(ctx context.Context, clock Clock, root *homefs.Root, seat paths.Seat) (*SeatHold, error) {
	f, err := acquire(ctx, clock, root, seat.Lock())
	if err != nil {
		return nil, err
	}
	return &SeatHold{held: held{file: f}}, nil
}

// Release closes the hold.
func (h *SeatHold) Release() error { return h.end(false) }

// acquire opens path through root and takes LOCK_EX|LOCK_NB on it, trying tries times gap
// apart; the file is closed unless it is returned held.
func acquire(ctx context.Context, clock Clock, root *homefs.Root, path string) (*os.File, error) {
	f, err := root.OpenLock(path)
	if err != nil {
		return nil, err
	}
	for try := 1; ; try++ {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		switch {
		case err == nil:
			return f, nil
		case !errors.Is(err, unix.EWOULDBLOCK):
			return nil, errors.Join(fmt.Errorf("lock: %s: %w", path, err), f.Close())
		case try == tries:
			return nil, errors.Join(&BusyError{Path: path}, f.Close())
		}
		if err := clock.Sleep(ctx, gap); err != nil {
			return nil, errors.Join(err, f.Close())
		}
	}
}
