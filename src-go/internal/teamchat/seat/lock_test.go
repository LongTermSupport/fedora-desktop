package seat

import (
	"context"
	"errors"
	"os"
	"testing"
)

// SL1: the allocation lock is exclusive; a second holder waits, bounded, then is busy.
func TestAllocationLock(t *testing.T) {
	w := newWorld(t)
	a := w.lock()
	if !w.store.holds(a) {
		t.Errorf("the store does not see its own lock")
	}
	other := w.open()
	before := w.clock.Now()
	if _, err := other.Lock(t.Context()); !errors.Is(err, ErrBusy) {
		t.Fatalf("a second lock: %v", err)
	}
	if waited := w.clock.Now().Sub(before); waited < allocWait || waited > allocWait+allocPoll {
		t.Errorf("waited %v, want about %v", waited, allocWait)
	}
	w.unlock(a)
	if w.store.holds(a) {
		t.Errorf("an unlocked lock still holds")
	}
	b, err := other.Lock(t.Context())
	if err != nil {
		t.Fatalf("lock after the unlock: %v", err)
	}
	if w.store.holds(b) || w.store.holds(nil) {
		t.Errorf("one store's lock holds for another, or no lock holds")
	}
	w.unlock(b)
	if err := b.Unlock(); !errors.Is(err, ErrNotLocked) {
		t.Errorf("a second unlock: %v", err)
	}
	lockDir := w.home + "/run/teamchat/alloc"
	if mode(t, lockDir).Perm() != 0o700 {
		t.Errorf("the lock directory is %v", mode(t, lockDir))
	}
}

// SL1: a cancelled wait ends with the context's error.
func TestAllocationLockCancelled(t *testing.T) {
	w := newWorld(t)
	a := w.lock()
	defer w.unlock(a)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := w.open().Lock(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled wait: %v", err)
	}
}

// SL1: the lock file is opened through no link, and its directory must be the owner's 0700.
func TestAllocationLockRefusesLinksAndOpenDirectories(t *testing.T) {
	w := newWorld(t)
	a := w.lock()
	w.unlock(a)
	lockDir := w.home + "/run/teamchat/alloc"
	entries, err := os.ReadDir(lockDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("lock directory %v, %v", entries, err)
	}
	lockFile := lockDir + "/" + entries[0].Name()
	if err := os.Remove(lockFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir()+"/elsewhere", lockFile); err != nil {
		t.Fatal(err)
	}
	if _, err := w.store.Lock(t.Context()); err == nil {
		t.Errorf("a linked lock file was locked")
	}
	if err := os.Remove(lockFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := w.store.Lock(t.Context()); !errors.Is(err, ErrConfig) {
		t.Errorf("a 0755 lock directory: %v", err)
	}
}
