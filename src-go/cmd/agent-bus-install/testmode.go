//go:build agentbustest

package main

import (
	"io"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/businstall"
)

// loadConfig is the configuration of a test host when AGENT_BUS_INSTALL_TEST_PREFIX names
// one, compiled in only by scripts/test-agent-bus-install.bash (-tags agentbustest).
func loadConfig(getenv func(string) string, stdout, stderr io.Writer, digest string) (businstall.Config, error) {
	if prefix := getenv("AGENT_BUS_INSTALL_TEST_PREFIX"); prefix != "" {
		return businstall.Test(prefix, stdout, stderr, digest)
	}
	return businstall.Production(stdout, stderr, digest), nil
}
