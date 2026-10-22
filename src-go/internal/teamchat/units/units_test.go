package units

import (
	"context"
	"errors"
	"io"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const (
	systemdOwner = ":1.7"
	jobPath      = dbus.ObjectPath("/org/freedesktop/systemd1/job/42")
	unitPath     = dbus.ObjectPath("/org/freedesktop/systemd1/unit/teamchat_2dserver_40alpha_2eservice")
)

func testUnit(t *testing.T) string {
	t.Helper()
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	return paths.ServerUnit(team)
}

// fakeCall is one method call the adapter made.
type fakeCall struct {
	dest   string
	path   dbus.ObjectPath
	method string
	args   []any
}

type answerFunc func(c fakeCall) ([]any, error)

// fakeConn is a scripted Conn. Each method name is answered by its script; the bus's own
// setup calls have defaults. Safe for concurrent use.
type fakeConn struct {
	mu      sync.Mutex
	script  map[string]answerFunc
	calls   []fakeCall
	signals chan<- *dbus.Signal
	closed  bool
	closes  int
	closeEr error
}

func newFakeConn() *fakeConn {
	f := &fakeConn{script: map[string]answerFunc{}}
	f.on(busAddMatch, func(fakeCall) ([]any, error) { return nil, nil })
	f.on(busGetNameOwner, func(fakeCall) ([]any, error) { return []any{systemdOwner}, nil })
	f.on(managerSubscribe, func(fakeCall) ([]any, error) { return nil, nil })
	f.on(managerReload, func(fakeCall) ([]any, error) { return nil, nil })
	return f
}

func (f *fakeConn) Call(_ context.Context, dest string, path dbus.ObjectPath, method string, args ...any) ([]any, error) {
	c := fakeCall{dest: dest, path: path, method: method, args: args}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	a, ok := f.script[method]
	f.mu.Unlock()
	if !ok {
		return nil, errors.New("fake: unscripted method " + method)
	}
	return a(c)
}

func (f *fakeConn) Signal(ch chan<- *dbus.Signal) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signals = ch
}

func (f *fakeConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	if !f.closed {
		f.closed = true
		if f.signals != nil {
			close(f.signals)
		}
	}
	return f.closeEr
}

func (f *fakeConn) on(method string, a answerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.script[method] = a
}

// emit delivers a signal as the bus would.
func (f *fakeConn) emit(s *dbus.Signal) {
	f.mu.Lock()
	ch := f.signals
	f.mu.Unlock()
	ch <- s
}

func (f *fakeConn) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.method)
	}
	return out
}

func (f *fakeConn) call(t *testing.T, method string) fakeCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.method == method {
			return c
		}
	}
	t.Fatalf("no call of %s in %v", method, f.calls)
	return fakeCall{}
}

func jobSignal(sender string, job dbus.ObjectPath, unit, result string) *dbus.Signal {
	return &dbus.Signal{
		Sender: sender, Path: managerPath, Name: jobRemovedSignal,
		Body: []any{uint32(42), job, unit, result},
	}
}

// rig is a Manager over one fake connection, with its hooks turned into channels.
type rig struct {
	m        *Manager
	conn     *fakeConn
	run      *execx.Fake
	dials    int
	dialErr  error
	handled  chan *dbus.Signal
	waiting  chan dbus.ObjectPath
	nextConn func() *fakeConn
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{
		conn: newFakeConn(), run: &execx.Fake{},
		handled: make(chan *dbus.Signal, 16), waiting: make(chan dbus.ObjectPath, 16),
	}
	r.m = NewManager(func() (Conn, error) {
		r.dials++
		if r.dialErr != nil {
			return nil, r.dialErr
		}
		if r.dials > 1 && r.nextConn != nil {
			r.conn = r.nextConn()
		}
		return r.conn, nil
	}, r.run)
	r.m.onSignal = func(s *dbus.Signal) { r.handled <- s }
	r.m.onWait = func(p dbus.ObjectPath) { r.waiting <- p }
	t.Cleanup(func() {
		if err := r.m.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return r
}

// jobAnswers scripts a job method to answer jobPath and to deliver result's signal first
// (before the reply) or last (once the adapter waits on the job).
func (r *rig) jobAnswers(method, unit, result string, before bool) {
	r.conn.on(method, func(fakeCall) ([]any, error) {
		if before {
			r.conn.emit(jobSignal(systemdOwner, jobPath, unit, result))
			<-r.handled
		} else {
			go func() {
				<-r.waiting
				r.conn.emit(jobSignal(systemdOwner, jobPath, unit, result))
			}()
		}
		return []any{jobPath}, nil
	})
}

func TestJobsWaitForTheirJobRemoved(t *testing.T) {
	type verb struct {
		name, method string
		run          func(ctx context.Context, m *Manager, unit string) error
	}
	verbs := []verb{
		{"start", managerStartUnit, func(ctx context.Context, m *Manager, u string) error { return m.Host().Start(ctx, u) }},
		{"restart", managerRestartUnit, func(ctx context.Context, m *Manager, u string) error { return m.Host().Restart(ctx, u) }},
		{"stop", managerStopUnit, func(ctx context.Context, m *Manager, u string) error { return m.Host().Stop(ctx, u) }},
		{"restart", managerRestartUnit, func(ctx context.Context, m *Manager, u string) error { return m.Health().Restart(ctx, u) }},
	}
	for _, v := range verbs {
		for _, before := range []bool{true, false} {
			t.Run(v.method, func(t *testing.T) {
				r := newRig(t)
				unit := testUnit(t)
				r.jobAnswers(v.method, unit, "done", before)
				if err := v.run(t.Context(), r.m, unit); err != nil {
					t.Fatalf("%s before=%v: %v", v.name, before, err)
				}
				checkJobCalls(t, r, v.method, unit)
			})
		}
	}
}

func checkJobCalls(t *testing.T, r *rig, method, unit string) {
	t.Helper()
	c := r.conn.call(t, method)
	if c.dest != destination || c.path != managerPath || len(c.args) != 2 || c.args[0] != unit || c.args[1] != "replace" {
		t.Errorf("call = %+v", c)
	}
	got := strings.Join(r.conn.methods(), " ")
	want := busAddMatch + " " + busAddMatch + " " + busGetNameOwner + " " + managerSubscribe + " " + method
	if got != want {
		t.Errorf("calls = %s, want %s", got, want)
	}
	rule := r.conn.call(t, busAddMatch)
	if rule.dest != busName || rule.path != busPath || len(rule.args) != 1 || rule.args[0] != jobRemovedMatch {
		t.Errorf("match = %+v", rule)
	}
}

func TestTheMatchRuleNamesSystemdAndJobRemoved(t *testing.T) {
	want := "type='signal',sender='org.freedesktop.systemd1',path='/org/freedesktop/systemd1'," +
		"interface='org.freedesktop.systemd1.Manager',member='JobRemoved'"
	if jobRemovedMatch != want {
		t.Errorf("rule = %s", jobRemovedMatch)
	}
}

func TestJobResultsOtherThanDoneAreErrors(t *testing.T) {
	for _, result := range []string{"failed", "timeout", "canceled", "dependency", "skipped", "invalid"} {
		t.Run(result, func(t *testing.T) {
			r := newRig(t)
			unit := testUnit(t)
			r.jobAnswers(managerStartUnit, unit, result, false)
			err := r.m.Host().Start(t.Context(), unit)
			if !errors.Is(err, ErrJobFailed) {
				t.Fatalf("err = %v", err)
			}
			for _, part := range []string{"start", unit, `"` + result + `"`} {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("%q does not name %q", err, part)
				}
			}
		})
	}
}

func TestAnUnprintableJobResultIsNotPrinted(t *testing.T) {
	for _, result := range []string{"bad\nresult", strings.Repeat("x", 257), "\x1b[2J"} {
		r := newRig(t)
		unit := testUnit(t)
		r.jobAnswers(managerStopUnit, unit, result, true)
		err := r.m.Host().Stop(t.Context(), unit)
		if !errors.Is(err, ErrJobFailed) || strings.Contains(err.Error(), result) || !strings.Contains(err.Error(), "an unprintable result") {
			t.Errorf("err = %v", err)
		}
	}
}

func TestSignalsNotFromSystemdOrNotOursAreIgnored(t *testing.T) {
	r := newRig(t)
	unit := testUnit(t)
	other := dbus.ObjectPath("/org/freedesktop/systemd1/job/7")
	r.conn.on(managerStartUnit, func(fakeCall) ([]any, error) {
		go func() {
			<-r.waiting
			r.conn.emit(jobSignal(":1.99", jobPath, unit, "done"))
			r.conn.emit(jobSignal(systemdOwner, other, unit, "done"))
			r.conn.emit(&dbus.Signal{Sender: systemdOwner, Path: managerPath, Name: managerIface + ".UnitNew", Body: []any{unit, unitPath}})
			r.conn.emit(&dbus.Signal{Sender: systemdOwner, Path: "/elsewhere", Name: jobRemovedSignal, Body: []any{uint32(42), jobPath, unit, "done"}})
			r.conn.emit(&dbus.Signal{Sender: systemdOwner, Path: managerPath, Name: jobRemovedSignal, Body: []any{"malformed"}})
			r.conn.emit(jobSignal(systemdOwner, jobPath, unit, "failed"))
		}()
		return []any{jobPath}, nil
	})
	if err := r.m.Host().Start(t.Context(), unit); !errors.Is(err, ErrJobFailed) || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("err = %v, want the systemd signal's failure", err)
	}
}

func TestANewSystemdOwnerIsFollowed(t *testing.T) {
	r := newRig(t)
	unit := testUnit(t)
	r.conn.on(managerRestartUnit, func(fakeCall) ([]any, error) {
		go func() {
			<-r.waiting
			// a peer claiming the owner changed is not the bus, so it is ignored
			r.conn.emit(&dbus.Signal{Sender: ":1.99", Path: busPath, Name: nameOwnerChanged, Body: []any{destination, systemdOwner, ":1.99"}})
			r.conn.emit(&dbus.Signal{Sender: busName, Path: busPath, Name: nameOwnerChanged, Body: []any{"org.example.other", "", ":1.50"}})
			r.conn.emit(&dbus.Signal{Sender: busName, Path: busPath, Name: nameOwnerChanged, Body: []any{1}})
			r.conn.emit(jobSignal(":1.99", jobPath, unit, "failed"))
			r.conn.emit(&dbus.Signal{Sender: busName, Path: busPath, Name: nameOwnerChanged, Body: []any{destination, systemdOwner, ":1.8"}})
			r.conn.emit(jobSignal(systemdOwner, jobPath, unit, "failed"))
			r.conn.emit(jobSignal(":1.8", jobPath, unit, "done"))
		}()
		return []any{jobPath}, nil
	})
	if err := r.m.Host().Restart(t.Context(), unit); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if got := r.conn.call(t, busAddMatch); len(got.args) != 1 {
		t.Errorf("add match args = %v", got.args)
	}
	match := r.conn.calls[0]
	if match.args[0] != jobRemovedMatch {
		t.Errorf("first match = %v", match.args)
	}
	found := false
	r.conn.mu.Lock()
	for _, c := range r.conn.calls {
		if c.method == busAddMatch && c.args[0] == ownerChangedMatch {
			found = true
		}
	}
	r.conn.mu.Unlock()
	if !found {
		t.Errorf("no NameOwnerChanged match in %v", r.conn.calls)
	}
}

func TestAContextThatEndsFirstIsTheError(t *testing.T) {
	r := newRig(t)
	unit := testUnit(t)
	ctx, cancel := context.WithCancel(t.Context())
	r.conn.on(managerStartUnit, func(fakeCall) ([]any, error) {
		go func() {
			<-r.waiting
			cancel()
		}()
		return []any{jobPath}, nil
	})
	err := r.m.Host().Start(ctx, unit)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), unit) {
		t.Fatalf("err = %v", err)
	}
	// the late signal finds no waiter and changes nothing
	r.conn.emit(jobSignal(systemdOwner, jobPath, unit, "done"))
	<-r.handled
	if r.dials != 1 || r.conn.closes != 0 {
		t.Errorf("dials %d closes %d: a context's end is not a lost bus", r.dials, r.conn.closes)
	}
}

func TestAContextEndedInTheCallIsTheError(t *testing.T) {
	r := newRig(t)
	unit := testUnit(t)
	r.conn.on(managerStopUnit, func(fakeCall) ([]any, error) { return nil, context.DeadlineExceeded })
	err := r.m.Host().Stop(t.Context(), unit)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v", err)
	}
	if r.conn.closes != 0 {
		t.Error("the connection was dropped")
	}
}

func TestDBusErrorsAreMappedByClass(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		class error
		text  string
	}{
		{"no such unit", dbus.Error{Name: "org.freedesktop.systemd1.NoSuchUnit", Body: []any{"Unit x not found."}}, ErrNoSuchUnit, "no such unit: Unit x not found."},
		{"denied", dbus.Error{Name: "org.freedesktop.DBus.Error.AccessDenied", Body: []any{"no"}}, ErrNotPermitted, "not permitted: no"},
		{"interactive", &dbus.Error{Name: "org.freedesktop.DBus.Error.InteractiveAuthorizationRequired"}, ErrNotPermitted, "not permitted"},
		{"masked", dbus.Error{Name: "org.freedesktop.systemd1.UnitMasked", Body: []any{"Unit is masked."}}, ErrMasked, "the unit is masked: Unit is masked."},
		{"other", dbus.Error{Name: "org.freedesktop.systemd1.JobTypeNotApplicable", Body: []any{"nope"}}, ErrSystemd, "org.freedesktop.systemd1.JobTypeNotApplicable: nope"},
		{"multi-line body", dbus.Error{Name: "org.example.Bad", Body: []any{"one\ntwo"}}, ErrSystemd, "org.example.Bad"},
		{"long body", dbus.Error{Name: "org.example.Bad", Body: []any{strings.Repeat("y", 257)}}, ErrSystemd, "org.example.Bad"},
		{"non-string body", dbus.Error{Name: "org.example.Bad", Body: []any{uint32(1)}}, ErrSystemd, "org.example.Bad"},
		{"unprintable name", dbus.Error{Name: "bad\nname"}, ErrSystemd, "an unprintable D-Bus error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			unit := testUnit(t)
			r.conn.on(managerStartUnit, func(fakeCall) ([]any, error) { return nil, c.err })
			err := r.m.Host().Start(t.Context(), unit)
			if !errors.Is(err, c.class) {
				t.Fatalf("err = %v, want %v", err, c.class)
			}
			want := "systemd start " + unit + ": " + c.text
			if err.Error() != want {
				t.Errorf("err = %q\nwant  %q", err, want)
			}
			if r.conn.closes != 0 {
				t.Error("a D-Bus error dropped the connection")
			}
		})
	}
}

func TestAnUnreachableBusIsDialledAgain(t *testing.T) {
	r := newRig(t)
	unit := testUnit(t)
	r.dialErr = errors.New("no socket")
	err := r.m.Host().Stop(t.Context(), unit)
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "systemd is not reachable on the system bus") {
		t.Fatalf("err = %v", err)
	}
	r.dialErr = nil
	r.jobAnswers(managerStopUnit, unit, "done", true)
	if err := r.m.Host().Stop(t.Context(), unit); err != nil {
		t.Fatalf("second stop: %v", err)
	}
	if r.dials != 2 {
		t.Errorf("dials = %d", r.dials)
	}
}

func TestALostConnectionIsDroppedAndDialledAgain(t *testing.T) {
	r := newRig(t)
	unit := testUnit(t)
	first := r.conn
	first.on(managerStopUnit, func(fakeCall) ([]any, error) { return nil, io.EOF })
	err := r.m.Host().Stop(t.Context(), unit)
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v", err)
	}
	if first.closes != 1 {
		t.Errorf("closes = %d", first.closes)
	}
	r.nextConn = func() *fakeConn {
		c := newFakeConn()
		return c
	}
	if err := r.m.Host().DaemonReload(t.Context()); err != nil {
		t.Fatalf("reload after the loss: %v", err)
	}
	if r.dials != 2 || r.conn == first {
		t.Errorf("dials = %d", r.dials)
	}
}

func TestADroppedConnectionsCloseErrorIsReported(t *testing.T) {
	r := newRig(t)
	unit := testUnit(t)
	r.conn.closeEr = errors.New("close failed")
	r.conn.on(managerStopUnit, func(fakeCall) ([]any, error) { return nil, io.EOF })
	err := r.m.Host().Stop(t.Context(), unit)
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "close failed") {
		t.Fatalf("err = %v", err)
	}
	r.conn.closeEr = nil
}

func TestAConnectionLostWhileWaitingIsUnreachable(t *testing.T) {
	r := newRig(t)
	unit := testUnit(t)
	first := r.conn
	r.conn.on(managerRestartUnit, func(fakeCall) ([]any, error) {
		go func() {
			<-r.waiting
			if err := first.Close(); err != nil {
				t.Error(err)
			}
		}()
		return []any{jobPath}, nil
	})
	err := r.m.Health().Restart(t.Context(), unit)
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v", err)
	}
	r.nextConn = newFakeConn
	if err := r.m.Host().DaemonReload(t.Context()); err != nil {
		t.Fatal(err)
	}
	if r.dials != 2 {
		t.Errorf("dials = %d", r.dials)
	}
}

func TestALostConnectionBeforeTheReplyIsUnreachable(t *testing.T) {
	r := newRig(t)
	unit := testUnit(t)
	first := r.conn
	r.conn.on(managerStartUnit, func(fakeCall) ([]any, error) {
		first.mu.Lock()
		first.closed = true
		close(first.signals)
		first.mu.Unlock()
		// wait until the dispatcher has seen the loss
		for !r.m.lostForTest() {
			<-r.handled
		}
		return []any{jobPath}, nil
	})
	if err := r.m.Host().Start(t.Context(), unit); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v", err)
	}
}

func TestSetupFailuresCloseTheConnection(t *testing.T) {
	for _, method := range []string{busAddMatch, busGetNameOwner, managerSubscribe} {
		t.Run(method, func(t *testing.T) {
			r := newRig(t)
			r.conn.on(method, func(fakeCall) ([]any, error) {
				return nil, dbus.Error{Name: "org.freedesktop.DBus.Error.AccessDenied"}
			})
			err := r.m.Host().DaemonReload(t.Context())
			if !errors.Is(err, ErrNotPermitted) {
				t.Fatalf("err = %v", err)
			}
			if r.conn.closes != 1 {
				t.Errorf("closes = %d", r.conn.closes)
			}
		})
	}
	t.Run("owner reply", func(t *testing.T) {
		r := newRig(t)
		r.conn.on(busGetNameOwner, func(fakeCall) ([]any, error) { return []any{uint32(3)}, nil })
		if err := r.m.Host().DaemonReload(t.Context()); !errors.Is(err, ErrMalformed) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("close error", func(t *testing.T) {
		r := newRig(t)
		r.conn.closeEr = errors.New("close failed")
		r.conn.on(managerSubscribe, func(fakeCall) ([]any, error) { return nil, errors.New("gone") })
		err := r.m.Host().DaemonReload(t.Context())
		if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "close failed") {
			t.Fatalf("err = %v", err)
		}
		r.conn.closeEr = nil
	})
}

func TestTheConnectionOpensLazilyAndOnce(t *testing.T) {
	r := newRig(t)
	if r.dials != 0 {
		t.Fatal("NewManager dialled")
	}
	for range 2 {
		if err := r.m.Host().DaemonReload(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if r.dials != 1 {
		t.Errorf("dials = %d", r.dials)
	}
	n := 0
	for _, m := range r.conn.methods() {
		if m == managerSubscribe {
			n++
		}
	}
	if n != 1 {
		t.Errorf("subscribed %d times", n)
	}
	if err := r.m.Close(); err != nil {
		t.Fatal(err)
	}
	if r.conn.closes != 1 {
		t.Errorf("closes = %d", r.conn.closes)
	}
	if err := r.m.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestCloseReportsTheConnectionsError(t *testing.T) {
	r := newRig(t)
	if err := r.m.Host().DaemonReload(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.conn.closeEr = errors.New("close failed")
	if err := r.m.Close(); err == nil || !strings.Contains(err.Error(), "close failed") {
		t.Fatalf("close = %v", err)
	}
	r.conn.closeEr = nil
}

func TestABadJobReplyIsMalformed(t *testing.T) {
	r := newRig(t)
	unit := testUnit(t)
	r.conn.on(managerStartUnit, func(fakeCall) ([]any, error) { return []any{"not a path"}, nil })
	if err := r.m.Host().Start(t.Context(), unit); !errors.Is(err, ErrMalformed) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnitNamesAreCheckedBeforeAnyCall(t *testing.T) {
	bad := []string{
		"", "sshd.service", "teamchat-server@.service", "teamchat-server@a b.service",
		"teamchat-server@a.service\n", "teamchat-server@a/b.service", "teamchat-server@a.socket",
		"teamchat-server@" + strings.Repeat("a", 240) + ".service", "teamchat-fence@a.service",
	}
	for _, unit := range bad {
		r := newRig(t)
		ctx := t.Context()
		h, hl := r.m.Host(), r.m.Health()
		_, stateErr := h.State(ctx, unit)
		_, healthErr := hl.State(ctx, unit)
		_, tailErr := h.JournalTail(ctx, unit)
		for i, err := range []error{
			h.Start(ctx, unit), h.Restart(ctx, unit), h.Stop(ctx, unit), h.Enable(ctx, unit),
			h.Disable(ctx, unit), h.CleanState(ctx, unit), stateErr, healthErr, tailErr, hl.Restart(ctx, unit),
		} {
			if !errors.Is(err, ErrUnitName) {
				t.Errorf("%q call %d: err = %v", unit, i, err)
			}
		}
		if r.dials != 0 || len(r.run.Calls()) != 0 {
			t.Errorf("%q reached the bus or a program", unit)
		}
	}
	long := "teamchat-server@" + strings.Repeat("a", 230) + ".service"
	if err := checkUnit(long); err != nil {
		t.Errorf("a %d-byte name is valid: %v", len(long), err)
	}
	if err := checkUnit(`teamchat-server@a\x2db:c_d.service`); err != nil {
		t.Errorf("escaped instance: %v", err)
	}
}

func TestReloadIsTheManagersReload(t *testing.T) {
	r := newRig(t)
	if err := r.m.Host().DaemonReload(t.Context()); err != nil {
		t.Fatal(err)
	}
	c := r.conn.call(t, managerReload)
	if c.dest != destination || c.path != managerPath || len(c.args) != 0 {
		t.Errorf("reload = %+v", c)
	}
	r.conn.on(managerReload, func(fakeCall) ([]any, error) {
		return nil, dbus.Error{Name: "org.freedesktop.DBus.Error.AccessDenied"}
	})
	err := r.m.Host().DaemonReload(t.Context())
	if !errors.Is(err, ErrNotPermitted) || err.Error() != "systemd reload: not permitted" {
		t.Fatalf("err = %v", err)
	}
}

func TestEnableAndDisableThenReload(t *testing.T) {
	r := newRig(t)
	unit := testUnit(t)
	r.conn.on(managerEnableUnitFiles, func(fakeCall) ([]any, error) { return []any{true, [][]any{}}, nil })
	r.conn.on(managerDisableUnitFiles, func(fakeCall) ([]any, error) { return []any{[][]any{}}, nil })
	if err := r.m.Host().Enable(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	if err := r.m.Host().Disable(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(r.conn.methods()[4:], " ")
	want := managerEnableUnitFiles + " " + managerReload + " " + managerDisableUnitFiles + " " + managerReload
	if got != want {
		t.Errorf("calls = %s", got)
	}
	e := r.conn.call(t, managerEnableUnitFiles)
	files, ok := e.args[0].([]string)
	if len(e.args) != 3 || !ok || len(files) != 1 || files[0] != unit || e.args[1] != false || e.args[2] != false {
		t.Errorf("enable args = %v", e.args)
	}
	d := r.conn.call(t, managerDisableUnitFiles)
	files, ok = d.args[0].([]string)
	if len(d.args) != 2 || !ok || len(files) != 1 || files[0] != unit || d.args[1] != false {
		t.Errorf("disable args = %v", d.args)
	}
}

func TestAFailedEnableDoesNotReload(t *testing.T) {
	r := newRig(t)
	unit := testUnit(t)
	r.conn.on(managerEnableUnitFiles, func(fakeCall) ([]any, error) {
		return nil, dbus.Error{Name: "org.freedesktop.systemd1.UnitMasked"}
	})
	r.conn.on(managerDisableUnitFiles, func(fakeCall) ([]any, error) {
		return nil, dbus.Error{Name: "org.freedesktop.systemd1.NoSuchUnit"}
	})
	if err := r.m.Host().Enable(t.Context(), unit); !errors.Is(err, ErrMasked) || !strings.Contains(err.Error(), "enable") {
		t.Fatalf("enable err = %v", err)
	}
	if err := r.m.Host().Disable(t.Context(), unit); !errors.Is(err, ErrNoSuchUnit) || !strings.Contains(err.Error(), "disable") {
		t.Fatalf("disable err = %v", err)
	}
	for _, m := range r.conn.methods() {
		if m == managerReload {
			t.Error("reloaded after a failure")
		}
	}
}

// propertyAnswers scripts LoadUnit and Properties.Get from a table of iface.name values.
func (r *rig) propertyAnswers(props map[string]any) {
	r.conn.on(managerLoadUnit, func(fakeCall) ([]any, error) { return []any{unitPath}, nil })
	r.conn.on(propertiesGet, func(c fakeCall) ([]any, error) {
		iface, isIface := c.args[0].(string)
		name, isName := c.args[1].(string)
		if !isIface || !isName {
			return nil, errors.New("fake: Get's arguments are not strings")
		}
		v, ok := props[iface+"."+name]
		if !ok {
			return nil, dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownProperty"}
		}
		return []any{dbus.MakeVariant(v)}, nil
	})
}

func TestHostState(t *testing.T) {
	cases := []struct {
		active, file string
		enabled      bool
	}{
		{"active", "enabled", true},
		{"inactive", "enabled-runtime", true},
		{"failed", "disabled", false},
		{"active", "static", false},
		{"inactive", "", false},
		{"activating", "linked", false},
	}
	for _, c := range cases {
		r := newRig(t)
		unit := testUnit(t)
		r.propertyAnswers(map[string]any{unitIface + ".ActiveState": c.active, unitIface + ".UnitFileState": c.file})
		st, err := r.m.Host().State(t.Context(), unit)
		if err != nil {
			t.Fatal(err)
		}
		if st.Active != c.active || st.Enabled != c.enabled {
			t.Errorf("%+v: state = %+v", c, st)
		}
		l := r.conn.call(t, managerLoadUnit)
		if l.path != managerPath || l.args[0] != unit {
			t.Errorf("load = %+v", l)
		}
		g := r.conn.call(t, propertiesGet)
		if g.path != unitPath || g.dest != destination {
			t.Errorf("get = %+v", g)
		}
	}
}

func TestHostStateOfAnUnloadableUnitIsInactive(t *testing.T) {
	r := newRig(t)
	unit := testUnit(t)
	r.conn.on(managerLoadUnit, func(fakeCall) ([]any, error) {
		return nil, dbus.Error{Name: "org.freedesktop.systemd1.NoSuchUnit"}
	})
	st, err := r.m.Host().State(t.Context(), unit)
	if err != nil || st.Active != "inactive" || st.Enabled {
		t.Fatalf("state = %+v, %v", st, err)
	}
	hs, err := r.m.Health().State(t.Context(), unit)
	if err != nil || hs.Active != "inactive" || hs.InvocationID != "" {
		t.Fatalf("health state = %+v, %v", hs, err)
	}
}

func TestStateErrors(t *testing.T) {
	cases := map[string]map[string]any{
		"wrong type":    {unitIface + ".ActiveState": uint32(1), unitIface + ".UnitFileState": "enabled"},
		"bad token":     {unitIface + ".ActiveState": "act ive", unitIface + ".UnitFileState": "enabled"},
		"empty active":  {unitIface + ".ActiveState": "", unitIface + ".UnitFileState": "enabled"},
		"file wrong":    {unitIface + ".ActiveState": "active", unitIface + ".UnitFileState": true},
		"file bad text": {unitIface + ".ActiveState": "active", unitIface + ".UnitFileState": "EN\x00"},
		"missing":       {unitIface + ".ActiveState": "active"},
	}
	for name, props := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.propertyAnswers(props)
			if _, err := r.m.Host().State(t.Context(), testUnit(t)); err == nil {
				t.Fatal("no error")
			}
		})
	}
	t.Run("load error", func(t *testing.T) {
		r := newRig(t)
		r.conn.on(managerLoadUnit, func(fakeCall) ([]any, error) { return nil, dbus.Error{Name: "org.freedesktop.DBus.Error.AccessDenied"} })
		if _, err := r.m.Host().State(t.Context(), testUnit(t)); !errors.Is(err, ErrNotPermitted) {
			t.Fatalf("err = %v", err)
		}
		if _, err := r.m.Health().State(t.Context(), testUnit(t)); !errors.Is(err, ErrNotPermitted) {
			t.Fatalf("health err = %v", err)
		}
	})
	t.Run("load reply", func(t *testing.T) {
		r := newRig(t)
		r.conn.on(managerLoadUnit, func(fakeCall) ([]any, error) { return []any{"x"}, nil })
		if _, err := r.m.Host().State(t.Context(), testUnit(t)); !errors.Is(err, ErrMalformed) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("get reply", func(t *testing.T) {
		r := newRig(t)
		r.conn.on(managerLoadUnit, func(fakeCall) ([]any, error) { return []any{unitPath}, nil })
		r.conn.on(propertiesGet, func(fakeCall) ([]any, error) { return []any{"not a variant"}, nil })
		if _, err := r.m.Host().State(t.Context(), testUnit(t)); !errors.Is(err, ErrMalformed) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("dial", func(t *testing.T) {
		r := newRig(t)
		r.dialErr = errors.New("no bus")
		if _, err := r.m.Host().State(t.Context(), testUnit(t)); !errors.Is(err, ErrUnreachable) {
			t.Fatalf("err = %v", err)
		}
		if _, err := r.m.Health().State(t.Context(), testUnit(t)); !errors.Is(err, ErrUnreachable) {
			t.Fatalf("err = %v", err)
		}
		if err := r.m.Host().Enable(t.Context(), testUnit(t)); !errors.Is(err, ErrUnreachable) {
			t.Fatalf("err = %v", err)
		}
	})
}

func healthProps(over map[string]any) map[string]any {
	p := map[string]any{
		unitIface + ".ActiveState":          "active",
		unitIface + ".SubState":             "running",
		unitIface + ".InvocationID":         []byte{0xde, 0xad, 0xbe, 0xef, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 0xff},
		serviceIface + ".NRestarts":         uint32(3),
		unitIface + ".ActiveEnterTimestamp": uint64(1_700_000_000_123_456),
	}
	maps.Copy(p, over)
	return p
}

func TestHealthState(t *testing.T) {
	r := newRig(t)
	r.propertyAnswers(healthProps(nil))
	st, err := r.m.Health().State(t.Context(), testUnit(t))
	if err != nil {
		t.Fatal(err)
	}
	if st.Active != "active" || st.Sub != "running" || st.InvocationID != "deadbeef000102030405060708090aff" || st.NRestarts != 3 {
		t.Errorf("state = %+v", st)
	}
	if !st.ActiveSince.Equal(time.UnixMicro(1_700_000_000_123_456)) {
		t.Errorf("since = %v", st.ActiveSince)
	}
}

func TestHealthStateOfAnInactiveUnit(t *testing.T) {
	r := newRig(t)
	r.propertyAnswers(healthProps(map[string]any{
		unitIface + ".ActiveState": "inactive", unitIface + ".SubState": "dead",
		unitIface + ".InvocationID": []byte{}, unitIface + ".ActiveEnterTimestamp": uint64(0),
		serviceIface + ".NRestarts": uint32(0),
	}))
	st, err := r.m.Health().State(t.Context(), testUnit(t))
	if err != nil {
		t.Fatal(err)
	}
	if st.InvocationID != "" || !st.ActiveSince.IsZero() || st.NRestarts != 0 {
		t.Errorf("state = %+v", st)
	}
}

func TestHealthStateWrongTypesAreErrors(t *testing.T) {
	cases := map[string]map[string]any{
		"active":       {unitIface + ".ActiveState": int32(1)},
		"sub":          {unitIface + ".SubState": []byte("x")},
		"sub token":    {unitIface + ".SubState": "RUNNING!"},
		"invocation":   {unitIface + ".InvocationID": "deadbeef"},
		"short id":     {unitIface + ".InvocationID": []byte{1, 2, 3}},
		"restarts":     {serviceIface + ".NRestarts": int64(3)},
		"timestamp":    {unitIface + ".ActiveEnterTimestamp": int64(5)},
		"overflow":     {unitIface + ".ActiveEnterTimestamp": uint64(1 << 63)},
		"no restarts":  {serviceIface + ".NRestarts": nil},
		"no timestamp": {unitIface + ".ActiveEnterTimestamp": nil},
	}
	for name, over := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			props := healthProps(nil)
			for k, v := range over {
				if v == nil {
					delete(props, k)
				} else {
					props[k] = v
				}
			}
			r.propertyAnswers(props)
			_, err := r.m.Health().State(t.Context(), testUnit(t))
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), testUnit(t)) {
				t.Errorf("err %q does not name the unit", err)
			}
		})
	}
}

func TestCleanStateAndJournalTailRunThroughExecx(t *testing.T) {
	r := newRig(t)
	unit := testUnit(t)
	r.run.On([]string{"systemctl", "clean", "--what=state", unit}, execx.Result{})
	r.run.On([]string{"journalctl", "-u", unit, "-n", "20", "--no-pager", "-o", "cat"}, execx.Result{Stdout: []byte("line 1\nline 2\n")})
	if err := r.m.Host().CleanState(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	tail, err := r.m.Host().JournalTail(t.Context(), unit)
	if err != nil || tail != "line 1\nline 2\n" {
		t.Fatalf("tail = %q, %v", tail, err)
	}
	if r.dials != 0 {
		t.Error("the programs dialled the bus")
	}
	r.run.On([]string{"systemctl"}, execx.Result{ExitCode: 1, Stderr: []byte("busy")})
	r.run.On([]string{"journalctl"}, execx.Result{ExitCode: 1})
	if err := r.m.Host().CleanState(t.Context(), unit); err == nil || !strings.Contains(err.Error(), "clean "+unit) {
		t.Errorf("clean err = %v", err)
	}
	if _, err := r.m.Host().JournalTail(t.Context(), unit); err == nil || !strings.Contains(err.Error(), "journal of "+unit) {
		t.Errorf("tail err = %v", err)
	}
}
