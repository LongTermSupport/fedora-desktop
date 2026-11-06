package matrix

import (
	"crypto/tls"
	"net/http"
)

// Transport is package matrix's own: the one home of a transport and a TLS configuration, clean.
func Transport() *http.Transport {
	return &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}}
}
