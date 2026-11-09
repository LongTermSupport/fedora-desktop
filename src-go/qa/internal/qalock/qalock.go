// Package qalock is the one QA lock: QA never runs in parallel (Plan 00237). qa-all, qa-changed
// and scripts/qa-go-qa.bash run on its own each take it once, at the top of the run, and a
// second run waits for the first instead of loading the machine twice.
//
// The lock is <git common dir>/fedora-desktop-qa.lock, held with flock(2). The common
// directory is the one every view of the repository shares: the main checkout, each linked
// worktree (whose own git dir is under the common one), every CCY container that bind-mounts
// the checkout, and the host. An advisory flock belongs to the open file description of one
// inode, and a bind mount shows the same inode, so a lock taken through one path on a kernel is
// seen through every other on that kernel. It is released by the kernel when the holder ends,
// however it ends, so a dead holder never blocks a run.
//
// The holder writes its record (pid, pid namespace, command, start, worktree) into the lock
// file; a waiter prints it once and then a short line every minute, both on stderr. The pid and
// the worktree are as the holder sees them: a run in a container names its container's pid and
// path.
//
// A run started inside a run inherits the lock: a run that finds the lock held, by a record
// whose pid namespace is its own and whose pid is among its own ancestors, does not wait (qa-all
// and qa-changed both run qa-go-qa.bash, which takes the lock when run alone). It is decided
// from the process tree alone, never from the environment, so a gate that scrubs its children's
// environment cannot make a run wait for its own parent; the namespace keeps a pid that only
// looks like an ancestor (the same number in another container) from counting.
package qalock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// FileName is the lock file in the git common directory.
const FileName = "fedora-desktop-qa.lock"

// TimeoutEnv bounds the wait, as a Go duration; empty or 0 waits for as long as the lock is held.
const TimeoutEnv = "QA_LOCK_TIMEOUT"

// pollEvery is how often a waiting run tries the lock again; remindEvery how often it says it
// is still waiting.
const (
	pollEvery   = time.Second
	remindEvery = time.Minute
)

// maxAncestry bounds the walk up the process tree; a real one is a few dozen deep.
const maxAncestry = 4096

// lockMode is the lock file's permission: it holds a pid, a command name and a path.
const lockMode = 0o644

// ErrBusy is a --no-wait run that found the lock held.
var ErrBusy = errors.New("the QA lock is held")

// ErrTimedOut is a wait that reached QA_LOCK_TIMEOUT.
var ErrTimedOut = errors.New("the wait for the QA lock timed out")

// Holder is the record a holder writes into the lock file.
type Holder struct {
	PID int `json:"pid"`
	// PIDNS is the holder's pid namespace (the target of /proc/self/ns/pid): its pid means a
	// process only to a run in the same namespace.
	PIDNS    string    `json:"pidns"`
	Command  string    `json:"command"`
	Started  time.Time `json:"started"`
	Worktree string    `json:"worktree"`
}

// stampLayout is how a holder's start is shown.
const stampLayout = "2006-01-02 15:04:05 MST"

// Describe names the holder for a person: "the QA run <pid> (<command>, started <time>, in
// <worktree>)".
func (h Holder) Describe() string {
	if h.PID <= 0 {
		return "a QA run that has not written its record yet"
	}
	return fmt.Sprintf("the QA run %d (%s, started %s, in %s)", h.PID, h.Command, h.Started.Format(stampLayout), h.Worktree)
}

// short names the holder in the minute line.
func (h Holder) short() string {
	if h.PID <= 0 {
		return h.Describe()
	}
	return fmt.Sprintf("the QA run %d (%s)", h.PID, h.Command)
}

// Clock is the time a wait reads and spends.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// RealClock is the machine's clock.
type RealClock struct{}

// Now implements Clock.
func (RealClock) Now() time.Time { return time.Now() }

// Sleep implements Clock: it ends early, with the context's error, when the context ends.
func (RealClock) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Env is everything a take touches outside the lock file.
type Env struct {
	Getenv func(string) string
	// Stderr receives the wait lines.
	Stderr io.Writer
	Clock  Clock
	Getpid func() int
	// Parent is a process's parent pid.
	Parent func(pid int) (int, error)
	// PIDNS is this process's pid namespace.
	PIDNS func() (string, error)
}

// OSEnv is the real environment of this process.
func OSEnv(stderr io.Writer) Env {
	return Env{Getenv: os.Getenv, Stderr: stderr, Clock: RealClock{}, Getpid: os.Getpid, Parent: ParentOf, PIDNS: OwnPIDNS}
}

// OwnPIDNS is this process's pid namespace, as /proc/self/ns/pid names it ("pid:[<inode>]").
func OwnPIDNS() (string, error) {
	return os.Readlink("/proc/self/ns/pid")
}

// Request is one take.
type Request struct {
	// Path is the lock file (Path gives the shared one).
	Path string
	// Command and Worktree go into the record.
	Command, Worktree string
	// NoWait fails at once with ErrBusy instead of waiting.
	NoWait bool
}

// Lock is a held QA lock, or one inherited from an ancestor run.
type Lock struct {
	path   string
	file   *os.File
	nested bool
}

// Path is the lock file.
func (l *Lock) Path() string { return l.path }

// Nested is whether the lock was inherited from an ancestor run rather than taken.
func (l *Lock) Nested() bool { return l.nested }

// Release lets the lock go, clearing the record first so no waiter reads a run that has ended.
// An inherited lock is the ancestor's to release: Release does nothing.
func (l *Lock) Release() error {
	if l.nested || l.file == nil {
		return nil
	}
	f := l.file
	l.file = nil
	return errors.Join(f.Truncate(0), f.Close())
}

// Taker is how a QA command takes the lock for its run over the checkout at root; release
// lets it go.
type Taker func(ctx context.Context, root string, noWait bool) (release func() error, err error)

// OSTaker is the real Taker for command: the shared lock of root's repository, its waits told
// on stderr.
func OSTaker(command string, stderr io.Writer) Taker {
	return func(ctx context.Context, root string, noWait bool) (func() error, error) {
		path, err := Path(ctx, proc.OS{}, root)
		if err != nil {
			return nil, err
		}
		l, err := Acquire(ctx, OSEnv(stderr), Request{Path: path, Command: command, Worktree: root, NoWait: noWait})
		if err != nil {
			return nil, err
		}
		return l.Release, nil
	}
}

// Path is the shared lock of the checkout at root: FileName in the repository's git common
// directory, absolute.
func Path(ctx context.Context, runner proc.Runner, root string) (string, error) {
	res, err := runner.Run(ctx, proc.Cmd{Argv: []string{"git", "-C", root, "rev-parse", "--path-format=absolute", "--git-common-dir"}})
	if err != nil {
		return "", fmt.Errorf("cannot find the QA lock: git: %w", err)
	}
	if res.Code != 0 {
		return "", fmt.Errorf("cannot find the QA lock: %s is not in a git repository (git exit %d: %s)", root, res.Code, strings.TrimSpace(res.Stderr))
	}
	common := strings.TrimSpace(res.Stdout)
	if common == "" || !filepath.IsAbs(common) {
		return "", fmt.Errorf("cannot find the QA lock: git named the common directory %q, not an absolute path", common)
	}
	return filepath.Join(common, FileName), nil
}

// Acquire takes the lock, waiting while another run holds it, or inherits it from an ancestor
// run.
func Acquire(ctx context.Context, env Env, req Request) (*Lock, error) {
	timeout, err := parseTimeout(env.Getenv(TimeoutEnv))
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(req.Path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, lockMode)
	if err != nil {
		return nil, fmt.Errorf("cannot open the QA lock: %w", err)
	}
	l, err := take(ctx, env, req, f, timeout)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	if l.nested {
		return l, f.Close()
	}
	return l, nil
}

// take is Acquire with the file open: the caller closes it unless a lock was taken on it.
func take(ctx context.Context, env Env, req Request, f *os.File, timeout time.Duration) (*Lock, error) {
	w := &wait{env: env, req: req, timeout: timeout, start: env.Clock.Now()}
	w.nextReminder = w.start.Add(remindEvery)
	for {
		flockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if flockErr == nil {
			return hold(env, req, f)
		}
		if !errors.Is(flockErr, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("cannot lock %s: %w", req.Path, flockErr)
		}
		// Held: by an ancestor run, which this run is part of, or by another run to wait for.
		inherited, err := heldByAncestor(env, req.Path)
		if err != nil {
			return nil, err
		}
		if inherited {
			return &Lock{path: req.Path, nested: true}, nil
		}
		if err := w.busy(); err != nil {
			return nil, err
		}
		if err := env.Clock.Sleep(ctx, pollEvery); err != nil {
			return nil, err
		}
	}
}

// wait is one run waiting for the lock.
type wait struct {
	env                 Env
	req                 Request
	timeout             time.Duration
	start, nextReminder time.Time
	waited              bool
}

// busy is one try that found the lock held: refuse under --no-wait or past the bound, else say
// whose run this one waits for, the first time and then once a minute.
func (w *wait) busy() error {
	holder := recordOf(w.req.Path)
	now := w.env.Clock.Now()
	first := !w.waited
	w.waited = true
	switch {
	case w.req.NoWait:
		return fmt.Errorf("%w by %s (lock %s); --no-wait was given, so this run does not wait", ErrBusy, holder.Describe(), w.req.Path)
	case first:
		_, err := fmt.Fprintf(w.env.Stderr, "qa: waiting for %s\n", holder.Describe())
		return err
	case w.timeout > 0 && now.Sub(w.start) >= w.timeout:
		return fmt.Errorf("%w: %s=%s passed and %s still holds %s", ErrTimedOut, TimeoutEnv, w.env.Getenv(TimeoutEnv), holder.Describe(), w.req.Path)
	case !now.Before(w.nextReminder):
		w.nextReminder = w.nextReminder.Add(remindEvery)
		_, err := fmt.Fprintf(w.env.Stderr, "qa: still waiting for %s, %dm so far\n", holder.short(), int(now.Sub(w.start)/time.Minute))
		return err
	}
	return nil
}

// hold writes this run's record into the locked file, before the run starts anything that
// could look for it.
func hold(env Env, req Request, f *os.File) (*Lock, error) {
	ns, err := env.PIDNS()
	if err != nil {
		return nil, fmt.Errorf("cannot read this run's pid namespace for the QA lock's record: %w", err)
	}
	h := Holder{PID: env.Getpid(), PIDNS: ns, Command: req.Command, Started: env.Clock.Now(), Worktree: req.Worktree}
	if err := writeRecord(f, h); err != nil {
		return nil, fmt.Errorf("cannot write the QA lock's record: %w", err)
	}
	return &Lock{path: req.Path, file: f}, nil
}

// heldByAncestor is whether the lock's record names, in this run's own pid namespace, this
// process or one of its ancestors.
func heldByAncestor(env Env, path string) (bool, error) {
	holder, ok := readRecord(path)
	if !ok {
		return false, nil
	}
	ns, err := env.PIDNS()
	if err != nil {
		return false, fmt.Errorf("cannot tell whether the QA lock is held by an ancestor: this run's pid namespace: %w", err)
	}
	if holder.PIDNS != ns {
		return false, nil
	}
	pid := env.Getpid()
	for range maxAncestry {
		if pid == holder.PID {
			return true, nil
		}
		if pid <= 1 {
			return false, nil
		}
		parent, err := env.Parent(pid)
		if err != nil {
			return false, fmt.Errorf("cannot tell whether the QA lock is held by an ancestor: the parent of process %d: %w", pid, err)
		}
		pid = parent
	}
	return false, nil
}

// writeRecord replaces the file's content with h.
func writeRecord(f *os.File, h Holder) error {
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}
	if truncErr := f.Truncate(0); truncErr != nil {
		return truncErr
	}
	_, err = f.WriteAt(append(data, '\n'), 0)
	return err
}

// recordOf is the holder the lock file names, the zero Holder when it names none.
func recordOf(path string) Holder {
	if h, ok := readRecord(path); ok {
		return h
	}
	return Holder{}
}

// readRecord is the holder the lock file names; false when it names none (not yet written,
// cleared, unreadable).
func readRecord(path string) (Holder, bool) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Holder{}, false
	}
	var h Holder
	if json.Unmarshal(raw, &h) != nil || h.PID <= 0 {
		return Holder{}, false
	}
	return h, true
}

// parseTimeout reads QA_LOCK_TIMEOUT: empty or 0 is no bound.
func parseTimeout(value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%s=%q is not a duration such as 30m (0 waits as long as the lock is held)", TimeoutEnv, value)
	}
	return d, nil
}

// ParentOf is a process's parent pid, from field 4 of /proc/<pid>/stat (counted after the
// command name's closing bracket, which may itself hold spaces).
func ParentOf(pid int) (int, error) {
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}
	line := string(raw)
	cut := strings.LastIndex(line, ") ")
	if cut < 0 {
		return 0, fmt.Errorf("process %d: unreadable stat", pid)
	}
	fields := strings.Fields(line[cut+2:])
	if len(fields) < 2 {
		return 0, fmt.Errorf("process %d: unreadable stat", pid)
	}
	return strconv.Atoi(fields[1])
}
