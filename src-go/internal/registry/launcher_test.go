package registry

import (
	"errors"
	"strings"
	"testing"
)

// A record's launcher is the fixed table's entry for its prefix, never the path it records: a
// record is a file any process of the user can write, and its launcher= line would otherwise be
// a program the next boot runs (Plan 00266 Task A.4).
func TestLauncherIsTheTablesForThePrefix(t *testing.T) {
	none := func(string) string { return "" }
	for prefix, want := range map[string]string{"ccy": "/var/local/claude-yolo/claude-yolo", "cc": "/var/local/claude-code/cc"} {
		got, err := Launcher(none, Record{Prefix: prefix, Launcher: "/tmp/evil"})
		if err != nil || got != want {
			t.Errorf("Launcher(%s) = %q, %v; want %q", prefix, got, err, want)
		}
	}
}

func TestLauncherRefusesAnUnknownPrefix(t *testing.T) {
	for _, prefix := range []string{"", "opencode", "../ccy", "CCY", "ccy/"} {
		_, err := Launcher(func(string) string { return "" }, Record{Prefix: prefix})
		var unknown *UnknownPrefixError
		if !errors.As(err, &unknown) {
			t.Fatalf("Launcher(%q) err = %v; want an UnknownPrefixError", prefix, err)
		}
		if msg := err.Error(); !strings.Contains(msg, "'"+prefix+"'") || !strings.Contains(msg, "ccy or cc") {
			t.Errorf("Launcher(%q) says %q; want the prefix and the two it knows", prefix, msg)
		}
	}
}

// The table's directory is host-only environment, for the suites that run a fake launcher: it
// must be absolute, and only the prefix's own name is joined to it.
func TestLauncherDirectoryOverride(t *testing.T) {
	env := map[string]string{LauncherDirEnv: "/launch"}
	got, err := Launcher(func(k string) string { return env[k] }, Record{Prefix: "cc"})
	if err != nil || got != "/launch/cc" {
		t.Fatalf("Launcher with %s=/launch = %q, %v; want /launch/cc", LauncherDirEnv, got, err)
	}
	env[LauncherDirEnv] = "launch"
	if _, err := Launcher(func(k string) string { return env[k] }, Record{Prefix: "ccy"}); err == nil ||
		!strings.Contains(err.Error(), LauncherDirEnv) {
		t.Fatalf("a relative %s: err = %v; want a refusal naming it", LauncherDirEnv, err)
	}
}
