package state

import (
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// heldSeat is w's seat taken under a keeper lock.
func heldSeat(t *testing.T, w *world) *lock.SeatLock {
	t.Helper()
	k, err := lock.TakeKeeper(t.Context(), noSleep{}, w.root, w.run)
	check(t, err)
	t.Cleanup(func() { check(t, k.Release()) })
	seat, err := k.TakeSeat(t.Context(), w.root, w.seat)
	check(t, err)
	return seat
}

func newWriter(t *testing.T, w *world, l *lock.SeatLock) *StoreWriter {
	t.Helper()
	sw, err := NewStoreWriter(w.root, l)
	check(t, err)
	t.Cleanup(func() { check(t, sw.Close()) })
	return sw
}

func TestStoreWriterWritesAndLoads(t *testing.T) {
	w := newWorld(t)
	l := heldSeat(t, w)
	t.Cleanup(func() { check(t, l.Close()) })
	sw := newWriter(t, w, l)
	if _, err := sw.Load(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Load of no store = %v, want fs.ErrNotExist", err)
	}
	s := sampleStore(t)
	check(t, sw.Write(s))
	got, err := sw.Load()
	check(t, err)
	want, err := s.encode()
	check(t, err)
	again, err := got.encode()
	check(t, err)
	if string(again) != string(want) {
		t.Fatalf("Load = %s, want %s", again, want)
	}
	info, err := os.Stat(w.seat.Store())
	check(t, err)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("the store's mode = %v, want 0600", info.Mode().Perm())
	}
	got.AddLedger(Ledger{ID: eventID(9), Room: s.Ledger[0].Room, Item: s.Ledger[0].Item, From: otherID, AtMS: 21})
	check(t, sw.Write(got))
	loaded, err := sw.Load()
	check(t, err)
	if len(loaded.Ledger) != 2 {
		t.Fatalf("the second write was not loaded: %+v", loaded.Ledger)
	}
}

func TestStoreWriterRefusesAnInvalidStore(t *testing.T) {
	w := newWorld(t)
	l := heldSeat(t, w)
	t.Cleanup(func() { check(t, l.Close()) })
	sw := newWriter(t, w, l)
	s := sampleStore(t)
	s.UserID = "nobody"
	if err := sw.Write(s); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an invalid store was written: %v", err)
	}
	if _, err := os.Stat(w.seat.Store()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a refused write left a file: %v", err)
	}
}

func TestStoreWriterNeedsTheLock(t *testing.T) {
	w := newWorld(t)
	if _, err := NewStoreWriter(w.root, nil); !errors.Is(err, ErrNoLock) {
		t.Fatalf("NewStoreWriter(nil) = %v, want ErrNoLock", err)
	}
	l := heldSeat(t, w)
	sw := newWriter(t, w, l)
	check(t, l.Close())
	if err := sw.Write(sampleStore(t)); !errors.Is(err, lock.ErrReleased) {
		t.Fatalf("Write after the lock was closed = %v, want ErrReleased", err)
	}
	if _, err := os.Stat(w.seat.Store()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a write after release left a file: %v", err)
	}
	if _, err := NewStoreWriter(w.root, l); !errors.Is(err, lock.ErrReleased) {
		t.Fatalf("NewStoreWriter of a closed lock = %v, want ErrReleased", err)
	}
}

func TestStoreWriterRefusesAnotherTeam(t *testing.T) {
	w := newWorld(t)
	l := heldSeat(t, w)
	t.Cleanup(func() { check(t, l.Close()) })
	sw := newWriter(t, w, l)
	beta, err := paths.ParseTeamID("beta")
	check(t, err)
	other := NewStore(beta, "@repo.2+site.podman:beta.teamchat.internal", StoreRun{ID: runID(t), StartedMS: 1}, Gate{})
	var inv *InvalidError
	if werr := sw.Write(other); !errors.As(werr, &inv) || inv.Code != CodeSeat {
		t.Fatalf("a store of another team = %v, want code seat", werr)
	}
	data, err := other.encode()
	check(t, err)
	check(t, w.root.Replace(w.seat.Store(), data, 0o600))
	if _, err := sw.Load(); !errors.As(err, &inv) || inv.Code != CodeSeat {
		t.Fatalf("Load of another team's store = %v, want code seat", err)
	}
}

func TestStoreWriterRefusesAStoreNotTheCallers(t *testing.T) {
	w := newWorld(t)
	l := heldSeat(t, w)
	t.Cleanup(func() { check(t, l.Close()) })
	sw := newWriter(t, w, l)
	check(t, sw.Write(sampleStore(t)))
	check(t, os.Lchown(w.seat.Store(), 4242, -1))
	if _, err := sw.Load(); !errors.Is(err, homefs.ErrConfig) {
		t.Fatalf("Load of another uid's store = %v, want ErrConfig", err)
	}
}

// TestWriteReplacesAMemberFile holds the plain writer every file but the store goes through.
func TestWriteReplacesAMemberFile(t *testing.T) {
	w := newWorld(t)
	full := w.seat.Dir() + "/record.json"
	check(t, Write(w.root, full, LockRecord{Run: runID(t), StartedMS: 4}))
	got, err := Read(w.root, full, MaxLockRecord, DecodeLockRecord)
	if err != nil || got.StartedMS != 4 {
		t.Fatalf("Read after Write = %+v, %v", got, err)
	}
	info, err := os.Stat(full)
	check(t, err)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("the file's mode = %v, want 0600", info.Mode().Perm())
	}
	if err := Write(w.root, full, LockRecord{StartedMS: 4}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an invalid file was written: %v", err)
	}
}

func TestStoreWriterNeedsAPrivateSeatDirectory(t *testing.T) {
	w := newWorld(t)
	l := heldSeat(t, w)
	t.Cleanup(func() { check(t, l.Close()) })
	check(t, os.Chmod(w.seat.Dir(), 0o750))
	if _, err := NewStoreWriter(w.root, l); !errors.Is(err, homefs.ErrConfig) {
		t.Fatalf("NewStoreWriter over a 0750 seat = %v, want ErrConfig", err)
	}
}
