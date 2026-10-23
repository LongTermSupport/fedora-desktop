package keeper

// The keeper's test harness: every seam faked, the link and CMD in-process children.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	digest    = "0123456789abcdef0123456789abcdef"
	linkName  = "fake-link"
	bootStart = time.Hour
	wallStart = int64(1760000000000)
)

// world is one keeper's machine: a private runtime parent, a kit root with its current link, a
// state root of seats and a desired directory under the test's own directory, and every seam
// faked. The link and CMD are in-process children: each Start hands the test a child it ends
// when it likes, and the link's control socket end, which the test speaks on as the link.
type world struct {
	t       *testing.T
	clk     *clock.Fake
	dir     string
	rt      string
	kitRoot string
	kit     paths.Kit
	layout  paths.Layout
	desired string
	proc    *procTable
	pidfds  *fakePidfds
	starter *execx.Fake
	runner  *execx.Fake
	execer  *execx.Fake
	sigs    chan os.Signal
	watch   *fakeWatcher
	links   chan *fakeLink
	ended   <-chan struct{}
	cmds    chan *child
	env     map[string]string
	stdout  syncBuffer
	stderr  syncBuffer
	launch  cli.Launch
	args    []string

	mu      sync.Mutex
	nextPID int
	linkErr error
	readies int
}

// newWorld is a world launched by args, in which DESIRED and STATE stand for the world's own
// desired and state directories.
func newWorld(t *testing.T, args ...string) *world {
	t.Helper()
	dir := t.TempDir()
	w := &world{
		t: t, clk: clock.NewFake(bootStart, wallStart), dir: dir,
		rt: dir + "/rt", kitRoot: dir + "/kits", desired: dir + "/desired",
		proc: &procTable{ticks: map[int]int64{os.Getpid(): 7}}, starter: &execx.Fake{}, runner: &execx.Fake{},
		execer: &execx.Fake{}, sigs: make(chan os.Signal, 8), watch: &fakeWatcher{events: make(chan WatchEvent, 16)},
		links: make(chan *fakeLink, 8), cmds: make(chan *child, 8), nextPID: 4000,
		env: map[string]string{"HOME": dir + "/home", "PATH": "/usr/bin"},
	}
	w.pidfds = &fakePidfds{open: map[int]*fakePidfd{}}
	for _, d := range []string{w.rt, w.desired, dir + "/state", dir + "/home"} {
		mustDo(t, os.Mkdir(d, 0o700))
	}
	kit, err := paths.KitAt(w.kitRoot + "/" + digest)
	mustDo(t, err)
	w.kit = kit
	mustDo(t, os.MkdirAll(kit.String(), 0o755))
	mustDo(t, os.WriteFile(kit.Settings(), []byte(`{"statusLine":{"type":"command"}}`), 0o644))
	mustDo(t, os.Symlink(digest, w.kitRoot+"/current"))
	layout, err := paths.HomeLayout(dir+"/home", w.rt)
	if err == nil {
		layout, err = layout.WithKitRoot(w.kitRoot)
	}
	if err == nil {
		layout, err = layout.WithState(dir + "/state")
	}
	mustDo(t, err)
	w.layout = layout
	w.runner.On([]string{kit.Binary(), "--version"}, execx.Result{Stdout: []byte(digest + "\n")})
	w.starter.OnStart([]string{linkName}, func(c execx.Cmd) (execx.Started, error) { return w.startLink(c) })
	w.parse(args...)
	return w
}

func (w *world) parse(args ...string) {
	w.t.Helper()
	args = slices.Clone(args)
	for i, a := range args {
		switch a {
		case "DESIRED":
			args[i] = w.desired
		case "STATE":
			args[i] = w.dir + "/state"
		}
	}
	l, err := cli.ParseLaunch(args)
	mustDo(w.t, err)
	w.launch, w.args = l, args
	if l.Desired != "" {
		layout, derr := w.layout.WithDesired(l.Desired)
		mustDo(w.t, derr)
		w.layout = layout
	}
	w.starter.OnStart([]string{l.Cmd[0]}, w.startCmd)
}

func (w *world) config() Config {
	return Config{
		Launch: w.launch, Layout: w.layout, RuntimeParent: w.rt, Digest: digest, Args: w.args,
		Env: func(k string) string { return w.env[k] }, Cwd: "/work/project-a",
		Clock: w.clk, Locks: lock.SystemClock(), Runner: w.runner, Execer: w.execer, Starter: w.starter,
		LinkCmd: func(run paths.Run, _ paths.Kit) execx.Cmd {
			return execx.Cmd{Name: linkName, Args: []string{"link", "--run", run.Dir()}}
		},
		Proc: w.proc, Pidfds: w.pidfds, Signals: w.sigs, Watcher: w.watch,
	}
}

// start runs the keeper; the channel gives its exit.
func (w *world) start() <-chan term.ExitCode { return w.startWith(w.config()) }

func (w *world) startWith(cfg Config) <-chan term.ExitCode {
	res := make(chan term.ExitCode, 1)
	ended := make(chan struct{})
	w.ended = ended
	go func() {
		code := Run(cfg, &w.stdout, &w.stderr)
		close(ended)
		res <- code
	}()
	return res
}

func (w *world) pid() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.nextPID++
	return w.nextPID
}

func (w *world) newChild(c execx.Cmd) *child {
	ch := &child{pid: w.pid(), cmd: c, sigs: make(chan syscall.Signal, 16), done: make(chan struct{})}
	w.proc.set(ch.pid, int64(ch.pid)*10)
	return ch
}

func (w *world) startLink(c execx.Cmd) (execx.Started, error) {
	w.mu.Lock()
	linkErr := w.linkErr
	w.mu.Unlock()
	if linkErr != nil {
		return nil, linkErr
	}
	if len(c.ExtraFiles) != 1 {
		return nil, errors.New("the link has no control socket")
	}
	fd, err := unix.Dup(int(c.ExtraFiles[0].Fd()))
	if err != nil {
		return nil, err
	}
	conn, err := wire.NewConn(os.NewFile(uintptr(fd), "link-end"))
	if err != nil {
		return nil, err
	}
	l := &fakeLink{child: w.newChild(c), conn: conn}
	// The link's socket end closes with it, as a process's descriptors do.
	l.onEx = func() { mustClose(conn) }
	w.links <- l
	return l, nil
}

func (w *world) startCmd(c execx.Cmd) (execx.Started, error) {
	ch := w.newChild(c)
	w.cmds <- ch
	return ch, nil
}

// link is the next link the keeper started.
func (w *world) link() *fakeLink {
	w.t.Helper()
	select {
	case l := <-w.links:
		return l
	case <-w.ended:
		w.t.Fatalf("the keeper ended before it started a link: %s", w.stderr.String())
		return nil
	}
}

// cmd is the next CMD the keeper started.
func (w *world) cmd() *child {
	w.t.Helper()
	select {
	case c := <-w.cmds:
		return c
	case <-w.ended:
		w.t.Fatalf("the keeper ended before it started CMD: %s", w.stderr.String())
		return nil
	}
}

// up is the next link, told ready once it has its launch and adds.
func (w *world) up(adds int) *fakeLink {
	w.t.Helper()
	l := w.link()
	l.expect(w.t, wire.KindLaunch)
	for range adds {
		l.expect(w.t, wire.KindAdd)
	}
	w.ready(l)
	return l
}

// ready tells l it is ready and waits until the keeper has taken it.
func (w *world) ready(l *fakeLink) {
	w.t.Helper()
	l.send(w.t, wire.Msg{Kind: wire.KindReady, Ready: &wire.Ready{}})
	w.readies++
	w.logged("the link is ready", w.readies)
}

// running is a launched keeper with its link ready and CMD started, the link told the pid.
func (w *world) running(adds int) (*fakeLink, *child, <-chan term.ExitCode) {
	w.t.Helper()
	res := w.start()
	l := w.up(adds)
	c := w.cmd()
	l.expect(w.t, wire.KindPID)
	w.sessionStarted(l)
	return l, c, res
}

// sessionStarted is the link's report of Claude Code's first SessionStart, for a --claude
// launch; without it CMD's end is a launch failure.
func (w *world) sessionStarted(l *fakeLink) {
	w.t.Helper()
	if w.launch.Mode == cli.LaunchClaude {
		l.send(w.t, wire.Msg{Kind: wire.KindStarted, Started: &wire.Started{}})
		w.logged("Claude Code's first SessionStart")
	}
}

// runDir is the session's runtime directory: the one entry of the run root with a keeper.log,
// which no leftover a test plants has.
func (w *world) runDir() paths.Run {
	w.t.Helper()
	entries, err := os.ReadDir(w.layout.RunRoot())
	mustDo(w.t, err)
	var runs []paths.Run
	for _, e := range entries {
		id, perr := paths.ParseRunID(e.Name())
		if perr == nil && exists(w.t, w.layout.Run(id).KeeperLog()) {
			runs = append(runs, w.layout.Run(id))
		}
	}
	if len(runs) != 1 {
		w.t.Fatalf("the run root holds %d runs with a keeper.log", len(runs))
	}
	return runs[0]
}

// runGone is whether the run root has no runtime left.
func (w *world) runGone() bool {
	entries, err := os.ReadDir(w.layout.RunRoot())
	return err == nil && len(entries) == 0
}

func (w *world) log() string {
	w.t.Helper()
	data, err := os.ReadFile(w.runDir().KeeperLog())
	mustDo(w.t, err)
	return string(data)
}

func (w *world) keeperJSON() state.Keeper {
	w.t.Helper()
	data, err := os.ReadFile(w.runDir().KeeperJSON())
	mustDo(w.t, err)
	k, err := state.DecodeKeeper(data)
	mustDo(w.t, err)
	return k
}

// seat makes team's seat directory under the state root, as area C places it.
func (w *world) seat(team string) paths.Seat {
	w.t.Helper()
	s := w.layout.Seat(mustTeam(w.t, team))
	mustDo(w.t, os.Mkdir(s.Dir(), 0o700))
	return s
}

func (w *world) report(s paths.Seat) state.Report {
	w.t.Helper()
	data, err := os.ReadFile(s.Report())
	mustDo(w.t, err)
	r, err := state.DecodeReport(data)
	mustDo(w.t, err)
	return r
}

func (w *world) lockRecord(s paths.Seat) state.LockRecord {
	w.t.Helper()
	data, err := os.ReadFile(s.Lock())
	mustDo(w.t, err)
	r, err := state.DecodeLockRecord(data)
	mustDo(w.t, err)
	return r
}

// held is whether another description holds s's seat lock.
func (w *world) held(s paths.Seat) bool {
	w.t.Helper()
	root, err := homefs.Open(w.layout.State(), homefs.Private)
	mustDo(w.t, err)
	defer func() { mustDo(w.t, root.Close()) }()
	busy, err := lock.Probe(root, s.Lock())
	mustDo(w.t, err)
	return busy
}

// writeDesired places desired.json as the host does: a fresh file renamed into place.
func (w *world) writeDesired(body string) {
	w.t.Helper()
	tmp := w.desired + "/.desired.tmp"
	mustDo(w.t, os.WriteFile(tmp, []byte(body), 0o600))
	mustDo(w.t, os.Rename(tmp, w.desired+"/desired.json"))
}

// changed tells the keeper the desired file changed, as inotify would.
func (w *world) changed() {
	w.watch.events <- WatchEvent{Name: "desired.json", Op: unix.IN_MOVED_TO}
}

// desiredJSON is a desired.json with the given seats ("team/seat") and inbound.
func desiredJSON(gen int, inbound string, seats ...string) string {
	list := make([]string, 0, len(seats))
	for _, s := range seats {
		team, seat, ok := strings.Cut(s, "/")
		if !ok {
			panic("a desired seat is team/seat: " + s)
		}
		list = append(list, fmt.Sprintf(`{"team":%q,"seat":%q}`, team, seat))
	}
	return fmt.Sprintf(`{"v":1,"session":"sess-a","generation":%d,"written_ms":%d,"seats":[%s],"farewell":[],"inbound":%q}`,
		gen, wallStart, strings.Join(list, ","), inbound)
}

func desiredFarewell(gen int, team, seat, reason string, seats ...string) string {
	doc := desiredJSON(gen, state.InboundAccept, seats...)
	return strings.Replace(doc, `"farewell":[]`, fmt.Sprintf(`"farewell":[{"team":%q,"seat":%q,"reason":%q}]`, team, seat, reason), 1)
}

// advance moves the clock once n timers are armed.
func (w *world) advance(n int, d time.Duration) {
	w.t.Helper()
	mustDo(w.t, w.clk.BlockUntil(context.Background(), n))
	w.clk.Advance(d)
}

// child is an in-process CMD or link: the test ends it, and a SIGKILL ends it at once.
type child struct {
	pid  int
	cmd  execx.Cmd
	sigs chan syscall.Signal
	done chan struct{}
	once sync.Once
	st   execx.ExitStatus
	onEx func()
}

func (c *child) Pid() int { return c.pid }

func (c *child) Wait() (execx.ExitStatus, error) {
	<-c.done
	return c.st, nil
}

func (c *child) Signal(sig syscall.Signal) error {
	c.sigs <- sig
	if sig == syscall.SIGKILL {
		c.exit(execx.ExitStatus{Code: 128 + int(sig), Signal: sig})
	}
	return nil
}

func (c *child) exit(st execx.ExitStatus) {
	c.once.Do(func() {
		if c.onEx != nil {
			c.onEx()
		}
		c.st = st
		close(c.done)
	})
}

func (c *child) code(n int) { c.exit(execx.ExitStatus{Code: n}) }

// signal is the next signal the keeper sent the child.
func (c *child) signal() syscall.Signal { return <-c.sigs }

// quiet fails the test if the keeper sent the child a signal.
func (c *child) quiet(t *testing.T) {
	t.Helper()
	select {
	case s := <-c.sigs:
		t.Fatalf("the child was sent %v", s)
	default:
	}
}

// env is the value the keeper added to the child's environment for name, "<unset>" for none.
func (c *child) env(name string) string {
	for _, kv := range c.cmd.Env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			return v
		}
	}
	return "<unset>"
}

// fakeLink is the link's end: its control socket, closed when it exits as a process's would be.
type fakeLink struct {
	*child

	conn *wire.Conn
}

func (l *fakeLink) recv(t *testing.T) (wire.Msg, *os.File) {
	t.Helper()
	m, fd, err := l.conn.Recv()
	mustDo(t, err)
	return m, fd
}

// expect is the next message, which must be of kind; an add's descriptor is closed.
func (l *fakeLink) expect(t *testing.T, kind string) wire.Msg {
	t.Helper()
	m, fd := l.recv(t)
	if fd != nil {
		mustDo(t, fd.Close())
	}
	if m.Kind != kind {
		t.Fatalf("the link was sent %s, want %s", m, kind)
	}
	return m
}

func (l *fakeLink) send(t *testing.T, m wire.Msg) {
	t.Helper()
	mustDo(t, l.conn.Send(m, nil))
}

func (l *fakeLink) state(t *testing.T, team, handle string) {
	t.Helper()
	l.send(t, wire.Msg{Kind: wire.KindState, State: &wire.State{
		Team: mustTeam(t, team), Handle: handle, Delivery: state.DeliverySocket, Store: state.StoreOK,
	}})
}

type procTable struct {
	mu    sync.Mutex
	ticks map[int]int64
}

func (p *procTable) StartTicks(pid int) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t, ok := p.ticks[pid]
	if !ok {
		return 0, fs.ErrNotExist
	}
	return t, nil
}

func (p *procTable) set(pid int, ticks int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ticks[pid] = ticks
}

type fakePidfds struct {
	mu   sync.Mutex
	open map[int]*fakePidfd
	err  error
}

func (f *fakePidfds) Open(pid int) (Pidfd, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	p := &fakePidfd{sigs: make(chan syscall.Signal, 16), exited: make(chan struct{})}
	f.open[pid] = p
	return p, nil
}

func (f *fakePidfds) get(pid int) *fakePidfd {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.open[pid]
}

type fakePidfd struct {
	sigs   chan syscall.Signal
	exited chan struct{}
	once   sync.Once
}

func (p *fakePidfd) Signal(sig syscall.Signal) error {
	p.sigs <- sig
	if sig == syscall.SIGKILL {
		p.exit()
	}
	return nil
}

func (p *fakePidfd) Exited() <-chan struct{} { return p.exited }
func (p *fakePidfd) Close() error            { return nil }

func (p *fakePidfd) exit() { p.once.Do(func() { close(p.exited) }) }

// fakeWatcher hands out events; the test sends on events from its own goroutine, and end and
// fail change what Watch gives under mu.
type fakeWatcher struct {
	mu     sync.Mutex
	events chan WatchEvent
	dirs   []string
	err    error
}

func (f *fakeWatcher) Watch(dir string) (<-chan WatchEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dirs = append(f.dirs, dir)
	if f.err != nil {
		return nil, f.err
	}
	return f.events, nil
}

func (f *fakeWatcher) Close() error { return nil }

// end closes the watch's channel, as a watch that ends does; a later Watch gets a fresh one.
func (f *fakeWatcher) end() {
	f.mu.Lock()
	defer f.mu.Unlock()
	close(f.events)
	f.events = make(chan WatchEvent, 16)
}

// fail makes Watch refuse with err, or watch again with nil.
func (f *fakeWatcher) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// watches is how many times Watch was called.
func (f *fakeWatcher) watches() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.dirs)
}

// syncBuffer is a buffer the keeper writes while the test reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// holder is a lock another session holds: a description of its own, flocked exclusively.
type holder struct{ f *os.File }

// holdFile holds the lock file full below the root at rootPath.
func holdFile(t *testing.T, rootPath, full string) *holder {
	t.Helper()
	root, err := homefs.Open(rootPath, homefs.Private)
	mustDo(t, err)
	defer func() { mustDo(t, root.Close()) }()
	f, err := root.OpenLock(full)
	mustDo(t, err)
	mustDo(t, unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB))
	return &holder{f: f}
}

// holdSeat holds s's seat lock as the run holderRun, its record written as a keeper's is.
func holdSeat(t *testing.T, w *world, s paths.Seat, holderRun string) *holder {
	t.Helper()
	h := holdFile(t, w.layout.State(), s.Lock())
	if holderRun != "" {
		rec := `{"v":1,"run":"` + holderRun + `","started_ms":1}`
		_, err := h.f.WriteAt([]byte(rec), 0)
		mustDo(t, err)
	}
	return h
}

func (h *holder) release(t *testing.T) {
	t.Helper()
	mustDo(t, h.f.Close())
}

// pollGap is how often a barrier looks again; bound caps the whole wait.
const pollGap = 5 * time.Millisecond

// eventually waits until cond holds: the barrier between the keeper's sources, which the loop
// takes in no fixed order.
func (w *world) eventually(what string, cond func() bool) {
	w.t.Helper()
	for range int(bound / pollGap) {
		if cond() {
			return
		}
		time.Sleep(pollGap)
	}
	w.t.Fatalf("never: %s", what)
}

// logged waits until keeper.log holds sub n times.
func (w *world) logged(sub string, n ...int) {
	w.t.Helper()
	want := 1
	if len(n) == 1 {
		want = n[0]
	}
	run := w.runDir()
	w.eventually("keeper.log says "+sub, func() bool { return w.seen(run, sub, want) })
}

// seen is whether run's keeper.log holds sub want times, as logged reads it; it runs on any
// goroutine, so a read that fails is not seen.
func (w *world) seen(run paths.Run, sub string, want int) bool {
	data, err := os.ReadFile(run.KeeperLog())
	return err == nil && strings.Count(string(data), sub) >= want
}

// claude is the link naming Claude Code as pid, a process apart from CMD, and the pidfd the
// keeper holds it by.
func (w *world) claude(l *fakeLink) *fakePidfd {
	w.t.Helper()
	pid := claudePID
	w.proc.set(pid, int64(pid)*10)
	l.send(w.t, wire.Msg{Kind: wire.KindClaude, Claude: &state.Process{PID: pid, StartTicks: int64(pid) * 10}})
	w.logged(fmt.Sprintf("Claude Code %d is held by a pidfd", pid))
	return w.pidfds.get(pid)
}

// expectSig fails unless the next signal on sigs is want.
func expectSig(t *testing.T, sigs <-chan syscall.Signal, want syscall.Signal) {
	t.Helper()
	if got := <-sigs; got != want {
		t.Fatalf("sent %v, want %v", got, want)
	}
}

// session is a running plain launch.
type session struct {
	w   *world
	l   *fakeLink
	c   *child
	res <-chan term.ExitCode
	run paths.Run
}

// startPlain runs cfg's launch to CMD's start, with no seat.
func (w *world) startPlain(cfg Config) *session {
	w.t.Helper()
	res := w.startWith(cfg)
	l := w.up(len(w.launch.Seats))
	c := <-w.cmds
	l.expect(w.t, wire.KindPID)
	w.sessionStarted(l)
	return &session{w: w, l: l, c: c, res: res, run: w.runDir()}
}

// finish ends the session by CMD's exit with code.
func (s *session) finish(code int) {
	s.w.t.Helper()
	s.c.code(code)
	s.l.expect(s.w.t, wire.KindEnd)
	s.l.code(0)
	exitOf(s.w.t, s.res, term.ExitCode(code))
}

// logOf is run's keeper.log.
func (w *world) logOf(run paths.Run) string {
	w.t.Helper()
	data, err := os.ReadFile(run.KeeperLog())
	mustDo(w.t, err)
	return string(data)
}

// mustClose closes c, which must not fail: a fake's descriptor.
func mustClose(c interface{ Close() error }) {
	if err := c.Close(); err != nil {
		panic(err)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func mustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func mustTeam(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	mustDo(t, err)
	return id
}

func exitOf(t *testing.T, res <-chan term.ExitCode, want term.ExitCode) {
	t.Helper()
	if got := <-res; got != want {
		t.Fatalf("run exited %d, want %d", got, want)
	}
}

func hasArgs(c execx.Cmd, want ...string) bool {
	return slices.Equal(append([]string{c.Name}, c.Args...), want)
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}
