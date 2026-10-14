//go:build !agentbustest

package main

import (
	"io"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/businstall"
)

// loadConfig is the configuration of the real host. The production binary never reads the
// test variable, so a root caller that carries it relocates nothing and trusts no PATH.
func loadConfig(_ func(string) string, stdout, stderr io.Writer, digest string) (businstall.Config, error) {
	return businstall.Production(stdout, stderr, digest), nil
}
