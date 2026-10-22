// Package guestbus is an LXC guest's own systemd, spoken to from the host over the guest's
// private manager socket (Plan 00209 SPEC-n3 SU2, C 3.9.4, DC17; n3-D24, n3-D25).
//
// The socket is peer to peer: there is no bus daemon, so no Hello, no NameOwnerChanged and no
// sender to check, and systemd is the only peer. The client authenticates by EXTERNAL as uid 0
// and calls the Manager's Reload, StartUnit, RestartUnit, StopUnit and ResetFailedUnit, each
// on a seat's session unit (paths.SessionUnit), and on no other unit. A job call waits for its reply, which
// means the job was accepted, and never for JobRemoved; each call is bounded by CallWait.
//
// The guest is not trusted (TB2). Every message systemd sends is judged by its header before
// godbus reads it: one whose whole length is over MaxMessage closes the connection, and so
// does a SASL line over its cap. Errors keep their class, so a caller can tell systemd's own
// refusal (ErrSystemd, with the D-Bus error name in a *SystemdError) from a peer that went
// away (ErrGone) and from a reply of the wrong shape (ErrMalformed). No guest text reaches an
// error (TB2.4): a refusal keeps only an error name that passes the D-Bus name grammar, never
// its message, and a wrong reply is named by its value count and signature.
package guestbus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Systemd is the guest's own systemd over its private socket (C 3.9.4, DC17).
type Systemd interface {
	Reload(ctx context.Context) error
	Start(ctx context.Context, unit string) error
	Restart(ctx context.Context, unit string) error
	Stop(ctx context.Context, unit string) error
	ResetFailed(ctx context.Context, unit string) error
	Close() error
}

// The bounds on a call and on a message from the guest.
const (
	CallWait   = 30 * time.Second // each call's bound (SU2)
	MaxMessage = 1 << 20          // a message's whole length, header and body (n3-D25)
)

// The classes of error: a message over its cap, a reply of the wrong shape, systemd's own
// refusal, a peer that is gone, and a call on a unit that is not a seat's session unit.
var (
	ErrTooLarge       = errors.New("guestbus: a message over its cap; the connection is closed")
	ErrMalformed      = errors.New("guestbus: systemd's reply is malformed")
	ErrSystemd        = errors.New("guestbus: systemd refused")
	ErrGone           = errors.New("guestbus: the guest's systemd is gone")
	ErrNotSessionUnit = errors.New("guestbus: not a seat's session unit")
)

// SystemdError is systemd's own refusal of a call, by its D-Bus error name, which passed
// validErrorName. It is ErrSystemd.
type SystemdError struct {
	Name string
}

func (e *SystemdError) Error() string { return "systemd refused: " + e.Name }

// Is makes every SystemdError ErrSystemd.
func (e *SystemdError) Is(target error) bool { return target == ErrSystemd }

// Conn is the part of a peer connection the client calls: godbus's in production, a fake in
// tests.
type Conn interface {
	Call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) ([]any, error)
	Close() error
}

const (
	destination = "org.freedesktop.systemd1"
	managerPath = dbus.ObjectPath("/org/freedesktop/systemd1")
	manager     = destination + ".Manager"
	jobMode     = "replace"
	rootUID     = "0"
)

// Dial is the production Conn over c, an already connected socket (guestroot.DialSocket):
// c wrapped in the capped transport, godbus.NewConn on it, AUTH EXTERNAL as uid 0, no Hello
// (peer-to-peer, n3-D24). A context that ends first ends the dial and closes c.
func Dial(ctx context.Context, c net.Conn) (Conn, error) {
	life, cancel := context.WithCancel(context.WithoutCancel(ctx))
	conn, err := dbus.NewConn(newCapped(c), dbus.WithContext(life))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("guestbus: a connection on the guest's systemd socket: %w", errors.Join(err, c.Close()))
	}
	stop := context.AfterFunc(ctx, cancel)
	err = conn.Auth([]dbus.Auth{dbus.AuthExternal(rootUID)})
	if !stop() {
		err = errors.Join(ctx.Err(), err)
	}
	if err != nil {
		cancel()
		return nil, fmt.Errorf("guestbus: authenticate to the guest's systemd: %w", errors.Join(classify(err), conn.Close()))
	}
	return peer{conn, cancel}, nil
}

// peer is a *dbus.Conn as a Conn; cancel ends the context it lives on.
type peer struct {
	c      *dbus.Conn
	cancel context.CancelFunc
}

func (p peer) Call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) ([]any, error) {
	call := p.c.Object(destination, path).CallWithContext(ctx, method, 0, args...)
	if call.Err != nil {
		return nil, call.Err
	}
	return call.Body, nil
}

// Close closes the connection.
func (p peer) Close() error {
	defer p.cancel()
	return p.c.Close()
}

// New is the Systemd over a Conn. Every unit is in paths.SessionUnit's form; there is no other.
func New(c Conn) Systemd { return newSystemd(c, CallWait) }

func newSystemd(c Conn, wait time.Duration) Systemd { return systemd{c, wait} }

type systemd struct {
	c    Conn
	wait time.Duration
}

func (s systemd) Reload(ctx context.Context) error {
	return s.call(ctx, "Reload", "", false)
}

func (s systemd) Start(ctx context.Context, unit string) error {
	return s.unitCall(ctx, "StartUnit", unit, true)
}

func (s systemd) Restart(ctx context.Context, unit string) error {
	return s.unitCall(ctx, "RestartUnit", unit, true)
}

func (s systemd) Stop(ctx context.Context, unit string) error {
	return s.unitCall(ctx, "StopUnit", unit, true)
}

func (s systemd) ResetFailed(ctx context.Context, unit string) error {
	return s.unitCall(ctx, "ResetFailedUnit", unit, false)
}

func (s systemd) Close() error { return s.c.Close() }

func (s systemd) unitCall(ctx context.Context, method, unit string, job bool) error {
	if _, err := paths.ParseSessionUnit(unit); err != nil {
		return fmt.Errorf("guest systemd %s %q: %w: %w", method, unit, ErrNotSessionUnit, err)
	}
	return s.call(ctx, method, unit, job)
}

// call sends one Manager method: a job method takes the unit and the mode and answers a job
// path; the others answer nothing.
func (s systemd) call(ctx context.Context, method, unit string, job bool) error {
	what := method
	var args []any
	if unit != "" {
		what += " " + unit
		args = append(args, unit)
	}
	if job {
		args = append(args, jobMode)
	}
	ctx, cancel := context.WithTimeout(ctx, s.wait)
	defer cancel()
	body, err := s.c.Call(ctx, managerPath, manager+"."+method, args...)
	if err != nil {
		return fmt.Errorf("guest systemd %s: %w", what, classify(err))
	}
	if !replyFits(body, job) {
		return fmt.Errorf("guest systemd %s: %w: the reply holds %d values of signature %s", what, ErrMalformed, len(body), dbus.SignatureOf(body...))
	}
	return nil
}

// replyFits says whether a reply's body is the method's: one object path for a job, else none.
func replyFits(body []any, job bool) bool {
	if !job {
		return len(body) == 0
	}
	if len(body) != 1 {
		return false
	}
	_, ok := body[0].(dbus.ObjectPath)
	return ok
}

// classify gives a call's error its class: systemd's refusal by name (a name outside the
// grammar is a malformed reply), or a peer that is gone.
func classify(err error) error {
	name, isDBus := "", false
	if e, ok := errors.AsType[dbus.Error](err); ok {
		name, isDBus = e.Name, true
	}
	if e, ok := errors.AsType[*dbus.Error](err); ok {
		name, isDBus = e.Name, true
	}
	if isDBus {
		if !validErrorName(name) {
			return fmt.Errorf("%w: a refusal whose error name is not a D-Bus name", ErrMalformed)
		}
		return &SystemdError{Name: name}
	}
	for _, gone := range []error{
		io.EOF, io.ErrUnexpectedEOF, io.ErrClosedPipe, dbus.ErrClosed, net.ErrClosed, syscall.ECONNRESET, syscall.EPIPE,
	} {
		if errors.Is(err, gone) {
			return fmt.Errorf("%w: %w", ErrGone, err)
		}
	}
	return err
}

// maxName is the longest D-Bus name.
const maxName = 255

// validErrorName is the D-Bus error name grammar: at most 255 bytes, two or more elements
// joined by dots, each [A-Za-z_][A-Za-z0-9_]*.
func validErrorName(name string) bool {
	elems := strings.Split(name, ".")
	if len(name) > maxName || len(elems) < 2 {
		return false
	}
	for _, el := range elems {
		if el == "" || (el[0] >= '0' && el[0] <= '9') {
			return false
		}
		for _, c := range []byte(el) {
			if c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				return false
			}
		}
	}
	return true
}
