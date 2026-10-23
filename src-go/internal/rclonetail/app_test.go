package rclonetail

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
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// world is a run's surroundings: a temporary home with the RC credential, a fake runner
// answering findmnt and rclone, /proc as a map, a clock that moves only when the run waits (or a
// test moves it), and a stop the test pulls by hand.
type world struct {
	t        *testing.T
	root     string
	home     string
	fake     *execx.Fake
	proc     fstest.MapFS
	mounts   []map[string]string
	env      map[string]string
	tty      bool
	noRC     bool
	now      time.Time
	waits    []time.Duration
	stopAt   int // the wait (1-based) at which the test stops the run; 0 never
	stop     func()
	released bool
	out      bytes.Buffer
	errOut   bytes.Buffer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &world{
		t: t, root: root, home: filepath.Join(root, "home"), fake: &execx.Fake{}, proc: fstest.MapFS{},
		now: time.Date(2026, 10, 23, 14, 5, 9, 0, time.UTC),
	}
	w.env = map[string]string{"HOME": w.home}
	dir := filepath.Join(w.home, ".config", "rclone")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rc-auth.env"), []byte("RCLONE_RC_USER=u\nRCLONE_RC_PASS=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.fake.OnFunc([]string{"findmnt"}, func(execx.Cmd) (execx.Result, error) {
		if len(w.mounts) == 0 {
			return execx.Result{ExitCode: 1}, nil
		}
		data, err := json.Marshal(map[string]any{"filesystems": w.mounts})
		return execx.Result{Stdout: data}, err
	})
	return w
}

// mount adds an rclone mount of source at /mnt/<dir>, served by a process with the given extra
// arguments (pid "" is no process).
func (w *world) mount(pid, source, dir string, extra ...string) {
	target := "/mnt/" + dir
	w.mounts = append(w.mounts, map[string]string{"source": source, "target": target})
	if pid != "" {
		args := append([]string{"/usr/bin/rclone", "mount", source, target}, extra...)
		w.proc[pid+"/cmdline"] = &fstest.MapFile{Data: []byte(strings.Join(args, "\x00") + "\x00")}
	}
}

func (w *world) answer(addr, endpoint, body string) {
	w.fake.On([]string{"rclone", "rc", "--url=http://" + addr, endpoint}, execx.Result{Stdout: []byte(body)})
}

// active answers both endpoints of addr: queued, in flight, and core/stats' bytes.
func (w *world) active(addr string, queued, inProgress, errored int, core string) {
	w.answer(addr, "vfs/stats", `{"diskCache":{"uploadsQueued":`+itoa(queued)+`,"uploadsInProgress":`+
		itoa(inProgress)+`,"erroredFiles":`+itoa(errored)+`,"bytesUsed":99}}`)
	w.answer(addr, "core/stats", core)
}

func itoa(n int) string {
	data, err := json.Marshal(n)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func (w *world) newEnv() Env {
	return Env{
		Runner: w.fake,
		Getenv: func(k string) string { return w.env[k] },
		Getwd:  func() (string, error) { return w.home, nil },
		LookPath: func(name string) (string, error) {
			if w.noRC && name == "rclone" {
				return "", exec.ErrNotFound
			}
			return "/usr/bin/" + name, nil
		},
		Proc: w.proc, Stdout: &w.out, Stderr: &w.errOut, StdoutIsTerminal: w.tty,
		Now: func() time.Time { return w.now },
		After: func(d time.Duration) <-chan time.Time {
			w.waits = append(w.waits, d)
			if len(w.waits) == w.stopAt {
				w.stop()
				return make(chan time.Time) // never fires: the stop ends the wait
			}
			w.now = w.now.Add(d)
			ch := make(chan time.Time, 1)
			ch <- w.now
			return ch
		},
		OnStop: func(stop func()) func() {
			w.stop = stop
			return func() { w.released = true }
		},
	}
}

func (w *world) run(args ...string) term.ExitCode {
	w.t.Helper()
	w.out.Reset()
	w.errOut.Reset()
	w.waits = nil
	return App{Env: w.newEnv()}.Run(context.Background(), args)
}

func (w *world) expect(code, want term.ExitCode, wantOut string) {
	w.t.Helper()
	if code != want || w.out.String() != wantOut {
		w.t.Fatalf("exit %d (want %d)\nstdout:\n%s\nwant:\n%s\nstderr:\n%s", code, want, w.out.String(), wantOut, w.errOut.String())
	}
}

func (w *world) expectErr(want string) {
	w.t.Helper()
	if w.errOut.String() != want {
		w.t.Fatalf("stderr %q, want %q", w.errOut.String(), want)
	}
}

const heading = "=== 14:05:09 ===\n"

// E1: the usage on stdout, before anything runs.
func TestHelp(t *testing.T) {
	w := newWorld(t)
	w.noRC = true
	code := w.run("--bogus", "-h")
	if code != term.ExitOK || !strings.HasPrefix(w.out.String(), "Usage: rclone-tail [--interval N] [--once] [MOUNT_NAME]\n") ||
		len(w.fake.Calls()) != 0 || w.stop != nil {
		t.Fatalf("%d %q %v", code, w.out.String(), w.fake.Calls())
	}
}

// E2: a usage error, before anything runs.
func TestUsageError(t *testing.T) {
	w := newWorld(t)
	w.expect(w.run("-i", "0"), term.ExitUsage, "")
	w.expectErr("rclone-tail: --interval must be a whole number of seconds from 1 to 86400 (got '0') — see 'rclone-tail --help'.\n")
	if len(w.fake.Calls()) != 0 {
		t.Fatalf("%v", w.fake.Calls())
	}
}

// E3: rclone missing, before any output.
func TestRcloneMissing(t *testing.T) {
	w := newWorld(t)
	w.noRC = true
	w.expect(w.run("--once"), term.ExitFailure, "")
	w.expectErr("rclone-tail: rclone is not installed; it is installed by play-rclone.yml.\n")
}

// E5, E7, E8, E11, E12, E13: one tick over every kind of mount line, plain; no snapshot.
func TestOnce(t *testing.T) {
	w := newWorld(t)
	w.mount("12", "lib:/Photos", "lib", "--rc", "--rc-addr=h:1")
	w.mount("13", "docs:", "docs", "--rc-addr=h:2")
	w.mount("14", "old:", "old", "--rc-addr=h:3")
	w.mount("", "nopid:", "nopid")
	w.mount("15", "norc:", "norc")
	w.mount("16", "two:", "two", "--rc-addr=a:1", "--rc-addr=b:2")
	w.active("h:1", 3, 2, 1, `{"bytes":1536,"totalBytes":10485760,"transfers":4,"totalTransfers":9,"eta":3665}`)
	w.active("h:2", 0, 0, 0, `{"bytes":0,"totalBytes":0,"transfers":0,"totalTransfers":0,"eta":null}`)
	w.active("h:3", 0, 0, 2, `{}`)
	w.expect(w.run("--once"), term.ExitOK, heading+
		"  lib  queued: 3 · active: 2 · done: 4/9 (1.5KB/10MB) · 0B/s · ETA 61m05s · 1 errored\n"+
		"  docs  idle\n"+
		"  old  idle (2 error(s))\n"+
		"  nopid  (no --rc; cannot query)\n"+
		"  norc  (no --rc; cannot query)\n"+
		"  two  error: more than one --rc-addr: a:1 b:2\n")
	w.expectErr("")
	for _, c := range w.fake.Calls() {
		if strings.Contains(c.Line(), "secret") {
			t.Fatalf("the password is in argv: %q", c.Line())
		}
	}
	if len(w.waits) != 0 || !w.released {
		t.Fatalf("waits %v released %v", w.waits, w.released)
	}
}

// E4: a mount name shows only that name's mounts.
func TestFilter(t *testing.T) {
	w := newWorld(t)
	w.mount("12", "lib:", "lib", "--rc-addr=h:1")
	w.mount("13", "docs:", "docs", "--rc-addr=h:2")
	w.active("h:2", 0, 0, 0, `{}`)
	w.expect(w.run("-o", "docs"), term.ExitOK, heading+"  docs  idle\n")
}

// E8: why an RC could not be asked; core/stats is not asked when vfs/stats failed.
func TestRCErrors(t *testing.T) {
	cases := []struct {
		name       string
		vfs, core  execx.Result
		line, diag string
		coreAsked  bool
	}{
		{
			"401",
			execx.Result{ExitCode: 1, Stderr: []byte("HTTP error 401: Unauthorized\n")},
			execx.Result{},
			"error: rc rejected credentials (401)", "rclone-tail: lib: HTTP error 401: Unauthorized\n", false,
		},
		{
			"unreachable",
			execx.Result{ExitCode: 1, Stderr: []byte("connection refused\n")},
			execx.Result{},
			"error: rc unreachable", "rclone-tail: lib: connection refused\n", false,
		},
		{
			"vfs parse",
			execx.Result{Stdout: []byte("[]")},
			execx.Result{},
			"error: parse failed", "rclone-tail: lib: vfs/stats: ", false,
		},
		{
			"core unreachable",
			execx.Result{Stdout: []byte("{}")},
			execx.Result{ExitCode: 1, Stderr: []byte("timeout\n")},
			"error: rc unreachable", "rclone-tail: lib: timeout\n", true,
		},
		{
			"core parse",
			execx.Result{Stdout: []byte("{}")},
			execx.Result{Stdout: []byte(`{"bytes":"x"}`)},
			"error: parse failed", "rclone-tail: lib: core/stats: ", true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.mount("12", "lib:", "lib", "--rc-addr=h:1")
			w.fake.On([]string{"rclone", "rc", "--url=http://h:1", "vfs/stats"}, tc.vfs)
			w.fake.On([]string{"rclone", "rc", "--url=http://h:1", "core/stats"}, tc.core)
			w.expect(w.run("--once"), term.ExitOK, heading+"  lib  "+tc.line+"\n")
			if !strings.HasPrefix(w.errOut.String(), tc.diag) {
				t.Fatalf("stderr %q, want %q", w.errOut.String(), tc.diag)
			}
			if got := w.fake.Called("rclone rc --url=http://h:1 core/stats"); got != tc.coreAsked {
				t.Fatalf("core/stats asked: %v", got)
			}
		})
	}
}

// E8: no credential is its reason, the advice on stderr.
func TestCredentialMissing(t *testing.T) {
	w := newWorld(t)
	w.mount("12", "lib:", "lib", "--rc-addr=h:1")
	if err := os.Remove(filepath.Join(w.home, ".config", "rclone", "rc-auth.env")); err != nil {
		t.Fatal(err)
	}
	w.expect(w.run("--once"), term.ExitOK, heading+"  lib  error: rc credential missing — run play-rclone.yml\n")
	if !strings.HasPrefix(w.errOut.String(), "rclone-tail: lib: rclone RC credential file not found") {
		t.Fatalf("%q", w.errOut.String())
	}
}

// E6: nothing to show is a diagnostic and exit 1.
func TestNoMount(t *testing.T) {
	w := newWorld(t)
	w.expect(w.run("--once"), term.ExitFailure, "")
	w.expectErr("rclone-tail: No rclone mounts found.\n")
	w.mount("12", "lib:", "lib", "--rc-addr=h:1")
	w.expect(w.run("--once", "photos"), term.ExitFailure, "")
	w.expectErr("rclone-tail: No rclone mount matching 'photos' found.\n")
}

// E4: findmnt failing is not "no mount".
func TestFindmntFailure(t *testing.T) {
	w := newWorld(t)
	w.fake.On([]string{"findmnt"}, execx.Result{ExitCode: 2, Stderr: []byte("findmnt: bad usage\n")})
	w.expect(w.run("--once"), term.ExitFailure, "")
	if !strings.HasPrefix(w.errOut.String(), "rclone-tail: findmnt could not list the rclone mounts: ") {
		t.Fatalf("%q", w.errOut.String())
	}
}

// coreSeries answers core/stats of addr with each body in turn, then the last again; each call
// also moves the clock by took, as a slow answer would.
func (w *world) coreSeries(addr string, took time.Duration, bodies ...string) {
	n := 0
	w.fake.OnFunc([]string{"rclone", "rc", "--url=http://" + addr, "core/stats"}, func(execx.Cmd) (execx.Result, error) {
		w.now = w.now.Add(took)
		body := bodies[min(n, len(bodies)-1)]
		n++
		return execx.Result{Stdout: []byte(body)}, nil
	})
}

const snapshot = "=== rclone cache status ===\n  lib  cache: 99B / ∞ · 0 files · max-age off · queued: 1\n"

// E10, E14, E15: the opening snapshot, ticks an interval apart with the rate between them, and
// the stop during a wait: an empty line and the closing snapshot, exit 0.
func TestStream(t *testing.T) {
	w := newWorld(t)
	w.mount("12", "lib:", "lib", "--rc-addr=h:1")
	w.answer("h:1", "vfs/stats", `{"diskCache":{"uploadsQueued":1,"bytesUsed":99}}`)
	w.coreSeries("h:1", 0, `{"bytes":1000}`, `{"bytes":8680}`, `{"bytes":8680}`)
	w.stopAt = 3
	w.expect(w.run("-i", "5"), term.ExitOK, snapshot+"\n"+
		"=== 14:05:09 ===\n  lib  queued: 1 · active: 0 · done: 0/0 (1000B/0B) · 0B/s · ETA —\n"+
		"=== 14:05:14 ===\n  lib  queued: 1 · active: 0 · done: 0/0 (8.5KB/0B) · 1.5KB/s · ETA —\n"+
		"=== 14:05:19 ===\n  lib  queued: 1 · active: 0 · done: 0/0 (8.5KB/0B) · 0B/s · ETA —\n"+
		"\n"+snapshot)
	if len(w.waits) != 3 || w.waits[0] != 5*time.Second || !w.released {
		t.Fatalf("waits %v released %v", w.waits, w.released)
	}
}

// E10: the elapsed time is fractional, a count that fell is 0, and the default interval is 5s.
func TestRate(t *testing.T) {
	w := newWorld(t)
	w.mount("12", "lib:", "lib", "--rc-addr=h:1")
	w.answer("h:1", "vfs/stats", `{"diskCache":{"uploadsQueued":1,"bytesUsed":99}}`)
	w.coreSeries("h:1", 500*time.Millisecond, `{"bytes":1000}`, `{"bytes":12000}`, `{"bytes":10}`)
	w.stopAt = 3
	code := w.run()
	lines := strings.Split(w.out.String(), "\n")
	var rates []string
	for _, l := range lines {
		if _, rest, ok := strings.Cut(l, ") · "); ok {
			rates = append(rates, strings.Fields(rest)[0])
		}
	}
	// 11000 bytes over 5.5 seconds is 2000 B/s; then the count fell.
	if code != term.ExitOK || strings.Join(rates, " ") != "0B/s 2.0KB/s 0B/s" || w.waits[0] != 5*time.Second {
		t.Fatalf("%d %q %v\n%s", code, rates, w.waits, w.out.String())
	}
}

// E10: two mounts of one remote keep their own counters (B6).
func TestRateByTarget(t *testing.T) {
	w := newWorld(t)
	w.mount("12", "lib:", "lib-a", "--rc-addr=h:1")
	w.mount("13", "lib:", "lib-b", "--rc-addr=h:2")
	for _, addr := range []string{"h:1", "h:2"} {
		w.answer(addr, "vfs/stats", `{"diskCache":{"uploadsQueued":1}}`)
	}
	w.coreSeries("h:1", 0, `{"bytes":0}`, `{"bytes":5120}`)
	w.coreSeries("h:2", 0, `{"bytes":100000}`, `{"bytes":100000}`)
	w.stopAt = 2
	w.run("--interval", "1")
	want := "=== 14:05:10 ===\n" +
		"  lib  queued: 1 · active: 0 · done: 0/0 (5.0KB/0B) · 5.0KB/s · ETA —\n" +
		"  lib  queued: 1 · active: 0 · done: 0/0 (98KB/0B) · 0B/s · ETA —\n"
	if !strings.Contains(w.out.String(), want) {
		t.Fatalf("%s", w.out.String())
	}
}

// E15: a stop during a tick ends it at once; the call it cancelled is neither printed nor
// diagnosed; the closing snapshot follows. --once too.
func TestStopDuringTick(t *testing.T) {
	for _, args := range [][]string{{"--once"}, {}} {
		w := newWorld(t)
		w.mount("12", "lib:", "lib", "--rc-addr=h:1")
		w.mount("13", "docs:", "docs", "--rc-addr=h:2")
		w.answer("h:1", "vfs/stats", `{"diskCache":{"uploadsQueued":1,"bytesUsed":99}}`)
		w.answer("h:2", "vfs/stats", `{"diskCache":{"uploadsQueued":1,"bytesUsed":99}}`)
		w.answer("h:2", "core/stats", `{}`)
		calls := 0
		w.fake.OnFunc([]string{"rclone", "rc", "--url=http://h:1", "core/stats"}, func(execx.Cmd) (execx.Result, error) {
			calls++
			w.stop()
			return execx.Result{}, errors.New("signal: interrupt")
		})
		code := w.run(args...)
		closing := "=== rclone cache status ===\n" +
			"  lib  cache: 99B / ∞ · 0 files · max-age off · queued: 1\n" +
			"  docs  cache: 99B / ∞ · 0 files · max-age off · queued: 1\n"
		want := heading + "\n" + closing
		if len(args) == 0 {
			want = closing + "\n" + want
		}
		w.expect(code, term.ExitOK, want)
		w.expectErr("")
		if calls != 1 || w.fake.Called("rclone rc --url=http://h:2 core/stats") || !w.released {
			t.Fatalf("calls %d, released %v, %v", calls, w.released, w.fake.Calls())
		}
	}
}

// E15: a stop after a mount's last call ends the tick before the next line, and --once still
// gets its closing snapshot; every command runs in a session of its own (D5).
func TestStopAfterLastCall(t *testing.T) {
	w := newWorld(t)
	w.mount("12", "lib:", "lib", "--rc-addr=h:1")
	w.mount("13", "norc:", "norc")
	w.answer("h:1", "vfs/stats", `{"diskCache":{"uploadsQueued":1,"bytesUsed":99}}`)
	w.fake.OnFunc([]string{"rclone", "rc", "--url=http://h:1", "core/stats"}, func(execx.Cmd) (execx.Result, error) {
		w.stop()
		return execx.Result{Stdout: []byte("{}")}, nil
	})
	closing := "=== rclone cache status ===\n" +
		"  lib  cache: 99B / ∞ · 0 files · max-age off · queued: 1\n  norc  (no --rc; cannot query)\n"
	w.expect(w.run("--once"), term.ExitOK, heading+"\n"+closing)
	for _, c := range w.fake.Calls() {
		if !c.NewSession {
			t.Fatalf("%q runs in the terminal's session", c.Line())
		}
	}
}

// E15: a stop during the opening snapshot lets it finish; the closing one follows with no tick,
// and the signals are released before it starts.
func TestStopDuringOpeningSnapshot(t *testing.T) {
	w := newWorld(t)
	w.mount("12", "lib:", "lib", "--rc-addr=h:1")
	calls := 0
	w.fake.OnFunc([]string{"rclone", "rc", "--url=http://h:1", "vfs/stats"}, func(execx.Cmd) (execx.Result, error) {
		calls++
		if calls == 1 {
			w.stop()
		} else if !w.released {
			t.Error("the closing snapshot ran with the signals still caught")
		}
		return execx.Result{Stdout: []byte(`{"diskCache":{"uploadsQueued":1,"bytesUsed":99}}`)}, nil
	})
	w.expect(w.run(), term.ExitOK, snapshot+"\n"+snapshot)
	if calls != 2 || w.fake.Called("rclone rc --url=http://h:1 core/stats") {
		t.Fatalf("calls %d %v", calls, w.fake.Calls())
	}
}

// E14: an opening snapshot that fails ends the run with its code, before any tick.
func TestOpeningSnapshotFails(t *testing.T) {
	w := newWorld(t)
	w.fake.On([]string{"findmnt"}, execx.Result{ExitCode: 2})
	w.expect(w.run(), term.ExitFailure, "")
	if !strings.HasPrefix(w.errOut.String(), "rclone-cache-status: findmnt could not list the rclone mounts: ") ||
		strings.Contains(w.errOut.String(), "rclone-tail:") || len(w.waits) != 0 {
		t.Fatalf("%q %v", w.errOut.String(), w.waits)
	}
}

// D2, E14: the snapshot is the summary of the named mount alone, never a directory walk.
func TestSnapshotNamesTheMount(t *testing.T) {
	w := newWorld(t)
	w.mount("12", "lib:", "lib", "--rc-addr=h:1")
	w.mount("13", "docs:", "docs")
	w.answer("h:1", "vfs/stats", `{"diskCache":{"uploadsQueued":1,"bytesUsed":99}}`)
	w.answer("h:1", "core/stats", `{}`)
	if err := os.MkdirAll(filepath.Join(w.home, "lib"), 0o750); err != nil {
		t.Fatal(err)
	}
	w.stopAt = 1
	w.expect(w.run("lib"), term.ExitOK, snapshot+"\n"+
		heading+"  lib  queued: 1 · active: 0 · done: 0/0 (0B/0B) · 0B/s · ETA —\n\n"+snapshot)
}

// E6: a mount gone at a later tick ends the stream with exit 1 and no closing snapshot.
func TestMountGoneLater(t *testing.T) {
	w := newWorld(t)
	w.mount("12", "lib:", "lib", "--rc-addr=h:1")
	w.answer("h:1", "vfs/stats", `{"diskCache":{"uploadsQueued":1,"bytesUsed":99}}`)
	w.fake.OnFunc([]string{"rclone", "rc", "--url=http://h:1", "core/stats"}, func(execx.Cmd) (execx.Result, error) {
		w.mounts = nil
		return execx.Result{Stdout: []byte("{}")}, nil
	})
	w.expect(w.run(), term.ExitFailure, snapshot+"\n"+
		heading+"  lib  queued: 1 · active: 0 · done: 0/0 (0B/0B) · 0B/s · ETA —\n")
	w.expectErr("rclone-tail: No rclone mounts found.\n")
}

// E16: colour on a terminal only, never with NO_COLOR.
func TestColour(t *testing.T) {
	w := newWorld(t)
	w.tty = true
	w.mount("12", "lib:", "lib", "--rc-addr=h:1")
	w.mount("13", "docs:", "docs", "--rc-addr=h:2")
	w.mount("14", "norc:", "norc")
	w.mount("15", "bad:", "bad", "--rc-addr=h:4")
	w.active("h:1", 1, 2, 3, `{"bytes":5,"totalBytes":6,"transfers":1,"totalTransfers":2,"eta":5}`)
	w.active("h:2", 0, 0, 1, `{}`)
	w.answer("h:4", "vfs/stats", "[]")
	const b, d, g, y, c, r, n = "\x1b[1m", "\x1b[2m", "\x1b[0;32m", "\x1b[1;33m", "\x1b[0;36m", "\x1b[0;31m", "\x1b[0m"
	w.expect(w.run("--once"), term.ExitOK, b+"=== 14:05:09 ==="+n+"\n"+
		"  "+c+"lib"+n+"  queued: "+c+"1"+n+" · active: "+c+"2"+n+" · done: "+c+"1/2"+n+" (5B/6B) · "+
		g+"0B/s"+n+" · ETA "+y+"0m05s"+n+" · "+r+"3 errored"+n+"\n"+
		"  "+c+"docs"+n+"  "+d+"idle"+n+" "+r+"(1 error(s))"+n+"\n"+
		"  "+c+"norc"+n+"  "+d+"(no --rc; cannot query)"+n+"\n"+
		"  "+c+"bad"+n+"  "+r+"error: parse failed"+n+"\n")
	w.env["NO_COLOR"] = "1"
	if code := w.run("--once"); code != term.ExitOK || strings.Contains(w.out.String(), "\x1b") {
		t.Fatalf("NO_COLOR: %d %q", code, w.out.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// E17: a stdout nobody can read ends the run with exit 1, at the tick that failed.
func TestStdoutWriteFailure(t *testing.T) {
	w := newWorld(t)
	w.mount("12", "lib:", "lib", "--rc-addr=h:1")
	w.mount("13", "docs:", "docs", "--rc-addr=h:2")
	w.active("h:1", 0, 0, 0, `{}`)
	w.active("h:2", 0, 0, 0, `{}`)
	env := w.newEnv()
	env.Stdout = failingWriter{}
	if code := (App{Env: env}).Run(context.Background(), []string{"--once"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if w.errOut.String() != "rclone-tail: cannot write the report: closed\n" ||
		w.fake.Called("rclone rc --url=http://h:2 vfs/stats") {
		t.Fatalf("%q %v", w.errOut.String(), w.fake.Calls())
	}
}

func TestNewEnv(t *testing.T) {
	env := NewEnv()
	if env.Runner == nil || env.Getenv == nil || env.Getwd == nil || env.LookPath == nil || env.Proc == nil ||
		env.Stdout == nil || env.Stderr == nil || env.Now == nil || env.After == nil || env.OnStop == nil {
		t.Fatalf("%+v", env)
	}
}
