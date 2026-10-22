package seat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// ErrNotLocked is a step that needs the store's allocation lock run without it held (SL1).
var ErrNotLocked = errors.New("seat: the allocation lock is not held")

const (
	// allocWait bounds the wait for the allocation lock (n2c-D6).
	allocWait = 30 * time.Second
	// allocPoll is how often a held allocation lock is tried again.
	allocPoll = 100 * time.Millisecond
)

// Alloc is the store's allocation lock, held (C 3.4, C26).
type Alloc struct {
	store *Store
	mu    sync.Mutex
	file  *os.File
}

// Lock takes the checkout's allocation lock: an exclusive flock, tried every allocPoll for at
// most allocWait by the store's clock; still held is ErrBusy (SL1). Its directory is made 0700
// and must be the owner's 0700 directory; the lock file is opened through no link.
func (s *Store) Lock(ctx context.Context) (*Alloc, error) {
	f, err := openLockFile(s.allocLock)
	if err != nil {
		return nil, err
	}
	deadline := s.clock.Now().Add(allocWait)
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return &Alloc{store: s, file: f}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return nil, errors.Join(fmt.Errorf("%s: %w", s.allocLock, err), f.Close())
		}
		if !s.clock.Now().Before(deadline) {
			return nil, errors.Join(fmt.Errorf("%w: the allocation lock %s is held", ErrBusy, s.allocLock), f.Close())
		}
		if err := s.clock.Sleep(ctx, allocPoll); err != nil {
			return nil, errors.Join(err, f.Close())
		}
	}
}

func openLockFile(path string) (*os.File, error) {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, dirMode); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfig, err)
	}
	d, err := openDir(parent, false)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Openat(d.fd, filepath.Base(path), unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, fileMode)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("%s: %w", path, err), d.close())
	}
	return os.NewFile(uintptr(fd), path), d.close()
}

// Unlock releases the lock; a second Unlock is ErrNotLocked.
func (a *Alloc) Unlock() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.file == nil {
		return ErrNotLocked
	}
	err := a.file.Close()
	a.file = nil
	return err
}

// holds is whether a is this store's allocation lock, still held.
func (s *Store) holds(a *Alloc) bool {
	if a == nil || a.store != s {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.file != nil
}
