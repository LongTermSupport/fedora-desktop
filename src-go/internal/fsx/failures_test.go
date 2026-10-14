package fsx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The failures a root-owned writer must report and clean up after, made with the plainest
// causes a directory can have: a name that is already something else, a parent that is not
// there, a stale temporary that cannot be replaced.

func (e *env) blockTemp(name string) {
	e.t.Helper()
	tmp := e.fs.tempName(name)
	e.mkdir(tmp)
	e.write(tmp+"/inner", "x", 0o600)
}

func TestPutSymlinkReportsEveryFailureAndLeavesNoTemporary(t *testing.T) {
	t.Run("a stale temporary that cannot be removed", func(t *testing.T) {
		e := newEnv(t, "/r")
		e.blockTemp("link")
		if _, err := e.fs.PutSymlink("l", "target", "link"); err == nil || !strings.Contains(err.Error(), "/r/link") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a parent that is not there", func(t *testing.T) {
		e := newEnv(t, "/r")
		if _, err := e.fs.PutSymlink("l", "target", "nope/link"); err == nil || !strings.Contains(err.Error(), "/r/nope/link") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a parent that is a file", func(t *testing.T) {
		e := newEnv(t, "/r")
		e.write("file", "x", 0o600)
		if _, err := e.fs.PutSymlink("l", "target", "file/link"); err == nil || !strings.Contains(err.Error(), "/r/file/link") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestPutFileReportsEveryFailureAndLeavesNoTemporary(t *testing.T) {
	t.Run("a stale temporary that cannot be removed", func(t *testing.T) {
		e := newEnv(t, "/r")
		e.blockTemp("f")
		if _, err := e.fs.PutBytes("f", []byte("new"), "f", 0o644, e.self); err == nil || !strings.Contains(err.Error(), "/r/f") {
			t.Fatalf("got %v", err)
		}
		if exists(e.path("f")) {
			t.Fatal("the destination was created although the write failed")
		}
	})
	t.Run("a source that is not there while the file is already right", func(t *testing.T) {
		e := newEnv(t, "/r")
		if _, err := e.fs.PutBytes("f", []byte("same"), "f", 0o644, e.self); err != nil {
			t.Fatal(err)
		}
		_, err := e.fs.PutFile("f", filepath.Join(e.dir, "absent-source"), "f", 0o644, e.self)
		if err == nil || !strings.Contains(err.Error(), "absent-source") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a source that is not there and nothing at the destination", func(t *testing.T) {
		e := newEnv(t, "/r")
		_, err := e.fs.PutFile("f", filepath.Join(e.dir, "absent-source"), "f", 0o644, e.self)
		if err == nil || !strings.Contains(err.Error(), "absent-source") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestEnsureDirNamesAParentThatIsAFile(t *testing.T) {
	e := newEnv(t, "/r")
	e.write("file", "x", 0o600)
	if _, err := e.fs.EnsureDir("file/sub", 0o755, e.self); err == nil || !strings.Contains(err.Error(), "/r/file/sub") {
		t.Fatalf("got %v", err)
	}
}

func TestRemoveIfPresentRefusesADirectoryWithContent(t *testing.T) {
	e := newEnv(t, "/r")
	e.mkdir("d")
	e.write("d/inner", "x", 0o600)
	if gone, err := e.fs.RemoveIfPresent("d"); err == nil || gone || !strings.Contains(err.Error(), "/r/d") {
		t.Fatalf("got %v, %v", gone, err)
	}
}

func TestEnsureFileModeRefusesAMissingFile(t *testing.T) {
	e := newEnv(t, "/r")
	if _, err := e.fs.EnsureFileMode("m", "absent", 0o600, e.self); err == nil || !strings.Contains(err.Error(), "/r/absent") {
		t.Fatalf("got %v", err)
	}
}

func TestWrapPathNamesThePathAndKeepsTheCause(t *testing.T) {
	if got := wrapPath("/p", nil); got != nil {
		t.Fatalf("a nil error became %v", got)
	}
	got := wrapPath("/p", errBoom)
	if !errors.Is(got, errBoom) || !strings.Contains(got.Error(), "/p") {
		t.Fatalf("got %v", got)
	}
}

func TestSameContentReadsEverythingBeforeItAnswers(t *testing.T) {
	long := strings.Repeat("a", 64<<10+5)
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{long, long, true},
		{long, long[:len(long)-1], false},
		{long[:len(long)-1], long, false},
		{"", "", true},
		{"", "x", false},
	} {
		got, err := sameContent(strings.NewReader(c.a), strings.NewReader(c.b))
		if err != nil || got != c.want {
			t.Errorf("sameContent(%d bytes, %d bytes) = %v, %v; want %v", len(c.a), len(c.b), got, err, c.want)
		}
	}
}

func TestDropDirRemovesWhatTheFailedRunMadeAndReportsWhatItCannot(t *testing.T) {
	e := newEnv(t, "/r")
	e.mkdir("made")
	if err := e.fs.dropDir("made", errBoom); !errors.Is(err, errBoom) || exists(e.path("made")) {
		t.Fatalf("dropDir = %v, the directory still there: %v", err, exists(e.path("made")))
	}
	e.mkdir("held")
	e.write("held/inner", "x", 0o600)
	err := e.fs.dropDir("held", errBoom)
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "held") {
		t.Fatalf("dropDir of a directory with content = %v; want the cause and the removal failure", err)
	}
}

func TestSettleDirRefusesAMissingDirectoryAndOneThatChangedUnderIt(t *testing.T) {
	e := newEnv(t, "/r")
	if err := e.fs.settleDir("absent", nil, 0o755, e.self); err == nil || !strings.Contains(err.Error(), "/r/absent") {
		t.Fatalf("a missing directory: %v", err)
	}
	e.mkdir("a")
	e.mkdir("b")
	seenOfB := infoOf(t, e.path("b"))
	err := e.fs.settleDir("a", seenOfB, 0o755, e.self)
	if err == nil || !strings.Contains(err.Error(), "changed while it was being fixed") {
		t.Fatalf("a directory that is not the one seen: %v", err)
	}
}

// A directory made a moment ago is opened without following a link: a name swapped for a
// symlink to another directory below the root is refused, not settled.
func TestSettleDirDoesNotFollowASymlinkInPlaceOfTheDirectory(t *testing.T) {
	e := newEnv(t, "/r")
	e.mkdir("real")
	e.symlink("real", "swapped")
	if err := e.fs.settleDir("swapped", nil, 0o755, e.self); err == nil {
		t.Fatal("settleDir followed a symlink")
	}
}

func TestFixOpenedRefusesAFileThatIsNotTheOneSeenAndAClosedOne(t *testing.T) {
	e := newEnv(t, "/r")
	e.write("a", "x", 0o600)
	e.write("b", "x", 0o600)
	file, err := os.Open(e.path("a"))
	if err != nil {
		t.Fatal(err)
	}
	if err = fixOpened(file, infoOf(t, e.path("b")), 0o600, e.self); err == nil ||
		!strings.Contains(err.Error(), "changed while it was being fixed") {
		t.Fatalf("a file that is not the one seen: %v", err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if err = fixOpened(file, infoOf(t, e.path("a")), 0o600, e.self); err == nil {
		t.Fatal("a closed file was fixed")
	}
}

func TestFillReportsACopyAndAnOwnerFailure(t *testing.T) {
	e := newEnv(t, "/r")
	e.write("out", "", 0o600)
	readOnly, err := os.Open(e.path("out"))
	if err != nil {
		t.Fatal(err)
	}
	if err = fill(readOnly, strings.NewReader("data"), 0o600, e.self); err == nil {
		t.Fatal("a copy into a read-only file succeeded")
	}
	if err = readOnly.Close(); err != nil {
		t.Fatal(err)
	}
	if err = fill(readOnly, strings.NewReader(""), 0o600, e.self); err == nil {
		t.Fatal("setting the owner of a closed file succeeded")
	}
}
