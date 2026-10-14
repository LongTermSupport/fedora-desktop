//go:build !agentbustest

package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunIgnoresTheTestPrefixVariable(t *testing.T) {
	restorePath(t)
	prefix := t.TempDir()
	var out, errw bytes.Buffer
	code := run([]string{"check", "--team", "alpha"}, env(map[string]string{"AGENT_BUS_INSTALL_TEST_PREFIX": prefix}), &out, &errw)
	// Not root: refused as such. Root (in a container): the real host has no such team.
	if code != term.ExitNotRoot && code != term.ExitConfig {
		t.Fatalf("exit %d, stderr %q", code, errw.String())
	}
	if entries, err := os.ReadDir(prefix); err != nil || len(entries) != 0 {
		t.Fatalf("the prefix was used: %v %v", entries, err)
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
