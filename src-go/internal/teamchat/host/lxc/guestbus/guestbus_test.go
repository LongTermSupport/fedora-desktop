package guestbus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

type sent struct {
	path   dbus.ObjectPath
	method string
	args   []any
}

// fakeConn records each call and answers it with body and err.
type fakeConn struct {
	calls  []sent
	body   []any
	err    error
	block  bool
	closed bool
}

func (f *fakeConn) Call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) ([]any, error) {
	f.calls = append(f.calls, sent{path, method, args})
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.body, f.err
}

func (f *fakeConn) Close() error {
	f.closed = true
	return nil
}

func unit(t *testing.T) string {
	t.Helper()
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	return paths.SessionUnit(team, "/srv/agent1/src/repo.1", 2)
}

// verb is one Systemd method, the D-Bus method it sends, its arguments and whether it is a job.
type verb struct {
	name   string
	call   func(Systemd, context.Context) error
	method string
	args   []any
	job    bool
}

func verbs(t *testing.T) []verb {
	t.Helper()
	u := unit(t)
	return []verb{
		{"reload", func(s Systemd, ctx context.Context) error { return s.Reload(ctx) }, "Reload", nil, false},
		{"start", func(s Systemd, ctx context.Context) error { return s.Start(ctx, u) }, "StartUnit", []any{u, "replace"}, true},
		{"restart", func(s Systemd, ctx context.Context) error { return s.Restart(ctx, u) }, "RestartUnit", []any{u, "replace"}, true},
		{"stop", func(s Systemd, ctx context.Context) error { return s.Stop(ctx, u) }, "StopUnit", []any{u, "replace"}, true},
		{"reset-failed", func(s Systemd, ctx context.Context) error { return s.ResetFailed(ctx, u) }, "ResetFailedUnit", []any{u}, false},
	}
}

func TestEachMethodSendsSystemdsCall(t *testing.T) {
	for _, v := range verbs(t) {
		f := &fakeConn{}
		if v.job {
			f.body = []any{dbus.ObjectPath("/org/freedesktop/systemd1/job/7")}
		}
		if err := v.call(New(f), t.Context()); err != nil {
			t.Errorf("%s: %v", v.name, err)
		}
		want := sent{"/org/freedesktop/systemd1", "org.freedesktop.systemd1.Manager." + v.method, v.args}
		if len(f.calls) != 1 || f.calls[0].path != want.path || f.calls[0].method != want.method || !slices.Equal(f.calls[0].args, want.args) {
			t.Errorf("%s: sent %+v, want %+v", v.name, f.calls, want)
		}
	}
}

func TestAWrongReplyIsMalformed(t *testing.T) {
	path := dbus.ObjectPath("/org/freedesktop/systemd1/job/7")
	for _, v := range verbs(t) {
		wrong := [][]any{{path}, {"x"}, {path, path}}
		if v.job {
			wrong = [][]any{nil, {"/org/freedesktop/systemd1/job/7"}, {uint32(7)}, {path, path}}
		}
		for _, body := range wrong {
			err := v.call(New(&fakeConn{body: body}), t.Context())
			if !errors.Is(err, ErrMalformed) || !strings.Contains(err.Error(), v.method) {
				t.Errorf("%s with %v: %v", v.name, body, err)
			}
		}
	}
}

// TestAMalformedReplyShowsNoGuestText is B2 (TB2.4): a wrong reply is named by its count and
// signature only, never by the values the guest sent.
func TestAMalformedReplyShowsNoGuestText(t *testing.T) {
	err := New(&fakeConn{body: []any{"\x1b[31mevil", uint32(7)}}).Start(t.Context(), unit(t))
	want := "guest systemd StartUnit " + unit(t) + ": " + ErrMalformed.Error() + ": the reply holds 2 values of signature su"
	if !errors.Is(err, ErrMalformed) || err.Error() != want {
		t.Errorf("%q", err)
	}
}

func TestARefusalKeepsSystemdsErrorNameAlone(t *testing.T) {
	for _, refusal := range []error{
		dbus.Error{Name: "org.freedesktop.systemd1.NoSuchUnit", Body: []any{"Unit \x1b[31mx not found."}},
		dbus.NewError("org.freedesktop.systemd1.NoSuchUnit", []any{"Unit \x1b[31mx not found."}),
	} {
		err := New(&fakeConn{err: refusal}).Start(t.Context(), unit(t))
		se, ok := errors.AsType[*SystemdError](err)
		if !errors.Is(err, ErrSystemd) || !ok || se.Name != "org.freedesktop.systemd1.NoSuchUnit" {
			t.Fatalf("%v", err)
		}
		if err.Error() != "guest systemd StartUnit "+unit(t)+": systemd refused: org.freedesktop.systemd1.NoSuchUnit" {
			t.Errorf("%q", err)
		}
	}
	err := New(&fakeConn{err: dbus.Error{Name: "org.example.Bare_1"}}).Reload(t.Context())
	if se, ok := errors.AsType[*SystemdError](err); !ok || se.Name != "org.example.Bare_1" {
		t.Errorf("%v", err)
	}
}

// TestARefusalWithABadNameIsMalformed is B2: an error name outside the D-Bus grammar is not
// printed; the refusal is a malformed reply.
func TestARefusalWithABadNameIsMalformed(t *testing.T) {
	for _, name := range []string{
		"", "plain", "org.\x1b[31m.x", "org..x", "org.1x", "org.x.", ".org.x", "org.x-y", "org.x\n.y",
		"o." + strings.Repeat("x", 254),
	} {
		err := New(&fakeConn{err: dbus.Error{Name: name}}).Reload(t.Context())
		if !errors.Is(err, ErrMalformed) || errors.Is(err, ErrSystemd) || (name != "" && strings.Contains(err.Error(), name)) {
			t.Errorf("%q: %q", name, err)
		}
	}
}

func TestAClosedPeerIsGone(t *testing.T) {
	for _, cause := range []error{
		io.EOF, io.ErrUnexpectedEOF, io.ErrClosedPipe, dbus.ErrClosed, net.ErrClosed,
		syscall.ECONNRESET, syscall.EPIPE, fmt.Errorf("read: %w", syscall.ECONNRESET),
	} {
		err := New(&fakeConn{err: cause}).Stop(t.Context(), unit(t))
		if !errors.Is(err, ErrGone) || !errors.Is(err, cause) {
			t.Errorf("%v: %v", cause, err)
		}
	}
	for _, other := range []error{ErrTooLarge, context.Canceled, errors.New("odd")} {
		err := New(&fakeConn{err: other}).Stop(t.Context(), unit(t))
		if errors.Is(err, ErrGone) || !errors.Is(err, other) {
			t.Errorf("%v: %v", other, err)
		}
	}
}

func TestACallThatNeverAnswersEndsAtItsBound(t *testing.T) {
	if CallWait != 30*time.Second {
		t.Errorf("CallWait = %v", CallWait)
	}
	if s, ok := New(&fakeConn{}).(systemd); !ok || s.clock != clock.System() {
		t.Errorf("New's clock: %+v", s)
	}
	clk := clock.NewFake(0, 0)
	f := &fakeConn{block: true}
	errc := make(chan error, 1)
	go func() { errc <- newSystemd(f, clk).Restart(t.Context(), unit(t)) }()
	if err := clk.BlockUntil(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	clk.Advance(CallWait - time.Nanosecond)
	if clk.Waiters() != 1 {
		t.Fatal("the call's bound fired short of CallWait")
	}
	clk.Advance(time.Nanosecond)
	if err := <-errc; !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrGone) {
		t.Errorf("%v", err)
	}
	if clk.Waiters() != 0 {
		t.Error("the call's bound was left armed")
	}
}

// The client names a seat's session unit and nothing else (TB2: the host never asks the guest's
// systemd to act on a unit C does not own).
func TestANameThatIsNotASessionUnitIsRefusedUnsent(t *testing.T) {
	for _, name := range []string{"", "sshd.service", "teamchat-session-alpha.service", unit(t) + "\n"} {
		f := &fakeConn{}
		for _, v := range []func(Systemd) error{
			func(s Systemd) error { return s.Start(t.Context(), name) },
			func(s Systemd) error { return s.Restart(t.Context(), name) },
			func(s Systemd) error { return s.Stop(t.Context(), name) },
			func(s Systemd) error { return s.ResetFailed(t.Context(), name) },
		} {
			if err := v(New(f)); !errors.Is(err, ErrNotSessionUnit) || len(f.calls) != 0 {
				t.Errorf("%q: %v %+v", name, err, f.calls)
			}
		}
	}
}

func TestCloseClosesTheConn(t *testing.T) {
	f := &fakeConn{}
	if err := New(f).Close(); err != nil || !f.closed {
		t.Errorf("%v %v", err, f.closed)
	}
}
