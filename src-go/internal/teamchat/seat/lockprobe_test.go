package seat

import (
	"os"
	"syscall"
	"testing"
)

// SX1: the L3 probe tells a missing, a free and a held seat lock apart, and reads the free
// one's record; a link, a FIFO or a directory is refused, never followed.
func TestSeatLockProbe(t *testing.T) {
	w := newWorld(t)
	seat := w.sessionSeat("s1")
	if err := w.store.root.mkdirAll(seat.Dir()); err != nil {
		t.Fatal(err)
	}
	got, err := w.store.probe(seat)
	if err != nil || got.state != lockMissing {
		t.Errorf("no lock file: %+v, %v", got, err)
	}
	const record = `{"v":1,"run":"0123456789abcdef","started_ms":1}`
	end := w.keep("s1", record)
	if got, err = w.store.probe(seat); err != nil || got.state != lockHeld {
		t.Errorf("a held lock: %+v, %v", got, err)
	}
	end()
	if got, err = w.store.probe(seat); err != nil || got.state != lockFree || string(got.record) != record {
		t.Errorf("a free lock: %+v, %v", got, err)
	}
}

// SX1: the probe refuses a link, a FIFO or a directory at the lock, and a linked seat directory.
func TestSeatLockProbeRefusals(t *testing.T) {
	w := newWorld(t)
	seat := w.sessionSeat("s1")
	if err := w.store.root.mkdirAll(seat.Dir()); err != nil {
		t.Fatal(err)
	}
	for name, plant := range map[string]func(string) error{
		"link to an owner file": func(p string) error { return os.Symlink(w.pool().Password(), p) },
		"fifo":                  func(p string) error { return syscall.Mkfifo(p, 0o600) },
		"directory":             func(p string) error { return os.Mkdir(p, 0o700) },
	} {
		if err := os.RemoveAll(seat.Lock()); err != nil {
			t.Fatal(err)
		}
		if err := plant(seat.Lock()); err != nil {
			t.Fatal(err)
		}
		if got, err := w.store.probe(seat); err == nil {
			t.Errorf("%s: probed as %+v", name, got)
		}
	}
	if err := os.RemoveAll(seat.Dir()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), seat.Dir()); err != nil {
		t.Fatal(err)
	}
	if got, err := w.store.probe(seat); err == nil {
		t.Errorf("a linked seat directory: probed as %+v", got)
	}
}
