package qalock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// started is the fixed instant every fake holder began at.
var started = time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC)

// fakeClock moves time only when the code under test sleeps; onSleep, when set, runs on each
// sleep with the time slept so far, so a test can act at a chosen point of the wait.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	sleeps  int
	onSleep func(n int)
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.sleeps++
	n, hook := c.sleeps, c.onSleep
	c.mu.Unlock()
	if hook != nil {
		hook(n)
	}
	return nil
}

// syncBuffer is a stderr several goroutines may write.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// world is one test's environment: a fake clock, a recorded environment and a process table.
type world struct {
	clock   *fakeClock
	stderr  *syncBuffer
	env     map[string]string
	pid     int
	parents map[int]int
	ns      string
	// real reads this process's own pid, parents and namespace (the multi-process tests).
	real bool
}

// testNS is the fake pid namespace every fake holder and waiter shares.
const testNS = "pid:[4026531836]"

func newWorld() *world {
	return &world{
		clock:   &fakeClock{now: started.Add(time.Hour)},
		stderr:  &syncBuffer{},
		env:     map[string]string{},
		pid:     4242,
		parents: map[int]int{4242: 1},
		ns:      testNS,
	}
}

func realWorld() *world {
	w := newWorld()
	w.real = true
	return w
}

func (w *world) Env() Env {
	if w.real {
		return Env{Getenv: func(k string) string { return w.env[k] }, Stderr: w.stderr, Clock: w.clock, Getpid: os.Getpid, Parent: ParentOf, PIDNS: OwnPIDNS}
	}
	return Env{
		Getenv: func(k string) string { return w.env[k] },
		Stderr: w.stderr,
		Clock:  w.clock,
		Getpid: func() int { return w.pid },
		Parent: func(pid int) (int, error) {
			ppid, ok := w.parents[pid]
			if !ok {
				return 0, os.ErrNotExist
			}
			return ppid, nil
		},
		PIDNS: func() (string, error) {
			if w.ns == "" {
				return "", os.ErrNotExist
			}
			return w.ns, nil
		},
	}
}

func lockPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), FileName)
}

// holdAs takes the lock on path through a second open file description, as another process
// would, and writes h as its record. The returned function lets it go.
func holdAs(t *testing.T, path string, h Holder) func() {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := writeRecord(f, h); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			if err := f.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(release)
	return release
}

var other = Holder{PID: 777, PIDNS: testNS, Command: "qa-all", Started: started, Worktree: "/checkout/main"}

func TestAFreeLockIsTakenAndItsRecordNamesTheHolder(t *testing.T) {
	w := newWorld()
	path := lockPath(t)
	l, err := Acquire(context.Background(), w.Env(), Request{Path: path, Command: "qa-changed", Worktree: "/checkout/wt"})
	if err != nil {
		t.Fatal(err)
	}
	if l.Nested() || l.Path() != path {
		t.Fatalf("lock %+v", l)
	}
	h, ok := readRecord(path)
	if !ok || h.PID != 4242 || h.PIDNS != testNS || h.Command != "qa-changed" || h.Worktree != "/checkout/wt" || !h.Started.Equal(w.clock.now) {
		t.Fatalf("record %+v ok=%v", h, ok)
	}
	if w.stderr.String() != "" {
		t.Fatalf("a free lock printed %q", w.stderr.String())
	}
	if relErr := l.Release(); relErr != nil {
		t.Fatal(relErr)
	}
}

func TestAReleaseClearsTheRecordAndFreesTheLock(t *testing.T) {
	w := newWorld()
	path := lockPath(t)
	l, err := Acquire(context.Background(), w.Env(), Request{Path: path, Command: "qa-changed"})
	if err != nil {
		t.Fatal(err)
	}
	if relErr := l.Release(); relErr != nil {
		t.Fatal(relErr)
	}
	if relErr := l.Release(); relErr != nil {
		t.Fatalf("a second release: %v", relErr)
	}
	if raw, readErr := os.ReadFile(path); readErr != nil || len(raw) != 0 {
		t.Fatalf("after release the record is %q (%v), want it cleared", raw, readErr)
	}
	again, err := Acquire(context.Background(), w.Env(), Request{Path: path, Command: "qa-all", NoWait: true})
	if err != nil {
		t.Fatalf("the lock is not free after release: %v", err)
	}
	if relErr := again.Release(); relErr != nil {
		t.Fatal(relErr)
	}
}

func TestASecondRunWaitsNamingTheHolderAndRemindsEveryMinute(t *testing.T) {
	w := newWorld()
	path := lockPath(t)
	release := holdAs(t, path, other)
	// Released after three minutes and a second of polling.
	w.clock.onSleep = func(n int) {
		if n == 181 {
			release()
		}
	}
	l, err := Acquire(context.Background(), w.Env(), Request{Path: path, Command: "qa-changed", Worktree: "/checkout/wt"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := l.Release(); err != nil {
			t.Error(err)
		}
	}()
	want := "qa: waiting for the QA run 777 (qa-all, started 2026-10-29 09:30:00 UTC, in /checkout/main)\n" +
		"qa: still waiting for the QA run 777 (qa-all), 1m so far\n" +
		"qa: still waiting for the QA run 777 (qa-all), 2m so far\n" +
		"qa: still waiting for the QA run 777 (qa-all), 3m so far\n"
	if got := w.stderr.String(); got != want {
		t.Fatalf("stderr:\n%s\nwant:\n%s", got, want)
	}
	if h, ok := readRecord(path); !ok || h.PID != 4242 {
		t.Fatalf("the waiter's record after taking the lock: %+v %v", h, ok)
	}
}

func TestTheReminderNamesANewHolderWhenTheLockChangedHands(t *testing.T) {
	w := newWorld()
	path := lockPath(t)
	release := holdAs(t, path, other)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.clock.onSleep = func(n int) {
		switch n {
		case 30:
			release()
			holdAs(t, path, Holder{PID: 888, Command: "qa-go-qa", Started: started, Worktree: "/checkout/b"})
		case 61:
			cancel()
		}
	}
	_, err := Acquire(ctx, w.Env(), Request{Path: path, Command: "qa-changed"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v, want the cancel", err)
	}
	if !strings.Contains(w.stderr.String(), "qa: still waiting for the QA run 888 (qa-go-qa), 1m so far\n") {
		t.Fatalf("stderr %q", w.stderr.String())
	}
}

func TestNoWaitFailsAtOnceNamingTheHolder(t *testing.T) {
	w := newWorld()
	path := lockPath(t)
	holdAs(t, path, other)
	_, err := Acquire(context.Background(), w.Env(), Request{Path: path, Command: "qa-changed", NoWait: true})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err %v, want ErrBusy", err)
	}
	if !strings.Contains(err.Error(), "the QA run 777 (qa-all, started 2026-10-29 09:30:00 UTC, in /checkout/main)") || !strings.Contains(err.Error(), "--no-wait") {
		t.Fatalf("message %q", err)
	}
	if w.clock.sleeps != 0 || w.stderr.String() != "" {
		t.Fatalf("--no-wait slept %d times and printed %q", w.clock.sleeps, w.stderr.String())
	}
}

func TestTheWaitEndsAtQALockTimeout(t *testing.T) {
	w := newWorld()
	w.env[TimeoutEnv] = "90s"
	path := lockPath(t)
	holdAs(t, path, other)
	_, err := Acquire(context.Background(), w.Env(), Request{Path: path, Command: "qa-changed"})
	if !errors.Is(err, ErrTimedOut) {
		t.Fatalf("err %v, want ErrTimedOut", err)
	}
	if !strings.Contains(err.Error(), "QA_LOCK_TIMEOUT=90s") || !strings.Contains(err.Error(), "the QA run 777") {
		t.Fatalf("message %q", err)
	}
	if w.clock.sleeps != 90 {
		t.Fatalf("slept %d seconds, want 90", w.clock.sleeps)
	}
}

func TestABadQALockTimeoutIsRefusedBeforeAnyWait(t *testing.T) {
	for _, value := range []string{"soon", "-1s"} {
		w := newWorld()
		w.env[TimeoutEnv] = value
		_, err := Acquire(context.Background(), w.Env(), Request{Path: lockPath(t), Command: "qa-all"})
		if err == nil || !strings.Contains(err.Error(), TimeoutEnv) {
			t.Fatalf("%q: err %v", value, err)
		}
	}
}

func TestARecordNotYetWrittenIsSaidSo(t *testing.T) {
	w := newWorld()
	path := lockPath(t)
	holdAs(t, path, Holder{})
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	_, err := Acquire(context.Background(), w.Env(), Request{Path: path, NoWait: true})
	if err == nil || !strings.Contains(err.Error(), "a QA run that has not written its record yet") {
		t.Fatalf("err %v", err)
	}
}

func TestANestedRunInheritsTheLockOfItsAncestor(t *testing.T) {
	w := newWorld()
	path := lockPath(t)
	// The holder is 777; this process is its grandchild, 4242 -> 900 -> 777.
	holdAs(t, path, other)
	w.parents = map[int]int{4242: 900, 900: 777, 777: 1}
	l, err := Acquire(context.Background(), w.Env(), Request{Path: path, Command: "qa-go-qa"})
	if err != nil {
		t.Fatal(err)
	}
	if !l.Nested() {
		t.Fatal("not nested")
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if h, ok := readRecord(path); !ok || h.PID != 777 {
		t.Fatalf("a nested release touched the holder's record: %+v %v", h, ok)
	}
	if w.clock.sleeps != 0 || w.stderr.String() != "" {
		t.Fatalf("a nested run waited: %d sleeps, %q", w.clock.sleeps, w.stderr.String())
	}
}

func TestAHolderThatIsNotAnAncestorIsWaitedFor(t *testing.T) {
	w := newWorld() // 4242 -> 1: the holder, 777, is another run
	path := lockPath(t)
	holdAs(t, path, other)
	_, err := Acquire(context.Background(), w.Env(), Request{Path: path, NoWait: true})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err %v, want ErrBusy", err)
	}
}

func TestTheSamePidInAnotherPidNamespaceIsNotAnAncestor(t *testing.T) {
	w := newWorld()
	path := lockPath(t)
	// 777 is this run's parent here, but the holder's 777 is a process of another namespace
	// (a run on the host, or in another container).
	w.parents = map[int]int{4242: 777, 777: 1}
	elsewhere := other
	elsewhere.PIDNS = "pid:[4026532999]"
	holdAs(t, path, elsewhere)
	_, err := Acquire(context.Background(), w.Env(), Request{Path: path, NoWait: true})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err %v, want ErrBusy", err)
	}
}

func TestAnUnreadableNamespaceIsAnError(t *testing.T) {
	w := newWorld()
	w.ns = ""
	path := lockPath(t)
	holdAs(t, path, other)
	if _, err := Acquire(context.Background(), w.Env(), Request{Path: path}); err == nil || !strings.Contains(err.Error(), "pid namespace") {
		t.Fatalf("held: err %v", err)
	}
	free := lockPath(t)
	if _, err := Acquire(context.Background(), w.Env(), Request{Path: free}); err == nil || !strings.Contains(err.Error(), "pid namespace") {
		t.Fatalf("free: err %v", err)
	}
}

// A hard link is a second path to the lock's inode, as a bind mount is: the lock is the inode's,
// so a run through either path waits for a holder through the other.
func TestTheLockBelongsToTheInodeNotThePath(t *testing.T) {
	w := newWorld()
	path := lockPath(t)
	holdAs(t, path, other)
	alias := filepath.Join(t.TempDir(), "alias.lock")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	_, err := Acquire(context.Background(), w.Env(), Request{Path: alias, NoWait: true})
	if !errors.Is(err, ErrBusy) || !strings.Contains(err.Error(), "the QA run 777") {
		t.Fatalf("through a second path: err %v, want ErrBusy naming 777", err)
	}
}

func TestAnUnreadableAncestryIsAnError(t *testing.T) {
	w := newWorld()
	path := lockPath(t)
	holdAs(t, path, other)
	w.parents = map[int]int{} // this process's parent cannot be read
	_, err := Acquire(context.Background(), w.Env(), Request{Path: path})
	if err == nil || !strings.Contains(err.Error(), "parent") {
		t.Fatalf("err %v", err)
	}
}

func TestALockThatCannotBeOpenedIsAnError(t *testing.T) {
	w := newWorld()
	_, err := Acquire(context.Background(), w.Env(), Request{Path: filepath.Join(t.TempDir(), "missing", FileName)})
	if err == nil || !strings.Contains(err.Error(), "cannot open the QA lock") {
		t.Fatalf("err %v", err)
	}
}

func TestDescribe(t *testing.T) {
	if got := other.Describe(); got != "the QA run 777 (qa-all, started 2026-10-29 09:30:00 UTC, in /checkout/main)" {
		t.Fatal(got)
	}
	if got := (Holder{}).Describe(); got != "a QA run that has not written its record yet" {
		t.Fatal(got)
	}
}

// fakeGit answers rev-parse with a fixed common directory, or fails.
type fakeGit struct {
	argv []string
	res  proc.Result
	err  error
}

func (g *fakeGit) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	g.argv = c.Argv
	return g.res, g.err
}

func TestPathIsInTheGitCommonDirectory(t *testing.T) {
	g := &fakeGit{res: proc.Result{Stdout: "/repo/.git\n"}}
	path, err := Path(context.Background(), g, "/repo/.claude/worktrees/x")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/repo/.git/"+FileName {
		t.Fatal(path)
	}
	want := "git -C /repo/.claude/worktrees/x rev-parse --path-format=absolute --git-common-dir"
	if strings.Join(g.argv, " ") != want {
		t.Fatalf("argv %q", g.argv)
	}
}

func TestPathRefusesWhatGitCannotAnswer(t *testing.T) {
	for name, g := range map[string]*fakeGit{
		"not a repository": {res: proc.Result{Code: 128, Stderr: "fatal: not a git repository\n"}},
		"git missing":      {err: errors.New("exec: git: not found")},
		"relative answer":  {res: proc.Result{Stdout: ".git\n"}},
		"empty answer":     {res: proc.Result{Stdout: "\n"}},
	} {
		if _, err := Path(context.Background(), g, "/somewhere"); err == nil || !strings.Contains(err.Error(), "QA lock") {
			t.Fatalf("%s: err %v", name, err)
		}
	}
}

func TestPathOfThisCheckoutIsTheSharedCommonDirectory(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path, err := Path(context.Background(), proc.OS{}, wd)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != FileName || !filepath.IsAbs(path) || strings.Contains(path, "/worktrees/") {
		t.Fatalf("path %q: want <common dir>/%s, shared by every worktree", path, FileName)
	}
}

// TestOSTakerHoldsTheLockOfTheRepositoryAtRoot takes the real lock of a scratch repository: it
// lands in that repository's git directory, is held until released, and a second take with
// --no-wait meanwhile is refused.
func TestOSTakerHoldsTheLockOfTheRepositoryAtRoot(t *testing.T) {
	t.Setenv(TimeoutEnv, "")
	root := t.TempDir()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q", root}})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	var stderr syncBuffer
	release, err := OSTaker("qa-test", &stderr)(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	lockFile := filepath.Join(root, ".git", FileName)
	h, ok := readRecord(lockFile)
	ns, nsErr := OwnPIDNS()
	if !ok || nsErr != nil || h.Command != "qa-test" || h.Worktree != root || h.PID != os.Getpid() || h.PIDNS != ns {
		t.Fatalf("record %+v %v (namespace %q %v)", h, ok, ns, nsErr)
	}
	// This process is the holder, so a second take from it is nested, not refused.
	again, err := OSTaker("qa-test", &stderr)(context.Background(), root, true)
	if err != nil {
		t.Fatalf("the holder's own second take: %v", err)
	}
	if err := again(); err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if _, err := OSTaker("qa-test", &stderr)(context.Background(), t.TempDir(), true); err == nil || !strings.Contains(err.Error(), "not in a git repository") {
		t.Fatalf("a root outside git: %v", err)
	}
}

func TestParentOfReadsProc(t *testing.T) {
	ppid, err := ParentOf(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if ppid != os.Getppid() {
		t.Fatalf("ParentOf(self) = %d, want %d", ppid, os.Getppid())
	}
	if _, err := ParentOf(-5); err == nil {
		t.Fatal("a pid with no /proc entry was read")
	}
}

func TestRealClockSleepEndsWithTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (RealClock{}).Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if err := (RealClock{}).Sleep(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if (RealClock{}).Now().IsZero() {
		t.Fatal("zero now")
	}
}

// childEnv names the lock path a re-run of this test binary holds, as a separate process.
const childEnv = "QALOCK_TEST_CHILD_LOCK"

// TestTwoProcessesShareTheLock runs a real second process: the child takes the lock on a file,
// says so, and holds it until the parent creates the go file; the parent's no-wait take is
// refused naming the child, and its waiting take waits until the child lets go. flock(2) is
// per open file description on one inode, which is what makes this hold across processes.
func TestTwoProcessesShareTheLock(t *testing.T) {
	if path := os.Getenv(childEnv); path != "" {
		holdUntilTold(t, path)
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan proc.Result, 1)
	go func() {
		res, runErr := proc.OS{}.Run(context.Background(), proc.Cmd{
			Argv: []string{self, "-test.run=^TestTwoProcessesShareTheLock$"},
			Env:  []string{childEnv + "=" + path},
		})
		if runErr != nil {
			res = proc.Result{Code: -2, Stderr: runErr.Error()}
		}
		done <- res
	}()
	waitForFile(t, path+".held", done)
	child, ok := readRecord(path)
	if !ok || child.Command != "qa-child" {
		t.Fatalf("the child's record %+v %v", child, ok)
	}
	w := realWorld()
	_, err = Acquire(context.Background(), w.Env(), Request{Path: path, Command: "qa-parent", NoWait: true})
	if !errors.Is(err, ErrBusy) || !strings.Contains(err.Error(), "the QA run "+strconv.Itoa(child.PID)+" (qa-child") {
		t.Fatalf("no-wait while the child holds: %v", err)
	}
	w.clock.onSleep = func(n int) {
		if n == 1 {
			if writeErr := os.WriteFile(path+".go", nil, 0o600); writeErr != nil {
				t.Error(writeErr)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	l, err := Acquire(context.Background(), w.Env(), Request{Path: path, Command: "qa-parent"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(w.stderr.String(), "qa: waiting for the QA run "+strconv.Itoa(child.PID)+" (qa-child, started ") {
		t.Fatalf("stderr %q", w.stderr.String())
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if res := <-done; res.Code != 0 {
		t.Fatalf("child exit %d: %s%s", res.Code, res.Stdout, res.Stderr)
	}
}

// holdUntilTold is the child's side: take the lock, mark it held, wait for the go file.
func holdUntilTold(t *testing.T, path string) {
	t.Helper()
	w := realWorld()
	l, err := Acquire(context.Background(), w.Env(), Request{Path: path, Command: "qa-child", Worktree: "/child"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".held", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for range 3000 {
		if _, err := os.Stat(path + ".go"); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
}

// waitForFile polls for the file, failing when the child ended first or never made it.
func waitForFile(t *testing.T, path string, done chan proc.Result) {
	t.Helper()
	for range 3000 {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case res := <-done:
			t.Fatalf("the child ended before taking the lock: exit %d: %s%s", res.Code, res.Stdout, res.Stderr)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the child never took the lock")
}
