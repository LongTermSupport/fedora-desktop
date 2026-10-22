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

func user(t *testing.T) paths.GuestUser {
	t.Helper()
	u, err := paths.ParseGuestUser("agent1")
	if err != nil {
		t.Fatal(err)
	}
	return u
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
	u := user(t)
	unit := "teamchat-session-agent1.service"
	return []verb{
		{"reload", func(s Systemd, ctx context.Context) error { return s.Reload(ctx) }, "Reload", nil, false},
		{"start", func(s Systemd, ctx context.Context) error { return s.Start(ctx, u) }, "StartUnit", []any{unit, "replace"}, true},
		{"restart", func(s Systemd, ctx context.Context) error { return s.Restart(ctx, u) }, "RestartUnit", []any{unit, "replace"}, true},
		{"stop", func(s Systemd, ctx context.Context) error { return s.Stop(ctx, u) }, "StopUnit", []any{unit, "replace"}, true},
		{"reset-failed", func(s Systemd, ctx context.Context) error { return s.ResetFailed(ctx, u) }, "ResetFailedUnit", []any{unit}, false},
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

func TestARefusalKeepsSystemdsErrorName(t *testing.T) {
	for _, refusal := range []error{
		dbus.Error{Name: "org.freedesktop.systemd1.NoSuchUnit", Body: []any{"Unit x not found."}},
		dbus.NewError("org.freedesktop.systemd1.NoSuchUnit", []any{"Unit x not found."}),
	} {
		err := New(&fakeConn{err: refusal}).Start(t.Context(), user(t))
		se, ok := errors.AsType[*SystemdError](err)
		if !errors.Is(err, ErrSystemd) || !ok || se.Name != "org.freedesktop.systemd1.NoSuchUnit" || se.Message != "Unit x not found." {
			t.Fatalf("%v", err)
		}
		if err.Error() != "guest systemd StartUnit teamchat-session-agent1.service: systemd refused: org.freedesktop.systemd1.NoSuchUnit: Unit x not found." {
			t.Errorf("%q", err)
		}
	}
	err := New(&fakeConn{err: dbus.Error{Name: "org.example.Bare"}}).Reload(t.Context())
	if se, ok := errors.AsType[*SystemdError](err); !ok || se.Name != "org.example.Bare" || se.Message != "" {
		t.Errorf("%v", err)
	}
}

func TestAClosedPeerIsGone(t *testing.T) {
	for _, cause := range []error{
		io.EOF, io.ErrUnexpectedEOF, io.ErrClosedPipe, dbus.ErrClosed, net.ErrClosed,
		syscall.ECONNRESET, syscall.EPIPE, fmt.Errorf("read: %w", syscall.ECONNRESET),
	} {
		err := New(&fakeConn{err: cause}).Stop(t.Context(), user(t))
		if !errors.Is(err, ErrGone) || !errors.Is(err, cause) {
			t.Errorf("%v: %v", cause, err)
		}
	}
	for _, other := range []error{ErrTooLarge, context.Canceled, errors.New("odd")} {
		err := New(&fakeConn{err: other}).Stop(t.Context(), user(t))
		if errors.Is(err, ErrGone) || !errors.Is(err, other) {
			t.Errorf("%v: %v", other, err)
		}
	}
}

func TestACallThatNeverAnswersEndsAtItsBound(t *testing.T) {
	if CallWait != 30*time.Second {
		t.Errorf("CallWait = %v", CallWait)
	}
	if s, ok := New(&fakeConn{}).(systemd); !ok || s.wait != CallWait {
		t.Errorf("New's bound: %+v", s)
	}
	f := &fakeConn{block: true}
	err := newSystemd(f, time.Millisecond).Restart(t.Context(), user(t))
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrGone) {
		t.Errorf("%v", err)
	}
}

func TestAZeroUserIsRefusedUnsent(t *testing.T) {
	f := &fakeConn{}
	if err := New(f).Start(t.Context(), paths.GuestUser{}); !errors.Is(err, ErrNoUser) || len(f.calls) != 0 {
		t.Errorf("%v %+v", err, f.calls)
	}
}

func TestCloseClosesTheConn(t *testing.T) {
	f := &fakeConn{}
	if err := New(f).Close(); err != nil || !f.closed {
		t.Errorf("%v %v", err, f.closed)
	}
}
