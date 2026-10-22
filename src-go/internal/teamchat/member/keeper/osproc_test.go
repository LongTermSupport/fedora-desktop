package keeper

import (
	"context"
	"encoding/binary"
	"os"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/reexec"
)

var helpers = reexec.Helpers{
	"main": runMain,
	"wait": func(h *reexec.Helper) int {
		if err := h.Ready(); err != nil {
			return 1
		}
		return 0
	},
}

func TestMain(m *testing.M) { os.Exit(reexec.Main(m, helpers)) }

// bound is how long a test waits for a real process or the kernel before it fails.
const bound = 30 * time.Second

func TestProcStartTicks(t *testing.T) {
	a, err := procStat{}.StartTicks(os.Getpid())
	mustDo(t, err)
	b, err := procStat{}.StartTicks(os.Getpid())
	mustDo(t, err)
	if a <= 0 || a != b {
		t.Fatalf("start ticks %d then %d", a, b)
	}
	p := reexec.Start(t, "wait")
	c, err := procStat{}.StartTicks(p.Pid)
	mustDo(t, err)
	if c < a {
		t.Fatalf("a child started at %d, before its parent's %d", c, a)
	}
	p.Release()
	if _, err := (procStat{}).StartTicks(p.Pid); err == nil {
		t.Fatal("a reaped child still has a start time")
	}
	if _, err := (procStat{}).StartTicks(-1); err == nil {
		t.Fatal("pid -1 has a start time")
	}
}

func TestParseStartTicks(t *testing.T) {
	ok := "12 (a) b) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 4242 20"
	if n, err := parseStartTicks(ok); err != nil || n != 4242 {
		t.Fatalf("%d %v", n, err)
	}
	for _, bad := range []string{"12 no name", "12 (a) S 1 2", "12 (a) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 x"} {
		if _, err := parseStartTicks(bad); err == nil {
			t.Errorf("%q read", bad)
		}
	}
}

// A pidfd's Exited closes when its process ends; a signal to an ended process is nil.
func TestPidfdSeesTheExit(t *testing.T) {
	p := reexec.Start(t, "wait")
	fd, err := osPidfds{}.Open(p.Pid)
	mustDo(t, err)
	select {
	case <-fd.Exited():
		t.Fatal("exited while running")
	default:
	}
	mustDo(t, fd.Signal(syscall.Signal(0)))
	p.Kill()
	select {
	case <-fd.Exited():
	case <-time.After(bound):
		t.Fatal("the pidfd never saw the exit")
	}
	mustDo(t, fd.Signal(syscall.SIGTERM))
	mustDo(t, fd.Close())
	if _, err := (osPidfds{}).Open(-1); err == nil {
		t.Fatal("a pidfd on pid -1")
	}
}

// Close ends a pidfd's wait without an exit.
func TestPidfdClose(t *testing.T) {
	p := reexec.Start(t, "wait")
	fd, err := osPidfds{}.Open(p.Pid)
	mustDo(t, err)
	mustDo(t, fd.Close())
	if err := fd.Signal(syscall.SIGTERM); err == nil {
		t.Fatal("a closed pidfd signalled")
	}
	p.Release()
}

// The watcher reports the desired file's rename into place, and its channel closes on Close.
func TestInotifyWatcher(t *testing.T) {
	dir := t.TempDir()
	w, err := newInotify()
	mustDo(t, err)
	ch, err := w.Watch(dir)
	mustDo(t, err)
	mustDo(t, os.WriteFile(dir+"/.tmp", []byte("{}"), 0o600))
	mustDo(t, os.Rename(dir+"/.tmp", dir+"/desired.json"))
	ctx, cancel := context.WithTimeout(context.Background(), bound)
	defer cancel()
	for found := false; !found; {
		select {
		case ev := <-ch:
			found = ev.Name == "desired.json" && ev.Op&unix.IN_MOVED_TO != 0
		case <-ctx.Done():
			t.Fatal("no event for the rename")
		}
	}
	mustDo(t, w.Close())
	for open := true; open; {
		select {
		case _, open = <-ch:
		case <-ctx.Done():
			t.Fatal("the channel stayed open after Close")
		}
	}
	if _, err := w.Watch(dir); err == nil {
		t.Fatal("a closed watcher watched")
	}
}

func TestInotifyWatchRefused(t *testing.T) {
	w, err := newInotify()
	mustDo(t, err)
	if _, err := w.Watch(t.TempDir() + "/missing"); err == nil {
		t.Fatal("a missing directory watched")
	}
	mustDo(t, w.Close())
}

func TestParseEvents(t *testing.T) {
	buf := event(1, unix.IN_MOVED_TO, "desired.json")
	buf = append(buf, event(1, unix.IN_Q_OVERFLOW, "")...)
	evs, err := parseEvents(buf)
	mustDo(t, err)
	if len(evs) != 2 || evs[0].Name != "desired.json" || evs[1].Op != unix.IN_Q_OVERFLOW {
		t.Fatalf("%+v", evs)
	}
	if _, err := parseEvents(buf[:10]); err == nil {
		t.Fatal("a short event read")
	}
	long := event(1, unix.IN_CREATE, "x")
	if _, err := parseEvents(long[:len(long)-1]); err == nil {
		t.Fatal("a cut name read")
	}
}

func putUint32(b []byte, v uint32) { binary.NativeEndian.PutUint32(b, v) }

// event is one inotify record as the kernel writes it, the name NUL padded to 16 bytes.
func event(wd uint32, mask uint32, name string) []byte {
	n := 0
	if name != "" {
		n = 16
	}
	b := make([]byte, unix.SizeofInotifyEvent+n)
	putUint32(b[0:], wd)
	putUint32(b[4:], mask)
	putUint32(b[12:], uint32(n))
	copy(b[unix.SizeofInotifyEvent:], name)
	return b
}
