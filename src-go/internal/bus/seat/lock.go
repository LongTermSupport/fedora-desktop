package seat

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

const lockFile = "seat.lock"

// probeLock is v1's probe of a seat's lock (inbox.probe_lock_at, V33): opened read-only
// without following a symlink and without waiting on a FIFO, it must be a regular file; a
// shared non-blocking flock that fails means another process holds it. No file is free.
// v1 caught only ENOENT and ELOOP from the open; any other failure is a crash.
func probeLock(path string) (bool, error) {
	fd, err := openRetrying(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC)
	switch {
	case errors.Is(err, unix.ENOENT):
		return false, nil
	case errors.Is(err, unix.ELOOP):
		return false, refusalf("%s is a symlink", pathText(path))
	case err != nil:
		return false, fmt.Errorf("open %s: %w", pathText(path), err)
	}
	held, err := probeOpen(fd, path)
	if closeErr := unix.Close(fd); err == nil && closeErr != nil {
		err = fmt.Errorf("close %s: %w", pathText(path), closeErr)
	}
	return held, err
}

func probeOpen(fd int, path string) (bool, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return false, fmt.Errorf("fstat %s: %w", pathText(path), err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return false, refusalf("%s is not a regular file", pathText(path))
	}
	switch err := flockRetrying(fd, unix.LOCK_SH|unix.LOCK_NB); {
	case errors.Is(err, unix.EWOULDBLOCK):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("flock %s: %w", pathText(path), err)
	}
	if err := flockRetrying(fd, unix.LOCK_UN); err != nil {
		return false, fmt.Errorf("unlock %s: %w", pathText(path), err)
	}
	return false, nil
}

// openRetrying and flockRetrying retry an interrupted call, as python does (PEP 475).
func openRetrying(path string, flags int) (int, error) {
	for {
		fd, err := unix.Open(path, flags, 0)
		if !errors.Is(err, unix.EINTR) {
			return fd, err
		}
	}
}

func flockRetrying(fd, how int) error {
	for {
		if err := unix.Flock(fd, how); !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}
