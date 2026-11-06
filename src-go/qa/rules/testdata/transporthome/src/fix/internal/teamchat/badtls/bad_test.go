package badtls

import (
	"crypto/tls"
	"net/http"
	"testing"
)

// A test may build both: it is the fixture of a client, not a client.
func TestBuildsBoth(t *testing.T) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS13}
	tr := &http.Transport{TLSClientConfig: cfg}
	if tr.TLSClientConfig != cfg {
		t.Fatal("the configuration was not kept")
	}
}
