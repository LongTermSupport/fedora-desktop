package guestroot

import (
	"errors"
	"io/fs"
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
	w.done.Add(1)
	go func() {
		defer w.done.Done()
		for {
			select {
			case <-w.stop:
				return
			default:
			}
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
	}()
	return w
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
		t.Run(h.name, func(t *testing.T) {
			g := newTree(t)
			all := append(slices.Clone(readPaths), lockPath)
			all = append(all, dirPaths...)
			all = append(all, socketPath)
			for _, p := range all {
				h.plant(g, p)
			}
			var fifos []string
			if h.name == "a FIFO" {
				for _, p := range all {
					fifos = append(fifos, g.host(p))
				}
			}
			w := startFIFOWriter(fifos)
			rec := newRecorder()
			r := g.root(rec)
			refused := func(what, p string, err error) {
				t.Helper()
				if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), p) {
					t.Errorf("%s %s: %v, want a refusal naming the path", what, p, err)
				}
			}
			for _, p := range readPaths {
				_, _, _, err := r.ReadFile(p, 1<<20)
				refused("ReadFile", p, err)
			}
			_, _, err := r.ProbeLock(lockPath)
			refused("ProbeLock", lockPath, err)
			for _, p := range dirPaths {
				_, _, err := r.ReadDir(p, func(string) bool { return true }, 10)
				refused("ReadDir", p, err)
			}
			if h.name != "a socket" {
				_, err := r.DialSocket(t.Context(), socketPath)
				refused("DialSocket", socketPath, err)
			}
			if h.name == "a FIFO" {
				w.finish(t)
			}
			if len(rec.badOpens) != 0 {
				t.Errorf("an open that was not O_PATH reached %v", rec.badOpens)
			}
			if d := rec.callsOf("dial"); h.name != "a socket" && len(d) != 0 {
				t.Errorf("connected: %v", d)
			}
		})
	}
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
	if _, err := r.Stat("/bin/tmux"); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "/bin/tmux") {
		t.Fatalf("tmux reached only through a link: %v", err)
	}
	if _, err := r.Stat("/bin"); !errors.Is(err, ErrRefused) {
		t.Fatalf("a link itself: %v", err)
	}
	if _, err := r.Stat("/usr/bin/none"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing file: %v", err)
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
	data, info, found, err := r.ReadFile("/etc/passwd", 1<<20)
	if err != nil || !found || string(data) != "root:x:0:0::/root:/bin/sh\n" || info.Size != int64(len(data)) || info.Mode != 0o644 {
		t.Fatalf("%q %+v %v %v", data, info, found, err)
	}
	if _, _, _, err := r.ReadFile("/big", 10); !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), "/big") {
		t.Errorf("over the cap: %v", err)
	}
	if data, _, _, err := r.ReadFile("/exact", 10); err != nil || len(data) != 10 {
		t.Errorf("at the cap: %d %v", len(data), err)
	}
	for _, p := range []string{"/none", "/no/such/dir/file"} {
		if _, _, found, err := r.ReadFile(p, 10); err != nil || found {
			t.Errorf("%s: found %v, %v", p, found, err)
		}
	}
	if _, _, _, err := r.ReadFile("/dir", 10); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "a directory") {
		t.Errorf("a directory: %v", err)
	}
	if _, _, _, err := r.ReadFile("/etc/passwd/x", 10); !errors.Is(err, ErrRefused) {
		t.Errorf("a file used as a directory: %v", err)
	}
	var reopens []call
	for _, c := range rec.callsOf("openat") {
		if strings.HasPrefix(c.path, "/proc/self/fd/") {
			reopens = append(reopens, c)
		}
	}
	if len(reopens) == 0 || slices.ContainsFunc(reopens, func(c call) bool { return c.flags != reopenFlags }) {
		t.Errorf("the reopens: %+v", reopens)
	}
	for _, c := range rec.callsOf("openat2") {
		if c.flags&unix.O_PATH == 0 || c.resolve != readResolve {
			t.Errorf("an open below the root not by TB1's flags: %+v", c)
		}
	}
	rec.swapReopen = g.host("/other")
	if _, _, _, err := r.ReadFile("/etc/passwd", 100); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "changed") {
		t.Errorf("an inode swapped at the reopen: %v", err)
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
	if _, _, err := r.ReadDir("/run/x", valid, 2); !errors.Is(err, ErrTooLarge) {
		t.Errorf("over max: %v", err)
	}
	_, _, err = r.ReadDir("/run/bad", func(s string) bool { return s == "ok" }, 10)
	if !errors.Is(err, ErrRefused) || strings.Contains(err.Error(), "evil") {
		t.Errorf("a name failing its grammar: %v", err)
	}
	if _, found, err := r.ReadDir("/run/none", valid, 3); err != nil || found {
		t.Errorf("missing: %v %v", found, err)
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
	if held, _, err := r.ProbeLock(lockPath); err != nil || held {
		t.Fatalf("released: %v %v", held, err)
	}
	reexec.Start(t, "flock", g.host(lockPath)).Kill()
	if held, _, err := r.ProbeLock(lockPath); err != nil || held {
		t.Fatalf("holder killed: %v %v", held, err)
	}
	for _, c := range rec.callsOf("flock") {
		if c.flags != unix.LOCK_SH|unix.LOCK_NB && c.flags != unix.LOCK_UN {
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

func TestDialSocket(t *testing.T) {
	g := newTree(t)
	ln := g.socket(socketPath)
	rec := newRecorder()
	r := g.root(rec)
	conn, err := r.DialSocket(t.Context(), socketPath)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err := accepted.Read(buf); err != nil || buf[0] != 'x' {
		t.Fatalf("%q %v", buf, err)
	}
	if err := errors.Join(conn.Close(), accepted.Close()); err != nil {
		t.Fatal(err)
	}
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
	t.Run("links to a listening socket", func(t *testing.T) {
		g2 := newTree(t)
		g2.socket("/real.sock")
		g2.link("/run/systemd/private", "../../real.sock")
		g2.link("/run/abs", g2.host("/real.sock"))
		rec2 := newRecorder()
		r2 := g2.root(rec2)
		for _, p := range []string{"/run/systemd/private", "/run/abs"} {
			if _, err := r2.DialSocket(t.Context(), p); !errors.Is(err, ErrRefused) {
				t.Errorf("%s: %v", p, err)
			}
		}
		if d := rec2.callsOf("dial"); len(d) != 0 {
			t.Errorf("connected: %v", d)
		}
	})
	t.Run("no socket", func(t *testing.T) {
		g3 := newTree(t)
		g3.file("/regular", "x", 0o644)
		r3 := g3.root(newRecorder())
		if _, err := r3.DialSocket(t.Context(), "/missing"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("missing: %v", err)
		}
		if _, err := r3.DialSocket(t.Context(), "/regular"); !errors.Is(err, ErrRefused) {
			t.Errorf("a regular file: %v", err)
		}
	})
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
	_, _, _, checks["ReadFile"] = r.ReadFile("/etc/hostname", 10)
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
