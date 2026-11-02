package guestroot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/reexec"
)

const kitParent = "/usr/local/lib/teamchat/kit"

func modeOf(t *testing.T, p string) (fs.FileMode, uint32) {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("no stat")
	}
	return fi.Mode(), st.Uid
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestMkdirAll(t *testing.T) {
	g := newTree(t)
	g.mkdir("/usr")
	rec := newRecorder()
	w := g.root(rec).Writer()
	changed, err := w.MkdirAll(kitParent)
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	for _, p := range []string{"/usr/local", "/usr/local/lib", "/usr/local/lib/teamchat", kitParent} {
		if m, uid := modeOf(t, g.host(p)); m != fs.ModeDir|0o755 || uid != 0 {
			t.Errorf("%s: %v uid %d", p, m, uid)
		}
	}
	if changed, err := w.MkdirAll(kitParent); err != nil || changed {
		t.Fatalf("again: %v %v", changed, err)
	}
	if changed, err := w.MkdirAll("/"); err != nil || changed {
		t.Fatalf("the root: %v %v", changed, err)
	}
	for _, c := range rec.callsOf("openat2") {
		if c.resolve != writeResolve {
			t.Errorf("a write open without RESOLVE_NO_XDEV: %+v", c)
		}
	}
	if len(rec.badOpens) != 0 {
		t.Errorf("%v", rec.badOpens)
	}
}

func TestWriterRefusesAnUnsafeDirectoryOnThePath(t *testing.T) {
	cases := []struct {
		name  string
		setup func(g tree)
		want  string
	}{
		{"group writable", func(g tree) {
			g.mkdir("/usr/local")
			if err := os.Chmod(g.host("/usr/local"), 0o775); err != nil {
				g.t.Fatal(err)
			}
		}, "/usr/local"},
		{"other writable", func(g tree) {
			g.mkdir("/usr")
			if err := os.Chmod(g.host("/usr"), 0o757); err != nil {
				g.t.Fatal(err)
			}
		}, "/usr"},
		{"not root's", func(g tree) {
			g.mkdir("/usr/local")
			if err := os.Chown(g.host("/usr/local"), 1000, 1000); err != nil {
				g.t.Skipf("cannot chown: %v", err)
			}
		}, "/usr/local"},
		{"a link", func(g tree) {
			g.mkdir("/opt/local")
			g.mkdir("/usr")
			g.link("/usr/local", "../opt/local")
		}, "/usr/local"},
		{"a file", func(g tree) { g.file("/usr/local", "x", 0o644) }, "/usr/local"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newTree(t)
			c.setup(g)
			w := g.root(newRecorder()).Writer()
			_, err := w.MkdirAll(kitParent)
			if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("MkdirAll: %v", err)
			}
			if _, err := w.WriteFile(kitParent, "f", []byte("x"), 0o644); !errors.Is(err, ErrRefused) {
				t.Fatalf("WriteFile: %v", err)
			}
		})
	}
}

// A bind mount below the root is refused by RESOLVE_NO_XDEV, on the real kernel: the mount is
// made by the bindMount helper in a user and mount namespace of its own, which needs no
// privilege on the host. A kernel that allows no such namespace skips it; TestMkdirAll shows
// only that the flag is passed, never that the kernel refuses.
func TestWriterRefusesABindMount(t *testing.T) {
	g := newTree(t)
	g.mkdir("/usr/local")
	other := t.TempDir()
	if err := os.Chmod(other, 0o755); err != nil {
		t.Fatal(err)
	}
	reexec.StartInUserNamespace(t, "bindMount", g.dir, other).Release()
}

// bindMount is TestWriterRefusesABindMount's child, root of its own user and mount namespace:
// a write below the root of Args[0] succeeds, and once Args[1] is bind-mounted on its
// /usr/local, a write through that mount is refused as a crossed mount (EXDEV).
func bindMount(h *reexec.Helper) int {
	r, err := openDirWith(linuxSys{}, h.Args[0])
	if err != nil {
		return failed("open the root", err)
	}
	if _, err := r.Writer().MkdirAll("/usr/share"); err != nil {
		return failed("a write that crosses no mount", err)
	}
	if err := unix.Mount(h.Args[1], filepath.Join(h.Args[0], "usr/local"), "", unix.MS_BIND, ""); err != nil {
		return failed("bind-mount", err)
	}
	if _, err := r.Writer().MkdirAll(kitParent); !errors.Is(err, ErrRefused) || !errors.Is(err, unix.EXDEV) {
		return failed("a write through the mount, not refused as a crossed mount", err)
	}
	if err := r.Close(); err != nil {
		return failed("close the root", err)
	}
	if err := h.Ready(); err != nil {
		return failed("ready", err)
	}
	return 0
}

// failed tells the parent, on the child's stderr, which step failed and why.
func failed(step string, err error) int {
	if _, werr := fmt.Fprintf(os.Stderr, "%s: %v\n", step, err); werr != nil {
		return 3
	}
	return 2
}

func TestWriteFile(t *testing.T) {
	g := newTree(t)
	g.mkdir("/etc/systemd/system")
	rec := newRecorder()
	w := g.root(rec).Writer()
	const dir, name = "/etc/systemd/system", "u.service"
	changed, err := w.WriteFile(dir, name, []byte("one"), 0o640)
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	if m, uid := modeOf(t, g.host(dir+"/"+name)); m != 0o640 || uid != 0 {
		t.Errorf("mode %v uid %d", m, uid)
	}
	creates := len(rec.callsOf("renameat2"))
	if again, werr := w.WriteFile(dir, name, []byte("one"), 0o640); werr != nil || again {
		t.Fatalf("the same bytes: %v %v", again, werr)
	}
	if len(rec.callsOf("renameat2")) != creates {
		t.Error("the same bytes were rewritten")
	}
	if remoded, werr := w.WriteFile(dir, name, []byte("one"), 0o644); werr != nil || !remoded {
		t.Fatalf("a new mode: %v %v", remoded, werr)
	}
	g.file(dir+"/.new-"+name, "stale", 0o600)
	if rewritten, werr := w.WriteFile(dir, name, []byte("two"), 0o644); werr != nil || !rewritten {
		t.Fatalf("new bytes over a stale temporary: %v %v", rewritten, werr)
	}
	got, err := os.ReadFile(g.host(dir + "/" + name))
	if err != nil || string(got) != "two" {
		t.Fatalf("%q %v", got, err)
	}
	if n := names(t, g.host(dir)); !slices.Equal(n, []string{name}) {
		t.Errorf("a temporary left behind: %v", n)
	}
}

func TestWriteFileOverOtherTypesAndRefusals(t *testing.T) {
	g := newTree(t)
	const dir = "/etc/systemd/system"
	g.mkdir(dir)
	w := g.root(newRecorder()).Writer()
	g.mkdir(dir + "/d")
	if _, err := w.WriteFile(dir, "d", []byte("x"), 0o644); err == nil {
		t.Error("a file over a directory")
	}
	g.fifo(dir + "/p")
	if changed, err := w.WriteFile(dir, "p", []byte("x"), 0o644); err != nil || !changed {
		t.Errorf("a file over a FIFO, which is replaced unopened: %v %v", changed, err)
	}
	for _, bad := range []string{"", ".", "..", "a/b", ".new-x", "a\x00"} {
		if _, err := w.WriteFile(dir, bad, []byte("x"), 0o644); !errors.Is(err, ErrRefused) {
			t.Errorf("name %q: %v", bad, err)
		}
	}
	if _, err := w.WriteFile(dir, "s", []byte("x"), fs.ModeSetuid|0o755); !errors.Is(err, ErrRefused) {
		t.Errorf("a setuid mode: %v", err)
	}
	if _, err := w.WriteFile("/none", "x", []byte("x"), 0o644); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a missing directory: %v", err)
	}
}

func fillKit(d *Dir) error {
	bin, err := d.Mkdir("bin")
	if err != nil {
		return err
	}
	if err := bin.WriteFile("teamchat", []byte("elf"), 0o755); err != nil {
		return err
	}
	return d.WriteFile("settings.json", []byte("{}"), 0o644)
}

// checkKitModes checks the modes and owner of what fillKit placed at dir.
func checkKitModes(t *testing.T, g tree, dir string) {
	t.Helper()
	for p, want := range map[string]fs.FileMode{
		"": fs.ModeDir | 0o755, "/bin": fs.ModeDir | 0o755, "/bin/teamchat": 0o755, "/settings.json": 0o644,
	} {
		if m, uid := modeOf(t, g.host(dir+p)); m != want || uid != 0 {
			t.Errorf("%s: %v uid %d", dir+p, m, uid)
		}
	}
}

func TestPlaceDir(t *testing.T) {
	g := newTree(t)
	g.mkdir(kitParent)
	rec := newRecorder()
	w := g.root(rec).Writer()
	changed, err := w.PlaceDir(kitParent, "d1", fillKit)
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	checkKitModes(t, g, kitParent+"/d1")
	renames := rec.callsOf("renameat2")
	if len(renames) != 1 || renames[0].flags != unix.RENAME_NOREPLACE {
		t.Errorf("renames %+v", renames)
	}
	called := false
	if again, perr := w.PlaceDir(kitParent, "d1", func(*Dir) error { called = true; return nil }); perr != nil || again || called {
		t.Fatalf("a digest already there: %v %v %v", again, perr, called)
	}
	g.mkdir(kitParent + "/.new-d2/junk")
	boom := errors.New("boom")
	if _, perr := w.PlaceDir(kitParent, "d2", func(d *Dir) error {
		if ferr := fillKit(d); ferr != nil {
			return ferr
		}
		return boom
	}); !errors.Is(perr, boom) {
		t.Fatalf("a failed fill: %v", perr)
	}
	if n := names(t, g.host(kitParent)); !slices.Equal(n, []string{"d1"}) {
		t.Errorf("after a failed fill: %v", n)
	}
	if placed, perr := w.PlaceDir(kitParent, "d2", fillKit); perr != nil || !placed {
		t.Fatalf("d2: %v %v", placed, perr)
	}
	if l := rec.leaked(); len(l) != 1 {
		t.Errorf("leaked %v", l)
	}
}

func TestPlaceDirRefuses(t *testing.T) {
	g := newTree(t)
	g.mkdir(kitParent)
	g.mkdir(kitParent + "/d1")
	rec := newRecorder()
	w := g.root(rec).Writer()
	g.mkdir(kitParent + "/gw")
	if err := os.Chmod(g.host(kitParent+"/gw"), 0o775); err != nil {
		t.Fatal(err)
	}
	g.file(kitParent+"/f", "x", 0o644)
	g.link(kitParent+"/l", "d1")
	for _, n := range []string{"gw", "f", "l", ".new-x", ""} {
		if _, err := w.PlaceDir(kitParent, n, fillKit); !errors.Is(err, ErrRefused) {
			t.Errorf("%q: %v", n, err)
		}
	}
	d := &Dir{w: w, fd: -1}
	if _, err := d.Mkdir("a/b"); !errors.Is(err, ErrRefused) {
		t.Errorf("Mkdir of a path: %v", err)
	}
	if err := d.WriteFile("..", nil, 0o644); !errors.Is(err, ErrRefused) {
		t.Errorf("WriteFile of ..: %v", err)
	}
	if err := d.WriteFile("x", nil, fs.ModeSetgid|0o644); !errors.Is(err, ErrRefused) {
		t.Errorf("WriteFile setgid: %v", err)
	}
	if l := rec.leaked(); len(l) != 1 {
		t.Errorf("leaked %v", l)
	}
}

func TestSymlink(t *testing.T) {
	g := newTree(t)
	g.mkdir("/usr/local/bin")
	w := g.root(newRecorder()).Writer()
	const dir = "/usr/local/bin"
	const target = "/usr/local/lib/teamchat/kit/current/bin/teamchat"
	if changed, err := w.Symlink(dir, "teamchat", target); err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	if got, err := os.Readlink(g.host(dir + "/teamchat")); err != nil || got != target {
		t.Fatalf("%q %v", got, err)
	}
	if changed, err := w.Symlink(dir, "teamchat", target); err != nil || changed {
		t.Fatalf("again: %v %v", changed, err)
	}
	if changed, err := w.Symlink(dir, "teamchat", "d2"); err != nil || !changed {
		t.Fatalf("a new target: %v %v", changed, err)
	}
	g.file(dir+"/plain", "x", 0o755)
	if changed, err := w.Symlink(dir, "plain", "d2"); err != nil || !changed {
		t.Fatalf("over a file: %v %v", changed, err)
	}
	g.mkdir(dir + "/d")
	if _, err := w.Symlink(dir, "d", "x"); err == nil {
		t.Error("a link over a directory")
	}
	if n := names(t, g.host(dir)); !slices.Equal(n, []string{"d", "plain", "teamchat"}) {
		t.Errorf("%v", n)
	}
}

func TestSymlinkRefusesABadTargetOrName(t *testing.T) {
	g := newTree(t)
	const dir = "/usr/local/bin"
	g.mkdir(dir)
	w := g.root(newRecorder()).Writer()
	for _, bad := range []string{"", "a\x00b"} {
		if _, err := w.Symlink(dir, "x", bad); !errors.Is(err, ErrRefused) {
			t.Errorf("target %q: %v", bad, err)
		}
	}
	if _, err := w.Symlink(dir, "a/b", "x"); !errors.Is(err, ErrRefused) {
		t.Error("a name with a slash")
	}
}

func TestRemove(t *testing.T) {
	g := newTree(t)
	g.file("/etc/systemd/system/u.service", "x", 0o644)
	g.link("/etc/systemd/system/l", "/etc/passwd")
	g.mkdir("/etc/systemd/system/d")
	w := g.root(newRecorder()).Writer()
	const dir = "/etc/systemd/system"
	for _, n := range []string{"u.service", "l"} {
		if removed, err := w.Remove(dir, n); err != nil || !removed {
			t.Fatalf("%s: %v %v", n, removed, err)
		}
	}
	if removed, err := w.Remove(dir, "u.service"); err != nil || removed {
		t.Fatalf("absent: %v %v", removed, err)
	}
	if _, err := w.Remove(dir, "d"); !errors.Is(err, ErrRefused) {
		t.Fatalf("a directory: %v", err)
	}
	if _, err := w.Remove(dir, ".."); !errors.Is(err, ErrRefused) {
		t.Fatalf("..: %v", err)
	}
}

func TestRemoveTree(t *testing.T) {
	g := newTree(t)
	outside := t.TempDir()
	if err := os.WriteFile(outside+"/keep", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	g.file(kitParent+"/old/bin/teamchat", "x", 0o755)
	g.file(kitParent+"/old/plugin/a/b/c", "x", 0o644)
	g.link(kitParent+"/old/out", outside)
	g.link(kitParent+"/old/bin/up", "../../..")
	g.file(kitParent+"/f", "x", 0o644)
	w := g.root(newRecorder()).Writer()
	if removed, err := w.RemoveTree(kitParent, "old"); err != nil || !removed {
		t.Fatalf("%v %v", removed, err)
	}
	if _, err := os.Stat(outside + "/keep"); err != nil {
		t.Fatalf("a file a link pointed at was removed: %v", err)
	}
	if n := names(t, g.host(kitParent)); !slices.Equal(n, []string{"f"}) {
		t.Errorf("%v", n)
	}
	if removed, err := w.RemoveTree(kitParent, "old"); err != nil || removed {
		t.Fatalf("absent: %v %v", removed, err)
	}
	if _, err := w.RemoveTree(kitParent, "f"); !errors.Is(err, ErrRefused) {
		t.Fatalf("a file: %v", err)
	}
	deep := kitParent + "/deep" + strings.Repeat("/d", maxTreeDepth+1)
	g.mkdir(deep)
	if _, err := w.RemoveTree(kitParent, "deep"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("too deep: %v", err)
	}
}

// Every system call each operation makes fails in turn: the operation fails, and no
// descriptor is left open.
// faultScenario is one operation of the fault sweep, over the fixture its setup builds.
type faultScenario struct {
	name  string
	setup func(g tree)
	run   func(r *Root) error
}

func TestEveryFailingCallFailsItsOperationAndLeaksNothing(t *testing.T) {
	kit := func(g tree) {
		g.mkdir(kitParent)
		g.file(kitParent+"/old/bin/teamchat", "x", 0o755)
		g.mkdir("/usr/local/bin")
		g.link("/usr/local/bin/teamchat", "old")
		g.file("/etc/u", "old", 0o644)
		g.file(lockPath, "{}", 0o600)
		g.mkdir("/run/x/a")
		g.mkdir("/proc/7")
		g.file("/proc/7/stat", "7 (x)", 0o444)
	}
	scenarios := []faultScenario{
		{"Stat", kit, func(r *Root) error { _, err := r.Stat("/etc/u"); return err }},
		{"ReadFile", kit, func(r *Root) error { _, _, _, err := r.ReadFile("/etc/u", 100); return err }},
		{"ReadProcStat", kit, func(r *Root) error { _, _, err := r.ReadProcStat(7); return err }},
		{"ReadDir", kit, func(r *Root) error {
			_, _, err := r.ReadDir("/run/x", func(string) bool { return true }, 4)
			return err
		}},
		{"ProbeLock", kit, func(r *Root) error { _, _, err := r.ProbeLock(lockPath); return err }},
		{"MkdirAll", kit, func(r *Root) error { _, err := r.Writer().MkdirAll(kitParent + "/a/b"); return err }},
		{"WriteFile new", kit, func(r *Root) error { _, err := r.Writer().WriteFile("/etc", "n", []byte("x"), 0o644); return err }},
		{"WriteFile same", kit, func(r *Root) error {
			changed, err := r.Writer().WriteFile("/etc", "u", []byte("old"), 0o644)
			if err == nil && changed {
				return errors.New("rewritten")
			}
			return err
		}},
		{"PlaceDir", kit, func(r *Root) error { _, err := r.Writer().PlaceDir(kitParent, "new", fillKit); return err }},
		{"PlaceDir existing", kit, func(r *Root) error {
			changed, err := r.Writer().PlaceDir(kitParent, "old", fillKit)
			if err == nil && changed {
				return errors.New("replaced")
			}
			return err
		}},
		{"Symlink", kit, func(r *Root) error { _, err := r.Writer().Symlink("/usr/local/bin", "teamchat", "new"); return err }},
		{"Symlink same", kit, func(r *Root) error {
			changed, err := r.Writer().Symlink("/usr/local/bin", "teamchat", "old")
			if err == nil && changed {
				return errors.New("relinked")
			}
			return err
		}},
		{"Remove", kit, func(r *Root) error { _, err := r.Writer().Remove("/etc", "u"); return err }},
		{"RemoveTree", kit, func(r *Root) error { _, err := r.Writer().RemoveTree(kitParent, "old"); return err }},
	}
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) { sweepFaults(t, s) })
	}
}

// sweepFaults counts the calls s makes with nothing failing, then fails each in turn on a
// fresh fixture.
func sweepFaults(t *testing.T, s faultScenario) {
	t.Helper()
	g := newTree(t)
	s.setup(g)
	rec := newRecorder()
	r := g.root(rec)
	rec.arm(-1, nil)
	if err := s.run(r); err != nil {
		t.Fatalf("with nothing failing: %v", err)
	}
	for i := range rec.count() {
		failOne(t, s, i)
	}
}

func failOne(t *testing.T, s faultScenario, i int) {
	t.Helper()
	g := newTree(t)
	s.setup(g)
	rec := newRecorder()
	r := g.root(rec)
	rec.arm(i, unix.EIO)
	if err := s.run(r); err == nil {
		t.Errorf("call %d (%s) failing did not fail it", i, rec.calls[i].op)
	}
	if l := rec.leaked(); len(l) != 1 {
		t.Errorf("call %d failing: open fds %v", i, l)
	}
}
