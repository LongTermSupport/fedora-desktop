// Package units is systemd over D-Bus for teamchat-admin (Plan 00209 SPEC-n2f section 6, SD1
// to SD9; DC17): the unit jobs and state of the teams' homeservers that apply, remove and the
// health tick need. A Manager holds one system-bus connection, opened on first use and dialled
// again after it is lost; Host is its host.Systemd and Health its health.Units.
//
// Trust: root acts on what this package reports, so every value systemd returns is checked for
// its D-Bus type, a job's end is taken only from a JobRemoved sent by the bus name's current
// owner (any peer may send root a signal), and no string the bus returned is printed unless it
// is one printable line of at most 256 bytes.
package units

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sync"
	"time"
	"unicode"

	"github.com/godbus/dbus/v5"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// The names systemd and the bus answer to.
const (
	destination  = "org.freedesktop.systemd1"
	managerPath  = dbus.ObjectPath("/org/freedesktop/systemd1")
	managerIface = "org.freedesktop.systemd1.Manager"
	unitIface    = "org.freedesktop.systemd1.Unit"
	serviceIface = "org.freedesktop.systemd1.Service"
	busName      = "org.freedesktop.DBus"
	busPath      = dbus.ObjectPath("/org/freedesktop/DBus")

	managerStartUnit        = managerIface + ".StartUnit"
	managerRestartUnit      = managerIface + ".RestartUnit"
	managerStopUnit         = managerIface + ".StopUnit"
	managerReload           = managerIface + ".Reload"
	managerSubscribe        = managerIface + ".Subscribe"
	managerLoadUnit         = managerIface + ".LoadUnit"
	managerEnableUnitFiles  = managerIface + ".EnableUnitFiles"
	managerDisableUnitFiles = managerIface + ".DisableUnitFiles"
	propertiesGet           = "org.freedesktop.DBus.Properties.Get"
	busAddMatch             = busName + ".AddMatch"
	busGetNameOwner         = busName + ".GetNameOwner"
	jobRemovedSignal        = managerIface + ".JobRemoved"
	nameOwnerChanged        = busName + ".NameOwnerChanged"

	jobRemovedMatch = "type='signal',sender='" + destination + "',path='" + string(managerPath) +
		"',interface='" + managerIface + "',member='JobRemoved'"
	ownerChangedMatch = "type='signal',sender='" + busName + "',path='" + string(busPath) +
		"',interface='" + busName + "',member='NameOwnerChanged',arg0='" + destination + "'"

	jobMode     = "replace"
	jobDone     = "done"
	inactive    = "inactive"
	maxShown    = 256
	maxUnitName = 255
	signalQueue = 64
	idBytes     = 16
)

// JobWait bounds the wait for a job's JobRemoved. It is above any homeserver unit's start
// timeout, so only a JobRemoved that is never sent reaches it.
const JobWait = 10 * time.Minute

// The classes of error a call ends in (SD7).
var (
	ErrUnitName     = errors.New("not a teamchat homeserver unit name")
	ErrNoSuchUnit   = errors.New("no such unit")
	ErrNotPermitted = errors.New("not permitted")
	ErrMasked       = errors.New("the unit is masked")
	ErrUnreachable  = errors.New("systemd is not reachable on the system bus")
	ErrJobFailed    = errors.New("the job did not finish")
	ErrMalformed    = errors.New("systemd's reply is malformed")
	ErrSystemd      = errors.New("systemd refused")
)

// serverUnit is a homeserver unit built by paths.ServerUnit, in systemd's unit-name characters.
var (
	serverUnit = regexp.MustCompile(`\A` + regexp.QuoteMeta(paths.Product+"-server@") + `[A-Za-z0-9:_.\\-]+\.service\z`)
	stateToken = regexp.MustCompile(`\A[a-z][a-z-]{0,63}\z`)
)

// Conn is the part of a bus connection the Manager uses: *dbus.Conn through busConn, a fake in
// tests. Call returns the reply's body; Signal hands the connection's signals to ch, which the
// connection closes when it is lost or closed.
type Conn interface {
	Call(ctx context.Context, dest string, path dbus.ObjectPath, method string, args ...any) ([]any, error)
	Signal(ch chan<- *dbus.Signal)
	Close() error
}

// Manager is systemd over one lazily opened bus connection. Safe for concurrent use.
type Manager struct {
	dial    func(context.Context) (Conn, error)
	run     execx.Runner
	jobWait time.Duration

	mu sync.Mutex
	s  *session

	// test hooks: a signal handled, a job's waiter registered
	onSignal func(*dbus.Signal)
	onWait   func(dbus.ObjectPath)
}

// NewManager is a Manager that dials with dial on first use and runs systemctl and journalctl
// through r.
func NewManager(dial func(context.Context) (Conn, error), r execx.Runner) *Manager {
	return &Manager{dial: dial, run: r, jobWait: JobWait}
}

// Host is the Manager as host.Systemd.
func (m *Manager) Host() host.Systemd { return hostUnits{m} }

// Health is the Manager as health.Units.
func (m *Manager) Health() health.Units { return healthUnits{m} }

// Close closes the connection if one is open.
func (m *Manager) Close() error {
	m.mu.Lock()
	s := m.s
	m.s = nil
	m.mu.Unlock()
	if s == nil {
		return nil
	}
	if err := s.conn.Close(); err != nil {
		return fmt.Errorf("close the system bus connection: %w", err)
	}
	return nil
}

// session is one connection and the jobs waiting on it.
type session struct {
	conn Conn
	m    *Manager

	mu       sync.Mutex
	owner    string
	lost     bool
	inflight int
	early    map[dbus.ObjectPath]string
	waiters  map[dbus.ObjectPath]chan string
}

// connect is the current session, dialled and set up when there is none or it was lost.
func (m *Manager) connect(ctx context.Context) (*session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.s != nil && !m.s.isLost() {
		return m.s, nil
	}
	if m.s != nil {
		old := m.s
		m.s = nil
		if err := old.conn.Close(); err != nil {
			return nil, fmt.Errorf("%w: close the lost connection: %w", ErrUnreachable, err)
		}
	}
	conn, err := m.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	s := &session{conn: conn, m: m, early: map[dbus.ObjectPath]string{}, waiters: map[dbus.ObjectPath]chan string{}}
	ch := make(chan *dbus.Signal, signalQueue)
	conn.Signal(ch)
	go s.dispatch(ch)
	if err := s.setup(ctx); err != nil {
		if cerr := conn.Close(); cerr != nil {
			return nil, fmt.Errorf("%w; close the connection: %w", err, cerr)
		}
		return nil, err
	}
	m.s = s
	return s, nil
}

// setup matches JobRemoved and the owner's changes, learns systemd's owner and subscribes, so
// no job's end can be missed.
func (s *session) setup(ctx context.Context) error {
	for _, rule := range []string{jobRemovedMatch, ownerChangedMatch} {
		if _, err := s.conn.Call(ctx, busName, busPath, busAddMatch, rule); err != nil {
			return classify(err)
		}
	}
	body, err := s.conn.Call(ctx, busName, busPath, busGetNameOwner, destination)
	if err != nil {
		return classify(err)
	}
	owner, err := reply[string](body, "GetNameOwner")
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.owner = owner
	s.mu.Unlock()
	if _, err := s.conn.Call(ctx, destination, managerPath, managerSubscribe); err != nil {
		return classify(err)
	}
	return nil
}

func (s *session) isLost() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lost
}

// lostForTest reports whether the current session has seen its connection end.
func (m *Manager) lostForTest() bool {
	m.mu.Lock()
	s := m.s
	m.mu.Unlock()
	return s == nil || s.isLost()
}

// dispatch reads every signal until the connection ends, then fails the waiting jobs.
func (s *session) dispatch(ch <-chan *dbus.Signal) {
	for sig := range ch {
		s.handle(sig)
		if s.m.onSignal != nil {
			s.m.onSignal(sig)
		}
	}
	s.mu.Lock()
	s.lost = true
	for path, w := range s.waiters {
		close(w)
		delete(s.waiters, path)
	}
	s.mu.Unlock()
	if s.m.onSignal != nil {
		s.m.onSignal(nil)
	}
}

func (s *session) handle(sig *dbus.Signal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case sig.Name == nameOwnerChanged && sig.Sender == busName && sig.Path == busPath:
		s.ownerChanged(sig.Body)
	case sig.Name == jobRemovedSignal && s.owner != "" && sig.Sender == s.owner && sig.Path == managerPath:
		s.jobRemoved(sig.Body)
	}
}

// ownerChanged follows NameOwnerChanged(name, old, new) for systemd's name; s.mu is held.
func (s *session) ownerChanged(body []any) {
	if len(body) != 3 {
		return
	}
	name, isName := body[0].(string)
	owner, isOwner := body[2].(string)
	if isName && isOwner && name == destination {
		s.owner = owner
	}
}

// jobRemoved is JobRemoved(id, job, unit, result) handed to its waiter, or kept while a job
// call is in flight; s.mu is held.
func (s *session) jobRemoved(body []any) {
	if len(body) != 4 {
		return
	}
	job, isJob := body[1].(dbus.ObjectPath)
	result, isResult := body[3].(string)
	if !isJob || !isResult {
		return
	}
	if w, ok := s.waiters[job]; ok {
		w <- result
		delete(s.waiters, job)
	} else if s.inflight > 0 {
		s.early[job] = result
	}
}

// begin counts a job call in flight, so a JobRemoved that beats its reply is kept.
func (s *session) begin() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lost {
		return ErrUnreachable
	}
	s.inflight++
	return nil
}

// await ends the call begun and is the channel job's result arrives on, already holding it when
// its JobRemoved came first; closed when the connection is lost.
func (s *session) await(job dbus.ObjectPath, called bool) chan string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inflight--
	defer func() {
		if s.inflight == 0 {
			clear(s.early)
		}
	}()
	if !called {
		return nil
	}
	w := make(chan string, 1)
	if result, ok := s.early[job]; ok {
		delete(s.early, job)
		w <- result
		return w
	}
	if s.lost {
		close(w)
		return w
	}
	s.waiters[job] = w
	if s.m.onWait != nil {
		s.m.onWait(job)
	}
	return w
}

func (s *session) forget(job dbus.ObjectPath) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.waiters, job)
}

// drop retires s after its connection failed, so the next call dials again.
func (m *Manager) drop(s *session) error {
	m.mu.Lock()
	if m.s == s {
		m.s = nil
	}
	m.mu.Unlock()
	if err := s.conn.Close(); err != nil {
		return fmt.Errorf("close the connection: %w", err)
	}
	return nil
}

// failed is err as the call's one-line error, dropping the connection when it is lost.
func (m *Manager) failed(s *session, what string, err error) error {
	err = classify(err)
	if errors.Is(err, ErrUnreachable) {
		if cerr := m.drop(s); cerr != nil {
			err = fmt.Errorf("%w; %w", err, cerr)
		}
	}
	return fmt.Errorf("systemd %s: %w", what, err)
}

// classify is err by its class: the context's end, a D-Bus error by name, or a lost bus.
func classify(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrMalformed) || errors.Is(err, ErrUnreachable) {
		return err
	}
	if value, ok := errors.AsType[dbus.Error](err); ok {
		return dbusError(value)
	}
	if pointer, ok := errors.AsType[*dbus.Error](err); ok {
		return dbusError(*pointer)
	}
	return fmt.Errorf("%w: %w", ErrUnreachable, err)
}

func dbusError(e dbus.Error) error {
	var class error
	switch e.Name {
	case "org.freedesktop.systemd1.NoSuchUnit":
		class = ErrNoSuchUnit
	case "org.freedesktop.DBus.Error.AccessDenied", "org.freedesktop.DBus.Error.InteractiveAuthorizationRequired":
		class = ErrNotPermitted
	case "org.freedesktop.systemd1.UnitMasked":
		class = ErrMasked
	default:
		class = namedError(e.Name)
	}
	if len(e.Body) > 0 {
		if text, ok := e.Body[0].(string); ok && printable(text) {
			return fmt.Errorf("%w: %s", class, text)
		}
	}
	return class
}

// namedError is a D-Bus error of no class of its own, shown by its name; it is ErrSystemd.
type namedError string

func (e namedError) Error() string {
	if printable(string(e)) {
		return string(e)
	}
	return "an unprintable D-Bus error"
}

func (namedError) Is(target error) bool { return target == ErrSystemd }

// printable is a string the bus returned that may be shown: one line of at most 256 bytes of
// printable characters.
func printable(s string) bool {
	if s == "" || len(s) > maxShown {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// reply is the one value of a reply's body, which must be a T. godbus's Store converts between
// kinds (a string into an object path, a number into a string), so the type is asserted.
func reply[T any](body []any, what string) (T, error) {
	var zero T
	if len(body) != 1 {
		return zero, fmt.Errorf("%w: %s answered %d values", ErrMalformed, what, len(body))
	}
	v, ok := body[0].(T)
	if !ok {
		return zero, fmt.Errorf("%w: %s answered a %T, not a %T", ErrMalformed, what, body[0], zero)
	}
	return v, nil
}

// checkUnit refuses a unit that is not a homeserver unit name (SD8).
func checkUnit(unit string) error {
	if len(unit) > maxUnitName || !serverUnit.MatchString(unit) {
		return fmt.Errorf("%w: %q", ErrUnitName, unit)
	}
	return nil
}

// job runs a Start, Restart or Stop and waits for its JobRemoved (SD1, n2e-D13).
func (m *Manager) job(ctx context.Context, verb, method, unit string) error {
	if err := checkUnit(unit); err != nil {
		return err
	}
	what := verb + " " + unit
	s, err := m.connect(ctx)
	if err != nil {
		return fmt.Errorf("systemd %s: %w", what, err)
	}
	if berr := s.begin(); berr != nil {
		return m.failed(s, what, berr)
	}
	body, err := s.conn.Call(ctx, destination, managerPath, method, unit, jobMode)
	var job dbus.ObjectPath
	if err == nil {
		job, err = reply[dbus.ObjectPath](body, method)
	}
	done := s.await(job, err == nil)
	if err != nil {
		return m.failed(s, what, err)
	}
	wait, cancel := context.WithTimeout(ctx, m.jobWait)
	defer cancel()
	select {
	case result, ok := <-done:
		if !ok {
			return m.failed(s, what, fmt.Errorf("%w: the connection ended before the job", ErrUnreachable))
		}
		if result != jobDone {
			shown := "an unprintable result"
			if printable(result) {
				shown = fmt.Sprintf("%q", result)
			}
			return fmt.Errorf("systemd %s: %w: it ended %s", what, ErrJobFailed, shown)
		}
		return nil
	case <-wait.Done():
		s.forget(job)
		if ctx.Err() != nil {
			return fmt.Errorf("systemd %s: %w", what, ctx.Err())
		}
		return fmt.Errorf("systemd %s: no JobRemoved within %v: %w", what, m.jobWait, wait.Err())
	}
}

// call is one Manager method that needs no job wait; its reply is not needed.
func (m *Manager) call(ctx context.Context, what, method string, args ...any) error {
	s, err := m.connect(ctx)
	if err != nil {
		return fmt.Errorf("systemd %s: %w", what, err)
	}
	if _, err := m.callOn(ctx, s, what, managerPath, method, args...); err != nil {
		return err
	}
	return nil
}

func (m *Manager) callOn(ctx context.Context, s *session, what string, path dbus.ObjectPath, method string, args ...any) ([]any, error) {
	body, err := s.conn.Call(ctx, destination, path, method, args...)
	if err != nil {
		return nil, m.failed(s, what, err)
	}
	return body, nil
}

func (m *Manager) reload(ctx context.Context) error {
	return m.call(ctx, "reload", managerReload)
}

func (m *Manager) unitFiles(ctx context.Context, verb, method, unit string, args ...any) error {
	if err := checkUnit(unit); err != nil {
		return err
	}
	if err := m.call(ctx, verb+" "+unit, method, append([]any{[]string{unit}}, args...)...); err != nil {
		return err
	}
	return m.reload(ctx)
}

// unit is the session and the unit's object, loaded; found false when systemd has no such unit.
func (m *Manager) unit(ctx context.Context, unit string) (*session, dbus.ObjectPath, bool, error) {
	if err := checkUnit(unit); err != nil {
		return nil, "", false, err
	}
	what := "state of " + unit
	s, err := m.connect(ctx)
	if err != nil {
		return nil, "", false, fmt.Errorf("systemd %s: %w", what, err)
	}
	body, err := m.callOn(ctx, s, what, managerPath, managerLoadUnit, unit)
	if errors.Is(err, ErrNoSuchUnit) {
		return s, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	path, err := reply[dbus.ObjectPath](body, "LoadUnit")
	if err != nil {
		return nil, "", false, fmt.Errorf("systemd %s: %w", what, err)
	}
	return s, path, true, nil
}

// property is one property of the unit's object, checked to hold a T (SD5).
func property[T any](ctx context.Context, m *Manager, s *session, unit string, path dbus.ObjectPath, iface, name string) (T, error) {
	var zero T
	what := "state of " + unit
	body, err := m.callOn(ctx, s, what, path, propertiesGet, iface, name)
	if err != nil {
		return zero, err
	}
	v, err := reply[dbus.Variant](body, name)
	if err != nil {
		return zero, fmt.Errorf("systemd %s: %w", what, err)
	}
	value, ok := v.Value().(T)
	if !ok {
		return zero, fmt.Errorf("systemd %s: %w: %s is %s, not %T", what, ErrMalformed, name, v.Signature(), zero)
	}
	return value, nil
}

// token is a state property: a lower-case systemd state word ("" only when empty is allowed).
func token(ctx context.Context, m *Manager, s *session, unit string, path dbus.ObjectPath, name string, empty bool) (string, error) {
	v, err := property[string](ctx, m, s, unit, path, unitIface, name)
	if err != nil {
		return "", err
	}
	if v == "" && empty || stateToken.MatchString(v) {
		return v, nil
	}
	return "", fmt.Errorf("systemd state of %s: %w: %s is not a state word", unit, ErrMalformed, name)
}

func (m *Manager) hostState(ctx context.Context, unit string) (host.UnitState, error) {
	s, path, found, err := m.unit(ctx, unit)
	if err != nil || !found {
		return host.UnitState{Active: inactive}, err
	}
	active, err := token(ctx, m, s, unit, path, "ActiveState", false)
	if err != nil {
		return host.UnitState{}, err
	}
	file, err := token(ctx, m, s, unit, path, "UnitFileState", true)
	if err != nil {
		return host.UnitState{}, err
	}
	return host.UnitState{Active: active, Enabled: file == "enabled" || file == "enabled-runtime"}, nil
}

func (m *Manager) healthState(ctx context.Context, unit string) (health.UnitState, error) {
	s, path, found, err := m.unit(ctx, unit)
	if err != nil {
		return health.UnitState{}, err
	}
	if !found {
		return health.UnitState{Active: inactive}, nil
	}
	var st health.UnitState
	if st.Active, err = token(ctx, m, s, unit, path, "ActiveState", false); err != nil {
		return health.UnitState{}, err
	}
	if st.Sub, err = token(ctx, m, s, unit, path, "SubState", false); err != nil {
		return health.UnitState{}, err
	}
	id, err := property[[]byte](ctx, m, s, unit, path, unitIface, "InvocationID")
	if err != nil {
		return health.UnitState{}, err
	}
	switch len(id) {
	case 0:
	case idBytes:
		st.InvocationID = hex.EncodeToString(id)
	default:
		return health.UnitState{}, fmt.Errorf("systemd state of %s: %w: InvocationID is %d bytes", unit, ErrMalformed, len(id))
	}
	restarts, err := property[uint32](ctx, m, s, unit, path, serviceIface, "NRestarts")
	if err != nil {
		return health.UnitState{}, err
	}
	st.NRestarts = int64(restarts)
	since, err := property[uint64](ctx, m, s, unit, path, unitIface, "ActiveEnterTimestamp")
	if err != nil {
		return health.UnitState{}, err
	}
	if since > math.MaxInt64 {
		return health.UnitState{}, fmt.Errorf("systemd state of %s: %w: ActiveEnterTimestamp is out of range", unit, ErrMalformed)
	}
	if since != 0 {
		st.ActiveSince = time.UnixMicro(int64(since))
	}
	return st, nil
}

// cleanState is `systemctl clean --what=state` (SD6, n2f-D4).
func (m *Manager) cleanState(ctx context.Context, unit string) error {
	if err := checkUnit(unit); err != nil {
		return err
	}
	if _, err := m.run.Run(ctx, program(execx.Cmd{Name: "/usr/bin/systemctl", Args: []string{"clean", "--what=state", unit}})); err != nil {
		return fmt.Errorf("systemctl clean %s: %w", unit, err)
	}
	return nil
}

// journalTail is the unit's last 20 journal lines, only ever shown (SD6).
func (m *Manager) journalTail(ctx context.Context, unit string) (string, error) {
	if err := checkUnit(unit); err != nil {
		return "", err
	}
	res, err := m.run.Run(ctx, program(execx.Cmd{Name: "/usr/bin/journalctl", Args: []string{"-u", unit, "-n", "20", "--no-pager", "-o", "cat"}}))
	if err != nil {
		return "", fmt.Errorf("the journal of %s: %w", unit, err)
	}
	return string(res.Stdout), nil
}

// program is c, a system program named by its absolute path, in an environment of its own, so
// nothing of the caller's PATH or locale reaches what root runs.
func program(c execx.Cmd) execx.Cmd {
	c.CleanEnv, c.Env = true, []string{"PATH=/usr/bin:/bin", "LANG=C"}
	return c
}

// hostUnits is the Manager as host.Systemd.
type hostUnits struct{ m *Manager }

func (h hostUnits) DaemonReload(ctx context.Context) error { return h.m.reload(ctx) }

func (h hostUnits) Start(ctx context.Context, unit string) error {
	return h.m.job(ctx, "start", managerStartUnit, unit)
}

func (h hostUnits) Restart(ctx context.Context, unit string) error {
	return h.m.job(ctx, "restart", managerRestartUnit, unit)
}

func (h hostUnits) Stop(ctx context.Context, unit string) error {
	return h.m.job(ctx, "stop", managerStopUnit, unit)
}

func (h hostUnits) Enable(ctx context.Context, unit string) error {
	return h.m.unitFiles(ctx, "enable", managerEnableUnitFiles, unit, false, false)
}

func (h hostUnits) Disable(ctx context.Context, unit string) error {
	return h.m.unitFiles(ctx, "disable", managerDisableUnitFiles, unit, false)
}

func (h hostUnits) State(ctx context.Context, unit string) (host.UnitState, error) {
	return h.m.hostState(ctx, unit)
}

func (h hostUnits) JournalTail(ctx context.Context, unit string) (string, error) {
	return h.m.journalTail(ctx, unit)
}

func (h hostUnits) CleanState(ctx context.Context, unit string) error {
	return h.m.cleanState(ctx, unit)
}

// healthUnits is the Manager as health.Units.
type healthUnits struct{ m *Manager }

func (h healthUnits) State(ctx context.Context, unit string) (health.UnitState, error) {
	return h.m.healthState(ctx, unit)
}

func (h healthUnits) Restart(ctx context.Context, unit string) error {
	return h.m.job(ctx, "restart", managerRestartUnit, unit)
}
