// Package pki stands for teamchat's certificate package, the second owner: clean.
package pki

import "crypto/tls"

// Server is a configuration pki may build.
func Server() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13}
}
