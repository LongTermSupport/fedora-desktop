// Package sessions is ccy-sessions: the host command that lists every CCY and cc session,
// attaches, takes over or ends one, warns them all before a reboot, and brings them back after
// one. Every external effect goes through the App's seams, so each flow is tested against a
// scripted runner and a clock that only moves when the App waits.
package sessions

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccyenv"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/transcript"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

//go:embed usage.txt
var usage string

const (
	// launcher is what Ctrl-N runs; it is not overridable.
	launcher = "/var/local/claude-yolo/claude-yolo"
	// defaultLib is where the CCY libraries are installed.
	defaultLib = "/var/local/claude-yolo/lib"
	// containerWorkspace is where ccy mounts a project inside its container.
	containerWorkspace = ccyenv.Workspace
	// cmdReboot names the reboot subcommand, and the systemctl verb it ends in.
	cmdReboot = "reboot"
	// flagDryRun is the flag that says what would be done and does none of it.
	flagDryRun = "--dry-run"
	// maxTries is how many refused picks end the picker.
	maxTries = 3
)

// Config is the environment the tool reads, parsed once per run.
type Config struct {
	CcyLib string
	// Minute is one "minute" of the reboot countdown; a test sets it to nothing.
	Minute time.Duration
	// Poll is how often verify-restore --wait and set-going look again.
	Poll time.Duration
	// SetGoingWait is the set-going lock wait and its prompt-wait deadline.
	SetGoingWait time.Duration
	// StartWindow is how long a typed input has to be taken.
	StartWindow time.Duration
	// SubmitDelay is the pause between typed text and Enter.
	SubmitDelay time.Duration
	// CPUSample is the length of the CPU sample; zero means the shortest possible.
	CPUSample          time.Duration
	TakeOverWaitTenths int
	CompactFloor       TokenFloor
	PassphraseFile     string
	BootIDFile         string
}

// loadConfig reads the knobs, each a whole or fractional number of seconds. A value that is not
// one is refused, not guessed.
func loadConfig(getenv func(string) string) (Config, error) {
	cfg := Config{
		CcyLib: getenv("CCY_LIB"), CompactFloor: ParseTokenFloor(getenv("CCY_RESTORE_COMPACT_FLOOR_TOKENS")),
		PassphraseFile: getenv(unlockFileEnv), BootIDFile: getenv("CCY_BOOT_ID_FILE"),
		TakeOverWaitTenths: 50,
	}
	if cfg.CcyLib == "" {
		cfg.CcyLib = defaultLib
	}
	knobs := []struct {
		name string
		def  time.Duration
		into *time.Duration
	}{
		{"CCY_SESSIONS_MINUTE_SECONDS", time.Minute, &cfg.Minute},
		{"CCY_SESSIONS_POLL_SECONDS", 5 * time.Second, &cfg.Poll},
		{"CCY_SESSIONS_SET_GOING_WAIT", 1200 * time.Second, &cfg.SetGoingWait},
		{"CCY_SESSIONS_START_WINDOW", 120 * time.Second, &cfg.StartWindow},
		{"CCY_SESSIONS_SUBMIT_DELAY", 200 * time.Millisecond, &cfg.SubmitDelay},
		{"CCY_CPU_SAMPLE_SECONDS", time.Second, &cfg.CPUSample},
	}
	for _, k := range knobs {
		d, err := seconds(getenv, k.name, k.def)
		if err != nil {
			return Config{}, err
		}
		*k.into = d
	}
	if raw := getenv("CCY_TMUX_TAKE_OVER_WAIT_TENTHS"); raw != "" {
		tenths, err := strconv.Atoi(raw)
		if err != nil || tenths < 0 {
			return Config{}, fmt.Errorf("CCY_TMUX_TAKE_OVER_WAIT_TENTHS is '%s', not a whole number of tenths of a second", raw)
		}
		cfg.TakeOverWaitTenths = tenths
	}
	return cfg, nil
}

// TokenFloor is the context size, in tokens, at or above which a restored session is typed
// /compact rather than continue. It keeps the text it was read from, because a value that is not
// a whole number of tokens is reported as it was written, when set-going needs it.
type TokenFloor struct {
	raw    string
	tokens int64
	valid  bool
}

// ParseTokenFloor reads a floor: a whole number of tokens, at least 1, written without a sign or
// leading zero. Anything else is a TokenFloor that is not Valid.
func ParseTokenFloor(raw string) TokenFloor {
	tokens, err := strconv.ParseInt(raw, 10, 64)
	if !wholeTokens.MatchString(raw) || err != nil {
		return TokenFloor{raw: raw}
	}
	return TokenFloor{raw: raw, tokens: tokens, valid: true}
}

// Tokens is the floor, and whether it is a whole number of tokens at all.
func (f TokenFloor) Tokens() (int64, bool) { return f.tokens, f.valid }

// String is the floor as it was written.
func (f TokenFloor) String() string { return f.raw }

// seconds is the named variable as a duration; unset is def.
func seconds(getenv func(string) string, name string, def time.Duration) (time.Duration, error) {
	raw := getenv(name)
	if raw == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || f < 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%s is '%s', not a number of seconds", name, raw)
	}
	return time.Duration(f * float64(time.Second)), nil
}

// App is one run of ccy-sessions. Every external effect goes through a seam: Runner for the
// programs it runs, Execer for the one process replacement, the streams, the environment, the
// clock and the waits. A test sets those it cares about and wire fills in the rest.
type App struct {
	Runner execx.Runner
	Execer execx.Execer
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	// Unsetenv removes a variable from this process, so a child never inherits it.
	Unsetenv func(string) error

	Term   *term.Term
	Tmux   *tmuxccy.Tmux
	Git    *gitx.Git
	Picker *ui.Picker
	Probe  *sessprobe.Probe
	// Transcripts reads only what each polled transcript gained since the last look.
	Transcripts *transcript.Cache

	// Now is the clock. Pause waits d, or until a signal arrives on sig, and reports whether
	// it was interrupted. Signals starts listening for INT and TERM and returns the channel
	// and the function that stops listening.
	Now     func() time.Time
	Pause   func(d time.Duration, sig <-chan os.Signal) bool
	Signals func() (<-chan os.Signal, func())

	// IsTerminal says whether stdin or stdout is a terminal; InContainer whether this is a CCY
	// container; Executable whether a path may be executed; Getuid the effective user.
	IsTerminal  func(v any) bool
	InContainer func() bool
	Executable  func(path string) bool
	Getuid      func() int

	cfg      Config
	writeErr error // the first failed write to Stdout through out
}

// NewApp is the production App: the real OS and the process's own streams.
func NewApp() *App {
	a := &App{}
	a.wire()
	return a
}

// Run dispatches args (without the program name) and returns the exit code. Output that could
// not be written turns a success into a failure.
func (a *App) Run(args []string) term.ExitCode {
	a.wire()
	return a.Term.Finish(a.run(args), a.writeErr)
}

func (a *App) run(args []string) term.ExitCode {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "notify", cmdReboot, "restore", "set-going", "verify-restore":
	case "-l", "--list":
		if len(args) > 1 {
			return a.usageError(fmt.Sprintf("'%s' takes no further arguments.", sub))
		}
	case "-h", "--help":
		a.out(usage)
		return term.ExitOK
	default:
		return a.usageError(fmt.Sprintf("unknown option '%s'.", sub))
	}
	lib := a.Getenv("CCY_LIB")
	if lib == "" {
		lib = defaultLib
	}
	if !a.ccyInstalled(lib) {
		return term.ExitFailure
	}
	cfg, err := loadConfig(a.Getenv)
	if err != nil {
		a.failErr(err)
		return term.ExitFailure
	}
	a.cfg = cfg
	a.wireProbe()
	rest := []string(nil)
	if len(args) > 1 {
		rest = args[1:]
	}
	return a.dispatch(context.Background(), sub, rest)
}

// wire fills every seam and adapter that is not set yet.
func (a *App) wire() {
	if a.Runner == nil {
		a.Runner = execx.OS{}
	}
	if a.Execer == nil {
		a.Execer = execx.OS{}
	}
	if a.Stdin == nil {
		a.Stdin = os.Stdin
	}
	if a.Stdout == nil {
		a.Stdout = os.Stdout
	}
	if a.Stderr == nil {
		a.Stderr = os.Stderr
	}
	if a.Getenv == nil {
		a.Getenv = os.Getenv
	}
	if a.Unsetenv == nil {
		a.Unsetenv = os.Unsetenv
	}
	a.wireSeams()
	if a.Term == nil {
		a.Term = &term.Term{Err: a.Stderr, IsTerminal: a.IsTerminal, Getenv: a.Getenv}
	}
	if a.Tmux == nil {
		a.Tmux = &tmuxccy.Tmux{Runner: a.Runner, Term: a.Term, Stdin: a.Stdin, Stdout: a.Stdout, Stderr: a.Stderr}
	}
	if a.Git == nil {
		a.Git = gitx.New(a.Runner)
	}
	if a.Picker == nil {
		a.Picker = &ui.Picker{Runner: a.Runner, Stderr: a.Stderr}
	}
	if a.Transcripts == nil {
		a.Transcripts = transcript.NewCache()
	}
}

// wireSeams fills the clock, wait, signal and file-system seams.
func (a *App) wireSeams() {
	if a.Now == nil {
		a.Now = time.Now
	}
	if a.Pause == nil {
		a.Pause = pauseFor
	}
	if a.Signals == nil {
		a.Signals = notifySignals
	}
	if a.IsTerminal == nil {
		a.IsTerminal = term.FileIsTerminal
	}
	if a.InContainer == nil {
		a.InContainer = ccyenv.InContainer
	}
	if a.Executable == nil {
		a.Executable = isExecutable
	}
	if a.Getuid == nil {
		a.Getuid = os.Geteuid
	}
}

// wireProbe builds the host probe for the configured CPU sample length.
func (a *App) wireProbe() {
	if a.Probe != nil {
		return
	}
	a.Probe = &sessprobe.Probe{
		Tmux: a.Tmux, Runner: a.Runner, SampleFor: max(a.cfg.CPUSample, time.Nanosecond),
	}
}

func (a *App) dispatch(ctx context.Context, sub string, rest []string) term.ExitCode {
	switch sub {
	case "notify":
		return a.notify(ctx, rest)
	case cmdReboot:
		return a.reboot(ctx, rest)
	case "restore":
		return a.runRestore(ctx, rest)
	case "set-going":
		return a.setGoing(ctx, rest)
	case "verify-restore":
		return a.verifyRestore(ctx, rest)
	case "-l", "--list":
		return a.list(ctx)
	default:
		return a.picker(ctx)
	}
}

// ccyInstalled refuses to go on without the CCY libraries, saying why: inside a container
// ccy-sessions is simply in the wrong place.
func (a *App) ccyInstalled(lib string) bool {
	msg := ccyenv.Missing("ccy-sessions", lib, a.InContainer)
	if msg != "" {
		a.sayf("%s", msg)
	}
	return msg == ""
}

// usageError is a mistake in how the command was called: exit 64.
func (a *App) usageError(msg string) term.ExitCode {
	a.sayf("ccy-sessions: %s See ccy-sessions --help.\n", msg)
	return term.ExitUsage
}

// out writes to stdout, the tool's payload: the help text, the table and the verify report.
// The first failed write is kept and Run turns it into a failure exit.
func (a *App) out(s string) {
	if _, err := io.WriteString(a.Stdout, s); err != nil && a.writeErr == nil {
		a.writeErr = err
	}
}

// say writes a status line to stderr.
func (a *App) sayf(format string, args ...any) { a.Term.Printf(format, args...) }

// fail prints an ERROR: line.
func (a *App) fail(msg string) { a.Term.PrintError(msg) }

func (a *App) failErr(err error) { a.Term.PrintError(err.Error()) }

// pauseFor waits d, or until a signal arrives on sig, and reports whether one did.
func pauseFor(d time.Duration, sig <-chan os.Signal) bool {
	if d <= 0 {
		select {
		case <-sig:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return false
	case <-sig:
		return true
	}
}

// notifySignals listens for INT and TERM until the returned function is called.
func notifySignals() (<-chan os.Signal, func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	return ch, func() { signal.Stop(ch) }
}

// isExecutable reports whether the path may be executed by this user.
func isExecutable(path string) bool {
	const execute = 1
	return syscall.Access(filepath.Clean(path), execute) == nil
}

var errReported = errors.New("already reported")
