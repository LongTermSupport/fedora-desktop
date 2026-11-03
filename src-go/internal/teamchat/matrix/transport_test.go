package matrix

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoopbackClientIsBoundedAndFollowsNothing(t *testing.T) {
	client := LoopbackClient(7 * time.Second)
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T", client.Transport)
	}
	if tr.Proxy != nil || !tr.DisableKeepAlives || tr.TLSClientConfig != nil {
		t.Fatalf("transport: proxy %v keep-alives disabled %v tls %v", tr.Proxy != nil, tr.DisableKeepAlives, tr.TLSClientConfig)
	}
	if client.Timeout != 7*time.Second {
		t.Fatalf("timeout = %v", client.Timeout)
	}
}

func TestLoopbackClientDoesNotFollowARedirect(t *testing.T) {
	var followed atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			followed.Store(true)
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer srv.Close()
	client := LoopbackClient(5 * time.Second)
	defer client.CloseIdleConnections()
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err = resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusFound || followed.Load() {
		t.Fatalf("status %d, followed %v", resp.StatusCode, followed.Load())
	}
}

func TestPublicTransportKeepsTheSystemRootsAndTheBounds(t *testing.T) {
	tr := PublicTransport(9*time.Second, 1<<16)
	if tr.Proxy != nil {
		t.Fatal("a proxy is set")
	}
	if tr.TLSClientConfig != nil {
		t.Fatal("a public service is verified against the system roots: no TLS configuration is set here")
	}
	if tr.TLSHandshakeTimeout != 9*time.Second || tr.MaxResponseHeaderBytes != 1<<16 || tr.DialContext == nil {
		t.Fatalf("bounds: handshake %v header cap %d dial set %v", tr.TLSHandshakeTimeout, tr.MaxResponseHeaderBytes, tr.DialContext != nil)
	}
	if !tr.ForceAttemptHTTP2 || tr.IdleConnTimeout != time.Minute {
		t.Fatalf("http2 %v idle %v", tr.ForceAttemptHTTP2, tr.IdleConnTimeout)
	}
}
