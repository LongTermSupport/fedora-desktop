package hostnet

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
)

// loopbackListener is a TCP listener on loopback and its address, closed with the test.
func loopbackListener(t *testing.T) (net.Listener, netip.AddrPort) {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cerr := l.Close(); cerr != nil && !errors.Is(cerr, net.ErrClosed) {
			t.Errorf("close: %v", cerr)
		}
	})
	addr, err := netip.ParseAddrPort(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return l, addr
}

// A socket that accepts is answered.
func TestAnswersAListeningSocket(t *testing.T) {
	_, addr := loopbackListener(t)
	ok, err := Answers(t.Context(), addr)
	if err != nil || !ok {
		t.Fatalf("Answers = %v, %v", ok, err)
	}
}

// A refused connect is not answered, and is no error: nothing listens there.
func TestAnswersARefusedConnectIsFalse(t *testing.T) {
	l, addr := loopbackListener(t)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	ok, err := Answers(t.Context(), addr)
	if err != nil || ok {
		t.Fatalf("Answers = %v, %v", ok, err)
	}
}

// A cancelled context is an error, never a "not answered" the drill would count as a pass.
func TestAnswersACancelledContextIsAnError(t *testing.T) {
	_, addr := loopbackListener(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ok, err := Answers(ctx, addr)
	if err == nil || ok {
		t.Fatalf("Answers = %v, %v", ok, err)
	}
}
