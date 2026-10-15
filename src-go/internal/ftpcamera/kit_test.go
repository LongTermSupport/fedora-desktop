package ftpcamera

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// kit is one ftp-camera run on a fake machine: a recording runner that answers every program
// it runs, an upload tree under a temporary directory, a config file path, an environment, a
// PATH, and a clock that moves only when the tool sleeps.
type kit struct {
	t        *testing.T
	fake     *execx.Fake
	out, err bytes.Buffer
	env      map[string]string
	onPath   map[string]bool
	upload   string
	config   string
	stdin    string
	stdinTTY bool
	colour   bool
	proc     fstest.MapFS

	mu    sync.Mutex
	now   time.Time
	slept []time.Duration
	// onSleep, when set, runs at each sleep (a test's interrupt arrives there).
	onSleep func(time.Duration)
	// feeds are the long-running programs (tail -F, inotifywait): what they print, and whether
	// they then run until they are stopped.
	feeds []feed
}

// feed is a long-running program's script.
type feed struct {
	prefix []string
	lines  []string
	hold   bool
}

// feedOn scripts a long-running program: it prints lines, then ends, or with hold runs until
// its context ends. The fake records the call.
func (k *kit) feedOn(hold bool, prefix []string, lines ...string) {
	k.fake.On(prefix, execx.Result{})
	k.feeds = append(k.feeds, feed{prefix: prefix, lines: lines, hold: hold})
}

// kitRunner is the fake runner with the feeds in front of it: a feed blocks without holding
// the fake's lock, so the other goroutines' commands still run.
type kitRunner struct{ k *kit }

func (r kitRunner) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	argv := append([]string{c.Name}, c.Args...)
	for _, f := range r.k.feeds {
		if len(argv) < len(f.prefix) || !slicesEqual(argv[:len(f.prefix)], f.prefix) {
			continue
		}
		res, err := r.k.fake.Run(ctx, c)
		if err != nil {
			return res, err
		}
		for _, l := range f.lines {
			if _, err := io.WriteString(c.Stdout, l+"\n"); err != nil {
				return res, err
			}
		}
		if f.hold {
			<-ctx.Done()
			return res, ctx.Err()
		}
		return res, nil
	}
	return r.k.fake.Run(ctx, c)
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// testStart is the fixed moment every run starts at.
var testStart = time.Date(2026, 10, 15, 9, 30, 0, 0, time.UTC)

func newKit(t *testing.T) *kit {
	t.Helper()
	base := t.TempDir()
	k := &kit{
		t:      t,
		fake:   &execx.Fake{},
		env:    map[string]string{"HOME": filepath.Join(base, "home")},
		onPath: map[string]bool{},
		upload: filepath.Join(base, "srv", "ftp-camera"),
		config: filepath.Join(base, "etc", "config"),
		now:    testStart,
		proc:   fstest.MapFS{},
	}
	if err := os.MkdirAll(k.upload, 0o755); err != nil {
		t.Fatal(err)
	}
	k.fake.On([]string{"getent", "passwd", "camera"}, execx.Result{Stdout: []byte("camera:x:1001:1001:Camera:" + k.upload + ":/sbin/nologin\n")})
	k.fake.On([]string{"sudo", "-n", "-v"}, execx.Result{})
	return k
}

func (k *kit) path(names ...string) {
	for _, n := range names {
		k.onPath[n] = true
	}
}

func (k *kit) writeConfig(text string) { writeFile(k.t, k.config, text) }

func (k *kit) app() *App {
	stdin := strings.NewReader(k.stdin)
	return &App{Env: Env{
		Runner: kitRunner{k: k},
		Execer: k.fake,
		Stdin:  stdin,
		Stdout: &k.out,
		Stderr: &k.err,
		Getenv: func(key string) string { return k.env[key] },
		LookPath: func(name string) (string, error) {
			if k.onPath[name] {
				return "/usr/bin/" + name, nil
			}
			return "", exec.ErrNotFound
		},
		Now:        k.clock,
		Sleep:      k.sleep,
		Self:       "/home/u/.local/bin/ftp-camera",
		PID:        4242,
		Proc:       k.proc,
		IsTerminal: func(v any) bool { return (v == stdin && k.stdinTTY) || (k.colour && v != stdin) },
		OpenTTY:    func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("")), nil },
		ConfigPath: k.config,
	}}
}

func (k *kit) clock() time.Time {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.now
}

func (k *kit) sleep(ctx context.Context, d time.Duration) error {
	k.mu.Lock()
	k.slept = append(k.slept, d)
	k.now = k.now.Add(d)
	hook := k.onSleep
	k.mu.Unlock()
	if hook != nil {
		hook(d)
	}
	return ctx.Err()
}

func (k *kit) run(args ...string) term.ExitCode {
	k.t.Helper()
	return k.runCtx(context.Background(), args...)
}

func (k *kit) runCtx(ctx context.Context, args ...string) term.ExitCode {
	k.t.Helper()
	k.out.Reset()
	k.err.Reset()
	return k.app().Run(ctx, args)
}

// lines is every command run, as space-joined argv.
func (k *kit) lines() []string {
	calls := k.fake.Calls()
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.Line())
	}
	return out
}

func (k *kit) ran(line string) bool { return slices.Contains(k.lines(), line) }

func (k *kit) wantRan(lines ...string) {
	k.t.Helper()
	for _, l := range lines {
		if !k.ran(l) {
			k.t.Errorf("did not run %q; ran:\n%s", l, strings.Join(k.lines(), "\n"))
		}
	}
}

func (k *kit) wantNotRan(prefix string) {
	k.t.Helper()
	for _, l := range k.lines() {
		if strings.HasPrefix(l, prefix) {
			k.t.Errorf("ran %q", l)
		}
	}
}

func (k *kit) wantOut(parts ...string) {
	k.t.Helper()
	for _, p := range parts {
		if !strings.Contains(k.out.String(), p) {
			k.t.Errorf("stdout lacks %q:\n%s", p, k.out.String())
		}
	}
}

func (k *kit) wantErr(parts ...string) {
	k.t.Helper()
	for _, p := range parts {
		if !strings.Contains(k.err.String(), p) {
			k.t.Errorf("stderr lacks %q:\n%s", p, k.err.String())
		}
	}
}

func (k *kit) wantCode(got, want term.ExitCode) {
	k.t.Helper()
	if got != want {
		k.t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, k.out.String(), k.err.String())
	}
}

// put writes a file under the upload directory and gives it a modification time.
func (k *kit) put(rel string, data []byte, mtime time.Time) {
	k.t.Helper()
	path := filepath.Join(k.upload, rel)
	writeFile(k.t, path, string(data))
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		k.t.Fatal(err)
	}
}

// realRoot makes the root mkdir, mv, rm and find do what they say on the temporary tree, so a
// test sees the files land where the tool put them.
func (k *kit) realRoot() {
	k.fake.OnFunc([]string{"sudo", "-n", "mkdir", "-p"}, func(c execx.Cmd) (execx.Result, error) {
		return execx.Result{}, os.MkdirAll(c.Args[len(c.Args)-1], 0o755)
	})
	k.fake.OnFunc([]string{"sudo", "-n", "mv"}, func(c execx.Cmd) (execx.Result, error) {
		src, dir := c.Args[2], c.Args[3]
		return execx.Result{}, os.Rename(src, filepath.Join(dir, filepath.Base(src)))
	})
	k.fake.OnFunc([]string{"sudo", "-n", "rm", "-f"}, func(c execx.Cmd) (execx.Result, error) {
		return execx.Result{}, os.Remove(c.Args[3])
	})
	k.fake.On([]string{"sudo", "-n", "find"}, execx.Result{})
}

// exifDates answers the capture-date question from a map of base name to date.
func (k *kit) exifDates(dates map[string]string) {
	k.fake.OnFunc([]string{"exiftool", "-s3", "-d"}, func(c execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: []byte(dates[filepath.Base(c.Args[len(c.Args)-1])]), ExitCode: 0}, nil
	})
}

// jpegBytes is a complete JPEG as far as the completeness check is concerned.
func jpegBytes() []byte { return []byte{0xFF, 0xD8, 0x01, 0x02, 0xFF, 0xD9} }
