package state

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The L12 proofs: every lock against a second real process, both directions. A peer is this
// test binary re-run as a child (TestLockChild) or a v1 process (testdata/v1locks.py running
// helpers/pingbus's own lock code from this checkout). Both speak one line protocol: `pid <n>`
// first; a lock taken is `held` (`held:<previous>` for a path lock) and kept until a line on
// stdin; refused is `busy [<holder>]`; a blocking taker says `waiting` before it asks.

const lockChildEnv = "BUS_STATE_LOCK_CHILD"

// waiterSpin bounds the look for a v1 process blocked in flock: v1 blocks in the kernel and
// says nothing, so /proc/locks is the one witness.
const waiterSpin = 20000

type child struct {
	name   string
	in     *os.File
	lines  chan string
	done   chan struct{}
	err    error // the run's outcome, set before done closes
	pid    string
	kernel bool // a waiter blocked in flock(2), seen in /proc/locks, not by a `blocked` line
	stderr lockedBuffer
}

type peer struct {
	name  string
	start func(t *testing.T, args ...string) *child
	// watchArg is what this peer names the session by: the socket (v1) or its key (Go).
	watchArg func(socket string) string
}

func peers(t *testing.T) []peer {
	t.Helper()
	root := checkoutRoot(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return []peer{
		{name: "go", watchArg: sessionKey, start: func(t *testing.T, args ...string) *child {
			t.Helper()
			return startChild(t, "go", false, execx.Cmd{
				Name: self, Args: append([]string{"-test.run=^TestLockChild$", "--"}, args...),
				Env: []string{lockChildEnv + "=1"},
			})
		}},
		{name: "v1", watchArg: func(socket string) string { return socket }, start: func(t *testing.T, args ...string) *child {
			t.Helper()
			script := filepath.Join(root, "src-go", "internal", "bus", "state", "testdata", "v1locks.py")
			return startChild(t, "v1", true, execx.Cmd{
				Name: "python3", Args: append([]string{"-I", script, root}, args...),
			})
		}},
	}
}

func sessionKey(socket string) string {
	sum := sha256.Sum256([]byte(socket))
	return hex.EncodeToString(sum[:])[:16]
}

func checkoutRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "helpers", "pingbus", "inbox.py")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no helpers/pingbus/inbox.py above the test directory")
		}
		dir = parent
	}
}

// startChild runs cmd with its stdin and stdout on OS pipes, so the runner copies nothing and
// returns as soon as the child exits; the child's exit closes this side's stdout reader.
func startChild(t *testing.T, name string, kernel bool, cmd execx.Cmd) *child {
	t.Helper()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	c := &child{name: name, in: inW, lines: make(chan string, 16), done: make(chan struct{}), kernel: kernel}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, &c.stderr
	go func() {
		_, runErr := execx.OS{}.Run(context.Background(), cmd)
		c.err = errors.Join(runErr, inR.Close(), outW.Close())
		close(c.done)
	}()
	go func() {
		scan := bufio.NewScanner(outR)
		for scan.Scan() {
			c.lines <- scan.Text()
		}
		close(c.lines)
	}()
	t.Cleanup(func() {
		closeErr := inW.Close()
		c.wait()
		if err := errors.Join(c.err, closeErr, outR.Close()); err != nil {
			t.Errorf("%s peer: %v\nstderr: %s", name, err, c.stderr.String())
		}
	})
	c.pid = strings.TrimPrefix(c.expect(t, "pid "), "pid ")
	return c
}

// wait drains the child's output until it exits.
func (c *child) wait() {
	for range c.lines {
		continue
	}
	<-c.done
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// expect is the peer's next line, which must start with prefix.
func (c *child) expect(t *testing.T, prefix string) string {
	t.Helper()
	line, ok := <-c.lines
	if !ok {
		<-c.done
		t.Fatalf("%s peer ended before %q: %v\nstderr: %s", c.name, prefix, c.err, c.stderr.String())
	}
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("%s peer said %q, want %q\nstderr: %s", c.name, line, prefix, c.stderr.String())
	}
	return line
}

func (c *child) say(t *testing.T, word string) {
	t.Helper()
	if _, err := io.WriteString(c.in, word+"\n"); err != nil {
		t.Fatal(err)
	}
}

// awaitBlocked returns once the peer is waiting for a lock this process holds.
func (c *child) awaitBlocked(t *testing.T) {
	t.Helper()
	c.expect(t, "waiting")
	if !c.kernel {
		c.expect(t, "blocked")
		return
	}
	for range waiterSpin {
		raw, err := os.ReadFile("/proc/locks")
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.Lines(string(raw)) {
			if f := strings.Fields(line); len(f) > 5 && f[1] == "->" && f[5] == c.pid {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%s peer %s never showed as a flock waiter in /proc/locks (a procfs of another pid namespace?)",
		c.name, c.pid)
}

// pathRole is one path lock: how a peer names it and how this process takes and probes it.
type pathRole struct {
	name  string
	kind  Kind
	args  func(p peer, dir string) []string
	take  func(l Locks, ctx context.Context, dir string) (*PathLock, error)
	probe func(dir string) (Kind, bool, error)
}

const proofSocket = "/run/user/1000/ccy/session.sock"

func pathRoles() []pathRole {
	key := sessionKey(proofSocket)
	syncRole := func(kind Kind) pathRole {
		return pathRole{
			name: "sync-" + string(kind), kind: kind,
			args: func(_ peer, dir string) []string { return []string{"sync-" + string(kind), dir} },
			take: func(l Locks, ctx context.Context, dir string) (*PathLock, error) {
				return l.Sync(ctx, dir, kind)
			},
			probe: ProbeSync,
		}
	}
	return []pathRole{
		syncRole(KindWatch), syncRole(KindWait), syncRole(KindRecv),
		{
			name: "seat", kind: KindSeat,
			args: func(_ peer, dir string) []string { return []string{"seat", dir, "1781000000000"} },
			take: func(l Locks, ctx context.Context, dir string) (*PathLock, error) {
				return l.Seat(ctx, dir, 1781000000001)
			},
			probe: ProbeSeat,
		},
		{
			name: "keeper", kind: KindKeeper,
			args: func(_ peer, dir string) []string { return []string{"keeper", dir} },
			take: func(l Locks, ctx context.Context, dir string) (*PathLock, error) {
				return l.Keeper(ctx, dir)
			},
			probe: ProbeKeeper,
		},
		{
			name: "watch", kind: KindWatch,
			args: func(p peer, dir string) []string { return []string{"watch", dir, p.watchArg(proofSocket)} },
			take: func(l Locks, ctx context.Context, dir string) (*PathLock, error) {
				return l.Watch(ctx, dir, key)
			},
			probe: func(dir string) (Kind, bool, error) { return ProbeWatch(dir, key) },
		},
	}
}

// L1, L2, L3, L11: each path lock excludes a peer and is excluded by one, names the holder's
// kind, and a release leaves the file empty for the next taker.
func TestEveryPathLockExcludesAndIsExcludedByAnotherProcess(t *testing.T) {
	for _, p := range peers(t) {
		for _, role := range pathRoles() {
			t.Run(p.name+"/"+role.name, func(t *testing.T) { provePathLock(t, p, role) })
		}
	}
}

func provePathLock(t *testing.T, p peer, role pathRole) {
	t.Helper()
	dir := t.TempDir()
	held := p.start(t, append([]string{"path"}, role.args(p, dir)...)...)
	held.expect(t, "held:")
	_, err := role.take((&sleeper{}).locks(), t.Context(), dir)
	if busy, ok := errors.AsType[*BusyError](err); !ok || busy.Holder != role.kind {
		t.Fatalf("with the %s peer holding: %v, want busy by %s", p.name, err, role.kind)
	}
	if kind, isHeld, probeErr := role.probe(dir); probeErr != nil || !isHeld || kind != role.kind {
		t.Errorf("probe of the peer's lock: %q %v %v", kind, isHeld, probeErr)
	}
	held.say(t, "release")
	held.expect(t, "released")
	lock, err := role.take((&sleeper{}).locks(), t.Context(), dir)
	if err != nil {
		t.Fatalf("after the peer released: %v", err)
	}
	if lock.Previous() != "" {
		t.Errorf("the peer's release left %q", lock.Previous())
	}
	refused := p.start(t, append([]string{"path"}, role.args(p, dir)...)...)
	if line := refused.expect(t, "busy"); line != "busy "+string(role.kind) {
		t.Errorf("the peer refused with %q", line)
	}
	probe := p.start(t, append([]string{"probe"}, role.args(p, dir)...)...)
	if line := probe.expect(t, "held"); line != "held "+string(role.kind) {
		t.Errorf("the peer's probe said %q", line)
	}
	release(t, lock)
	p.start(t, append([]string{"probe"}, role.args(p, dir)...)...).expect(t, "free")
}

// L2, L6: a v1 holder that dies leaves its claim, which v2 reads and sees as the previous text.
func TestADeadV1SeatHolderLeavesItsClaimForV2(t *testing.T) {
	v1 := peers(t)[1]
	dir := t.TempDir()
	held := v1.start(t, "path", "seat", dir, "1781000000000")
	held.expect(t, "held:")
	held.say(t, "die")
	held.wait()
	if claim, ok, err := ReadClaim(dir); err != nil || !ok || claim != 1781000000000 {
		t.Fatalf("claim %d %v %v", claim, ok, err)
	}
	lock, err := (&sleeper{}).locks().Seat(t.Context(), dir, 1781000000002)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Previous() != "seat 1781000000000" {
		t.Errorf("previous %q", lock.Previous())
	}
	release(t, lock)
	if line := v1.start(t, "claim", dir).expect(t, ""); line != "none" {
		t.Errorf("v1 reads the released lock's claim as %q", line)
	}
	lock, err = (&sleeper{}).locks().Seat(t.Context(), dir, 1781000000003)
	if err != nil {
		t.Fatal(err)
	}
	defer release(t, lock)
	if line := v1.start(t, "claim", dir).expect(t, ""); line != "1781000000003" {
		t.Errorf("v1 reads v2's claim as %q", line)
	}
}

// releaseOnFirstWait is a wait that tells the peer to let go the first time this process waits.
func releaseOnFirstWait(t *testing.T, c *child) *sleeper {
	t.Helper()
	var once sync.Once
	return &sleeper{onWait: func(int) {
		once.Do(func() { c.say(t, "release") })
		if err := sleepFor(context.Background(), time.Millisecond); err != nil {
			t.Error(err)
		}
	}}
}

// L7, L8: the blocking locks wait for a peer holder and hold off a peer taker until released.
func TestTheOutboxAndDeliveryLocksBlockAndAreBlockedByAnotherProcess(t *testing.T) {
	type blocking struct {
		name, command string
		where         func(home, stateDir string) string
		with          func(l Locks, ctx context.Context, home, stateDir string, fn func(context.Context) error) error
	}
	for _, p := range peers(t) {
		for _, b := range []blocking{
			{
				"outbox", "outbox", func(_, stateDir string) string { return stateDir },
				func(l Locks, ctx context.Context, _, stateDir string, fn func(context.Context) error) error {
					return l.WithOutbox(ctx, stateDir, fn)
				},
			},
			{
				"delivery", "delivery", func(home, _ string) string { return home },
				func(l Locks, ctx context.Context, home, _ string, fn func(context.Context) error) error {
					return l.WithDelivery(ctx, home, fn)
				},
			},
			{
				"pair-delivery", "delivery", func(home, _ string) string { return home },
				func(l Locks, ctx context.Context, home, stateDir string, fn func(context.Context) error) error {
					return l.WithDeliveryAndOutbox(ctx, home, stateDir, fn)
				},
			},
			{
				"pair-outbox", "outbox", func(_, stateDir string) string { return stateDir },
				func(l Locks, ctx context.Context, home, stateDir string, fn func(context.Context) error) error {
					return l.WithDeliveryAndOutbox(ctx, home, stateDir, fn)
				},
			},
		} {
			t.Run(p.name+"/"+b.name, func(t *testing.T) {
				home := t.TempDir()
				stateDir := filepath.Join(home, "team", "state")
				if err := os.MkdirAll(stateDir, 0o700); err != nil {
					t.Fatal(err)
				}
				held := p.start(t, b.command, b.where(home, stateDir))
				held.expect(t, "waiting")
				held.expect(t, "held")
				s := releaseOnFirstWait(t, held)
				ran := false
				if err := b.with(s.locks(), t.Context(), home, stateDir, func(context.Context) error {
					ran = true
					return nil
				}); err != nil || !ran {
					t.Fatalf("after the peer let go: %v ran %v", err, ran)
				}
				if s.count() == 0 {
					t.Fatal("this process never waited for the peer")
				}
				held.expect(t, "released")
				var waiter *child
				if err := b.with((&sleeper{}).locks(), t.Context(), home, stateDir, func(context.Context) error {
					waiter = p.start(t, b.command, b.where(home, stateDir))
					waiter.awaitBlocked(t)
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				waiter.expect(t, "held")
				waiter.say(t, "release")
				waiter.expect(t, "released")
			})
		}
	}
}

// L8 non-blocking, L9: the one-try locks refuse a peer and are refused by one.
func TestTheOneTryLocksRefuseAndAreRefusedByAnotherProcess(t *testing.T) {
	for _, p := range peers(t) {
		t.Run(p.name+"/try-delivery", func(t *testing.T) {
			home := t.TempDir()
			held := p.start(t, "try-delivery", home)
			held.expect(t, "held")
			err := (&sleeper{}).locks().TryDelivery(t.Context(), home, func(context.Context) error {
				t.Error("ran while the peer holds the delivery lock")
				return nil
			})
			if !errors.Is(err, ErrBusy) {
				t.Errorf("with the peer holding: %v", err)
			}
			held.say(t, "release")
			held.expect(t, "released")
			if err = (&sleeper{}).locks().TryDelivery(t.Context(), home, func(context.Context) error {
				p.start(t, "try-delivery", home).expect(t, "busy")
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
		t.Run(p.name+"/told", func(t *testing.T) {
			home := t.TempDir()
			held := p.start(t, "told", home)
			held.expect(t, "held")
			ran, err := WithTold(home, func() error {
				t.Error("told while the peer tells")
				return nil
			})
			if ran || err != nil {
				t.Errorf("with the peer telling: %v %v", ran, err)
			}
			held.say(t, "release")
			held.expect(t, "released")
			ran, err = WithTold(home, func() error {
				p.start(t, "told", home).expect(t, "busy")
				return nil
			})
			if !ran || err != nil {
				t.Errorf("after the peer: %v %v", ran, err)
			}
		})
	}
}

// L5: a lock this process holds is not inherited by a program it starts, unless handed over.
func TestALockReachesAChildOnlyWhenHandedOver(t *testing.T) {
	root := checkoutRoot(t)
	dir := t.TempDir()
	lock, err := (&sleeper{}).locks().Sync(t.Context(), dir, KindWatch)
	if err != nil {
		t.Fatal(err)
	}
	defer release(t, lock)
	path := filepath.Join(dir, "lock")
	script := filepath.Join(root, "src-go", "internal", "bus", "state", "testdata", "v1locks.py")
	for files, want := range map[int]string{0: "none", 1: "inherited"} {
		cmd := execx.Cmd{Name: "python3", Args: []string{"-I", script, root, "fds", path}}
		if files == 1 {
			cmd.ExtraFiles = []*os.File{lock.File()}
		}
		res, runErr := execx.OS{}.Run(t.Context(), cmd)
		if runErr != nil {
			t.Fatal(runErr)
		}
		lines := strings.Split(strings.TrimSpace(string(res.Stdout)), "\n")
		if got := lines[len(lines)-1]; got != want {
			t.Errorf("with %d files handed over the child says %q, want %q", files, got, want)
		}
	}
}

// L2: the explicit unlock in Release frees the lock for every copy of the descriptor, so a lock
// handed to a child that is still alive is free to v1 and Go takers once this process releases.
func TestAReleaseFreesTheLockForEveryCopyOfItsDescriptor(t *testing.T) {
	root := checkoutRoot(t)
	script := filepath.Join(root, "src-go", "internal", "bus", "state", "testdata", "v1locks.py")
	for _, p := range peers(t) {
		t.Run(p.name, func(t *testing.T) {
			dir := t.TempDir()
			lock, err := (&sleeper{}).locks().Sync(t.Context(), dir, KindWatch)
			if err != nil {
				t.Fatal(err)
			}
			// The child gets a copy of the lock's description through a duplicate this test closes
			// once the child is gone, so the runner's read of it and Release's close never meet.
			dupFD, err := unix.Dup(int(lock.File().Fd()))
			if err != nil {
				t.Fatal(err)
			}
			handed := os.NewFile(uintptr(dupFD), "handed")
			keeper := startChild(t, "keeper", true, execx.Cmd{
				Name: "python3", Args: []string{"-I", script, root, "keep"},
				ExtraFiles: []*os.File{handed},
			})
			keeper.expect(t, "kept")
			release(t, lock)
			taker := p.start(t, "path", "sync-recv", dir)
			taker.expect(t, "held:")
			taker.say(t, "release")
			taker.expect(t, "released")
			keeper.say(t, "done")
			keeper.wait()
			if err = handed.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestLockChild is the Go peer: this test binary re-run by the proofs above.
func TestLockChild(t *testing.T) {
	if os.Getenv(lockChildEnv) == "" {
		t.Skip("the Go peer of the lock proofs; runs only as their child process")
	}
	out := &childOut{w: os.Stdout}
	in := bufio.NewReader(os.Stdin)
	out.say("pid", strconv.Itoa(os.Getpid()))
	if err := runLockChild(t.Context(), flag.Args(), in, out); err != nil {
		t.Fatal(err)
	}
	if out.err != nil {
		t.Fatal(out.err)
	}
}

type childOut struct {
	w   io.Writer
	err error
}

func (o *childOut) say(words ...string) {
	if _, err := fmt.Fprintln(o.w, strings.Join(words, " ")); err != nil && o.err == nil {
		o.err = err
	}
}

func awaitLine(in *bufio.Reader) error {
	_, err := in.ReadString('\n')
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func runLockChild(ctx context.Context, args []string, in *bufio.Reader, out *childOut) error {
	if len(args) < 2 {
		return fmt.Errorf("lock child: arguments %q", args)
	}
	var once sync.Once
	waits := Locks{Sleep: func(ctx context.Context, d time.Duration) error {
		once.Do(func() { out.say("blocked") })
		return sleepFor(ctx, d)
	}}
	holdFor := func(context.Context) error {
		out.say("held")
		return awaitLine(in)
	}
	switch command, where := args[0], args[1]; command {
	case "path", "probe":
		return childPath(ctx, command, args[1:], in, out)
	case "outbox":
		out.say("waiting")
		return released(out, waits.WithOutbox(ctx, where, holdFor))
	case "delivery":
		out.say("waiting")
		return released(out, waits.WithDelivery(ctx, where, holdFor))
	case "try-delivery":
		err := waits.TryDelivery(ctx, where, holdFor)
		if errors.Is(err, ErrBusy) {
			out.say("busy")
			return nil
		}
		return released(out, err)
	case "told":
		ran, err := WithTold(where, func() error { return holdFor(ctx) })
		if !ran && err == nil {
			out.say("busy")
			return nil
		}
		return released(out, err)
	default:
		return fmt.Errorf("lock child: no command %q", command)
	}
}

func released(out *childOut, err error) error {
	if err == nil {
		out.say("released")
	}
	return err
}

func childPath(ctx context.Context, command string, args []string, in *bufio.Reader, out *childOut) error {
	for _, role := range pathRoles() {
		if role.name != args[0] {
			continue
		}
		dir := args[1]
		if command == "probe" {
			kind, held, err := role.probe(dir)
			if held {
				out.say("held", string(kind))
			} else {
				out.say("free")
			}
			return err
		}
		lock, err := role.take(Locks{Sleep: func(context.Context, time.Duration) error { return nil }}, ctx, dir)
		if busy, ok := errors.AsType[*BusyError](err); ok {
			out.say("busy", string(busy.Holder))
			return nil
		}
		if err != nil {
			return err
		}
		out.say("held:" + lock.Previous())
		if err = awaitLine(in); err != nil {
			return errors.Join(err, lock.Release())
		}
		return released(out, lock.Release())
	}
	return fmt.Errorf("lock child: no lock role %q", args[0])
}
