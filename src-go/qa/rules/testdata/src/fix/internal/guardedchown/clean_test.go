package guardedchown

import (
	"os"
	"testing"
)

// None of these is flagged: each cannot fail for the user a runner is, or says so openly.

func TestTheCallersOwnOwner(t *testing.T) {
	path := t.TempDir()
	if err := os.Chown(path, os.Geteuid(), os.Getegid()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, -1, -1); err != nil {
		t.Fatal(err)
	}
	if err := os.Lchown(path, os.Getuid(), -1); err != nil {
		t.Fatal(err)
	}
}

// A branch on the user being root: the call runs only for root.
func TestInARootBranch(t *testing.T) {
	path := t.TempDir()
	if os.Geteuid() == 0 {
		if err := os.Lchown(path, 4242, -1); err != nil {
			t.Fatal(err)
		}
	}
}

// A skip when the user is not root guards the whole test.
func TestSkipsWhenNotRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root to make a file another user's")
	}
	if err := os.Lchown(t.TempDir(), 4242, 4242); err != nil {
		t.Fatal(err)
	}
}

// A skip on the call's own error.
func TestSkipsOnTheError(t *testing.T) {
	if err := os.Chown(t.TempDir(), 1000, 1000); err != nil {
		t.Skipf("cannot chown: %v", err)
	}
}

// Another call of the same names is not a chown.
func TestOtherCalls(t *testing.T) {
	if err := os.Chmod(t.TempDir(), 0o600); err != nil {
		t.Fatal(err)
	}
}
