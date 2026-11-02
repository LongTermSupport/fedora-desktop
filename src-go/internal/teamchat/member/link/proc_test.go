package link

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// fakeProcTree writes a /proc of one process: its stat, status and fd directory.
func fakeProcTree(t *testing.T, pid int, stat, status string, fds int) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, strconv.Itoa(pid))
	check(t, os.MkdirAll(filepath.Join(p, "fd"), 0o700))
	check(t, os.WriteFile(filepath.Join(p, "stat"), []byte(stat), 0o600))
	check(t, os.WriteFile(filepath.Join(p, "status"), []byte(status), 0o600))
	for i := range fds {
		check(t, os.WriteFile(filepath.Join(p, "fd", strconv.Itoa(i)), nil, 0o600))
	}
	return dir
}

// A stat line whose command name holds ") (" and spaces, as a hostile name may.
const statLine = "42 (we ird) (x) S 7 42 42 0 -1 4194560 100 0 0 0 250 50 0 0 20 0 1 0 12345 1000 300 18446744073709551615\n"

const statusText = "Name:\twe ird\nVmRSS:\t   1200 kB\nvoluntary_ctxt_switches:\t30\nnonvoluntary_ctxt_switches:\t12\n"

func TestOSProcReadsAStatWhoseNameHoldsParens(t *testing.T) {
	p := OSProc{Dir: fakeProcTree(t, 42, statLine, statusText, 3)}
	ppid, err := p.PPid(42)
	check(t, err)
	ticks, err := p.StartTicks(42)
	check(t, err)
	if ppid != 7 || ticks != 12345 {
		t.Fatalf("ppid %d ticks %d, want 7 and 12345", ppid, ticks)
	}
	c, err := p.Cost(42)
	check(t, err)
	want := Cost{RSSKiB: 1200, CPUMS: 3000, FDs: 3, Wakeups: 42, StartTicks: 12345}
	if c != want {
		t.Fatalf("cost %+v, want %+v", c, want)
	}
}

func TestOSProcRefusesWhatIsNotAProcess(t *testing.T) {
	cases := map[string]struct{ stat, status string }{
		"no name":         {"42 S 7", statusText},
		"short":           {"42 (x) S 7 1", statusText},
		"not a number":    {"42 (x) S seven" + " 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0", statusText},
		"no rss":          {statLine, "voluntary_ctxt_switches:\t1\nnonvoluntary_ctxt_switches:\t1\n"},
		"bad rss":         {statLine, "VmRSS:\tlots kB\nvoluntary_ctxt_switches:\t1\nnonvoluntary_ctxt_switches:\t1\n"},
		"no switches":     {statLine, "VmRSS:\t1 kB\n"},
		"bad switches":    {statLine, "VmRSS:\t1 kB\nvoluntary_ctxt_switches:\tx\nnonvoluntary_ctxt_switches:\t1\n"},
		"a field missing": {statLine, "VmRSS:\t1 kB\nvoluntary_ctxt_switches\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p := OSProc{Dir: fakeProcTree(t, 42, c.stat, c.status, 0)}
			_, ppidErr := p.PPid(42)
			_, costErr := p.Cost(42)
			if ppidErr == nil && costErr == nil {
				t.Fatal("a broken process was read")
			}
		})
	}
}

func TestOSProcRefusesAMissingProcess(t *testing.T) {
	p := OSProc{Dir: t.TempDir()}
	_, err := p.StartTicks(9)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err %v, want not exist", err)
	}
	if _, err := p.Cost(9); err == nil {
		t.Fatal("a missing process has a cost")
	}
	tree := fakeProcTree(t, 9, statLine, statusText, 0)
	check(t, os.Remove(filepath.Join(tree, "9", "status")))
	if _, err := (OSProc{Dir: tree}).Cost(9); err == nil {
		t.Fatal("a process with no status has a cost")
	}
	check(t, os.RemoveAll(filepath.Join(tree, "9", "fd")))
	check(t, os.WriteFile(filepath.Join(tree, "9", "status"), []byte(statusText), 0o600))
	if _, err := (OSProc{Dir: tree}).Cost(9); err == nil {
		t.Fatal("a process with no fd directory has a cost")
	}
}

func TestOSProcReadsThisProcess(t *testing.T) {
	p := OSProc{Dir: "/proc"}
	ppid, err := p.PPid(os.Getpid())
	check(t, err)
	if ppid != os.Getppid() {
		t.Fatalf("ppid %d, want %d", ppid, os.Getppid())
	}
	c, err := p.Cost(os.Getpid())
	check(t, err)
	if c.RSSKiB <= 0 || c.FDs <= 0 {
		t.Fatalf("cost %+v of a live process", c)
	}
}
