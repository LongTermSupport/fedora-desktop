package hot

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
)

func TestALockHeldRefusesASecondTakerAndNamesTheHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".change.lock")
	first, err := takeLock(path, "ccy-teams session add ccy-a alpha (pid 1)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = takeLock(path, "ccy-teams session remove ccy-a alpha (pid 2)")
	var busy *busyError
	if !errors.As(err, &busy) || busy.holder != "ccy-teams session add ccy-a alpha (pid 1)" {
		t.Fatalf("a second taker: %v", err)
	}
	if releaseErr := first.release(); releaseErr != nil {
		t.Fatal(releaseErr)
	}
	if releaseErr := first.release(); releaseErr != nil {
		t.Fatalf("a second release is a no-op: %v", releaseErr)
	}
	second, err := takeLock(path, "ccy-teams session remove ccy-a alpha (pid 2)")
	if err != nil {
		t.Fatalf("free once released: %v", err)
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil || strings.TrimSpace(string(data)) != "ccy-teams session remove ccy-a alpha (pid 2)" {
		t.Fatalf("the file names its holder: %q %v", data, err)
	}
	if err := second.release(); err != nil {
		t.Fatal(err)
	}
}

func TestALockIsNeverTakenThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "precious")
	if err := os.WriteFile(target, []byte("keep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".change.lock")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := takeLock(link, "x"); err == nil {
		t.Fatal("a lock through a symlink was taken")
	}
	if data, err := os.ReadFile(filepath.Clean(target)); err != nil || string(data) != "keep me\n" {
		t.Fatalf("the symlink's target was written: %q %v", data, err)
	}
}

func TestTheAllocationLockIsOnePerCheckoutWhateverItsPath(t *testing.T) {
	runtime, base := t.TempDir(), t.TempDir()
	checkout := filepath.Join(base, "proj")
	if err := os.Mkdir(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "via")
	if err := os.Symlink(checkout, link); err != nil {
		t.Fatal(err)
	}
	direct, err := allocPath(runtime, checkout)
	if err != nil {
		t.Fatal(err)
	}
	viaLink, err := allocPath(runtime, link)
	if err != nil || viaLink != direct {
		t.Fatalf("%q and %q are one checkout: %v", direct, viaLink, err)
	}
	other, err := allocPath(runtime, base)
	if err != nil || other == direct {
		t.Fatalf("another checkout has its own lock: %q %v", other, err)
	}
	if filepath.Dir(direct) != filepath.Join(runtime, "ccy", "alloc") || !strings.HasSuffix(direct, ".lock") {
		t.Fatalf("path %q", direct)
	}
}

// Fable's M4 B1: the membership directory is mounted into the session's container, and flock
// needs no write permission, so a change lock there could be held for good from inside.
func TestTheChangeLockIsOutsideTheMountedMembershipDirectory(t *testing.T) {
	runtime := t.TempDir()
	path, err := changePath(runtime, "ccy-proj-1")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(runtime, "ccy", "change", "ccy-proj-1.lock") {
		t.Fatalf("path %q", path)
	}
	mounted := membership.Dir(runtime, "ccy-proj-1")
	if rel, relErr := filepath.Rel(mounted, path); relErr == nil && !strings.HasPrefix(rel, "..") {
		t.Fatalf("the change lock %q is under the mounted %q", path, mounted)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil || info.Mode().Perm() != lockDirMode {
		t.Fatalf("the lock directory: %v %v", info, err)
	}
}

func TestAChangeLockNeedsASessionNameThatIsOneFileName(t *testing.T) {
	for _, name := range []string{"", ".", "..", "a/b", "../x"} {
		if _, err := changePath(t.TempDir(), name); err == nil {
			t.Fatalf("%q was taken as a lock name", name)
		}
	}
}

// The same digest as agent-bus's seat take (helpers/agent_bus/checkout.py alloc_lock_path, whose
// test pins the same value): sha256 of the resolved path, its first 32 hex digits.
func TestTheAllocationLockNameIsAgentBussToo(t *testing.T) {
	if got := allocName("/srv/proj"); got != "6b9af143446a411aefa3edb76e469379.lock" {
		t.Fatalf("name %q", got)
	}
}
