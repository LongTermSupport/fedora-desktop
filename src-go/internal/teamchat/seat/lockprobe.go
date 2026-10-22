package seat

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// maxLockRecord bounds the seat lock's record (B 2.5) a free lock is read for (SD1).
const maxLockRecord = 4 << 10

type lockState int

const (
	// lockMissing is a seat with no lock file: no keeper ever held it.
	lockMissing lockState = iota
	lockFree
	lockHeld
	// lockRefused is a seat lock the probe would not open: a link anywhere on its path, not a
	// regular file, or behind a mode or owner its container set that denies the owner. It is free
	// and names no run, as a deleted lock already is: the lock is the member's, so it is advisory,
	// and never what a release or a take waits on (n2c-D14).
	lockRefused
)

// lockProbe is what the L3 probe found, and a free lock's record.
type lockProbe struct {
	state  lockState
	record []byte
}

// memberRefused is whether err is a path the container made unusable to the owner: a link
// (RESOLVE_NO_SYMLINKS, O_NOFOLLOW), a non-directory where a directory was, a socket, or a mode
// or owner that denies the owner.
func memberRefused(err error) bool {
	for _, errno := range []unix.Errno{unix.ELOOP, unix.ENOTDIR, unix.ENXIO, unix.EACCES, unix.EPERM} {
		if errors.Is(err, errno) {
			return true
		}
	}
	return false
}

// probe is L3 (SX1): the seat lock found through no link from the store root, by O_PATH, which
// opens nothing for reading and cannot block; only a regular file is then opened
// O_RDONLY|O_NONBLOCK, checked to be the same file, and tried LOCK_SH|LOCK_NB, a free one's
// record read through that fd. Anything else the container put there is lockRefused.
func (s *Store) probe(seat paths.Seat) (lockProbe, error) {
	found, err := s.lockFile(seat, unix.O_PATH)
	switch {
	case errors.Is(err, unix.ENOENT):
		return lockProbe{state: lockMissing}, nil
	case memberRefused(err):
		return lockProbe{state: lockRefused}, nil
	case err != nil:
		return lockProbe{}, err
	}
	got, err := s.probeFound(seat, found)
	err = errors.Join(err, found.Close())
	// Gone, or changed, between the two opens is the container's doing too.
	if memberRefused(err) || errors.Is(err, ErrInvalid) || errors.Is(err, unix.ENOENT) {
		return lockProbe{state: lockRefused}, nil
	}
	if err != nil {
		return lockProbe{}, fmt.Errorf("%s: %w", seat.Lock(), err)
	}
	return got, nil
}

// lockFile opens the seat lock with flags through no link.
func (s *Store) lockFile(seat paths.Seat, flags uint64) (*os.File, error) {
	fd, err := s.root.open(seat.Lock(), flags)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), seat.Lock()), nil
}

// probeFound judges the lock found: only a regular file is opened for reading.
func (s *Store) probeFound(seat paths.Seat, found *os.File) (lockProbe, error) {
	var was unix.Stat_t
	if err := unix.Fstat(int(found.Fd()), &was); err != nil {
		return lockProbe{}, err
	}
	if was.Mode&unix.S_IFMT != unix.S_IFREG {
		return lockProbe{}, errNotRegularLock
	}
	f, err := s.lockFile(seat, unix.O_RDONLY|unix.O_NONBLOCK)
	if err != nil {
		return lockProbe{}, err
	}
	got, err := probeOpen(&was, f)
	return got, errors.Join(err, f.Close())
}

var errNotRegularLock = invalid("the seat lock is not a regular file")

// probeOpen judges the lock f opened for reading, which must be the regular file was.
func probeOpen(was *unix.Stat_t, f *os.File) (lockProbe, error) {
	var is unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &is); err != nil {
		return lockProbe{}, err
	}
	if is.Dev != was.Dev || is.Ino != was.Ino {
		return lockProbe{}, errNotRegularLock
	}
	err := unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return lockProbe{state: lockHeld}, nil
	}
	if err != nil {
		return lockProbe{}, err
	}
	record, err := readRegular(f, maxLockRecord)
	if errors.Is(err, ErrInvalid) {
		record, err = nil, nil
	}
	return lockProbe{state: lockFree, record: record}, err
}
