package guestbus

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

// fakePeer is the guest's systemd at the far end of a net.Pipe: it speaks the server half of
// SASL by hand, then decodes the client's calls and writes what the test says.
type fakePeer struct {
	conn net.Conn
	in   *bufio.Reader
}

func pipe(t *testing.T) (net.Conn, *fakePeer) {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	})
	return client, &fakePeer{conn: server, in: bufio.NewReader(server)}
}

func (p *fakePeer) line() (string, error) {
	s, err := p.in.ReadString('\n')
	return strings.TrimSuffix(s, "\r\n"), err
}

func (p *fakePeer) write(s string) error {
	_, err := io.WriteString(p.conn, s)
	return err
}

// expect reads one SASL line and fails unless it is want.
func (p *fakePeer) expect(want string) error {
	got, err := p.line()
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("the client sent %q, want %q", got, want)
	}
	return nil
}

// sasl is the server half as sd-bus speaks it: the client's NUL, a bare AUTH, then EXTERNAL
// with no initial response (godbus sends none, so systemd takes the uid from the socket's
// peer credentials), an empty DATA each way, OK, then the client's BEGIN.
func (p *fakePeer) sasl() error {
	if b, err := p.in.ReadByte(); err != nil || b != 0 {
		return fmt.Errorf("the NUL byte: %q %w", b, err)
	}
	steps := []struct{ expect, answer string }{
		{"AUTH", "REJECTED EXTERNAL\r\n"},
		{"AUTH EXTERNAL", "DATA\r\n"},
		{"DATA", "OK 0123456789abcdef0123456789abcdef\r\n"},
	}
	for _, s := range steps {
		if err := p.expect(s.expect); err != nil {
			return err
		}
		if err := p.write(s.answer); err != nil {
			return err
		}
	}
	return p.expect("BEGIN")
}

func (p *fakePeer) send(m *dbus.Message) error { return m.EncodeTo(p.conn, binary.LittleEndian) }

func reply(to *dbus.Message, body ...any) *dbus.Message {
	h := map[dbus.HeaderField]dbus.Variant{dbus.FieldReplySerial: dbus.MakeVariant(to.Serial())}
	if len(body) > 0 {
		h[dbus.FieldSignature] = dbus.MakeVariant(dbus.SignatureOf(body...))
	}
	return &dbus.Message{Type: dbus.TypeMethodReply, Headers: h, Body: body}
}

func nameOwnerChanged() *dbus.Message {
	body := []any{"org.freedesktop.systemd1", ":1.1", ""}
	return &dbus.Message{Type: dbus.TypeSignal, Headers: map[dbus.HeaderField]dbus.Variant{
		dbus.FieldPath:      dbus.MakeVariant(dbus.ObjectPath("/org/freedesktop/DBus")),
		dbus.FieldInterface: dbus.MakeVariant("org.freedesktop.DBus"),
		dbus.FieldMember:    dbus.MakeVariant("NameOwnerChanged"),
		dbus.FieldSender:    dbus.MakeVariant("org.freedesktop.DBus"),
		dbus.FieldSignature: dbus.MakeVariant(dbus.SignatureOf(body...)),
	}, Body: body}
}

// serve runs the peer's script in a goroutine and then closes the peer's end, so a client
// still reading sees the end; the returned channel gives the script's error.
func serve(p *fakePeer, script func(*fakePeer) error) <-chan error {
	done := make(chan error, 1)
	go func() { done <- errors.Join(script(p), p.conn.Close()) }()
	return done
}

// drain reads until the client closes its end.
func (p *fakePeer) drain() error {
	if _, err := io.Copy(io.Discard, p.in); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		return err
	}
	return nil
}

// TestOverAPipe is the whole client against a fake systemd: SASL, a StartUnit answered after a
// signal, then an oversized reply that closes the connection before godbus reads its body.
func TestOverAPipe(t *testing.T) {
	client, p := pipe(t)
	var call *dbus.Message
	bodyWrite := make(chan error, 1)
	done := serve(p, func(p *fakePeer) error {
		m, err := p.answerStart()
		call = m
		if err != nil {
			return err
		}
		if _, err = dbus.DecodeMessage(p.in); err != nil {
			return err
		}
		if _, err = p.conn.Write(header('l', 1, 0, MaxMessage)); err != nil {
			return err
		}
		_, err = p.conn.Write([]byte("the body"))
		bodyWrite <- err
		return nil
	})
	c, err := Dial(t.Context(), client)
	if err != nil {
		t.Fatal(err)
	}
	s := New(c)
	if err := s.Start(t.Context(), user(t)); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := s.Restart(t.Context(), user(t)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("restart over an oversized reply: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-bodyWrite; !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("the body was read: %v", err)
	}
	checkStartCall(t, call)
	if err := s.Stop(t.Context(), user(t)); !errors.Is(err, ErrGone) {
		t.Errorf("a call on the closed connection: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

// answerStart completes SASL, reads one call and answers it with a job, a signal first.
func (p *fakePeer) answerStart() (*dbus.Message, error) {
	if err := p.sasl(); err != nil {
		return nil, err
	}
	m, err := dbus.DecodeMessage(p.in)
	if err != nil {
		return nil, err
	}
	if err = p.send(nameOwnerChanged()); err != nil {
		return m, err
	}
	return m, p.send(reply(m, dbus.ObjectPath("/org/freedesktop/systemd1/job/1")))
}

func checkStartCall(t *testing.T, call *dbus.Message) {
	t.Helper()
	if call == nil || call.Type != dbus.TypeMethodCall {
		t.Fatalf("%v", call)
	}
	want := map[dbus.HeaderField]any{
		dbus.FieldPath:        dbus.ObjectPath("/org/freedesktop/systemd1"),
		dbus.FieldInterface:   "org.freedesktop.systemd1.Manager",
		dbus.FieldMember:      "StartUnit",
		dbus.FieldDestination: "org.freedesktop.systemd1",
	}
	for f, v := range want {
		if got := call.Headers[f].Value(); got != v {
			t.Errorf("header %d = %v, want %v", f, got, v)
		}
	}
	if len(call.Body) != 2 || call.Body[0] != "teamchat-session-agent1.service" || call.Body[1] != "replace" {
		t.Errorf("body %v", call.Body)
	}
}

func TestAnOversizedSASLLineEndsTheDial(t *testing.T) {
	client, p := pipe(t)
	done := serve(p, func(p *fakePeer) error {
		if _, err := p.in.ReadByte(); err != nil {
			return err
		}
		if err := p.expect("AUTH"); err != nil {
			return err
		}
		if err := p.write("REJECTED " + strings.Repeat("EXTERNAL ", 80) + "\r\n"); err != nil {
			return err
		}
		// The client refused the line and closed its end: nothing more comes.
		if b, err := p.in.ReadByte(); !errors.Is(err, io.EOF) {
			return fmt.Errorf("after the long line the client sent %q: %w", b, err)
		}
		return nil
	})
	c, err := Dial(t.Context(), client)
	if !errors.Is(err, ErrTooLarge) || c != nil {
		t.Errorf("%v %v", c, err)
	}
	if err := <-done; err != nil {
		t.Error(err)
	}
}

func TestADialEndsWithItsContext(t *testing.T) {
	client, p := pipe(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := serve(p, func(p *fakePeer) error {
		if _, err := p.in.ReadByte(); err != nil {
			return err
		}
		if err := p.expect("AUTH"); err != nil {
			return err
		}
		cancel()
		return p.drain()
	})
	c, err := Dial(ctx, client)
	if !errors.Is(err, context.Canceled) || c != nil {
		t.Errorf("%v %v", c, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAPeerThatRefusesAuthentication(t *testing.T) {
	client, p := pipe(t)
	done := serve(p, func(p *fakePeer) error {
		if _, err := p.in.ReadByte(); err != nil {
			return err
		}
		if err := p.expect("AUTH"); err != nil {
			return err
		}
		if err := p.write("REJECTED EXTERNAL\r\n"); err != nil {
			return err
		}
		if err := p.expect("AUTH EXTERNAL"); err != nil {
			return err
		}
		return p.write("REJECTED EXTERNAL\r\n")
	})
	c, err := Dial(t.Context(), client)
	if err == nil || c != nil || !strings.Contains(err.Error(), "authenticate") {
		t.Errorf("%v %v", c, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
