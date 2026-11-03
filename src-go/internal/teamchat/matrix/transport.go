package matrix

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
)

const (
	schemeHTTP        = "http"
	schemeHTTPS       = "https"
	detailBadCA       = "the CA certificate is not one PEM certificate block of a CA"
	detailNeedCA      = "an https URL needs the team's CA certificate"
	detailCAOnPlain   = "a CA certificate is for an https URL, not a loopback http one"
	detailPlainHTTP   = "plain http is only for a loopback IP literal (127.0.0.0/8 or ::1)"
	detailAdminNoCA   = "an admin client takes no CA certificate"
	remoteErrorOp     = "remote error"
	classUnknownCA    = "unknown authority"
	classNameMismatch = "name mismatch"
	classValidity     = "expired or not yet valid"
	classCertUse      = "certificate not valid for this use"
	classRejected     = "certificate rejected"
	classNotTLS       = "not a TLS server"
	classRefused      = "handshake refused (TLS 1.3 is the minimum)"
)

// WallClock is the wall time in milliseconds, what a certificate's validity is judged by.
// clock.Clock meets it, and clock.Fake is what a test drives.
type WallClock interface {
	WallMS() int64
}

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

// pin is what a member client trusts over https (SPEC X16): the team's CA and no other, TLS 1.3 at
// least, and the URL's host as the name the certificate must carry. The system's roots are not
// consulted.
type pin struct {
	cfg *tls.Config
	// host is the URL's host[:port] as it is printed in an error.
	host string
	// fingerprint is the pinned CA certificate's SHA-256 in hex, what a person compares.
	fingerprint string
}

// newPin builds the pin for base, an https URL memberBase gave, from the CA certificate in
// caPEM. Certificate validity is read from wall, or from the system clock when it is nil.
func newPin(caPEM, base string, wall WallClock) (*pin, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != schemeHTTPS {
		return nil, invalid("the base URL is not an https URL")
	}
	pool, err := pki.Pool([]byte(caPEM))
	if err != nil {
		return nil, invalid(detailBadCA)
	}
	fingerprint, err := pki.Fingerprint([]byte(caPEM))
	if err != nil {
		return nil, invalid(detailBadCA)
	}
	if wall == nil {
		wall = clock.System()
	}
	return &pin{
		cfg: &tls.Config{
			RootCAs:    pool,
			MinVersion: tls.VersionTLS13,
			ServerName: u.Hostname(),
			Time:       func() time.Time { return time.UnixMilli(wall.WallMS()) },
		},
		host:        u.Host,
		fingerprint: fingerprint,
	}, nil
}

// detail is the fixed text of a request that failed in the TLS handshake with the pinned server:
// the host, the pinned CA's fingerprint and the class of failure, never a certificate or the
// library's own words. It is empty for any other failure, and for a client with no pin.
func (p *pin) detail(err error) string {
	if p == nil {
		return ""
	}
	class := handshakeClass(err)
	if class == "" {
		return ""
	}
	return "TLS verification of " + p.host + " failed against the pinned CA sha256:" + p.fingerprint + ": " + class
}

// handshakeClass is the class of a TLS handshake failure, or "" when err is not one.
func handshakeClass(err error) string {
	var (
		invalidCert x509.CertificateInvalidError
		unknownCA   x509.UnknownAuthorityError
		wrongName   x509.HostnameError
		verifyFail  *tls.CertificateVerificationError
		notTLS      tls.RecordHeaderError
		op          *net.OpError
	)
	switch {
	case errors.As(err, &invalidCert) && invalidCert.Reason == x509.Expired:
		return classValidity
	case errors.As(err, &invalidCert):
		return classCertUse
	case errors.As(err, &unknownCA):
		return classUnknownCA
	case errors.As(err, &wrongName):
		return classNameMismatch
	case errors.As(err, &verifyFail):
		return classRejected
	case errors.As(err, &notTLS), errors.Is(err, http.ErrSchemeMismatch):
		return classNotTLS
	case errors.As(err, &op) && op.Op == remoteErrorOp:
		return classRefused
	}
	return ""
}

// newHTTPClient is a member or admin client's HTTP client: no proxy, no redirect followed, the dial
// and the TLS handshake bounded, and, for an https URL, the pin's TLS configuration.
func newHTTPClient(p *pin) *http.Client {
	tr := &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: dialLimit}).DialContext,
		MaxResponseHeaderBytes: maxHeaderBytes,
		DisableCompression:     true,
		TLSHandshakeTimeout:    dialLimit,
		IdleConnTimeout:        time.Minute,
	}
	if p != nil {
		tr.TLSClientConfig = p.cfg
	}
	return &http.Client{
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
