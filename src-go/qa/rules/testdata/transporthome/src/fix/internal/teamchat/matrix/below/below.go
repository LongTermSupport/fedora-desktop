// Package below is a package under matrix that is not matrix (the fake homeserver is one): it
// takes the configuration it serves with, and building one here is flagged.
package below

import (
	"crypto/tls"
	"net"
)

// Serve takes the configuration from its caller, clean.
func Serve(ln net.Listener, cfg *tls.Config) net.Listener {
	return tls.NewListener(ln, cfg)
}

// Own builds the configuration itself, flagged.
func Own() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13} // want `tls.Config built outside package matrix`
}
