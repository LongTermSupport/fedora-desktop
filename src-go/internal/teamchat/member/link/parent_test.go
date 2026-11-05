package link

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/reexec"
)

func TestTheWatchFiresWhenTheProcessEnds(t *testing.T) {
	p := reexec.Start(t, helperHold)
	w, err := watch(p.Pid)
	check(t, err)
	if w.PID() != p.Pid {
		t.Fatalf("pid %d, want %d", w.PID(), p.Pid)
	}
	select {
	case <-w.Exited():
		t.Fatal("fired while the process runs")
	default:
	}
	p.Kill()
	select {
	case <-w.Exited():
	case <-t.Context().Done():
		t.Fatal("the watch never fired")
	}
	check(t, w.Close())
}

// TestAKeeperDeadBeforeTheWatchReadsIsSeen holds the pidfd open, registered with the poller,
// while the keeper dies and the poller sees it readable; RawConn.Read then resets that
// readiness before the watch's first call, so only a watch that tests the pidfd sees the end.
func TestAKeeperDeadBeforeTheWatchReadsIsSeen(t *testing.T) {
	p := reexec.Start(t, helperHold)
	f, err := openPidfd(p.Pid)
	check(t, err)
	p.Kill()
	time.Sleep(100 * time.Millisecond)
	w, err := watchFile(p.Pid, f)
	check(t, err)
	select {
	case <-w.Exited():
	case <-time.After(10 * time.Second):
		t.Error("the watch never saw a keeper that died before it read")
	}
	check(t, w.Close())
}

func TestClosingTheWatchStopsIt(t *testing.T) {
	w, err := watch(os.Getpid())
	check(t, err)
	check(t, w.Close())
	select {
	case <-w.Exited():
		t.Fatal("a closed watch fired")
	default:
	}
}

func TestAPidThatIsNotAProcessCannotBeWatched(t *testing.T) {
	if _, err := watch(-5); err == nil {
		t.Fatal("watched pid -5")
	}
}

func TestTheParentIsCheckedAfterItsWatchIsOpen(t *testing.T) {
	self := os.Getpid()
	for name, c := range map[string]struct {
		ppids []int
		ok    bool
	}{
		"the same parent": {[]int{self, self}, true},
		"reparented":      {[]int{self, 1}, false},
		"init already":    {[]int{1}, false},
	} {
		n := 0
		getppid := func() int {
			n++
			return c.ppids[n-1]
		}
		p, err := watchParent(getppid)
		if (err == nil) != c.ok {
			t.Fatalf("%s: err %v", name, err)
		}
		if err == nil {
			check(t, p.Close())
		} else if !errors.Is(err, errParentGone) {
			t.Fatalf("%s: err %v is not errParentGone", name, err)
		}
	}
}
