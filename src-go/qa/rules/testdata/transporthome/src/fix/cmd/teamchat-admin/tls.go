package main

import "crypto/tls"

// config builds a TLS configuration in the command, which is in the rule's scope.
func config() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13} // want `tls.Config built outside package matrix`
}
