package proof

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/leakcheck"
)

func TestMain(m *testing.M) { os.Exit(leakcheck.Tests(m).Run()) }

// event is one inotify record as the kernel writes it: wd, mask, cookie and length, then the
// name padded with NULs to that length.
func event(wd int32, mask uint32, name string) []byte {
	padded := 0
	if name != "" {
		padded = (len(name)/16 + 1) * 16
	}
	buf, err := binary.Append(nil, binary.NativeEndian, inotifyEvent{Wd: wd, Mask: mask, Len: uint32(padded)})
	if err != nil {
		panic(err)
	}
	return append(buf, append([]byte(name), make([]byte, padded-len(name))...)...)
}

// patience bounds a wait for an inotify event the kernel has already queued, so a broken
// watcher fails the test instead of hanging it; no test sleeps.
const patience = 10 * time.Second

func watch(t *testing.T, path string) Watcher {
	t.Helper()
	w, err := Watch(path)
	check(t, err)
	t.Cleanup(func() { check(t, w.Close()) })
	return w
}

// changed waits for one change, then drains any others coalesced behind it.
func changed(t *testing.T, w Watcher, what string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), patience)
	defer cancel()
	select {
	case _, ok := <-w.Changes():
		if !ok {
			t.Fatalf("%s: Changes closed", what)
		}
	case <-ctx.Done():
		t.Fatalf("%s: no change", what)
	}
}

// quiet is a fence: it makes a file in another watched directory and waits for that change,
// so any change w would have queued before it has arrived; w must then have none.
func quiet(t *testing.T, w Watcher, what string) {
	t.Helper()
	fenceDir := t.TempDir()
	fence := watch(t, filepath.Join(fenceDir, "fence.jsonl"))
	drain(w)
	check(t, os.WriteFile(filepath.Join(fenceDir, "fence.jsonl"), nil, 0o600))
	changed(t, fence, "the fence")
	select {
	case <-w.Changes():
		t.Fatalf("%s: a change, want none", what)
	default:
	}
}

func drain(w Watcher) {
	for {
		select {
		case <-w.Changes():
		default:
			return
		}
	}
}

func TestWatchSeesGrowth(t *testing.T) {
	path := transcript(t, "")
	w := watch(t, path)
	appendTo(t, path, idleLine("x"))
	changed(t, w, "growth")
}

func TestWatchSeesARotationAndGrowthAfterIt(t *testing.T) {
	path := transcript(t, "")
	w := watch(t, path)
	check(t, os.WriteFile(path+".new", nil, 0o600))
	check(t, os.Rename(path+".new", path))
	changed(t, w, "rotation")
	drain(w)
	appendTo(t, path, idleLine("after"))
	changed(t, w, "growth of the new file")
}

func TestWatchSeesANewFileInTheDirectory(t *testing.T) {
	path := transcript(t, "")
	w := watch(t, path)
	check(t, os.WriteFile(filepath.Join(filepath.Dir(path), "cleared.jsonl"), nil, 0o600))
	changed(t, w, "a new file")
}

func TestWatchBeforeTheFileExists(t *testing.T) {
	path := filepath.Join(filepath.Dir(transcript(t, "")), "later.jsonl")
	w := watch(t, path)
	check(t, os.WriteFile(path, nil, 0o600))
	changed(t, w, "creation")
	drain(w)
	appendTo(t, path, "grown\n")
	changed(t, w, "growth of the created file")
}

func TestWatchIgnoresAnotherFilesGrowth(t *testing.T) {
	path := transcript(t, "")
	other := filepath.Join(filepath.Dir(path), "other.jsonl")
	check(t, os.WriteFile(other, nil, 0o600))
	w := watch(t, path)
	appendTo(t, other, "noise\n")
	quiet(t, w, "another file's growth")
}

func TestWatchOfAMissingDirectoryFails(t *testing.T) {
	if _, err := Watch(filepath.Join(t.TempDir(), "absent", "s.jsonl")); err == nil {
		t.Fatal("Watch of a missing directory succeeded")
	}
	if _, err := Watch("relative.jsonl"); !errors.Is(err, ErrRefused) {
		t.Fatalf("Watch of a relative path = %v, want ErrRefused", err)
	}
}

// TestAStoppedSessionHoldsNoOther is the offline rule at this layer: one session's transcript
// gone, its watcher closed, and the other session's watcher and scan go on; the gone one's
// scan fails at once instead of waiting.
func TestAStoppedSessionHoldsNoOther(t *testing.T) {
	stopped := transcript(t, "")
	running := transcript(t, "")
	gone, err := Watch(stopped)
	check(t, err)
	live := watch(t, running)
	check(t, gone.Close())
	check(t, os.RemoveAll(filepath.Dir(stopped)))
	if _, err := Scan(stopped, Mark{}, []string{nonceA}, big); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Scan of the stopped session = %v, want fs.ErrNotExist", err)
	}
	appendTo(t, running, idleLine("teamchat delivery "+nonceA+": live"))
	changed(t, live, "the running session's growth")
	if got := scan(t, running, Mark{}, []string{nonceA}, big); len(got.Found) != 1 {
		t.Fatalf("Scan of the running session = %+v, want its needle", got)
	}
}

func TestWatchCloseEndsChanges(t *testing.T) {
	w, err := Watch(transcript(t, ""))
	check(t, err)
	check(t, w.Close())
	if _, ok := <-w.Changes(); ok {
		t.Fatal("Changes still open after Close")
	}
}

// Close unblocks the watcher's read of the inotify fd, which the runtime's poller holds, and
// returns only once the watcher's goroutine has ended.
func TestCloseUnblocksTheReadAndWaitsForTheWatcher(t *testing.T) {
	leakcheck.Check(t)
	got, err := Watch(transcript(t, ""))
	check(t, err)
	w, ok := got.(*watcher)
	if !ok {
		t.Fatalf("Watch returned a %T", got)
	}
	for !readParked() {
		runtime.Gosched()
	}
	check(t, w.Close())
	select {
	case <-w.done:
	default:
		t.Fatal("Close returned before the watcher's goroutine ended")
	}
}

// readParked is whether a watcher's goroutine is parked in its read, waiting on the poller.
func readParked() bool {
	buf := make([]byte, 1<<20)
	dump := string(buf[:runtime.Stack(buf, true)])
	for g := range strings.SplitSeq(dump, "\n\n") {
		if strings.Contains(g, "[IO wait") && strings.Contains(g, "proof.(*watcher).run") {
			return true
		}
	}
	return false
}

func TestEventsDecodesAndJudges(t *testing.T) {
	const fileWD, dirWD = 1, 2
	w := &watcher{file: fileWD, dir: dirWD, base: "s.jsonl"}
	for name, tc := range map[string]struct {
		wd      int32
		mask    uint32
		name    string
		change  bool
		rewatch bool
	}{
		"the file grew":          {fileWD, unix.IN_MODIFY, "", true, false},
		"the file's watch ended": {fileWD, unix.IN_IGNORED, "", false, false},
		"an overflow":            {-1, unix.IN_Q_OVERFLOW, "", true, false},
		"the file made again":    {dirWD, unix.IN_CREATE, "s.jsonl", true, true},
		"the file moved in":      {dirWD, unix.IN_MOVED_TO, "s.jsonl", true, true},
		"another file made":      {dirWD, unix.IN_CREATE, "t.jsonl", true, false},
		"another file grew":      {dirWD, unix.IN_MODIFY, "t.jsonl", false, false},
		"a stale watch's event":  {9, unix.IN_MODIFY, "", false, false},
	} {
		t.Run(name, func(t *testing.T) {
			buf := event(tc.wd, tc.mask, tc.name)
			buf = append(buf, event(tc.wd, tc.mask, tc.name)...)
			change, rewatch, err := w.judge(buf)
			check(t, err)
			if change != tc.change || rewatch != tc.rewatch {
				t.Fatalf("judge = %v, %v; want %v, %v", change, rewatch, tc.change, tc.rewatch)
			}
		})
	}
	if _, _, err := w.judge([]byte{1, 2, 3}); err == nil {
		t.Fatal("judge of a short event succeeded")
	}
	short := event(dirWD, unix.IN_CREATE, "s.jsonl")
	if _, _, err := w.judge(short[:len(short)-1]); err == nil {
		t.Fatal("judge of a cut name succeeded")
	}
}
