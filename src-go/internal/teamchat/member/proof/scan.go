// Package proof is the member's transcript proof primitives (B 2.7.4, DV4): the needle a
// delivery's first line carries, a bounded scan of a session's transcript for the needles of
// the deliveries waiting for proof, and a watcher that says when to scan again. The states the
// scan drives (awaiting, proven, missed) are the engine's, not this package's.
//
// The needle, `teamchat delivery <nonce>:`, is printable ASCII with no character JSON escapes,
// so it appears byte for byte inside the JSON string Claude Code writes the envelope into (the
// idle `user` record and the busy `queued_command` attachment alike), and a byte search of the
// JSONL is exact.
//
// The transcript is opened as TB1 opens a file it must not be fooled by: its directory and then
// the file by O_PATH with O_NOFOLLOW, each fstat'd; anything but the caller's own directory and
// regular file, neither writable by its group or others, is refused by name (a symbolic link, a
// FIFO, a directory, another user's file); only then is that inode reopened for reading through
// /proc/self/fd/<n>, and its device and inode checked again. So a FIFO is never opened, and no
// decision is taken from a file another uid can write.
package proof

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

// ErrRefused is a transcript that is not as a proof may be read from, or a call that cannot
// be scanned exactly (a nonce JSON would escape, a read no longer than the overlap).
var ErrRefused = errors.New("proof: refused")

const (
	needlePrefix = "teamchat delivery "
	// sharedWrite is the group and other write bits the transcript and its directory must not
	// have, so no other uid can write what a decision is taken from.
	sharedWrite = 0o022
)

// Needle is the bytes a delivery's first line carries for nonce: "teamchat delivery <nonce>:".
func Needle(nonce string) []byte { return []byte(needlePrefix + nonce + ":") }

// Mark is a place in a transcript: an offset in the file of that device and inode. Only Offset
// is persisted (n3e-D10); a zero Ino means the inode is not known, and is not compared.
type Mark struct {
	Offset   int64
	Dev, Ino uint64
}

// Result is one scan: the nonces whose needles were found, in the order asked, each once; the
// mark the next scan continues from; and whether the file was scanned from 0 because it shrank
// or is another inode.
type Result struct {
	Found []string
	End   Mark
	Reset bool
}

// Scan reads path from from.Offset less len(Needle)-1 bytes of overlap (never below 0) to its
// end, at most maxRead bytes. A file shorter than from.Offset, or another inode, is scanned from
// 0 (Reset). A needle is found only when its last byte lies past from.Offset, so what an
// earlier scan reported is not reported again. A missing file is fs.ErrNotExist.
func Scan(path string, from Mark, nonces []string, maxRead int64) (Result, error) {
	needles, overlap, err := needlesOf(nonces)
	switch {
	case err != nil:
		return Result{}, err
	case from.Offset < 0:
		return Result{}, fmt.Errorf("%w: offset %d is negative", ErrRefused, from.Offset)
	case maxRead <= overlap:
		return Result{}, fmt.Errorf("%w: a read of %d bytes is no longer than the %d of overlap", ErrRefused, maxRead, overlap)
	case !filepath.IsAbs(path) || filepath.Clean(path) != path:
		return Result{}, fmt.Errorf("%w: %s is not an absolute, clean path", ErrRefused, path)
	}
	f, st, err := open(path)
	if err != nil {
		return Result{}, err
	}
	res, err := scanOpen(f, st, from, needles, overlap, maxRead)
	if err = errors.Join(err, f.Close()); err != nil {
		return Result{}, err
	}
	return res, nil
}

func scanOpen(f *os.File, st *unix.Stat_t, from Mark, needles []needle, overlap, maxRead int64) (Result, error) {
	res := Result{End: Mark{Dev: st.Dev, Ino: st.Ino}}
	offset := from.Offset
	if offset > st.Size || (from.Ino != 0 && (from.Dev != st.Dev || from.Ino != st.Ino)) {
		offset, res.Reset = 0, true
	}
	start := max(0, offset-overlap)
	buf := make([]byte, min(st.Size-start, maxRead))
	n, err := f.ReadAt(buf, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return Result{}, fmt.Errorf("proof: read %s: %w", f.Name(), err)
	}
	buf = buf[:n]
	res.End.Offset = start + int64(n)
	for _, n := range needles {
		if foundPast(buf, n.bytes, offset-start) {
			res.Found = append(res.Found, n.nonce)
		}
	}
	return res, nil
}

// foundPast is whether needle occurs in buf with its last byte at or past index past.
func foundPast(buf, needle []byte, past int64) bool {
	for pos := 0; ; {
		i := bytes.Index(buf[pos:], needle)
		if i < 0 {
			return false
		}
		if int64(pos+i+len(needle)) > past {
			return true
		}
		pos += i + 1
	}
}

// needle is one distinct nonce asked for and its bytes.
type needle struct {
	nonce string
	bytes []byte
}

// needlesOf is each distinct nonce's needle, in the order asked, and the overlap the longest
// needs.
func needlesOf(nonces []string) ([]needle, int64, error) {
	var overlap int64
	needles := make([]needle, 0, len(nonces))
	seen := map[string]bool{}
	for _, nonce := range nonces {
		if err := checkNonce(nonce); err != nil {
			return nil, 0, err
		}
		if seen[nonce] {
			continue
		}
		seen[nonce] = true
		needles = append(needles, needle{nonce: nonce, bytes: Needle(nonce)})
		overlap = max(overlap, int64(len(nonce)+len(needlePrefix)))
	}
	return needles, overlap, nil
}

// checkNonce refuses a nonce whose needle would not appear byte for byte inside a JSON string:
// empty, or holding anything but printable ASCII other than '"' and '\'.
func checkNonce(nonce string) error {
	if nonce == "" {
		return fmt.Errorf("%w: an empty nonce", ErrRefused)
	}
	for _, c := range []byte(nonce) {
		if c < 0x20 || c > 0x7e || c == '"' || c == '\\' {
			return fmt.Errorf("%w: nonce %q holds a byte JSON would escape", ErrRefused, nonce)
		}
	}
	return nil
}

// open opens path's directory and then path by O_PATH, checks each, and reopens the file for
// reading.
func open(path string) (*os.File, *unix.Stat_t, error) {
	dir := filepath.Dir(path)
	dirFD, err := openPath(unix.AT_FDCWD, dir)
	if err != nil {
		return nil, nil, err
	}
	f, st, err := openIn(dirFD, dir, path)
	if err = errors.Join(err, unix.Close(dirFD)); err != nil {
		if f != nil {
			err = errors.Join(err, f.Close())
		}
		return nil, nil, err
	}
	return f, st, nil
}

func openIn(dirFD int, dir, path string) (*os.File, *unix.Stat_t, error) {
	if _, err := judge(dirFD, dir, unix.S_IFDIR); err != nil {
		return nil, nil, err
	}
	fd, err := openPath(dirFD, filepath.Base(path))
	if err != nil {
		return nil, nil, fmt.Errorf("proof: %s: %w", path, errors.Unwrap(err))
	}
	st, err := judge(fd, path, unix.S_IFREG)
	if err != nil {
		return nil, nil, errors.Join(err, unix.Close(fd))
	}
	f, err := reopen(fd, st, path)
	if err = errors.Join(err, unix.Close(fd)); err != nil {
		if f != nil {
			err = errors.Join(err, f.Close())
		}
		return nil, nil, err
	}
	return f, st, nil
}

// openPath opens name at dirFD by O_PATH, never following a link: a link at name is opened
// as itself, for judge to refuse.
func openPath(dirFD int, name string) (int, error) {
	fd, err := unix.Openat(dirFD, name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("proof: %s: %w", name, err)
	}
	return fd, nil
}

// judge is fd's status when it is of the type want, the caller's, and not writable by its
// group or others; anything else is ErrRefused naming what it is.
func judge(fd int, path string, want uint32) (*unix.Stat_t, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, fmt.Errorf("proof: %s: %w", path, err)
	}
	if kind := st.Mode & unix.S_IFMT; kind != want {
		return nil, fmt.Errorf("%w: %s is %s", ErrRefused, path, typeName(kind, want))
	}
	if int(st.Uid) != os.Geteuid() {
		return nil, fmt.Errorf("%w: %s is not this user's", ErrRefused, path)
	}
	if st.Mode&sharedWrite != 0 {
		return nil, fmt.Errorf("%w: %s is writable by its group or others", ErrRefused, path)
	}
	return &st, nil
}

func typeName(kind, want uint32) string {
	switch kind {
	case unix.S_IFLNK:
		return "a symbolic link"
	case unix.S_IFIFO:
		return "a FIFO"
	case unix.S_IFDIR:
		return "a directory"
	}
	if want == unix.S_IFDIR {
		return "not a directory"
	}
	return "not a regular file"
}

// reopen opens the inode held by the O_PATH fd pathFD for reading through /proc/self/fd, and
// refuses it unless it is still want's device and inode. This is TB1's one open without
// O_NOFOLLOW: the /proc entry is a magic link, which O_NOFOLLOW would refuse.
func reopen(pathFD int, want *unix.Stat_t, path string) (*os.File, error) {
	fd, err := unix.Open("/proc/self/fd/"+strconv.Itoa(pathFD), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("proof: reopen %s: %w", path, err)
	}
	var st unix.Stat_t
	err = unix.Fstat(fd, &st)
	if err == nil && (st.Dev != want.Dev || st.Ino != want.Ino) {
		err = fmt.Errorf("%w: %s changed while it was opened", ErrRefused, path)
	}
	if err != nil {
		return nil, errors.Join(err, unix.Close(fd))
	}
	return os.NewFile(uintptr(fd), path), nil
}
