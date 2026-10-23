// Package rclonetail is the rclone-tail command (Plan 00192 SPEC-rclone-tail.md, E1-E17): a live
// view of each rclone mount's uploads, a tick at a time. The mounts come from findmnt and the
// mount processes' arguments in /proc (rclonerc.Client.Mounts), the figures from two RC calls per
// mount (vfs/stats and core/stats through rclonerc.Client.Ask, the credential in the child's
// environment), and the cache snapshot that brackets a stream is the rclone-cache-status report
// run in process (rclonecache).
package rclonetail

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/rclonecache"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/rclonerc"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

//go:embed usage.txt
var usageText string

const tool = "rclone-tail"

// Env is everything a run touches outside its own memory; each field is a seam.
type Env struct {
	Runner           execx.Runner
	Getenv           func(string) string
	Getwd            func() (string, error)
	LookPath         func(string) (string, error)
	Proc             fs.FS
	Stdout           io.Writer
	Stderr           io.Writer
	StdoutIsTerminal bool
	// Now is the time of day for the heading and the clock the rate is measured on.
	Now func() time.Time
	// After is the wait between ticks.
	After func(time.Duration) <-chan time.Time
	// OnStop calls stop each time SIGINT or SIGTERM arrives, until the release it returns is
	// called.
	OnStop func(stop func()) (release func())
}

// NewEnv is the production environment.
func NewEnv() Env {
	return Env{
		Runner: execx.OS{}, Getenv: os.Getenv, Getwd: os.Getwd, LookPath: exec.LookPath,
		Proc: os.DirFS("/proc"), Stdout: os.Stdout, Stderr: os.Stderr,
		StdoutIsTerminal: term.FileIsTerminal(os.Stdout),
		Now:              time.Now, After: time.After, OnStop: onSignal,
	}
}

// onSignal is OnStop over the process's SIGINT and SIGTERM, every one of them until released.
func onSignal(stop func()) func() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-sigs:
				stop()
			case <-done:
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			signal.Stop(sigs)
			close(done)
		})
	}
}

// App is one rclone-tail invocation.
type App struct {
	Env Env
}

// sample is a mount's core/stats bytes and when they were read (E10).
type sample struct {
	bytes int64
	at    time.Time
}

// run holds one invocation's state.
type run struct {
	env    Env
	opts   options
	rc     *rclonerc.Client
	colour bool
	p      term.Printer
	prev   map[string]sample // by mount target
}

// errStopped ends a tick that a signal cut short.
var errStopped = errors.New("stopped")

// errNoMount is a tick with nothing to show (E6); its diagnostic is already written.
var errNoMount = errors.New("no mount")

const (
	bold   = "\x1b[1m"
	dim    = "\x1b[2m"
	green  = "\x1b[0;32m"
	yellow = "\x1b[1;33m"
	cyan   = "\x1b[0;36m"
	red    = "\x1b[0;31m"
	reset  = "\x1b[0m"
)

// Run is the whole command; it returns the exit code.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	env := a.Env
	// Every child in a session of its own (D5): the terminal's Ctrl-C reaches this run alone,
	// which cancels a call in flight itself, so a killed child is never read as an RC failure.
	env.Runner = detached{env.Runner}
	r := &run{
		env:    env,
		rc:     &rclonerc.Client{Runner: env.Runner, Getenv: env.Getenv, Proc: env.Proc},
		colour: env.StdoutIsTerminal && env.Getenv("NO_COLOR") == "",
		prev:   map[string]sample{},
	}
	opts, err := parseArgs(args)
	if err != nil {
		r.diagf("%s — see '%s --help'.", err, tool)
		return term.ExitUsage
	}
	if opts.help {
		r.p.Print(env.Stdout, usageText)
		return r.written()
	}
	r.opts = opts
	if _, err := env.LookPath("rclone"); err != nil {
		r.diagf("rclone is not installed; it is installed by play-rclone.yml.")
		return term.ExitFailure
	}
	// The first stop ends the ticks (stopCtx); a second ends the run, snapshots too (ctx, E15).
	ctx, abort := context.WithCancel(ctx)
	defer abort()
	stopCtx, stop := context.WithCancel(ctx)
	defer stop()
	release := env.OnStop(func() {
		if stopCtx.Err() != nil {
			abort()
		}
		stop()
	})
	defer release()
	if opts.once {
		err := r.tick(stopCtx)
		if err == nil && stopCtx.Err() != nil {
			err = errStopped
		}
		return r.ended(ctx, err)
	}
	return r.stream(ctx, stopCtx)
}

// stream is E14: the opening snapshot, then ticks an interval apart until stopped.
func (r *run) stream(ctx, stopCtx context.Context) term.ExitCode {
	if code := r.snapshot(ctx); code != term.ExitOK {
		return code
	}
	if stopCtx.Err() != nil {
		return r.ended(ctx, errStopped)
	}
	if r.line(""); r.p.Err() != nil {
		return r.written()
	}
	for {
		if err := r.tick(stopCtx); err != nil {
			return r.ended(ctx, err)
		}
		select {
		case <-r.env.After(r.opts.interval):
		case <-stopCtx.Done():
			return r.ended(ctx, errStopped)
		}
	}
}

// ended is the exit for a tick's error: a stop is the closing snapshot (E15), a write that
// failed or no mount exit 1, anything else named and exit 1; nil is a --once tick written.
func (r *run) ended(ctx context.Context, err error) term.ExitCode {
	switch {
	case err == nil:
		return r.written()
	case errors.Is(err, errStopped):
		r.line("")
		if code := r.written(); code != term.ExitOK {
			return code
		}
		return r.snapshot(ctx)
	case errors.Is(err, errNoMount):
		return term.ExitFailure
	case r.p.Err() != nil:
		return r.written()
	default:
		r.diagf("%s", err)
		return term.ExitFailure
	}
}

// snapshot is the rclone-cache-status summary of the watched mounts, in process (D2). Once ctx
// ends (a second stop, E15) it prints nothing more and the exit is 130.
func (r *run) snapshot(ctx context.Context) term.ExitCode {
	args := []string{"--no-dir"}
	if r.opts.filter != "" {
		args = append(args, "--", r.opts.filter)
	}
	env := r.env
	code := rclonecache.App{Env: rclonecache.Env{
		Runner: env.Runner, Getenv: env.Getenv, Getwd: env.Getwd, LookPath: env.LookPath, Proc: env.Proc,
		Stdout: untilDone{ctx.Done(), env.Stdout}, Stderr: untilDone{ctx.Done(), env.Stderr},
		StdoutIsTerminal: env.StdoutIsTerminal,
	}}.Run(ctx, args)
	if ctx.Err() != nil {
		return term.ExitCancelled
	}
	return code
}

// written is exit 0, or 1 naming the write to stdout that failed (E17).
func (r *run) written() term.ExitCode {
	if err := r.p.Err(); err != nil {
		r.diagf("cannot write the report: %v", err)
		return term.ExitFailure
	}
	return term.ExitOK
}

// diagf writes one diagnostic line on stderr.
func (r *run) diagf(format string, args ...any) {
	r.p.Print(r.env.Stderr, fmt.Sprintf(tool+": "+format+"\n", args...))
}

func (r *run) line(s string) { r.p.Print(r.env.Stdout, s+"\n") }

// paint wraps s in an escape code on a colour terminal (E16).
func (r *run) paint(code, s string) string {
	if !r.colour {
		return s
	}
	return code + s + reset
}

// tick is E4-E12 once: the mounts listed afresh, the heading, a line per mount. A stop ends it
// before the next line (errStopped); a failed write ends it at once.
func (r *run) tick(ctx context.Context) error {
	mounts, err := r.rc.Mounts(ctx)
	if ctx.Err() != nil {
		return errStopped
	}
	if err != nil {
		return err
	}
	var shown []rclonerc.Mount
	for _, m := range mounts {
		if r.opts.filter == "" || m.Name == r.opts.filter {
			shown = append(shown, m)
		}
	}
	if len(shown) == 0 {
		if r.opts.filter != "" {
			r.diagf("No rclone mount matching '%s' found.", r.opts.filter)
		} else {
			r.diagf("No rclone mounts found.")
		}
		return errNoMount
	}
	if r.line(r.paint(bold, "=== "+r.env.Now().Format("15:04:05")+" ===")); r.p.Err() != nil {
		return r.p.Err()
	}
	for _, m := range shown {
		text, err := r.describe(ctx, m)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return errStopped
		}
		if r.line("  " + r.paint(cyan, m.Name) + "  " + text); r.p.Err() != nil {
			return r.p.Err()
		}
	}
	return nil
}

// describe is a mount's line after its name (E8-E12).
func (r *run) describe(ctx context.Context, m rclonerc.Mount) (string, error) {
	addr, refusal := rcAddr(m)
	if refusal != "" {
		return r.paint(red, "error: "+refusal), nil
	}
	if addr == "" {
		return r.paint(dim, "(no --rc; cannot query)"), nil
	}
	raw, reason, err := r.ask(ctx, m, addr, "vfs/stats")
	if err != nil || reason != "" {
		return r.paint(red, "error: "+reason), err
	}
	q, err := parseQueue(raw)
	if err != nil {
		r.diagf("%s: vfs/stats: %v", m.Name, err)
		return r.paint(red, "error: parse failed"), nil
	}
	raw, reason, err = r.ask(ctx, m, addr, "core/stats")
	if err != nil || reason != "" {
		return r.paint(red, "error: "+reason), err
	}
	t, err := parseTransfers(raw)
	if err != nil {
		r.diagf("%s: core/stats: %v", m.Name, err)
		return r.paint(red, "error: parse failed"), nil
	}
	rate := r.rate(m.Target, t.bytes)
	if q.inProgress == 0 && q.queued == 0 && rate == 0 {
		text := r.paint(dim, "idle")
		if q.errored > 0 {
			text += " " + r.paint(red, fmt.Sprintf("(%d error(s))", q.errored))
		}
		return text, nil
	}
	text := fmt.Sprintf("queued: %s · active: %s · done: %s (%s/%s) · %s · ETA %s",
		r.paint(cyan, strconv.FormatInt(q.queued, 10)), r.paint(cyan, strconv.FormatInt(q.inProgress, 10)),
		r.paint(cyan, fmt.Sprintf("%d/%d", t.done, t.total)),
		rclonecache.IEC(t.bytes), rclonecache.IEC(t.totalBytes),
		r.paint(green, rclonecache.IEC(rate)+"/s"), r.paint(yellow, eta(t.eta, t.hasETA)))
	if q.errored > 0 {
		text += " · " + r.paint(red, fmt.Sprintf("%d errored", q.errored))
	}
	return text, nil
}

// detached runs every command in a new session (execx.Cmd.NewSession), whose group is killed
// when the run's context ends.
type detached struct{ execx.Runner }

func (d detached) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	if err := ctx.Err(); err != nil {
		return execx.Result{}, fmt.Errorf("run %s: %w", c.Name, err)
	}
	c.NewSession = true
	return d.Runner.Run(ctx, c)
}

// untilDone writes to w until done (a context's Done) is closed, and then drops what it is given.
type untilDone struct {
	done <-chan struct{}
	w    io.Writer
}

func (u untilDone) Write(p []byte) (int, error) {
	select {
	case <-u.done:
		return len(p), nil
	default:
	}
	n, err := u.w.Write(p)
	if err != nil {
		return n, fmt.Errorf("write: %w", err)
	}
	return n, nil
}

// rcAddr is the mount's RC address, or why it has none that can be used (E5).
func rcAddr(m rclonerc.Mount) (string, string) {
	addr, err := m.RCAddr()
	if err != nil {
		return "", err.Error()
	}
	return addr, ""
}

// ask is one RC call: the answer, or the reason the line gives with rclonerc's detail on
// stderr, or errStopped when a stop cancelled it (nothing printed, E15).
func (r *run) ask(ctx context.Context, m rclonerc.Mount, addr, endpoint string) ([]byte, string, error) {
	out, err := r.rc.Ask(ctx, addr, endpoint)
	if err == nil {
		return out, "", nil
	}
	var rcErr *rclonerc.RCError
	if ctx.Err() != nil || !errors.As(err, &rcErr) {
		return nil, "", errStopped
	}
	r.diagf("%s: %s", m.Name, rcErr.Detail)
	return nil, rcErr.Reason, nil
}

// rate is E10: bytes per second since the mount's last answer, rounded down.
func (r *run) rate(target string, bytes int64) int64 {
	now := r.env.Now()
	prev, seen := r.prev[target]
	r.prev[target] = sample{bytes: bytes, at: now}
	elapsed := now.Sub(prev.at).Seconds()
	if !seen || bytes < prev.bytes || elapsed <= 0 {
		return 0
	}
	return int64(float64(bytes-prev.bytes) / elapsed)
}
