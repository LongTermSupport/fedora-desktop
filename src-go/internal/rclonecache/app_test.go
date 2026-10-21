package rclonecache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// world is a run's surroundings: a temporary root holding the home directory and the
// mountpoints, a fake runner answering findmnt and rclone, and /proc as a map.
type world struct {
	t      *testing.T
	root   string
	home   string
	cwd    string
	fake   *execx.Fake
	proc   fstest.MapFS
	mounts []map[string]string
	env    map[string]string
	tty    bool
	noRC   bool // rclone not on PATH
	out    bytes.Buffer
	errOut bytes.Buffer
}

const statsJSON = `{"diskCache":{"bytesUsed":429496729600,"files":36176,"uploadsQueued":0,` +
	`"uploadsInProgress":0,"erroredFiles":0,"outOfSpace":false},` +
	`"opt":{"CacheMaxSize":429496729600,"CacheMaxAge":31536000000000000}}`

func newWorld(t *testing.T) *world {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, root: root, home: filepath.Join(root, "home"), fake: &execx.Fake{}, proc: fstest.MapFS{}}
	w.cwd = w.home
	w.env = map[string]string{"HOME": w.home}
	if err := os.MkdirAll(filepath.Join(w.home, ".config", "rclone"), 0o700); err != nil {
		t.Fatal(err)
	}
	w.write(filepath.Join(w.home, ".config", "rclone", "rc-auth.env"), "RCLONE_RC_USER=u\nRCLONE_RC_PASS=secret\n")
	w.fake.OnFunc([]string{"findmnt"}, func(execx.Cmd) (execx.Result, error) {
		if len(w.mounts) == 0 {
			return execx.Result{ExitCode: 1}, nil
		}
		data, err := json.Marshal(map[string]any{"filesystems": w.mounts})
		return execx.Result{Stdout: data}, err
	})
	return w
}

func (w *world) write(path, data string) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

// mount adds an rclone mount of source at <root>/mnt/<dir>, served by a process with the
// given extra arguments (pid orders the processes); it returns the mountpoint.
func (w *world) mount(pid, source, dir string, extra ...string) string {
	w.t.Helper()
	target := filepath.Join(w.root, "mnt", dir)
	if err := os.MkdirAll(target, 0o750); err != nil {
		w.t.Fatal(err)
	}
	w.mounts = append(w.mounts, map[string]string{"source": source, "target": target})
	if pid != "" {
		args := append([]string{"/usr/bin/rclone", "mount", "--config=x", source, target}, extra...)
		w.proc[pid+"/cmdline"] = &fstest.MapFile{Data: []byte(strings.Join(args, "\x00") + "\x00")}
	}
	return target
}

func (w *world) stats(addr, body string) {
	w.fake.On([]string{"rclone", "rc", "--url=http://" + addr, "vfs/stats"}, execx.Result{Stdout: []byte(body)})
}

func (w *world) run(args ...string) term.ExitCode {
	w.t.Helper()
	w.out.Reset()
	w.errOut.Reset()
	app := App{Env: Env{
		Runner: w.fake,
		Getenv: func(k string) string { return w.env[k] },
		Getwd:  func() (string, error) { return w.cwd, nil },
		LookPath: func(name string) (string, error) {
			if w.noRC && name == "rclone" {
				return "", exec.ErrNotFound
			}
			return "/usr/bin/" + name, nil
		},
		Proc:             w.proc,
		Stdout:           &w.out,
		Stderr:           &w.errOut,
		StdoutIsTerminal: w.tty,
	}}
	return app.Run(context.Background(), args)
}

func (w *world) expect(code, want term.ExitCode, wantOut string) {
	w.t.Helper()
	if code != want || w.out.String() != wantOut {
		w.t.Fatalf("exit %d (want %d)\nstdout:\n%s\nwant:\n%s\nstderr:\n%s", code, want, w.out.String(), wantOut, w.errOut.String())
	}
}

// E1: the usage on stdout, before anything runs.
func TestHelp(t *testing.T) {
	w := newWorld(t)
	w.noRC = true
	code := w.run("--bogus", "-h")
	if code != term.ExitOK || !strings.HasPrefix(w.out.String(), "Usage: rclone-cache-status [--label TEXT] [--no-dir] [TARGET]") ||
		len(w.fake.Calls()) != 0 {
		t.Fatalf("%d %q %v", code, w.out.String(), w.fake.Calls())
	}
}

// E2: usage errors.
func TestUsageError(t *testing.T) {
	w := newWorld(t)
	w.expect(w.run("--bogus"), term.ExitUsage, "")
	if w.errOut.String() != "rclone-cache-status: unknown option '--bogus' — see 'rclone-cache-status --help'.\n" {
		t.Fatalf("%q", w.errOut.String())
	}
}

// E3: rclone missing, before any output.
func TestRcloneMissing(t *testing.T) {
	w := newWorld(t)
	w.noRC = true
	w.expect(w.run(), term.ExitFailure, "")
	if !strings.Contains(w.errOut.String(), "rclone is not installed; it is installed by play-rclone.yml.") {
		t.Fatalf("%q", w.errOut.String())
	}
}

// E10: no mounts at all, none by that name, none serving the path.
func TestNoMounts(t *testing.T) {
	w := newWorld(t)
	w.expect(w.run(), term.ExitOK, "=== rclone cache status ===\n  No rclone mounts found.\n")
	w.mount("12", "gdrive:", "gdrive", "--rc-addr=localhost:5572")
	w.expect(w.run("--label", "before", "photos"), term.ExitOK,
		"=== rclone cache status (before) ===\n  No rclone mount matching 'photos' found.\n")
}

// E7: findmnt failing is not "no mounts".
func TestFindmntFailure(t *testing.T) {
	w := newWorld(t)
	w.fake.On([]string{"findmnt"}, execx.Result{ExitCode: 2, Stderr: []byte("findmnt: bad usage\n")})
	w.expect(w.run(), term.ExitFailure, "")
	if !strings.Contains(w.errOut.String(), "findmnt could not list the rclone mounts: ") {
		t.Fatalf("%q", w.errOut.String())
	}
	w.fake.On([]string{"findmnt"}, execx.Result{Stdout: []byte("not json")})
	w.expect(w.run(), term.ExitFailure, "")
	w.fake.On([]string{"findmnt"}, execx.Result{ExitCode: 1, Stdout: []byte("{}")})
	w.expect(w.run(), term.ExitFailure, "")
	if call := w.fake.Calls()[0]; call.Line() != "findmnt -J -l -o SOURCE,TARGET -t fuse.rclone" {
		t.Fatalf("argv %q", call.Line())
	}
}

// E11-E14: the summary over every kind of mount line, plain.
func TestSummary(t *testing.T) {
	w := newWorld(t)
	w.mount("12", "gdrive:", "gdrive", "--rc", "--rc-addr=localhost:5572")
	w.mount("13", "photos:/Camera", "photos", "--rc-addr=localhost:5573")
	w.mount("", "nopid:", "nopid")
	w.mount("15", "norc:", "norc")
	w.mount("16", "two:", "two", "--rc-addr=a:1", "--rc-addr=b:2")
	w.stats("localhost:5572", statsJSON)
	w.stats("localhost:5573", `{"diskCache":{"bytesUsed":1536,"files":3,"uploadsQueued":2,"uploadsInProgress":1,`+
		`"erroredFiles":4,"outOfSpace":true},"opt":{"CacheMaxSize":0,"CacheMaxAge":0}}`)
	w.expect(w.run("--no-dir"), term.ExitOK, "=== rclone cache status ===\n"+
		"  gdrive  cache: 400GB / 400GB (100%) · 36176 files · max-age 365d\n"+
		"  photos  cache: 1.5KB / ∞ · 3 files · max-age off · queued: 2 · uploading: 1 · 4 errored · OUT OF SPACE\n"+
		"  nopid  (no --rc; cannot query)\n"+
		"  norc  (no --rc; cannot query)\n"+
		"  two  error: more than one --rc-addr: a:1 b:2\n")
	for _, c := range w.fake.Calls() {
		if strings.Contains(c.Line(), "secret") {
			t.Fatalf("the password is in argv: %q", c.Line())
		}
	}
}

// E14, E20: the usage colours, on a terminal only and never with NO_COLOR.
func TestSummaryColour(t *testing.T) {
	w := newWorld(t)
	w.tty = true
	w.mount("12", "a:", "a", "--rc-addr=h:1")
	w.mount("13", "b:", "b", "--rc-addr=h:2")
	w.mount("14", "c:", "c", "--rc-addr=h:3")
	w.mount("15", "d:", "d")
	w.mount("16", "e:", "e", "--rc-addr=h:5")
	w.stats("h:1", `{"diskCache":{"bytesUsed":79},"opt":{"CacheMaxSize":100,"CacheMaxAge":7200000000000}}`)
	w.stats("h:2", `{"diskCache":{"bytesUsed":80,"erroredFiles":1},"opt":{"CacheMaxSize":100}}`)
	w.stats("h:3", `{"diskCache":{"bytesUsed":95,"outOfSpace":true},"opt":{"CacheMaxSize":100}}`)
	w.stats("h:5", `[]`)
	const (
		b, d, g, y, c, r, n = "\x1b[1m", "\x1b[2m", "\x1b[0;32m", "\x1b[1;33m", "\x1b[0;36m", "\x1b[0;31m", "\x1b[0m"
	)
	w.expect(w.run("--no-dir"), term.ExitOK, b+"=== rclone cache status ==="+n+"\n"+
		"  "+c+"a"+n+"  cache: 79B / 100B "+g+"(79%)"+n+" · 0 files · max-age "+d+"2h"+n+"\n"+
		"  "+c+"b"+n+"  cache: 80B / 100B "+y+"(80%)"+n+" · 0 files · max-age "+d+"off"+n+" · "+r+"1 errored"+n+"\n"+
		"  "+c+"c"+n+"  cache: 95B / 100B "+r+"(95%)"+n+" · 0 files · max-age "+d+"off"+n+" · "+r+"OUT OF SPACE"+n+"\n"+
		"  "+c+"d"+n+"  "+d+"(no --rc; cannot query)"+n+"\n"+
		"  "+c+"e"+n+"  "+r+"error: parse failed"+n+"\n")
	w.env["NO_COLOR"] = "1"
	code := w.run("--no-dir")
	if code != term.ExitOK || strings.Contains(w.out.String(), "\x1b") {
		t.Fatalf("NO_COLOR: %d %q", code, w.out.String())
	}
}

// E12: why an RC could not be asked.
func TestRCErrors(t *testing.T) {
	w := newWorld(t)
	w.mount("12", "gdrive:", "gdrive", "--rc-addr=localhost:5572")
	w.fake.On([]string{"rclone", "rc"}, execx.Result{ExitCode: 1, Stderr: []byte("Failed to rc: HTTP error 401: Unauthorized\n")})
	w.expect(w.run("gdrive"), term.ExitOK, "=== rclone cache status ===\n  gdrive  error: rc rejected credentials (401)\n")
	if w.errOut.String() != "rclone-cache-status: gdrive: Failed to rc: HTTP error 401: Unauthorized\n" {
		t.Fatalf("%q", w.errOut.String())
	}
	w.fake.On([]string{"rclone", "rc"}, execx.Result{ExitCode: 1, Stderr: []byte("connection refused\n")})
	w.expect(w.run("gdrive"), term.ExitOK, "=== rclone cache status ===\n  gdrive  error: rc unreachable\n")
	if w.errOut.String() != "rclone-cache-status: gdrive: connection refused\n" {
		t.Fatalf("%q", w.errOut.String())
	}
	w.fake.OnFunc([]string{"rclone", "rc"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{}, errors.New("exec: no such file")
	})
	w.expect(w.run("gdrive"), term.ExitOK, "=== rclone cache status ===\n  gdrive  error: rc unreachable\n")
	if w.errOut.String() != "rclone-cache-status: gdrive: exec: no such file\n" {
		t.Fatalf("%q", w.errOut.String())
	}
	if err := os.Remove(filepath.Join(w.home, ".config", "rclone", "rc-auth.env")); err != nil {
		t.Fatal(err)
	}
	w.expect(w.run("gdrive"), term.ExitOK, "=== rclone cache status ===\n  gdrive  error: rc credential missing — run play-rclone.yml\n")
	if !strings.Contains(w.errOut.String(), "rclone RC credential file not found") {
		t.Fatalf("%q", w.errOut.String())
	}
}

// E4, E6, E8, E16-E18: a directory target analysed against its cache mirror.
func TestDirectory(t *testing.T) {
	w := newWorld(t)
	w.mount("12", "gdrive:", "gdrive", "--rc-addr=localhost:5572")
	mnt := w.mount("13", "photos:/Camera", "photos", "--rc-addr=localhost:5573", "--cache-dir="+filepath.Join(w.root, "cachedir"))
	w.stats("localhost:5573", `{"diskCache":{"bytesUsed":2048,"files":1},"opt":{"CacheMaxSize":4096,"CacheMaxAge":3600000000000}}`)
	w.write(filepath.Join(mnt, "2026", "a.raw"), strings.Repeat("x", 3000))
	w.write(filepath.Join(mnt, "2026", "b.raw"), strings.Repeat("x", 1000))
	w.write(filepath.Join(w.root, "cachedir", "vfs", "photos", "Camera", "2026", "a.raw"), strings.Repeat("x", 2048))
	// A relative, symlinked path reaches the same directory.
	if err := os.Symlink(filepath.Join(mnt, "2026"), filepath.Join(w.home, "year")); err != nil {
		t.Fatal(err)
	}
	w.cwd = w.home
	w.expect(w.run("year"), term.ExitOK, "=== rclone cache status ===\n"+
		"  photos  cache: 2.0KB / 4.0KB (50%) · 1 files · max-age 1h\n"+
		"    └─ scanning "+mnt+"/2026 ...\n"+
		"    └─ in "+mnt+"/2026\n"+
		"       2 files · 4.0KB total\n"+
		"       cached:   1 files (50%)  ·  2.0KB (51%)\n"+
		"       uncached: 1 files (50%)  ·  2.0KB (49%)\n"+
		"       top uncached:\n"+
		"            1000B  2026/b.raw\n")
}

// E5, E18: the working directory inside a mount is analysed; the default cache directory;
// an empty directory; --no-dir turns it off.
func TestWorkingDirectory(t *testing.T) {
	w := newWorld(t)
	mnt := w.mount("12", "gdrive:", "gdrive", "--rc-addr=localhost:5572")
	w.stats("localhost:5572", statsJSON)
	w.cwd = mnt
	w.expect(w.run(), term.ExitOK, "=== rclone cache status ===\n"+
		"  gdrive  cache: 400GB / 400GB (100%) · 36176 files · max-age 365d\n"+
		"    └─ scanning "+mnt+" ...\n"+
		"    └─ in "+mnt+"\n"+
		"       (no files)\n")
	w.write(filepath.Join(mnt, "doc.txt"), "hello")
	w.write(filepath.Join(w.home, ".cache", "rclone", "vfs", "gdrive", "doc.txt"), "hello")
	w.expect(w.run(), term.ExitOK, "=== rclone cache status ===\n"+
		"  gdrive  cache: 400GB / 400GB (100%) · 36176 files · max-age 365d\n"+
		"    └─ scanning "+mnt+" ...\n"+
		"    └─ in "+mnt+"\n"+
		"       1 files · 5B total\n"+
		"       cached:   1 files (100%)  ·  5B (100%)\n"+
		"       uncached: 0 files (0%)  ·  0B (0%)\n")
	w.expect(w.run("--no-dir"), term.ExitOK, "=== rclone cache status ===\n"+
		"  gdrive  cache: 400GB / 400GB (100%) · 36176 files · max-age 365d\n")
	// Outside every mount, the summary covers them all.
	w.cwd = w.home
	w.expect(w.run(), term.ExitOK, "=== rclone cache status ===\n"+
		"  gdrive  cache: 400GB / 400GB (100%) · 36176 files · max-age 365d\n")
	w.cwd = "/nonexistent-for-the-test"
	w.expect(w.run(), term.ExitOK, "=== rclone cache status ===\n"+
		"  gdrive  cache: 400GB / 400GB (100%) · 36176 files · max-age 365d\n")
}

// E5: a working directory that cannot be read is named.
func TestWorkingDirectoryUnknown(t *testing.T) {
	w := newWorld(t)
	w.mount("12", "gdrive:", "gdrive", "--rc-addr=localhost:5572")
	app := App{Env: Env{
		Runner: w.fake, Getenv: func(k string) string { return w.env[k] },
		Getwd:    func() (string, error) { return "", errors.New("getwd: gone") },
		LookPath: func(string) (string, error) { return "/usr/bin/rclone", nil },
		Proc:     w.proc, Stdout: &w.out, Stderr: &w.errOut,
	}}
	w.expect(app.Run(context.Background(), nil), term.ExitFailure, "")
	if !strings.Contains(w.errOut.String(), "cannot read the working directory: getwd: gone") {
		t.Fatalf("%q", w.errOut.String())
	}
}

// E6: a directory inside no mount; nested mounts choose the deepest.
func TestQueryPathMounts(t *testing.T) {
	w := newWorld(t)
	outer := w.mount("12", "outer:", "outer")
	inner := w.mount("13", "inner:", "outer/inner")
	w.expect(w.run(w.home), term.ExitFailure, "")
	if w.errOut.String() != "rclone-cache-status: "+w.home+" is not inside any rclone mount\n" {
		t.Fatalf("%q", w.errOut.String())
	}
	w.expect(w.run(inner), term.ExitOK, "=== rclone cache status ===\n"+
		"  inner  (no --rc; cannot query)\n"+
		"    └─ scanning "+inner+" ...\n"+
		"    └─ in "+inner+"\n"+
		"       (no files)\n")
	if _, err := os.Stat(outer); err != nil {
		t.Fatal(err)
	}
}

// E19: an unreadable entry is named, the analysis says it is incomplete, the run exits 1.
func TestDirectoryIncomplete(t *testing.T) {
	w := newWorld(t)
	mnt := w.mount("12", "gdrive:", "gdrive")
	w.write(filepath.Join(mnt, "a"), "abc")
	if err := os.MkdirAll(filepath.Join(w.home, ".cache", "rclone", "vfs", "gdrive"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a", filepath.Join(w.home, ".cache", "rclone", "vfs", "gdrive", "a")); err != nil {
		t.Fatal(err)
	}
	w.expect(w.run(mnt), term.ExitFailure, "=== rclone cache status ===\n"+
		"  gdrive  (no --rc; cannot query)\n"+
		"    └─ scanning "+mnt+" ...\n"+
		"    └─ in "+mnt+"\n"+
		"       1 files · 3B total\n"+
		"       cached:   0 files (0%)  ·  0B (0%)\n"+
		"       uncached: 1 files (100%)  ·  3B (100%)\n"+
		"       incomplete: 1 entry could not be read\n")
	if !strings.HasPrefix(w.errOut.String(), "rclone-cache-status: cannot read ") {
		t.Fatalf("%q", w.errOut.String())
	}
}

// E19: the mount's target vanishing under the walk claims no counts.
func TestDirectoryGone(t *testing.T) {
	w := newWorld(t)
	w.mounts = []map[string]string{{"source": "gdrive:", "target": filepath.Join(w.root, "gone")}}
	w.cwd = filepath.Join(w.root, "gone", "sub")
	w.expect(w.run(), term.ExitFailure, "=== rclone cache status ===\n"+
		"  gdrive  (no --rc; cannot query)\n"+
		"    └─ scanning "+w.cwd+" ...\n"+
		"    └─ in "+w.cwd+"\n"+
		"       incomplete: 1 entry could not be read\n")
}

// E21: a report nobody could read is a failure.
func TestStdoutWriteFailure(t *testing.T) {
	w := newWorld(t)
	app := App{Env: Env{
		Runner: w.fake, Getenv: func(k string) string { return w.env[k] },
		Getwd:    func() (string, error) { return w.home, nil },
		LookPath: func(string) (string, error) { return "/usr/bin/rclone", nil },
		Proc:     w.proc, Stdout: failingWriter{}, Stderr: &w.errOut,
	}}
	if code := app.Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.errOut.String(), "cannot write the report: ") {
		t.Fatalf("%q", w.errOut.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestNewEnv(t *testing.T) {
	env := NewEnv()
	if env.Runner == nil || env.Getenv == nil || env.Getwd == nil || env.LookPath == nil || env.Proc == nil ||
		env.Stdout == nil || env.Stderr == nil {
		t.Fatalf("%+v", env)
	}
}
