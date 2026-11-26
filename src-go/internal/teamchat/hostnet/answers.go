package hostnet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"
)

// Answers is whether a TCP connect to addr is accepted within the probes' bound: a refused or
// timed-out connect is false; a cancelled context, or any other failure, is an error, so a
// probe that could not judge never reads as "nothing answered".
func Answers(ctx context.Context, addr netip.AddrPort) (bool, error) {
	d := net.Dialer{Timeout: probeTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr.String())
	if err == nil {
		return true, conn.Close()
	}
	if ctx.Err() != nil {
		return false, fmt.Errorf("the connect to %s: %w", addr, ctx.Err())
	}
	var ne net.Error
	if errors.Is(err, syscall.ECONNREFUSED) || (errors.As(err, &ne) && ne.Timeout()) {
		return false, nil
	}
	return false, fmt.Errorf("the connect to %s: %w", addr, err)
}
