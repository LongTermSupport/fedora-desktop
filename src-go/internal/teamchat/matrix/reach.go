package matrix

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"syscall"
	"time"
)

// ReachLimit bounds one probe through the terminator, on the injected timers.
const ReachLimit = 5 * time.Second

// The fixed causes of a probe through the terminator that failed; a failed handshake is the
// pin's own fixed text instead, naming the address and the pinned CA's fingerprint (X16).
const (
	ReachRefused  = "the connection was refused"
	ReachNoAnswer = "no answer within 5s"
	ReachLost     = "the connection failed"
)

// reachPath is the endpoint a probe asks: it takes no credential.
const reachPath = "/_matrix/client/versions"

// ReachConfig is a probe of a team's homeserver through its TLS terminator on one listen address
// (Plan 00246 SPEC-cross-network X32). Every field but Wall is required.
type ReachConfig struct {
	// Addr is the listen address and the team's port.
	Addr netip.AddrPort
	// CAPEM is the team's CA certificate, the one certificate the probe trusts.
	CAPEM string
	// Wall is the clock a certificate's validity is judged by; nil is the system clock.
	Wall WallClock
	// Timers arm ReachLimit.
	Timers Timers
}

// ReachError is a probe that reached no answer of 200 from the address: Cause is a fixed text a
// status problem can carry, never a lower-level error's.
type ReachError struct {
	Cause string
	err   error
}

func (e *ReachError) Error() string { return "matrix: the probe through the terminator: " + e.Cause }

func (e *ReachError) Unwrap() error { return e.err }

// Reach asks /_matrix/client/versions of cfg.Addr over TLS as a member's client would (X16): the
// team's CA the one root, TLS 1.3 at least, and the address itself the name verified, with no
// credential sent. nil is an answer of 200; a failure of the address is a *ReachError, and the
// caller's own cancellation is the context's error.
func Reach(ctx context.Context, cfg ReachConfig) error {
	if !cfg.Addr.IsValid() || cfg.Timers == nil {
		return invalid("a probe through the terminator needs an address and timers")
	}
	trust, err := newPin(cfg.CAPEM, reachBase(cfg.Addr), cfg.Wall)
	if err != nil {
		return err
	}
	hc := newHTTPClient(trust, nil)
	defer hc.CloseIdleConnections()
	tryCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := cfg.Timers.AfterFunc(ReachLimit, cancel)
	defer stop()
	req, err := http.NewRequestWithContext(tryCtx, http.MethodGet, reachBase(cfg.Addr)+reachPath, http.NoBody)
	if err != nil {
		return invalid("the request could not be built")
	}
	req.Header.Set("Accept", contentTypeJSON)
	req.Header.Set("User-Agent", memberAgent)
	resp, err := hc.Do(req)
	if err != nil {
		return reachFailed(ctx, tryCtx, trust, err)
	}
	_, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, maxAnswerBytes))
	if err = errors.Join(readErr, resp.Body.Close()); err != nil {
		return reachFailed(ctx, tryCtx, nil, err)
	}
	if resp.StatusCode != http.StatusOK {
		return &ReachError{Cause: "it answered HTTP " + strconv.Itoa(resp.StatusCode)}
	}
	return nil
}

// reachBase is the https base of a listen address, an IPv6 one in brackets.
func reachBase(addr netip.AddrPort) string { return schemeHTTPS + "://" + addr.String() }

// reachFailed is a probe that got no whole answer: the caller's cancellation first, then the
// bound, then a failed handshake with the pinned CA, a refused connection, or any other.
func reachFailed(ctx, tryCtx context.Context, trust *pin, err error) error {
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case tryCtx.Err() != nil:
		return &ReachError{Cause: ReachNoAnswer, err: err}
	}
	if detail := trust.detail(err); detail != "" {
		return &ReachError{Cause: detail, err: err}
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return &ReachError{Cause: ReachRefused, err: err}
	}
	return &ReachError{Cause: ReachLost, err: err}
}
