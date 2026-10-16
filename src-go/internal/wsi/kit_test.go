package wsi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// fakeRecording is a recorder the test stops or that stops by itself.
type fakeRecording struct {
	once    sync.Once
	done    chan struct{}
	stopped bool
	mu      sync.Mutex
}

func (f *fakeRecording) Stop() {
	f.mu.Lock()
	f.stopped = true
	f.mu.Unlock()
	f.once.Do(func() { close(f.done) })
}

func (f *fakeRecording) Done() <-chan struct{} { return f.done }

func (f *fakeRecording) Err() error { return nil }

// fakeRecorder writes the audio file when it starts (unless silent) and hands out recordings.
type fakeRecorder struct {
	k       *kit
	started int
	current *fakeRecording
}

func (f *fakeRecorder) Start(_ context.Context, path string, _ io.Writer) Recording {
	f.started++
	if !f.k.silent {
		if err := os.WriteFile(path, []byte("RIFF-stub-audio"), 0o600); err != nil {
			f.k.t.Error(err)
		}
	}
	f.current = &fakeRecording{done: make(chan struct{})}
	if f.k.recorderEndsAtOnce {
		f.current.Stop()
	}
	return f.current
}

// kit is one wsi run's world.
type kit struct {
	t     *testing.T
	run   *execx.Fake
	rec   *fakeRecorder
	env   map[string]string
	unset []string
	out   bytes.Buffer
	errb  bytes.Buffer

	home, tmp, bin, socket string

	missing            []string
	alive              map[int]bool
	free               uint64
	silent             bool
	recorderEndsAtOnce bool

	signals chan os.Signal
	// onState runs when the named state is signalled (the panel's view of the run).
	onState map[string]func()
	// autoFire lists the timer durations that fire as soon as they are set.
	autoFire map[time.Duration]bool
	sleeps   []time.Duration
	// held, when set, receives the prepare delay's channel so a test fires it by hand.
	held chan chan time.Time

	mu        sync.Mutex
	pasteKeys []string // PasteKey replies, the last repeating; none: the method is missing
	asked     int
	copied    map[string]string // clipboard tool line -> stdin
	// staleReads is how many read-backs still see previousDictation: wl-copy returns
	// before its forked child owns the selection.
	staleReads, reads int
}

func newKit(t *testing.T) *kit {
	t.Helper()
	root := t.TempDir()
	k := &kit{
		t: t, run: &execx.Fake{},
		home: filepath.Join(root, "home"), tmp: filepath.Join(root, "shm"), bin: filepath.Join(root, "bin"),
		socket: filepath.Join(root, "ydotool.socket"),
		env: map[string]string{
			"USER": "tester", "XDG_SESSION_TYPE": "", "WAYLAND_DISPLAY": "", "DISPLAY": "", "DESKTOP_SESSION": "",
		},
		alive:    map[int]bool{},
		free:     1 << 20,
		signals:  make(chan os.Signal, 8),
		autoFire: map[time.Duration]bool{prepareDelay: true},
		copied:   map[string]string{},
	}
	k.unset = []string{"WHISPER_LANGUAGE", "WHISPER_MODEL"}
	k.env["HOME"] = k.home
	k.env["WSI_YDOTOOL_SOCKET"] = k.socket
	for _, d := range []string{k.home, k.tmp, k.bin} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	k.rec = &fakeRecorder{k: k}
	// Insert is pressed as soon as the panel shows RECORDING.
	k.onState = map[string]func(){"RECORDING": func() { k.signals <- syscall.SIGTERM }}
	k.script()
	return k
}

// script answers every program a dictation runs with what a working desktop says.
func (k *kit) script() {
	f := k.run
	f.OnFunc([]string{"gdbus", "emit"}, func(c execx.Cmd) (execx.Result, error) {
		if c.Args[5] == signalIface+"."+stateChanged {
			if hook := k.onState[c.Args[6]]; hook != nil {
				hook()
			}
		}
		return execx.Result{}, nil
	})
	f.On([]string{"gdbus", "call", "--session", "--dest", "org.freedesktop.Notifications"}, execx.Result{Stdout: []byte("(uint32 7,)\n")})
	f.OnFunc([]string{"gdbus", "call", "--session", "--dest", "org.gnome.Shell"}, func(execx.Cmd) (execx.Result, error) {
		k.mu.Lock()
		defer k.mu.Unlock()
		k.asked++
		if len(k.pasteKeys) == 0 {
			return execx.Result{ExitCode: 1, Stderr: []byte("Error: GDBus.Error:org.freedesktop.DBus.Error.UnknownMethod: No such method")}, nil
		}
		i := min(k.asked, len(k.pasteKeys)) - 1
		return execx.Result{Stdout: []byte(k.pasteKeys[i] + "\n")}, nil
	})
	f.On([]string{filepath.Join(k.bin, "wsi-stop-grace")}, execx.Result{Stdout: []byte("0\n")})
	f.On([]string{filepath.Join(k.bin, "wsi-setting"), "language"}, execx.Result{Stdout: []byte("system\n")})
	f.OnFunc([]string{filepath.Join(k.bin, "wsi-resolve-model")}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: []byte("small\n")}, nil
	})
	f.OnFunc([]string{"sox"}, func(c execx.Cmd) (execx.Result, error) {
		return execx.Result{}, copyFile(c.Args[0], c.Args[1])
	})
	f.On([]string{"soxi", "-D"}, execx.Result{Stdout: []byte("2.000000\n")})
	f.On([]string{"faster-whisper-transcribe"}, execx.Result{Stdout: []byte("hello world\n")})
	for _, tool := range [][]string{{"wl-copy"}, {"xsel"}} {
		f.OnFunc(tool, func(c execx.Cmd) (execx.Result, error) {
			data, err := io.ReadAll(c.Stdin)
			if err != nil {
				return execx.Result{}, err
			}
			k.mu.Lock()
			k.copied[c.Line()] = string(data)
			k.mu.Unlock()
			return execx.Result{}, nil
		})
	}
	// Reading a selection back: the last copy to it, once the copy has taken (staleReads).
	// In order: a later rule wins, so "wl-paste -n -p" must come after its prefix "wl-paste -n".
	readers := [][2]string{
		{"wl-paste -n", "wl-copy"}, {"wl-paste -n -p", "wl-copy -p"}, {"xsel -ob", "xsel -ib"}, {"xsel -op", "xsel -ip"},
	}
	for _, rw := range readers {
		writer := rw[1]
		f.OnFunc(strings.Fields(rw[0]), func(execx.Cmd) (execx.Result, error) {
			k.mu.Lock()
			defer k.mu.Unlock()
			k.reads++
			if k.reads <= k.staleReads {
				return execx.Result{Stdout: []byte(previousDictation)}, nil
			}
			return execx.Result{Stdout: []byte(k.copied[writer])}, nil
		})
	}
	f.On([]string{"ydotool"}, execx.Result{})
	f.On([]string{"curl"}, execx.Result{Stdout: []byte("200")})
}

// previousDictation is what the selection held before this dictation copied to it.
const previousDictation = "The previous dictation"

func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, 0o600)
}

func (k *kit) desktop() {
	k.env["XDG_SESSION_TYPE"] = "wayland"
	k.env["WAYLAND_DISPLAY"] = "wayland-0"
}

func (k *kit) app() App {
	return App{Env: Env{
		Runner: k.run,
		Stdout: &k.out,
		Stderr: &k.errb,
		Getenv: func(key string) string { return k.env[key] },
		LookupEnv: func(key string) (string, bool) {
			if slices.Contains(k.unset, key) {
				return "", false
			}
			v, ok := k.env[key]
			return v, ok
		},
		LookPath: func(name string) (string, error) {
			if slices.Contains(k.missing, name) {
				return "", errors.New("not found")
			}
			return "/usr/bin/" + name, nil
		},
		Now: func() time.Time { return time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC) },
		Sleep: func(d time.Duration) {
			k.mu.Lock()
			k.sleeps = append(k.sleeps, d)
			k.mu.Unlock()
		},
		After: func(d time.Duration) <-chan time.Time {
			ch := make(chan time.Time, 1)
			if k.held != nil && d == prepareDelay {
				k.held <- ch
				return ch
			}
			if k.autoFire[d] {
				ch <- time.Time{}
			}
			return ch
		},
		Signals:  k.signals,
		PID:      4242,
		Alive:    func(pid int) bool { return k.alive[pid] },
		FreeKB:   func(string) (uint64, error) { return k.free, nil },
		Writable: func(path string) bool { return !slices.Contains(k.missing, "writable:"+path) },
		IsSocket: func(path string) bool { return path == k.socket && !slices.Contains(k.missing, "socket") },
		Account:  func() (string, error) { return "account", nil },
		BinDir:   k.bin,
		TempDir:  k.tmp,
		Recorder: k.rec,
	}}
}

func (k *kit) exec(args ...string) term.ExitCode {
	k.t.Helper()
	done := make(chan term.ExitCode, 1)
	go func() { done <- k.app().Run(context.Background(), args) }()
	select {
	case code := <-done:
		return code
	case <-time.After(10 * time.Second):
		k.t.Fatalf("wsi did not finish; stderr so far:\n%s", k.errb.String())
		return 0
	}
}

// calls is every command line run, in order.
func (k *kit) calls() []string {
	cs := k.run.Calls()
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Line())
	}
	return out
}

// states is every StateChanged the panel was sent.
func (k *kit) states() []string {
	var out []string
	for _, c := range k.run.Calls() {
		if c.Name == "gdbus" && c.Args[0] == "emit" && c.Args[5] == signalIface+"."+stateChanged {
			out = append(out, c.Args[6])
		}
	}
	return out
}

// notes is every notification as "<message literal>|<icon literal>|<ms>".
func (k *kit) notes() []string {
	var out []string
	for _, c := range k.run.Calls() {
		if c.Name == "gdbus" && c.Args[0] == "call" && c.Args[3] == "org.freedesktop.Notifications" {
			out = append(out, c.Args[12]+"|"+c.Args[10]+"|"+c.Args[15])
		}
	}
	return out
}

// keys is every ydotool chord, joined by " | ".
func (k *kit) keys() string {
	var out []string
	for _, c := range k.run.Calls() {
		if c.Name == "ydotool" {
			out = append(out, strings.Join(c.Args, " "))
		}
	}
	return strings.Join(out, " | ")
}

func (k *kit) called(prefix string) bool { return k.run.Called(prefix) }

func (k *kit) wantStderr(text string) {
	k.t.Helper()
	if !strings.Contains(k.errb.String(), text) {
		k.t.Fatalf("stderr lacks %q:\n%s", text, k.errb.String())
	}
}

func (k *kit) pidFile() string { return filepath.Join(k.tmp, "stt-recording-tester.pid") }

func (k *kit) wav() string { return filepath.Join(k.tmp, "wfile-tester.wav") }
