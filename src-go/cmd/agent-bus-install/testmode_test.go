//go:build agentbustest

package main

import (
	"bytes"
	"testing"
)

func TestTheTestBuildRootsTheHostUnderThePrefix(t *testing.T) {
	prefix := t.TempDir()
	getenv := func(key string) string {
		if key == "AGENT_BUS_INSTALL_TEST_PREFIX" {
			return prefix
		}
		return ""
	}
	cfg, err := loadConfig(getenv, &bytes.Buffer{}, &bytes.Buffer{}, "digest")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Prefix != prefix || cfg.AdminTool != "agent-bus" || cfg.Path != "" {
		t.Fatalf("config %+v", cfg)
	}
}

func TestTheTestBuildWithoutThePrefixIsTheProductionConfiguration(t *testing.T) {
	cfg, err := loadConfig(func(string) string { return "" }, &bytes.Buffer{}, &bytes.Buffer{}, "digest")
	if err != nil || cfg.Prefix != "" || cfg.AdminTool != "/usr/local/bin/agent-bus" {
		t.Fatalf("config %+v, %v", cfg, err)
	}
}

func TestTheTestBuildRefusesABadPrefix(t *testing.T) {
	getenv := func(key string) string {
		if key == "AGENT_BUS_INSTALL_TEST_PREFIX" {
			return "relative"
		}
		return ""
	}
	if _, err := loadConfig(getenv, &bytes.Buffer{}, &bytes.Buffer{}, "digest"); err == nil {
		t.Fatal("a relative prefix was accepted")
	}
}
