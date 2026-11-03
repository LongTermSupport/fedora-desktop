package matrix

import (
	"net"
	"net/http"
	"time"
)

// LoopbackClient is the client for a probe of a homeserver's own loopback listener (readiness,
// health): no proxy, no redirect followed, no connection kept, and the whole request bounded by
// timeout. It is plain http and trusts nothing; a request to a member's homeserver is NewMember's.
func LoopbackClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// PublicTransport is the transport for a public https service that is not a homeserver (the
// forge a member's claims are proved against): no proxy, the dial and the TLS handshake bounded
// by limit, response headers capped at maxHeaderBytes. It sets no TLS configuration, so the
// service is verified against the system's roots; a team's homeserver is never reached with it.
func PublicTransport(limit time.Duration, maxHeaderBytes int64) *http.Transport {
	return &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: limit}).DialContext,
		TLSHandshakeTimeout:    limit,
		MaxResponseHeaderBytes: maxHeaderBytes,
		IdleConnTimeout:        time.Minute,
		ForceAttemptHTTP2:      true,
	}
}
