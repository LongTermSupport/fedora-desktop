package badchown

import (
	"os"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

const otherUID = 4242

// A test that makes a file another user's passes as root and fails for CI's unprivileged user
// with "operation not permitted".
func TestGivesAFileAway(t *testing.T) {
	path := t.TempDir()
	if err := os.Lchown(path, otherUID, -1); err != nil { // want `os.Lchown gives a file to another user`
		t.Fatal(err)
	}
	if err := os.Chown(path, 4242, 4242); err != nil { // want `os.Chown gives a file to another user`
		t.Fatal(err)
	}
	if err := os.Chown(path, os.Geteuid(), 4242); err != nil { // want `os.Chown gives a file to another user`
		t.Fatal(err)
	}
	if err := os.Chown(path, 4242, os.Getegid()); err != nil { // want `os.Chown gives a file to another user`
		t.Fatal(err)
	}
}

func TestEveryWayOfWritingIt(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	t.Cleanup(func() {
		if cerr := f.Close(); cerr != nil {
			t.Fatal(cerr)
		}
	})
	if err := f.Chown(otherUID, -1); err != nil { // want `File.Chown gives a file to another user`
		t.Fatal(err)
	}
	if err := syscall.Chown(path, otherUID, -1); err != nil { // want `syscall.Chown gives a file to another user`
		t.Fatal(err)
	}
	if err := syscall.Lchown(path, otherUID, -1); err != nil { // want `syscall.Lchown gives a file to another user`
		t.Fatal(err)
	}
	if err := syscall.Fchown(int(f.Fd()), otherUID, -1); err != nil { // want `syscall.Fchown gives a file to another user`
		t.Fatal(err)
	}
	if err := unix.Chown(path, otherUID, -1); err != nil { // want `unix.Chown gives a file to another user`
		t.Fatal(err)
	}
	if err := unix.Lchown(path, otherUID, -1); err != nil { // want `unix.Lchown gives a file to another user`
		t.Fatal(err)
	}
	if err := unix.Fchown(int(f.Fd()), otherUID, -1); err != nil { // want `unix.Fchown gives a file to another user`
		t.Fatal(err)
	}
	if err := unix.Fchownat(-100, path, otherUID, -1, 0); err != nil { // want `unix.Fchownat gives a file to another user`
		t.Fatal(err)
	}
}

// A helper is judged on its own: the test that calls it may not be root-gated, and a helper that
// is gated by a caller still fails for the next test that calls it.
func giveAway(t *testing.T, path string) {
	t.Helper()
	if err := os.Lchown(path, otherUID, -1); err != nil { // want `os.Lchown gives a file to another user`
		t.Fatal(err)
	}
}

func TestThroughAHelper(t *testing.T) {
	giveAway(t, t.TempDir())
}

// A skip on the error is a skip when the user is not root, which is no more a test than the
// branch is: the first of these is flagged because the failure is fatal, not skipped.
func TestFatalOnTheError(t *testing.T) {
	if err := os.Lchown(t.TempDir(), otherUID, -1); err != nil { // want `os.Lchown gives a file to another user`
		t.Fatalf("cannot chown: %v", err)
	}
}
