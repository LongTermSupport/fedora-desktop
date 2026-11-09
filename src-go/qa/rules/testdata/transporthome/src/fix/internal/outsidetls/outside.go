// Package outsidetls is outside teamchat: the rule does not judge it.
package outsidetls

import (
	"crypto/tls"
	"net/http"
)

// Client builds both, clean here.
func Client() *http.Client {
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}}}
}
