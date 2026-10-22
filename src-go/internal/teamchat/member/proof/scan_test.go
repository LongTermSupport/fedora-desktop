package proof

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

// otherUID owns the files and directories planted as another user's.
const otherUID = 4242

const (
	nonceA = "0a1b2c3d"
	nonceB = "deadbeef"
	big    = 1 << 20
)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// transcript makes a private directory holding name with content, mode 0600, and returns the
// file's path.
func transcript(t *testing.T, content string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "project")
	check(t, os.Mkdir(dir, 0o700))
	path := filepath.Join(dir, "session.jsonl")
	check(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func appendTo(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	check(t, err)
	_, err = f.WriteString(content)
	check(t, errors.Join(err, f.Close()))
}

// idleLine is the record an idle session writes for an envelope (host check U20).
func idleLine(envelope string) string {
	return `{"type":"user","message":{"role":"user","content":"` + envelope + `"},"origin":{"kind":"peer"}}` + "\n"
}

func scan(t *testing.T, path string, from Mark, nonces []string, maxRead int64) Result {
	t.Helper()
	got, err := Scan(path, from, nonces, maxRead)
	check(t, err)
	return got
}

func TestNeedle(t *testing.T) {
	if got := string(Needle(nonceA)); got != "teamchat delivery 0a1b2c3d:" {
		t.Fatalf("Needle = %q", got)
	}
}

func TestScanFindsANeedleInsideAJSONString(t *testing.T) {
	envelope := `\"quoted\"\né teamchat delivery ` + nonceA + `: 2 items\\n\tend`
	path := transcript(t, `{"type":"assistant"}`+"\n"+idleLine(envelope))
	got := scan(t, path, Mark{}, []string{nonceA, nonceB}, big)
	if !slices.Equal(got.Found, []string{nonceA}) || got.Reset {
		t.Fatalf("Scan = %+v, want only %s found and no reset", got, nonceA)
	}
	st, err := os.Stat(path)
	check(t, err)
	if got.End.Offset != st.Size() || got.End.Ino == 0 {
		t.Fatalf("End = %+v, want the file's end %d and its inode", got.End, st.Size())
	}
}

func TestScanReportsEachNonceOnce(t *testing.T) {
	line := idleLine("teamchat delivery " + nonceA + ": first")
	path := transcript(t, line+line+idleLine("teamchat delivery "+nonceB+": second")+line)
	got := scan(t, path, Mark{}, []string{nonceB, nonceA, nonceA}, big)
	if !slices.Equal(got.Found, []string{nonceB, nonceA}) {
		t.Fatalf("Found = %v, want each nonce once, in the order asked", got.Found)
	}
}

func TestScanFindsANeedleSplitAcrossTwoReadsThroughTheOverlap(t *testing.T) {
	head := `{"type":"user","message":{"role":"user","content":"teamchat deliv`
	path := transcript(t, head)
	first := scan(t, path, Mark{}, []string{nonceA}, big)
	if len(first.Found) != 0 || first.End.Offset != int64(len(head)) {
		t.Fatalf("first Scan = %+v, want nothing found, ended at %d", first, len(head))
	}
	appendTo(t, path, `ery `+nonceA+`: x"}}`+"\n")
	second := scan(t, path, first.End, []string{nonceA}, big)
	if !slices.Equal(second.Found, []string{nonceA}) || second.Reset {
		t.Fatalf("second Scan = %+v, want %s found through the overlap", second, nonceA)
	}
}

func TestScanSkipsWhatWasAlreadyScanned(t *testing.T) {
	old := idleLine("teamchat delivery " + nonceA + ": old")
	padding := strings.Repeat("x", 100)
	path := transcript(t, old+padding)
	from := Mark{Offset: int64(len(old) + len(padding))}
	if got := scan(t, path, from, []string{nonceA}, big); len(got.Found) != 0 {
		t.Fatalf("Found = %v, want nothing: the needle lies wholly before the overlap", got.Found)
	}
	// A needle ending exactly at from was found by the scan that ended there.
	needle := "teamchat delivery " + nonceA + ":"
	path = transcript(t, padding+needle)
	if got := scan(t, path, Mark{Offset: int64(len(padding) + len(needle))}, []string{nonceA}, big); len(got.Found) != 0 {
		t.Fatalf("Found = %v, want nothing: the needle ends at from", got.Found)
	}
	if got := scan(t, path, Mark{Offset: int64(len(padding) + len(needle) - 1)}, []string{nonceA}, big); len(got.Found) != 1 {
		t.Fatalf("Found = %v, want the needle whose last byte is new", got.Found)
	}
}

func TestScanFromAnOffsetBelowTheOverlapStartsAtZero(t *testing.T) {
	path := transcript(t, "teamchat delivery "+nonceA+": early")
	got := scan(t, path, Mark{Offset: 3}, []string{nonceA}, big)
	if !slices.Equal(got.Found, []string{nonceA}) || got.Reset {
		t.Fatalf("Scan = %+v, want the needle found from 0 without a reset", got)
	}
}

func TestScanResetsATruncatedARotatedAndAShrunkFile(t *testing.T) {
	line := idleLine("teamchat delivery " + nonceA + ": again")
	for name, change := range map[string]func(t *testing.T, path string){
		"truncated": func(t *testing.T, path string) {
			t.Helper()
			check(t, os.Truncate(path, 0))
			appendTo(t, path, line)
		},
		"shrunk": func(t *testing.T, path string) {
			t.Helper()
			check(t, os.WriteFile(path, []byte(line), 0o600))
		},
		"rotated": func(t *testing.T, path string) {
			t.Helper()
			fresh := path + ".new"
			check(t, os.WriteFile(fresh, []byte(line+strings.Repeat("y", 4096)), 0o600))
			check(t, os.Rename(fresh, path))
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := transcript(t, strings.Repeat("z", 2048))
			first := scan(t, path, Mark{}, []string{nonceA}, big)
			change(t, path)
			got := scan(t, path, first.End, []string{nonceA}, big)
			if !got.Reset || !slices.Equal(got.Found, []string{nonceA}) {
				t.Fatalf("Scan = %+v, want a reset that finds the needle from 0", got)
			}
		})
	}
}

func TestScanWithOnlyAPersistedOffsetChecksNoInode(t *testing.T) {
	path := transcript(t, strings.Repeat("z", 64)+idleLine("teamchat delivery "+nonceA+": after"))
	got := scan(t, path, Mark{Offset: 64}, []string{nonceA}, big)
	if got.Reset || !slices.Equal(got.Found, []string{nonceA}) {
		t.Fatalf("Scan = %+v, want the needle found from the persisted offset, no reset", got)
	}
}

func TestScanIsBoundedByMaxReadAndContinuesFromEnd(t *testing.T) {
	needle := "teamchat delivery " + nonceA + ":"
	content := strings.Repeat("a", 90) + needle + strings.Repeat("b", 90)
	path := transcript(t, content)
	var found []string
	from := Mark{}
	calls := 0
	for from.Offset < int64(len(content)) {
		got := scan(t, path, from, []string{nonceA}, 40)
		if read := got.End.Offset - max(0, from.Offset-int64(len(needle)-1)); read > 40 {
			t.Fatalf("one Scan read %d bytes, over its 40", read)
		}
		found = append(found, got.Found...)
		from = got.End
		calls++
	}
	if !slices.Equal(found, []string{nonceA}) || calls < 5 {
		t.Fatalf("found %v in %d calls, want the needle once over several bounded calls", found, calls)
	}
}

func TestScanWithNoNoncesMovesToTheEnd(t *testing.T) {
	path := transcript(t, "teamchat delivery "+nonceA+":")
	got := scan(t, path, Mark{}, nil, big)
	if len(got.Found) != 0 || got.End.Offset != int64(len("teamchat delivery "+nonceA+":")) {
		t.Fatalf("Scan = %+v, want nothing found and the end reached", got)
	}
}

func TestScanRefusesABadCallByName(t *testing.T) {
	path := transcript(t, "")
	for name, call := range map[string]func() error{
		"an empty nonce":            func() error { _, err := Scan(path, Mark{}, []string{""}, big); return err },
		"a nonce JSON would escape": func() error { _, err := Scan(path, Mark{}, []string{`a"b`}, big); return err },
		"a non-ASCII nonce":         func() error { _, err := Scan(path, Mark{}, []string{"é"}, big); return err },
		"a read within the overlap": func() error { _, err := Scan(path, Mark{}, []string{nonceA}, 26); return err },
		"a negative offset":         func() error { _, err := Scan(path, Mark{Offset: -1}, nil, big); return err },
		"a relative path":           func() error { _, err := Scan("session.jsonl", Mark{}, nil, big); return err },
		"an unclean path":           func() error { _, err := Scan(filepath.Dir(path)+"/./session.jsonl", Mark{}, nil, big); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, ErrRefused) {
				t.Fatalf("err = %v, want ErrRefused", err)
			}
		})
	}
}

func TestScanOfAMissingFileIsNotExist(t *testing.T) {
	path := filepath.Join(filepath.Dir(transcript(t, "")), "absent.jsonl")
	if _, err := Scan(path, Mark{}, nil, big); !errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
	if _, err := Scan("/nonexistent-dir-for-proof/x.jsonl", Mark{}, nil, big); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist for a missing directory", err)
	}
}

func TestScanRefusesWhatIsNotTheCallersRegularFile(t *testing.T) {
	for name, tc := range map[string]struct {
		plant func(t *testing.T, path string)
		says  string
	}{
		"a symbolic link": {func(t *testing.T, path string) {
			t.Helper()
			check(t, os.Remove(path))
			check(t, os.Symlink(path+".real", path))
			check(t, os.WriteFile(path+".real", nil, 0o600))
		}, "a symbolic link"},
		"a directory": {func(t *testing.T, path string) {
			t.Helper()
			check(t, os.Remove(path))
			check(t, os.Mkdir(path, 0o700))
		}, "a directory"},
		"a socket": {func(t *testing.T, path string) {
			t.Helper()
			check(t, os.Remove(path))
			fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
			check(t, err)
			check(t, errors.Join(unix.Bind(fd, &unix.SockaddrUnix{Name: path}), unix.Close(fd)))
		}, "not a regular file"},
		"another user's file": {func(t *testing.T, path string) {
			t.Helper()
			check(t, os.Lchown(path, otherUID, -1))
		}, "not this user's"},
		"a file others can write": {func(t *testing.T, path string) {
			t.Helper()
			check(t, os.Chmod(path, 0o602))
		}, "writable by its group or others"},
		"a directory another user owns": {func(t *testing.T, path string) {
			t.Helper()
			check(t, os.Lchown(filepath.Dir(path), otherUID, -1))
		}, "not this user's"},
		"a directory its group can write": {func(t *testing.T, path string) {
			t.Helper()
			check(t, os.Chmod(filepath.Dir(path), 0o770))
		}, "writable by its group or others"},
		"a directory that is a link": {func(t *testing.T, path string) {
			t.Helper()
			dir := filepath.Dir(path)
			check(t, os.Rename(dir, dir+".real"))
			check(t, os.Symlink(dir+".real", dir))
		}, "a symbolic link"},
	} {
		t.Run(name, func(t *testing.T) {
			path := transcript(t, idleLine("teamchat delivery "+nonceA+": x"))
			tc.plant(t, path)
			_, err := Scan(path, Mark{}, []string{nonceA}, big)
			if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("err = %v, want ErrRefused saying %q", err, tc.says)
			}
		})
	}
}

// TestScanNeverOpensAFIFO polls a FIFO at the path for a writer while Scan is refusing it: a
// non-blocking writer's open is ENXIO whenever no reader holds the FIFO, so every poll failing
// so means Scan never opened it for reading.
func TestScanNeverOpensAFIFO(t *testing.T) {
	path := transcript(t, "")
	check(t, os.Remove(path))
	check(t, unix.Mkfifo(path, 0o600))
	const rounds = 200
	var wg sync.WaitGroup
	errs := make(chan error, rounds)
	wg.Go(func() {
		for range rounds {
			_, err := Scan(path, Mark{}, []string{nonceA}, big)
			errs <- err
		}
	})
	for range rounds {
		fd, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err == nil {
			check(t, unix.Close(fd))
			t.Fatal("a writer opened the FIFO: something held it for reading")
		}
		if !errors.Is(err, unix.ENXIO) {
			t.Fatalf("writer's open = %v, want ENXIO", err)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "a FIFO") {
			t.Fatalf("Scan = %v, want ErrRefused naming a FIFO", err)
		}
	}
}

func TestReopenRefusesAnotherInode(t *testing.T) {
	path := transcript(t, "x")
	fd, err := unix.Open(path, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	check(t, err)
	defer func() { check(t, unix.Close(fd)) }()
	var st unix.Stat_t
	check(t, unix.Fstat(fd, &st))
	st.Ino++
	if _, err := reopen(fd, &st, path); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("reopen = %v, want ErrRefused: the file changed", err)
	}
	if _, err := reopen(-1, &st, path); err == nil || errors.Is(err, ErrRefused) {
		t.Fatalf("reopen of a bad fd = %v, want its errno", err)
	}
}
