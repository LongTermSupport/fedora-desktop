package statusline

import (
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
)

// racyWindow is how recently a path may have changed and still be trusted to the cache. A
// file's timestamps are as coarse as the file system's clock, so a second change inside one
// tick could leave the fingerprint as it was; a render that read such a path is not cached.
const racyWindow = 2 * time.Second

var (
	// errNotRegular is a path that opened but is not a regular file.
	errNotRegular = errors.New("is not a regular file")
	// errTooLarge is a file longer than the limit its reader allows.
	errTooLarge = errors.New("is larger than the limit")
)

// stat is what one stat of a path showed: the errno it failed with, or the file's identity and
// change marks (S33).
type stat struct {
	Errno int    `json:"errno,omitempty"`
	Dev   uint64 `json:"dev,omitempty"`
	Ino   uint64 `json:"ino,omitempty"`
	Size  int64  `json:"size,omitempty"`
	Mtime int64  `json:"mtime,omitempty"`
	Ctime int64  `json:"ctime,omitempty"`
	Mode  uint32 `json:"mode,omitempty"`
	UID   uint32 `json:"uid,omitempty"`
}

func statOf(path string, follow bool) stat {
	var st syscall.Stat_t
	var err error
	if follow {
		err = syscall.Stat(path, &st)
	} else {
		err = syscall.Lstat(path, &st)
	}
	if err != nil {
		if errno, ok := errors.AsType[syscall.Errno](err); ok {
			return stat{Errno: int(errno)}
		}
		return stat{Errno: int(syscall.EIO)}
	}
	return stat{
		Dev: st.Dev, Ino: st.Ino, Size: st.Size,
		Mtime: st.Mtim.Nano(), Ctime: st.Ctim.Nano(),
		Mode: st.Mode, UID: st.Uid,
	}
}

func (s stat) exists() bool    { return s.Errno == 0 }
func (s stat) kind() uint32    { return s.Mode & syscall.S_IFMT }
func (s stat) isRegular() bool { return s.exists() && s.kind() == syscall.S_IFREG }
func (s stat) isDir() bool     { return s.exists() && s.kind() == syscall.S_IFDIR }
func (s stat) isLink() bool    { return s.exists() && s.kind() == syscall.S_IFLNK }

// perm is Python's stat.S_IMODE: the permission bits with setuid, setgid and sticky.
func (s stat) perm() uint32 { return s.Mode & 0o7777 }

// fingerprint is a path as the render saw it: its lstat and, for a symlink, the stat of what it
// points at.
type fingerprint struct {
	Path   string `json:"path"`
	Lstat  stat   `json:"lstat"`
	Target *stat  `json:"target,omitempty"`
}

func fingerprintOf(path string) fingerprint {
	fp := fingerprint{Path: path, Lstat: statOf(path, false)}
	if fp.Lstat.isLink() {
		target := statOf(path, true)
		fp.Target = &target
	}
	return fp
}

// unchanged is whether the path still shows what the fingerprint recorded.
func (fp fingerprint) unchanged() bool {
	now := fingerprintOf(fp.Path)
	if now.Lstat != fp.Lstat || (now.Target == nil) != (fp.Target == nil) {
		return false
	}
	return now.Target == nil || *now.Target == *fp.Target
}

// world is the files one segment render reads, as it reads them: every path is fingerprinted
// before it is read (S33), so a change between the fingerprint and the read costs a cache miss,
// never a stale hit.
type world struct {
	dir      string
	now      time.Time
	uid      int
	userHome func(uid int) (string, error)
	prints   []fingerprint
	seen     map[string]bool
}

func newWorld(dir string, now time.Time, userHome func(int) (string, error)) *world {
	return &world{dir: dir, now: now, uid: os.Getuid(), userHome: userHome, seen: map[string]bool{}}
}

// abs is path read from the render's working directory, as the Python's relative paths are.
func (w *world) abs(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return pyJoin(w.dir, path)
}

// note fingerprints path and returns its lstat.
func (w *world) note(path string) stat {
	fp := fingerprintOf(w.abs(path))
	if !w.seen[fp.Path] {
		w.seen[fp.Path] = true
		w.prints = append(w.prints, fp)
	}
	return fp.Lstat
}

// racy is whether any path the render read changed within racyWindow of it.
func (w *world) racy() bool {
	since := w.now.Add(-racyWindow).UnixNano()
	for _, fp := range w.prints {
		if fp.Lstat.exists() && fp.Lstat.Ctime >= since {
			return true
		}
		if fp.Target != nil && fp.Target.exists() && fp.Target.Ctime >= since {
			return true
		}
	}
	return false
}

// opened is a file read whole: its bytes and the stat of the descriptor they came from.
type opened struct {
	raw  []byte
	info syscall.Stat_t
}

// read opens path as the Python does (O_RDONLY|O_NONBLOCK|O_CLOEXEC, and O_NOFOLLOW when
// noFollow, so a FIFO never blocks and a symlink is refused with ELOOP), refuses anything but a
// regular file (errNotRegular) and more than limit bytes (errTooLarge). An open or read failure
// is returned as the errno.
func (w *world) read(path string, noFollow bool, limit int64) (opened, error) {
	w.note(path)
	flags := syscall.O_RDONLY | syscall.O_NONBLOCK | syscall.O_CLOEXEC
	if noFollow {
		flags |= syscall.O_NOFOLLOW
	}
	fd, err := syscall.Open(w.abs(path), flags, 0)
	if err != nil {
		return opened{}, err
	}
	file := os.NewFile(uintptr(fd), path)
	got, readErr := readOpened(file, limit)
	closeErr := file.Close()
	if readErr != nil {
		return opened{}, readErr
	}
	return got, closeErr
}

func readOpened(file *os.File, limit int64) (opened, error) {
	var got opened
	if err := syscall.Fstat(int(file.Fd()), &got.info); err != nil {
		return opened{}, err
	}
	if got.info.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return opened{}, errNotRegular
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return opened{}, err
	}
	if int64(len(raw)) > limit {
		return opened{}, errTooLarge
	}
	got.raw = raw
	return got, nil
}

func isErrno(err error, want syscall.Errno) bool {
	errno, ok := errors.AsType[syscall.Errno](err)
	return ok && errno == want
}

// pyJoin is pathlib's join of base and rel: rel when it is absolute; then empty and "."
// components dropped and ".." kept, as pathlib leaves them for the kernel.
func pyJoin(base, rel string) string {
	joined := rel
	if !strings.HasPrefix(rel, "/") {
		joined = base + "/" + rel
	}
	var parts []string
	for part := range strings.SplitSeq(joined, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	root := ""
	switch {
	case strings.HasPrefix(joined, "//") && !strings.HasPrefix(joined, "///"):
		root = "//"
	case strings.HasPrefix(joined, "/"):
		root = "/"
	}
	if len(parts) == 0 {
		if root == "" {
			return "."
		}
		return root
	}
	return root + strings.Join(parts, "/")
}
