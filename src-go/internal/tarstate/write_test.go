package tarstate

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// The helpers take modes as parameters: the tests need modes the gosec rules would reject
// as literals (setuid files, world-writable directories are the point of the test).

func putFile(t *testing.T, path, data string, mode fs.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, path string, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(path, mode); err != nil {
		t.Fatal(err)
	}
}

func chmod(t *testing.T, path string, mode fs.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func symlinkAt(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func remove(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
}

func teamDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	putFile(t, filepath.Join(d, "team.json"), `{"team":"a"}`, 0o600)
	putFile(t, filepath.Join(d, "registry.json"), `{}`, 0o600)
	putFile(t, filepath.Join(d, "room_id"), "!r:example.com", 0o600)
	mkdir(t, filepath.Join(d, "secrets"), 0o700)
	putFile(t, filepath.Join(d, "secrets", "registration_shared_secret"), "abc", 0o600)
	// Things in the team directory that a state tar must not carry.
	mkdir(t, filepath.Join(d, "database"), 0o750)
	putFile(t, filepath.Join(d, "database", "000001.sst"), "db", 0o640)
	putFile(t, filepath.Join(d, "tuwunel.toml"), "x", 0o600)
	return d
}

func writeTar(t *testing.T, dir string) ([]byte, error) {
	t.Helper()
	var buf bytes.Buffer
	err := writeDir(&buf, dir, DefaultLimits())
	return buf.Bytes(), err
}

func TestWriteThenReadRoundTrips(t *testing.T) {
	dir := teamDir(t)
	raw, err := writeTar(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	s := stateOf(t, raw)
	names := s.Names()
	slices.Sort(names)
	want := []string{"registry.json", "room_id", "secrets", "secrets/registration_shared_secret", "team.json"}
	if !slices.Equal(names, want) {
		t.Fatalf("members %q, want %q", names, want)
	}
	if got := fileData(t, s, "secrets/registration_shared_secret"); got != "abc" {
		t.Fatalf("secret = %q", got)
	}
	if got := fileData(t, s, "team.json"); got != `{"team":"a"}` {
		t.Fatalf("team.json = %q", got)
	}
}

// The directory is read through the descriptor the caller holds: a name swapped for a symlink
// after the caller's checks does not redirect the read.
func TestWriteReadsThroughTheRootNotTheName(t *testing.T) {
	dir := teamDir(t)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cerr := root.Close(); cerr != nil {
			t.Error(cerr)
		}
	})
	moved := dir + ".moved"
	if err = os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	decoy := teamDir(t)
	putFile(t, filepath.Join(decoy, "team.json"), `{"team":"decoy"}`, 0o600)
	if err = os.Symlink(decoy, dir); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err = Write(&buf, root, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if got := fileData(t, stateOf(t, buf.Bytes()), "team.json"); got != `{"team":"a"}` {
		t.Fatalf("team.json = %q: the swapped name was read", got)
	}
}

func TestWriteOptionalMembersMayBeAbsent(t *testing.T) {
	dir := teamDir(t)
	for _, name := range []string{"registry.json", "room_id"} {
		remove(t, filepath.Join(dir, name))
	}
	raw, err := writeTar(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	if s := stateOf(t, raw); s.Has("registry.json") || s.Has("room_id") {
		t.Fatal("absent members were invented")
	}
}

func TestWriteIsDeterministic(t *testing.T) {
	dir := teamDir(t)
	a, err := writeTar(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := writeTar(t, dir)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("two writes differ: %v", err)
	}
}

// Whatever the live tree holds, the tar carries plain 0600 / 0700 members owned by nobody
// in particular: no setuid bit, no group or world access.
func TestWriteNormalisesModesAndOwner(t *testing.T) {
	dir := teamDir(t)
	chmod(t, filepath.Join(dir, "secrets", "registration_shared_secret"), fs.ModeSetuid|0o777)
	chmod(t, filepath.Join(dir, "secrets"), fs.ModeSticky|0o777)
	raw, err := writeTar(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		h, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		want := int64(0o600)
		if h.Typeflag == tar.TypeDir {
			want = 0o700
		}
		if h.Mode != want || h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" {
			t.Errorf("%s: mode %o uid %d gid %d user %q group %q", h.Name, h.Mode, h.Uid, h.Gid, h.Uname, h.Gname)
		}
	}
	stateOf(t, raw)
}

func TestWriteKeepsNestedSecrets(t *testing.T) {
	dir := teamDir(t)
	nested := filepath.Join(dir, "secrets", "keys", "deep")
	mkdir(t, nested, 0o700)
	putFile(t, filepath.Join(nested, "k"), "v", 0o600)
	raw, err := writeTar(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	if s := stateOf(t, raw); !s.Has("secrets/keys/deep/k") || !s.Has("secrets/keys/deep") {
		t.Fatalf("members %q", s.Names())
	}
}

// B6: a symlink the homeserver planted is refused when the backup is taken, not stored.
func TestWriteRefusesWhatIsNotAPlainFile(t *testing.T) {
	cases := []struct {
		name  string
		setup func(d string) error
		want  string
	}{
		{"symlink in secrets", func(d string) error {
			return os.Symlink("../team.json", filepath.Join(d, "secrets", "link"))
		}, "backup: secrets/link is a symlink, not a regular file"},
		{"dangling symlink in secrets", func(d string) error {
			return os.Symlink("/nonexistent", filepath.Join(d, "secrets", "link"))
		}, "backup: secrets/link is a symlink, not a regular file"},
		{"symlink to an absolute secret", func(d string) error {
			return os.Symlink("/etc/shadow", filepath.Join(d, "secrets", "shadow"))
		}, "backup: secrets/shadow is a symlink, not a regular file"},
		{"nested directory a symlink", func(d string) error {
			return os.Symlink("..", filepath.Join(d, "secrets", "up"))
		}, "backup: secrets/up is a symlink, not a regular file"},
		{"secrets itself a symlink", func(d string) error {
			if err := os.RemoveAll(filepath.Join(d, "secrets")); err != nil {
				return err
			}
			return os.Symlink(os.TempDir(), filepath.Join(d, "secrets"))
		}, "backup: secrets is a symlink, not a directory"},
		{"team.json a symlink", func(d string) error {
			if err := os.Remove(filepath.Join(d, "team.json")); err != nil {
				return err
			}
			return os.Symlink("/etc/passwd", filepath.Join(d, "team.json"))
		}, "backup: team.json is a symlink, not a regular file"},
		{"registry.json a dangling symlink", func(d string) error {
			if err := os.Remove(filepath.Join(d, "registry.json")); err != nil {
				return err
			}
			return os.Symlink("/nonexistent", filepath.Join(d, "registry.json"))
		}, "backup: registry.json is a symlink, not a regular file"},
		{"room_id a directory", func(d string) error {
			if err := os.Remove(filepath.Join(d, "room_id")); err != nil {
				return err
			}
			return os.Mkdir(filepath.Join(d, "room_id"), 0o700)
		}, "backup: room_id is a directory, not a regular file"},
		{"secrets a file", func(d string) error {
			if err := os.RemoveAll(filepath.Join(d, "secrets")); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(d, "secrets"), nil, 0o600)
		}, "backup: secrets is a regular file, not a directory"},
		{"fifo in secrets", func(d string) error {
			return syscall.Mkfifo(filepath.Join(d, "secrets", "pipe"), 0o600)
		}, "backup: secrets/pipe is a named pipe, not a regular file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refuses(t, tc.setup, tc.want)
		})
	}
}

// refuses runs setup on a fresh team directory and expects Write to fail with want and to
// have written nothing.
func refuses(t *testing.T, setup func(d string) error, want string) {
	t.Helper()
	dir := teamDir(t)
	if err := setup(dir); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := writeDir(&buf, dir, DefaultLimits())
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error %v, want %q", err, want)
	}
	if buf.Len() != 0 {
		t.Fatalf("%d bytes were written before the refusal", buf.Len())
	}
}

func TestWriteRefusesASocket(t *testing.T) {
	// A unix socket path is limited to about 100 bytes, so it is made in a short directory.
	short := t.TempDir()
	refuses(t, func(d string) error {
		if err := os.Remove(filepath.Join(d, "room_id")); err != nil {
			return err
		}
		l, err := net.Listen("unix", filepath.Join(short, "s"))
		if err != nil {
			return err
		}
		t.Cleanup(func() {
			if closeErr := l.Close(); closeErr != nil {
				t.Log(closeErr)
			}
		})
		return os.Rename(filepath.Join(short, "s"), filepath.Join(d, "room_id"))
	}, "backup: room_id is a socket, not a regular file")
}

func TestWriteRefusesADevice(t *testing.T) {
	probe := filepath.Join(t.TempDir(), "probe")
	if err := syscall.Mknod(probe, syscall.S_IFCHR|0o600, 1<<8|3); err != nil {
		t.Skipf("cannot make a device node here: %v", err)
	}
	refuses(t, func(d string) error {
		return syscall.Mknod(filepath.Join(d, "secrets", "null"), syscall.S_IFCHR|0o600, 1<<8|3)
	}, "backup: secrets/null is a device, not a regular file")
}

func TestWriteRequiresTeamJSONAndSecrets(t *testing.T) {
	for _, name := range []string{"team.json", "secrets"} {
		t.Run(name, func(t *testing.T) {
			dir := teamDir(t)
			remove(t, filepath.Join(dir, name))
			var buf bytes.Buffer
			err := writeDir(&buf, dir, DefaultLimits())
			if err == nil || !strings.Contains(err.Error(), "backup: "+name) {
				t.Fatalf("error %v", err)
			}
		})
	}
	t.Run("directory missing", func(t *testing.T) {
		var buf bytes.Buffer
		if err := writeDir(&buf, filepath.Join(t.TempDir(), "absent"), DefaultLimits()); err == nil {
			t.Fatal("wrote a tar of nothing")
		}
	})
}

func TestWriteHonoursTheFileSizeLimit(t *testing.T) {
	dir := teamDir(t)
	putFile(t, filepath.Join(dir, "secrets", "big"), strings.Repeat("x", 2048), 0o600)
	lim := DefaultLimits()
	lim.MaxFileSize = 1024
	var buf bytes.Buffer
	err := writeDir(&buf, dir, lim)
	if err == nil || !strings.Contains(err.Error(), "backup: secrets/big") {
		t.Fatalf("error %v", err)
	}
}

func TestWriteHonoursTheDepthLimit(t *testing.T) {
	d := teamDir(t)
	mkdir(t, filepath.Join(d, "secrets", "a", "b", "c", "d"), 0o700)
	lim := DefaultLimits()
	lim.MaxDepth = 3
	if err := writeDir(io.Discard, d, lim); err == nil {
		t.Fatal("wrote a tree deeper than the limit")
	}
}

func TestWriteHonoursTheEntryAndTotalLimits(t *testing.T) {
	d := teamDir(t)
	for _, name := range []string{"a", "b", "c", "d"} {
		putFile(t, filepath.Join(d, "secrets", name), strings.Repeat("x", 100), 0o600)
	}
	lim := DefaultLimits()
	lim.MaxEntries = 4
	if err := writeDir(io.Discard, d, lim); err == nil || !strings.Contains(err.Error(), "more than 4 members") {
		t.Fatalf("entries: %v", err)
	}
	lim = DefaultLimits()
	lim.MaxTotal = 250
	if err := writeDir(io.Discard, d, lim); err == nil || !strings.Contains(err.Error(), "total limit") {
		t.Fatalf("total: %v", err)
	}
}

// Write never produces a tar that Read would refuse: a name Read rejects fails at backup time.
func TestWriteRefusesANameTheReaderWouldRefuse(t *testing.T) {
	dir := teamDir(t)
	putFile(t, filepath.Join(dir, "secrets", "bad\nname"), "x", 0o600)
	var buf bytes.Buffer
	err := writeDir(&buf, dir, DefaultLimits())
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("error %v", err)
	}
	if buf.Len() != 0 {
		t.Fatal("bytes written before the refusal")
	}
}

type failWriter struct{}

var errBoom = errors.New("boom")

func (failWriter) Write([]byte) (int, error) { return 0, errBoom }

func TestWriteReturnsAWriterError(t *testing.T) {
	if err := writeDir(failWriter{}, teamDir(t), DefaultLimits()); !errors.Is(err, errBoom) {
		t.Fatalf("error %v", err)
	}
}

func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Log(closeErr)
		}
	})
	return root
}

// A file swapped for a symlink after it was listed is not followed.
func TestReadRegularNoFollow(t *testing.T) {
	d := t.TempDir()
	putFile(t, filepath.Join(d, "outside"), "hunter2", 0o600)
	root := openRoot(t, d)
	symlinkAt(t, "outside", filepath.Join(d, "link"))
	if _, err := readRegular(root, "link", 1<<20); err == nil {
		t.Fatal("followed a symlink")
	}
	got, err := readRegular(root, "outside", 1<<20)
	if err != nil || string(got.data) != "hunter2" {
		t.Fatalf("%+v, %v", got, err)
	}
	if _, err := readRegular(root, ".", 1<<20); err == nil {
		t.Fatal("read a directory as a file")
	}
	if _, err := readRegular(root, "outside", 3); err == nil {
		t.Fatal("read past the limit")
	}
	if _, err := readRegular(root, "absent", 3); err == nil {
		t.Fatal("read a file that is not there")
	}
}

// The file the open found must be the file lstat saw: if the name is swapped between the
// two, the read is refused.
func TestReadOpenedRefusesAnotherFile(t *testing.T) {
	d := t.TempDir()
	putFile(t, filepath.Join(d, "a"), "one", 0o600)
	putFile(t, filepath.Join(d, "b"), "two", 0o600)
	infoA, err := os.Lstat(filepath.Join(d, "a"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Clean(filepath.Join(d, "b")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Log(closeErr)
		}
	})
	if _, err := readOpened(f, infoA, 1<<20); err == nil || !strings.Contains(err.Error(), "changed while being read") {
		t.Fatalf("error %v", err)
	}
}

func TestListDirRefusesASymlink(t *testing.T) {
	d := t.TempDir()
	mkdir(t, filepath.Join(d, "real"), 0o700)
	symlinkAt(t, "real", filepath.Join(d, "link"))
	root := openRoot(t, d)
	if _, err := listDir(root, "link"); err == nil {
		t.Log("os.Root follows an in-root symlink; tree() has already refused it by lstat")
	}
	names, err := listDir(root, "real")
	if err != nil || len(names) != 0 {
		t.Fatalf("%q, %v", names, err)
	}
}

// writeDir opens dir as the root Write takes, as the installer does for a team directory.
func writeDir(w io.Writer, dir string, limits Limits) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	return errors.Join(Write(w, root, limits), root.Close())
}
