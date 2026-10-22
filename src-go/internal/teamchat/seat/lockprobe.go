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
)

// lockProbe is what the L3 probe found, and a free lock's record.
type lockProbe struct {
	state  lockState
	record []byte
}

// probe is L3 (SX1): the seat lock opened O_RDONLY|O_NONBLOCK through no link from the store
// root, a regular file, tried LOCK_SH|LOCK_NB; a free one's record is read through the same fd.
// A link anywhere on the path, a FIFO, a device or a directory is refused.
func (s *Store) probe(seat paths.Seat) (lockProbe, error) {
	fd, err := s.root.open(seat.Lock(), unix.O_RDONLY|unix.O_NONBLOCK)
	if errors.Is(err, unix.ENOENT) {
		return lockProbe{state: lockMissing}, nil
	}
	if err != nil {
		return lockProbe{}, err
	}
	f := os.NewFile(uintptr(fd), seat.Lock())
	got, err := probeOpen(f)
	if err = errors.Join(err, f.Close()); err != nil {
		return lockProbe{}, fmt.Errorf("%s: %w", seat.Lock(), err)
	}
	return got, nil
}

func probeOpen(f *os.File) (lockProbe, error) {
	fi, err := f.Stat()
	if err != nil {
		return lockProbe{}, err
	}
	if !fi.Mode().IsRegular() {
		return lockProbe{}, invalid("the seat lock is not a regular file")
	}
	err = unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB)
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
