// Package state holds the host's state files the way C 3.6.1 requires (Plan 00209,
// DESIGN-v2-C-host.md 3.6.1, S15; SPEC-n2b.md ST1-ST2): a write is a temporary file beside the
// target, created exclusively without following a link, given its mode and group, written,
// synced and renamed over the target, then the directory synced; a read refuses a link, a file
// that is not regular, and a file over its cap.
package state

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// tempSuffixBytes is the random part of a temporary file's name.
const tempSuffixBytes = 8

// WriteFile replaces path with data, mode and (unless gid is -1) group gid, so a reader sees
// either the old content or the new, never a part (ST1).
func WriteFile(path string, data []byte, mode fs.FileMode, gid int) error {
	dir := filepath.Dir(path)
	suffix := make([]byte, tempSuffixBytes)
	if _, err := rand.Read(suffix); err != nil {
		return fmt.Errorf("cannot name a temporary file for %s: %w", path, err)
	}
	temp := filepath.Join(dir, "."+filepath.Base(path)+"."+hex.EncodeToString(suffix)+".tmp")
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("cannot create a temporary file for %s: %w", path, err)
	}
	if err := fill(f, data, mode, gid); err != nil {
		return errors.Join(fmt.Errorf("cannot write %s: %w", path, err), os.Remove(temp))
	}
	if err := os.Rename(temp, path); err != nil {
		return errors.Join(fmt.Errorf("cannot put %s in place: %w", path, err), os.Remove(temp))
	}
	return syncDir(dir)
}

// fill gives the temporary file its mode and group and its content, and closes it.
func fill(f *os.File, data []byte, mode fs.FileMode, gid int) error {
	err := f.Chmod(mode)
	if err == nil && gid != -1 {
		err = f.Chown(-1, gid)
	}
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("cannot open %s to sync it: %w", dir, err)
	}
	return errors.Join(d.Sync(), d.Close())
}

// ReadFile is path's content; found is false when there is no file. A link, a file that is not
// regular, and a file over limit bytes are refused (ST2).
func ReadFile(path string, limit int64) ([]byte, bool, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cannot open %s: %w", path, err)
	}
	data, err := readOpen(f, path, limit)
	if closeErr := f.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("cannot close %s: %w", path, closeErr)
	}
	return data, err == nil, err
}

func readOpen(f *os.File, path string, limit int64) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("cannot stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is over %d bytes", path, limit)
	}
	return data, nil
}
