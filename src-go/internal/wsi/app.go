// Package wsi is the batch speech-to-text recorder the panel extension starts: it records
// with pw-record, resamples with sox, transcribes with faster-whisper-transcribe (or a
// whisper.cpp server) and puts the text on a selection or pastes it with ydotool, telling the
// panel its state over D-Bus. The specification is CLAUDE/Plan/00192-.../SPEC-wsi-watermark.md;
// the expectation numbers (W1...) and departures from the bash (B1...) the comments cite are its.
package wsi

import (
	"context"
	_ "embed" // usage.txt is embedded verbatim
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

//go:embed usage.txt
var usageText string

// exitUsage is a command line that cannot be run (W3, B3).
const exitUsage term.ExitCode = 2

// Env is everything a run touches outside its own memory and its files; each field is a seam.
type Env struct {
	Runner    execx.Runner
	Stdout    io.Writer
	Stderr    io.Writer
	Getenv    func(string) string
	LookupEnv func(string) (string, bool)
	LookPath  func(string) (string, error)
	Now       func() time.Time
	Sleep     func(time.Duration)
	After     func(time.Duration) <-chan time.Time
	// Signals carries SIGTERM (Insert) and SIGINT; the main subscribes before anything runs.
	Signals <-chan os.Signal
	PID     int
	// Alive reports whether a process exists (kill -0).
	Alive func(pid int) bool
	// FreeKB is the space left for an unprivileged writer in a directory, in KiB.
	FreeKB   func(dir string) (uint64, error)
	Writable func(path string) bool
	IsSocket func(path string) bool
	// Account is the login name, for a $USER that is unset.
	Account func() (string, error)
	// BinDir holds the sibling helpers (wsi-stop-grace, wsi-setting, wsi-resolve-model).
	BinDir string
	// TempDir is /dev/shm: the PID file and the recording.
	TempDir  string
	Recorder Recorder
}

// NewEnv is the production environment, reading signals from signals.
func NewEnv(signals <-chan os.Signal) Env {
	binDir := "."
	if self, err := os.Executable(); err == nil {
		binDir = filepath.Dir(self)
	}
	return Env{
		Runner: execx.OS{}, Stdout: os.Stdout, Stderr: os.Stderr,
		Getenv: os.Getenv, LookupEnv: os.LookupEnv, LookPath: exec.LookPath,
		Now: time.Now, Sleep: time.Sleep, After: time.After, Signals: signals,
		PID: os.Getpid(), Alive: alive, FreeKB: freeKB, Writable: writable, IsSocket: isSocket,
		Account: account, BinDir: binDir, TempDir: "/dev/shm", Recorder: ExecRecorder{Runner: execx.OS{}},
	}
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func freeKB(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", dir, err)
	}
	if st.Bsize <= 0 {
		return 0, fmt.Errorf("statfs %s: block size %d", dir, st.Bsize)
	}
	return st.Bavail * uint64(st.Bsize) / 1024, nil
}

// writable is access(2) with W_OK, the test [ -w ] makes.
func writable(path string) bool { return syscall.Access(path, 0o2) == nil }

func isSocket(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode()&fs.ModeSocket != 0
}

func account() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("who is this account: %w", err)
	}
	return u.Username, nil
}

// App is one wsi invocation.
type App struct {
	Env Env
}

// exitError ends a run with code; what it means has been reported already.
type exitError struct{ code term.ExitCode }

func (e *exitError) Error() string { return "exit " + strconv.Itoa(int(e.code)) }

var errFailed = &exitError{code: term.ExitFailure}

// run is the state of one dictation.
type run struct {
	env   Env
	o     options
	log   *logger
	panel *panel
	out   term.Printer

	home, wav, pidFile, socket, session string

	grace           int
	language, model string
	recording       Recording

	pasteWithShift string
	saveAfter      bool
	refocused      bool
	target         string
}

// Run is one invocation (W1-W35); its exit code is 0, 1, or 2 for a command line error.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	o, argErr := parseArgs(args)
	if argErr == nil && o.help {
		if _, err := io.WriteString(a.Env.Stderr, usageText); err != nil {
			return term.ExitFailure
		}
		return term.ExitOK
	}
	r, err := newRun(a.Env, o)
	if err != nil {
		if _, werr := fmt.Fprintf(a.Env.Stderr, "wsi: %v\n", err); werr != nil {
			return term.ExitFailure
		}
		return term.ExitFailure
	}
	if argErr != nil {
		r.logError(ctx, argErr.Error())
		return exitUsage
	}
	if pid, ok := r.otherRecorder(); ok {
		r.log.say(fmt.Sprintf("wsi: already recording (PID %d); not starting a second recorder", pid))
		return term.ExitFailure
	}
	err = r.dictate(ctx)
	r.cleanup(ctx)
	var e *exitError
	switch {
	case err == nil && r.out.Err() != nil:
		r.log.say("wsi: could not write the text to stdout: " + r.out.Err().Error())
		return term.ExitFailure
	case err == nil:
		r.log.debug("WSI completed successfully")
		return term.ExitOK
	case errors.As(err, &e):
		return e.code
	default:
		r.logError(ctx, err.Error())
		return term.ExitFailure
	}
}

// newRun fixes the paths and the two outputs (log and panel) from the environment.
func newRun(env Env, o options) (*run, error) {
	home := env.Getenv("HOME")
	if home == "" {
		return nil, errors.New("HOME is not set")
	}
	name := env.Getenv("USER")
	if name == "" {
		var err error
		if name, err = env.Account(); err != nil {
			return nil, err
		}
	}
	socket := env.Getenv("WSI_YDOTOOL_SOCKET")
	if socket == "" {
		socket = "/run/ydotool.socket"
	}
	session := env.Getenv("XDG_SESSION_TYPE")
	if session == "" {
		session = "unknown"
	}
	l := &logger{
		stderr: env.Stderr, now: env.Now, immediate: o.debug || o.verbose,
		file: filepath.Join(home, ".local/share/speech-to-text/debug.log"),
	}
	return &run{
		env: env, o: o, log: l, home: home, socket: socket, session: session, target: targetReady,
		pasteWithShift: o.pasteWithShift,
		wav:            filepath.Join(env.TempDir, "wfile-"+name+".wav"),
		pidFile:        filepath.Join(env.TempDir, "stt-recording-"+name+".pid"),
		panel:          &panel{runner: env.Runner, log: l, token: o.token, notifID: "0", noNotify: o.noNotify},
	}, nil
}

// otherRecorder is W5: the PID file naming a live process.
func (r *run) otherRecorder() (int, bool) {
	data, err := os.ReadFile(r.pidFile)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err == nil && pid > 0 && r.env.Alive(pid) {
		return pid, true
	}
	r.log.debug("Stale PID file found (no live process for " + strings.TrimSpace(string(data)) + "); will overwrite")
	return 0, false
}

// cleanup is W6, after every dictation that got past W5.
func (r *run) cleanup(ctx context.Context) {
	r.log.debug("Cleanup triggered")
	r.stopRecorder()
	data, err := os.ReadFile(r.pidFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		r.log.say("[WSI] cannot read " + r.pidFile + "; left in place")
	case strings.TrimSpace(string(data)) == strconv.Itoa(r.env.PID):
		r.remove(r.pidFile)
	default:
		r.log.debug("PID file names " + strings.TrimSpace(string(data)) + ", not this client; left in place")
	}
	for _, f := range []string{r.wav, r.wav + ".resampled.wav", r.wav + ".padded"} {
		r.remove(f)
	}
	r.panel.state(ctx, stateIdle)
}

func (r *run) remove(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		r.log.say("[WSI] cannot remove " + path + ": " + err.Error())
	}
}

// logError is W9's error that ends the dictation: the line, the Error signal, then ERROR with
// its notification, the held log lines written first.
func (r *run) logError(ctx context.Context, msg string) {
	r.log.errorLine(msg)
	r.panel.errorText(ctx, msg)
	note := msg
	flushed := 0
	if !r.o.debug {
		flushed = r.log.flush()
	}
	if flushed > 0 {
		note = fmt.Sprintf("%s\n\n📝 Debug logs saved (%d entries):\n%s", msg, flushed, r.log.file)
	}
	r.panel.notifyState(ctx, stateError, note, "")
	if flushed > 0 {
		r.log.direct(fmt.Sprintf("Flushed %d buffered log entries to disk on error", flushed))
	}
}

// fail is logError and the exit 1 that follows it.
func (r *run) fail(ctx context.Context, msg string) error {
	r.logError(ctx, msg)
	return errFailed
}

// problem is W9's problem: logged and shown beside the panel's state, and the dictation goes on.
func (r *run) problem(ctx context.Context, msg string) {
	r.log.errorLine(msg)
	r.panel.errorText(ctx, msg)
}

// exitCodeOf is a finished command's status as a shell would give it.
func exitCodeOf(err error) int {
	var exit *execx.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.Code
	case execx.IsNotInstalled(err):
		return 127
	case errors.Is(err, context.DeadlineExceeded):
		return 124
	default:
		return 1
	}
}

// dictate is everything after W5, in order.
func (r *run) dictate(ctx context.Context) error {
	steps := []func(context.Context) error{
		r.probeServer, r.debugChecks, r.preflight, r.chooseModel, r.record, r.checkRecording,
	}
	for _, step := range steps {
		if err := step(ctx); err != nil {
			return err
		}
	}
	text, err := r.transcribe(ctx)
	if err != nil {
		return err
	}
	return r.deliver(ctx, text)
}
