package lxc

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// recordedTicks is the keeper start time every planted session.pid records.
const recordedTicks = 77

// sessionPID plants a session.pid naming the old digest under t1's unit run root, for a keeper
// of pid 4242 whose guest /proc/4242/stat starts at ticks.
func sessionPID(t *testing.T, b *box, ticks int64) {
	t.Helper()
	sessionPIDUnder(t, b, paths.GuestUnitRunRoot(keyOf(t, "t1")), ticks)
}

// sessionPIDUnder is sessionPID under the run root dir.
func sessionPIDUnder(t *testing.T, b *box, dir string, ticks int64) {
	t.Helper()
	run := dir[1:] + "/" + runID + "/session.pid"
	writeTree(t, b.dir, map[string]string{
		run: `{"v":1,"run":"` + runID + `","keeper":{"pid":4242,"start_ticks":` + strconv.Itoa(recordedTicks) +
			`},"session":null,"kit":"` + oldDigest + `","launch":"claude","inbound":"accept","kind":"lxc"}`,
		"proc/4242/stat": "4242 (teamchat) S 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 " + strconv.FormatInt(ticks, 10) + " 0 0\n",
	})
}

func oldKit(t *testing.T, b *box) string {
	t.Helper()
	dir := filepath.Join(b.dir, paths.HostKitRoot, oldDigest)
	writeTree(t, b.dir, map[string]string{filepath.Join(paths.HostKitRoot, oldDigest, "bin/teamchat")[1:]: "old"})
	return dir
}

func TestOldKitRemovedUnlessALiveSessionNamesIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant func(t *testing.T, b *box)
		kept  bool
	}{
		{"no session", func(*testing.T, *box) {}, false},
		{"live session", func(t *testing.T, b *box) { t.Helper(); sessionPID(t, b, 77) }, true},
		{"live session of a seat this apply does not act on", func(t *testing.T, b *box) {
			t.Helper()
			sessionPIDUnder(t, b, paths.GuestUnitRunRoot(keyOf(t, "t2")), 77)
		}, true},
		{"live login session", func(t *testing.T, b *box) {
			t.Helper()
			root, err := paths.GuestLoginRunRoot(agentUID)
			if err != nil {
				t.Fatal(err)
			}
			sessionPIDUnder(t, b, root, 77)
		}, true},
		{"live session under a name /run's grammar refuses", func(t *testing.T, b *box) {
			t.Helper()
			sessionPIDUnder(t, b, "/run/other/teamchat/run", 77)
		}, false},
		{"/run cannot be listed", func(t *testing.T, b *box) {
			t.Helper()
			writeTree(t, b.dir, map[string]string{"run": "->tmp"})
		}, true},
		{"start time differs", func(t *testing.T, b *box) { t.Helper(); sessionPID(t, b, 78) }, false},
		{"keeper gone", func(t *testing.T, b *box) {
			t.Helper()
			sessionPID(t, b, 77)
			if err := os.Remove(filepath.Join(b.dir, "proc/4242/stat")); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"forged stat", func(t *testing.T, b *box) {
			t.Helper()
			sessionPID(t, b, 77)
			writeTree(t, b.dir, map[string]string{"proc/4242/stat": "not a stat"})
		}, true},
		{"forged record", func(t *testing.T, b *box) {
			t.Helper()
			writeTree(t, b.dir, map[string]string{paths.GuestUnitRunRoot(keyOf(t, "t1"))[1:] + "/" + runID + "/session.pid": "{not json"})
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			b := w.addBox("c1")
			dir := oldKit(t, b)
			tc.plant(t, b)
			w.declare([]string{"t1"}, w.member("t1", "c1"))
			if _, err := w.apply(PhasePresent); err != nil {
				t.Fatal(err)
			}
			_, err := os.Stat(dir)
			if kept := err == nil; kept != tc.kept {
				t.Fatalf("old kit kept %v, want %v (%v)", kept, tc.kept, err)
			}
			if _, err := os.Stat(filepath.Join(b.dir, paths.HostKitRoot, kitDigest)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestKitWriteRefusals(t *testing.T) {
	w := newWorld(t)
	b := w.addBox("c1")
	if err := os.MkdirAll(filepath.Join(b.dir, "usr/local/lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(b.dir, "usr/local/lib"), 0o775); err != nil {
		t.Fatal(err)
	}
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	_, err := w.apply(PhasePresent)
	if err == nil || !strings.Contains(err.Error(), "kit") {
		t.Fatalf("a group-writable /usr/local/lib was not refused: %v", err)
	}
}

func TestExistingKitDigestChecked(t *testing.T) {
	w := newWorld(t)
	b := w.addBox("c1")
	writeTree(t, b.dir, map[string]string{filepath.Join(paths.HostKitRoot, kitDigest, "bin/teamchat")[1:]: "theirs"})
	if err := os.Chmod(filepath.Join(b.dir, paths.HostKitRoot, kitDigest), 0o777); err != nil {
		t.Fatal(err)
	}
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	if _, err := w.apply(PhasePresent); err == nil {
		t.Fatal("a world-writable kit digest was kept")
	}
}

func TestHostKitWalkRefusals(t *testing.T) {
	for name, plant := range map[string]func(t *testing.T, kit string){
		"a link": func(t *testing.T, kit string) {
			t.Helper()
			if err := os.Symlink("/etc/passwd", filepath.Join(kit, "bin/evil")); err != nil {
				t.Fatal(err)
			}
		},
		"too deep": func(t *testing.T, kit string) {
			t.Helper()
			writeTree(t, kit, map[string]string{"a/b/c/d/e/f/g/x": "deep"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.addBox("c1")
			plant(t, filepath.Join(w.host, "kit"))
			w.declare([]string{"t1"}, w.member("t1", "c1"))
			if _, err := w.apply(PhasePresent); err == nil {
				t.Fatal("the host kit was copied")
			}
		})
	}
	w := newWorld(t)
	w.addBox("c1")
	if err := os.RemoveAll(filepath.Join(w.host, "kit")); err != nil {
		t.Fatal(err)
	}
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	if _, err := w.apply(PhasePresent); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing host kit: %v", err)
	}
}
