package guestroot

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/reexec"
)

// The paths P4's steps read below a guest root (TB1's list).
var (
	readPaths = []string{
		"/etc/passwd",
		"/etc/group",
		"/home/<user>/repo/.git/config",
		"/home/<user>/repo/.claude/ccy/ccy.env.local",
		"/run/teamchat-session-agent/teamchat/run/r1/session.pid",
		"/etc/systemd/system/teamchat-session-agent.service",
		"/usr/lib/systemd/systemd",
		"/usr/bin/tmux",
	}
	lockPath   = "/home/<user>/.local/state/teamchat/seats/t/seat.lock"
	dirPaths   = []string{"/usr/local/lib/teamchat/kit", "/run/user/1000/teamchat/run"}
	socketPath = "/run/systemd/private"
)

// fifoWriter polls each FIFO with O_WRONLY|O_NONBLOCK until stopped. Each open must fail with
// ENXIO: no reader ever existed.
type fifoWriter struct {
	stop chan struct{}
	done sync.WaitGroup
	mu   sync.Mutex
	bad  []string
	runs int
}

func startFIFOWriter(paths []string) *fifoWriter {
	w := &fifoWriter{stop: make(chan struct{})}
	w.done.Go(func() {
		// A round of polls comes before every look at stop, so a fast test still polls.
		for {
			w.pollRound(paths)
			select {
			case <-w.stop:
				return
			default:
			}
		}
	})
	return w
}

func (w *fifoWriter) pollRound(paths []string) {
	for _, p := range paths {
		fd, err := unix.Open(p, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		w.mu.Lock()
		w.runs++
		if !errors.Is(err, unix.ENXIO) {
			w.bad = append(w.bad, p)
		}
		w.mu.Unlock()
		if err == nil {
			if cerr := unix.Close(fd); cerr != nil {
				panic(cerr)
			}
		}
	}
}

func (w *fifoWriter) finish(t *testing.T) {
	t.Helper()
	close(w.stop)
	w.done.Wait()
	if len(w.bad) != 0 || w.runs == 0 {
		t.Errorf("the FIFO writer found a reader at %v (%d polls)", w.bad, w.runs)
	}
}

// planted is one hostile thing at a path.
type planted struct {
	name  string
	plant func(g tree, p string)
}

func hostile() []planted {
	return []planted{
		{"a FIFO", func(g tree, p string) { g.fifo(p) }},
		{"a socket", func(g tree, p string) { g.socket(p) }},
		{"a relative link", func(g tree, p string) {
			g.file("/elsewhere/real", "x", 0o644)
			g.link(p, strings.Repeat("../", strings.Count(p, "/")-1)+"elsewhere/real")
		}},
		{"an absolute link to a host path", func(g tree, p string) {
			hostFile := g.t.TempDir() + "/real"
			if err := os.WriteFile(hostFile, []byte("host"), 0o644); err != nil {
				g.t.Fatal(err)
			}
			g.link(p, hostFile)
		}},
		{"a directory link", func(g tree, p string) {
			g.mkdir("/elsewhere/d")
			g.link(p, g.host("/elsewhere/d"))
		}},
	}
}

func TestTB1RefusesEveryNonRegularRead(t *testing.T) {
	for _, h := range hostile() {
		t.Run(h.name, func(t *testing.T) { refusesPlanted(t, h) })
	}
}

// refusesPlanted plants h at every path TB1 reads and checks that each read refuses it.
func refusesPlanted(t *testing.T, h planted) {
	t.Helper()
	g := newTree(t)
	all := append(slices.Clone(readPaths), lockPath)
	all = append(all, dirPaths...)
	all = append(all, socketPath)
	for _, p := range all {
		h.plant(g, p)
	}
	isFIFO := h.name == "a FIFO"
	var w *fifoWriter
	if isFIFO {
		w = startFIFOWriter(hostPaths(g, all))
	}
	rec := newRecorder()
	r := g.root(rec)
	for _, p := range readPaths {
		refused(t, "ReadFile", p, readFileErr(r, p))
	}
	refused(t, "ProbeLock", lockPath, probeLockErr(r, lockPath))
	for _, p := range dirPaths {
		refused(t, "ReadDir", p, readDirErr(r, p))
	}
	if h.name != "a socket" {
		_, err := r.DialSocket(t.Context(), socketPath)
		refused(t, "DialSocket", socketPath, err)
		if d := rec.callsOf("dial"); len(d) != 0 {
			t.Errorf("connected: %v", d)
		}
	}
	if isFIFO {
		w.finish(t)
	}
	if len(rec.badOpens) != 0 {
		t.Errorf("an open that was not O_PATH reached %v", rec.badOpens)
	}
}

func hostPaths(g tree, ps []string) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, g.host(p))
	}
	return out
}

func refused(t *testing.T, what, p string, err error) {
	t.Helper()
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), p) {
		t.Errorf("%s %s: %v, want a refusal naming the path", what, p, err)
	}
}

// readResult is ReadFile's four results as one value.
type readResult struct {
	data  []byte
	info  Info
	found bool
	err   error
}

func readFile(r *Root, p string, maxSize int64) readResult {
	data, info, found, err := r.ReadFile(p, maxSize)
	return readResult{data: data, info: info, found: found, err: err}
}

func readFileErr(r *Root, p string) error { return readFile(r, p, 1<<20).err }

func probeLockErr(r *Root, p string) error {
	_, _, err := r.ProbeLock(p)
	return err
}

func readDirErr(r *Root, p string) error {
	_, _, err := r.ReadDir(p, func(string) bool { return true }, 10)
	return err
}

func TestStatDoesNotOpenAFIFOAndRefusesALink(t *testing.T) {
	g := newTree(t)
	g.fifo("/usr/bin/tmux")
	g.link("/bin", "usr/bin")
	g.file("/usr/lib/systemd/systemd", "elf", 0o755)
	w := startFIFOWriter([]string{g.host("/usr/bin/tmux")})
	r := g.root(newRecorder())
	info, err := r.Stat("/usr/bin/tmux")
	if err != nil || info.Mode.Type() != fs.ModeNamedPipe {
		t.Fatalf("Stat of a FIFO: %+v %v", info, err)
	}
	if _, serr := r.Stat("/bin/tmux"); !errors.Is(serr, ErrRefused) || !strings.Contains(serr.Error(), "/bin/tmux") {
		t.Fatalf("tmux reached only through a link: %v", serr)
	}
	if _, serr := r.Stat("/bin"); !errors.Is(serr, ErrRefused) {
		t.Fatalf("a link itself: %v", serr)
	}
	if _, serr := r.Stat("/usr/bin/none"); !errors.Is(serr, fs.ErrNotExist) {
		t.Fatalf("a missing file: %v", serr)
	}
	info, err = r.Stat("/usr/lib/systemd/systemd")
	if err != nil || !info.Mode.IsRegular() || info.Mode.Perm() != 0o755 || info.Size != 3 || info.Ino == 0 {
		t.Fatalf("Stat of a file: %+v %v", info, err)
	}
	w.finish(t)
}

func TestInfoModeNamesEveryType(t *testing.T) {
	cases := []struct {
		raw  uint32
		want fs.FileMode
	}{
		{unix.S_IFREG | 0o644, 0o644},
		{unix.S_IFDIR | 0o755, fs.ModeDir | 0o755},
		{unix.S_IFLNK | 0o777, fs.ModeSymlink | 0o777},
		{unix.S_IFIFO | 0o600, fs.ModeNamedPipe | 0o600},
		{unix.S_IFSOCK | 0o600, fs.ModeSocket | 0o600},
		{unix.S_IFCHR | 0o600, fs.ModeDevice | fs.ModeCharDevice | 0o600},
		{unix.S_IFBLK | 0o600, fs.ModeDevice | 0o600},
		{unix.S_IFREG | unix.S_ISUID | unix.S_ISGID | unix.S_ISVTX | 0o755, fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky | 0o755},
		{0o170000 | 0o644, fs.ModeIrregular | 0o644},
	}
	for _, c := range cases {
		if got := fileMode(c.raw); got != c.want {
			t.Errorf("fileMode(%o) = %v, want %v", c.raw, got, c.want)
		}
	}
	for _, m := range []fs.FileMode{0, fs.ModeDir, fs.ModeSymlink, fs.ModeNamedPipe, fs.ModeSocket, fs.ModeDevice, fs.ModeIrregular} {
		if kind(m) == "" {
			t.Errorf("no kind for %v", m)
		}
	}
}

func TestPathsMustBeCleanAndAbsolute(t *testing.T) {
	g := newTree(t)
	g.file("/b", "x", 0o644)
	r := g.root(newRecorder())
	for _, p := range []string{"", "b", "/a/../b", "/b/", "//b", "/./b", "/b\x00", "/.."} {
		if _, err := r.Stat(p); !errors.Is(err, ErrRefused) {
			t.Errorf("Stat(%q): %v", p, err)
		}
		if _, _, _, err := r.ReadFile(p, 10); !errors.Is(err, ErrRefused) {
			t.Errorf("ReadFile(%q): %v", p, err)
		}
	}
}

func TestReadFile(t *testing.T) {
	g := newTree(t)
	g.file("/etc/passwd", "root:x:0:0::/root:/bin/sh\n", 0o644)
	g.file("/big", strings.Repeat("x", 11), 0o600)
	g.file("/exact", strings.Repeat("x", 10), 0o600)
	g.file("/other", "swapped", 0o600)
	g.mkdir("/dir")
	rec := newRecorder()
	r := g.root(rec)
	got := readFile(r, "/etc/passwd", 1<<20)
	if got.err != nil || !got.found || string(got.data) != "root:x:0:0::/root:/bin/sh\n" || got.info.Size != int64(len(got.data)) || got.info.Mode != 0o644 {
		t.Fatalf("%+v", got)
	}
	if got := readFile(r, "/big", 10); !errors.Is(got.err, ErrTooLarge) || !strings.Contains(got.err.Error(), "/big") {
		t.Errorf("over the cap: %v", got.err)
	}
	if got := readFile(r, "/exact", 10); got.err != nil || len(got.data) != 10 {
		t.Errorf("at the cap: %d %v", len(got.data), got.err)
	}
	checkReadFlags(t, rec)
	rec.swapReopen = g.host("/other")
	if _, _, _, err := r.ReadFile("/etc/passwd", 100); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "changed") {
		t.Errorf("an inode swapped at the reopen: %v", err)
	}
}

func TestReadFileMissingOrNotAFile(t *testing.T) {
	g := newTree(t)
	g.file("/etc/passwd", "x", 0o644)
	g.mkdir("/dir")
	r := g.root(newRecorder())
	for _, p := range []string{"/none", "/no/such/dir/file"} {
		if got := readFile(r, p, 10); got.err != nil || got.found {
			t.Errorf("%s: found %v, %v", p, got.found, got.err)
		}
	}
	if got := readFile(r, "/dir", 10); !errors.Is(got.err, ErrRefused) || !strings.Contains(got.err.Error(), "a directory") {
		t.Errorf("a directory: %v", got.err)
	}
	if got := readFile(r, "/etc/passwd/x", 10); !errors.Is(got.err, ErrRefused) {
		t.Errorf("a file used as a directory: %v", got.err)
	}
}

// checkReadFlags checks that every reopen used TB1's reopen flags and every open below the
// root was O_PATH with TB1's resolve flags.
func checkReadFlags(t *testing.T, rec *recorder) {
	t.Helper()
	var reopens []call
	for _, c := range rec.callsOf("openat") {
		if strings.HasPrefix(c.path, "/proc/self/fd/") {
			reopens = append(reopens, c)
		}
	}
	if len(reopens) == 0 || slices.ContainsFunc(reopens, func(c call) bool { return c.oflags != reopenFlags }) {
		t.Errorf("the reopens: %+v", reopens)
	}
	for _, c := range rec.callsOf("openat2") {
		if c.flags&unix.O_PATH == 0 || c.resolve != readResolve {
			t.Errorf("an open below the root not by TB1's flags: %+v", c)
		}
	}
}

func TestOpenRetriesARacedResolution(t *testing.T) {
	g := newTree(t)
	g.file("/f", "x", 0o644)
	rec := newRecorder()
	r := g.root(rec)
	rec.arm(0, unix.EAGAIN)
	if _, err := r.Stat("/f"); err != nil {
		t.Fatalf("one EAGAIN: %v", err)
	}
	rec.failOp, rec.failErr = "openat2", unix.EAGAIN
	if _, err := r.Stat("/f"); !errors.Is(err, unix.EAGAIN) {
		t.Fatalf("EAGAIN every time: %v", err)
	}
	if n := len(rec.callsOf("openat2")); n != 2+openTries {
		t.Errorf("%d tries", n)
	}
}

func TestReadDir(t *testing.T) {
	g := newTree(t)
	for _, n := range []string{"b", "a", "c"} {
		g.mkdir("/run/x/" + n)
	}
	g.mkdir("/run/bad/ok")
	g.mkdir("/run/bad/\x1b[31mevil")
	g.file("/run/f", "x", 0o644)
	r := g.root(newRecorder())
	valid := func(s string) bool { return len(s) == 1 }
	names, found, err := r.ReadDir("/run/x", valid, 3)
	if err != nil || !found || !slices.Equal(names, []string{"a", "b", "c"}) {
		t.Fatalf("%v %v %v", names, found, err)
	}
	if _, _, derr := r.ReadDir("/run/x", valid, 2); !errors.Is(derr, ErrTooLarge) {
		t.Errorf("over max: %v", derr)
	}
	_, _, err = r.ReadDir("/run/bad", func(s string) bool { return s == "ok" }, 10)
	if !errors.Is(err, ErrRefused) || strings.Contains(err.Error(), "evil") {
		t.Errorf("a name failing its grammar: %v", err)
	}
	if _, found, err := r.ReadDir("/run/none", valid, 3); err != nil || found {
		t.Errorf("missing: %v %v", found, err)
	}
	g.mkdir("/run/empty")
	if names, found, err := r.ReadDir("/run/empty", valid, 3); err != nil || !found || len(names) != 0 {
		t.Errorf("empty: %v %v %v", names, found, err)
	}
	if _, _, err := r.ReadDir("/run/f", valid, 3); !errors.Is(err, ErrRefused) {
		t.Errorf("a file: %v", err)
	}
	if _, _, err := r.ReadDir("/run/x", valid, 0); err == nil {
		t.Errorf("max 0 accepted")
	}
}

func TestProbeLock(t *testing.T) {
	g := newTree(t)
	g.file(lockPath, `{"run":"r1"}`, 0o600)
	rec := newRecorder()
	r := g.root(rec)

	held, record, err := r.ProbeLock(lockPath)
	if err != nil || held || string(record) != `{"run":"r1"}` {
		t.Fatalf("no holder: %v %q %v", held, record, err)
	}
	holder := reexec.Start(t, "flock", g.host(lockPath))
	held, record, err = r.ProbeLock(lockPath)
	if err != nil || !held || string(record) != `{"run":"r1"}` {
		t.Fatalf("held: %v %q %v", held, record, err)
	}
	holder.Release()
	wantFree(t, r, "released")
	reexec.Start(t, "flock", g.host(lockPath)).Kill()
	wantFree(t, r, "holder killed")
	for _, c := range rec.callsOf("flock") {
		if c.oflags != unix.LOCK_SH|unix.LOCK_NB && c.oflags != unix.LOCK_UN {
			t.Errorf("a lock that is not LOCK_SH|LOCK_NB: %+v", c)
		}
	}
	g.file("/big.lock", strings.Repeat("x", lockCap+1), 0o600)
	if _, _, err := r.ProbeLock("/big.lock"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a record over its cap: %v", err)
	}
	if _, _, err := r.ProbeLock("/none"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("no lock: %v", err)
	}
	g.mkdir("/dir.lock")
	if _, _, err := r.ProbeLock("/dir.lock"); !errors.Is(err, ErrRefused) {
		t.Errorf("a directory: %v", err)
	}
}

func wantFree(t *testing.T, r *Root, when string) {
	t.Helper()
	if held, _, err := r.ProbeLock(lockPath); err != nil || held {
		t.Fatalf("%s: %v %v", when, held, err)
	}
}

func TestDialSocket(t *testing.T) {
	g := newTree(t)
	ln := g.socket(socketPath)
	rec := newRecorder()
	r := g.root(rec)
	conn, err := r.DialSocket(t.Context(), socketPath)
	if err != nil {
		t.Fatal(err)
	}
	echoOnce(t, conn, ln)
	dials := rec.callsOf("dial")
	if len(dials) != 1 || !strings.HasPrefix(dials[0].path, "/proc/self/fd/") {
		t.Errorf("not connected through its own fd: %+v", dials)
	}
	if l := rec.leaked(); len(l) != 1 {
		t.Errorf("only the root's fd should be open: %v", l)
	}

	t.Run("a socket not root's", func(t *testing.T) {
		if err := os.Lchown(g.host(socketPath), 1000, 1000); err != nil {
			t.Skipf("cannot chown the socket: %v", err)
		}
		before := len(rec.callsOf("dial"))
		if _, err := r.DialSocket(t.Context(), socketPath); !errors.Is(err, ErrRefused) {
			t.Fatalf("got %v", err)
		}
		if len(rec.callsOf("dial")) != before {
			t.Fatal("connected")
		}
	})
}

// echoOnce sends one byte over conn and reads it at the listener's end, then closes both.
func echoOnce(t *testing.T, conn *net.UnixConn, ln *net.UnixListener) {
	t.Helper()
	accepted, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if _, werr := conn.Write([]byte("x")); werr != nil {
		t.Fatal(werr)
	}
	buf := make([]byte, 1)
	if _, rerr := accepted.Read(buf); rerr != nil || buf[0] != 'x' {
		t.Fatalf("%q %v", buf, rerr)
	}
	if cerr := errors.Join(conn.Close(), accepted.Close()); cerr != nil {
		t.Fatal(cerr)
	}
}

func TestDialSocketRefusesLinksToAListeningSocket(t *testing.T) {
	g := newTree(t)
	g.socket("/real.sock")
	g.link("/run/systemd/private", "../../real.sock")
	g.link("/run/abs", g.host("/real.sock"))
	rec := newRecorder()
	r := g.root(rec)
	for _, p := range []string{"/run/systemd/private", "/run/abs"} {
		if _, err := r.DialSocket(t.Context(), p); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: %v", p, err)
		}
	}
	if d := rec.callsOf("dial"); len(d) != 0 {
		t.Errorf("connected: %v", d)
	}
}

func TestDialSocketWithNoSocket(t *testing.T) {
	g := newTree(t)
	g.file("/regular", "x", 0o644)
	r := g.root(newRecorder())
	if _, err := r.DialSocket(t.Context(), "/missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
	if _, err := r.DialSocket(t.Context(), "/regular"); !errors.Is(err, ErrRefused) {
		t.Errorf("a regular file: %v", err)
	}
}

// A root whose init dies during a read answers ErrGone, not what the torn-down mount said.
func TestReadsOnADeadRootAreGone(t *testing.T) {
	p := reexec.Start(t, "idle")
	r, err := host().Pin(t.Context(), pids(p.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	p.Kill()
	checks := map[string]error{}
	checks["ReadFile"] = readFile(r, "/etc/hostname", 10).err
	_, _, checks["ReadDir"] = r.ReadDir("/etc", func(string) bool { return true }, 10)
	_, _, checks["ProbeLock"] = r.ProbeLock("/etc/hostname")
	_, _, checks["ReadProcStat"] = r.ReadProcStat(1)
	_, checks["DialSocket"] = r.DialSocket(t.Context(), "/run/systemd/private")
	_, checks["MkdirAll"] = r.Writer().MkdirAll("/tmp")
	for name, err := range checks {
		if !errors.Is(err, ErrGone) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestALeasedFileIsRefused is B1: a guest user's write lease on a file it owns makes the
// reopen's O_NONBLOCK open answer EWOULDBLOCK, which is that file refused (the member's own
// failure), never a host fault; the lock probe reads it as free.
func TestALeasedFileIsRefused(t *testing.T) {
	g := newTree(t)
	g.file("/leased", "x", 0o600)
	g.file(lockPath, `{"run":"r1"}`, 0o600)
	r := g.root(newRecorder())
	for _, p := range []string{"/leased", lockPath} {
		holder, err := os.Open(g.host(p))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := unix.FcntlInt(holder.Fd(), unix.F_SETLEASE, unix.F_WRLCK); err != nil {
			t.Fatalf("take a lease on %s: %v", p, err)
		}
		t.Cleanup(func() {
			if _, err := unix.FcntlInt(holder.Fd(), unix.F_SETLEASE, unix.F_UNLCK); err != nil {
				t.Error(err)
			}
			if err := holder.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	if got := readFile(r, "/leased", 10); !errors.Is(got.err, ErrRefused) || !strings.Contains(got.err.Error(), "/leased") {
		t.Errorf("a leased file: %v", got.err)
	}
	if _, _, err := r.ProbeLock(lockPath); !errors.Is(err, ErrRefused) {
		t.Errorf("a leased lock: %v", err)
	}
}
