package state

import (
	"errors"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
)

// ErrNoLock is a StoreWriter asked for without a seat lock.
var ErrNoLock = errors.New("state: a store writer needs the seat lock")

// StoreWriter is the one writer of a seat's store.json (MS2): made only from the held
// *lock.SeatLock, it writes through the seat directory it opened, each change one atomic
// replace, and refuses once the lock is closed.
type StoreWriter struct {
	lock *lock.SeatLock
	dir  *homefs.Root
	team string
}

// NewStoreWriter opens l's seat directory below root, which must be the member's own, mode 0700.
func NewStoreWriter(root *homefs.Root, l *lock.SeatLock) (*StoreWriter, error) {
	if l == nil {
		return nil, ErrNoLock
	}
	if err := l.Held(); err != nil {
		return nil, err
	}
	seat := l.Seat()
	dir, err := root.Sub(seat.Dir(), homefs.Private)
	if err != nil {
		return nil, err
	}
	return &StoreWriter{lock: l, dir: dir, team: filepath.Base(seat.Dir())}, nil
}

// Load is the seat's store; a missing one is fs.ErrNotExist, and one of another team is
// refused (CodeSeat).
func (w *StoreWriter) Load() (Store, error) {
	s, err := Read(w.dir, w.lock.Seat().Store(), MaxStore, DecodeStore)
	if err != nil {
		return Store{}, err
	}
	if err := w.ownTeam(s); err != nil {
		return Store{}, err
	}
	return s, nil
}

// Write replaces the seat's store with s, while the lock is still held.
func (w *StoreWriter) Write(s Store) error {
	if err := w.lock.Held(); err != nil {
		return err
	}
	if err := w.ownTeam(s); err != nil {
		return err
	}
	data, err := s.encode()
	if err != nil {
		return err
	}
	return w.dir.Replace(w.lock.Seat().Store(), data, fileMode)
}

// Close closes the seat directory; the lock stays its holder's to close.
func (w *StoreWriter) Close() error { return w.dir.Close() }

// ownTeam refuses a store whose team is not the seat directory's.
func (w *StoreWriter) ownTeam(s Store) error {
	if s.Team.String() != w.team {
		return &InvalidError{File: storeFile, Code: CodeSeat, Rule: "the team is not the seat directory's"}
	}
	return nil
}
