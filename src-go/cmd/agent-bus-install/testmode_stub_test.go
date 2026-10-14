//go:build !agentbustest

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunIgnoresTheTestPrefixVariable(t *testing.T) {
	restorePath(t)
	prefix := t.TempDir()
	var out, errw bytes.Buffer
	code := run([]string{"check", "--team", "alpha"}, env(map[string]string{"AGENT_BUS_INSTALL_TEST_PREFIX": prefix}), &out, &errw)
	// Not root: refused as such, before any path is looked at. Root (in a container): the
	// message names the real path, not one under the prefix.
	if code != term.ExitNotRoot && code != term.ExitConfig {
		t.Fatalf("exit %d, stderr %q", code, errw.String())
	}
	if code == term.ExitConfig && !strings.Contains(errw.String(), " /var/lib/agent-bus/alpha") {
		t.Fatalf("the refusal does not name the real path: %q", errw.String())
	}
	if strings.Contains(errw.String(), prefix) {
		t.Fatalf("the prefix reached the run: %q", errw.String())
	}
}

func TestTheProductionBuildReadsNoTestVariable(t *testing.T) {
	asked := false
	getenv := func(string) string {
		asked = true
		return "/tmp"
	}
	cfg, err := loadConfig(getenv, &bytes.Buffer{}, &bytes.Buffer{}, "digest")
	if err != nil {
		t.Fatal(err)
	}
	if asked {
		t.Fatal("the production build looked at the environment")
	}
	if cfg.Prefix != "" || cfg.AdminTool != "/usr/local/bin/agent-bus" || cfg.SourceDigest != "digest" {
		t.Fatalf("config %+v", cfg)
	}
}
