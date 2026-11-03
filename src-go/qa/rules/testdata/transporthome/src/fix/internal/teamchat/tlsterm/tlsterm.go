// Package tlsterm stands for teamchat's TLS terminator, the third owner: clean.
package tlsterm

import "crypto/tls"

// Server is the configuration the terminator serves with.
func Server(cert tls.Certificate) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}
}
