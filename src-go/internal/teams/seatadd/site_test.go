package seatadd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hostVars(t *testing.T, text string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "localhost.yml")
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

// The site is the host_vars' teamchat_site, read by the strict v2 reader; other keys are not read.
func TestHostVarsSite(t *testing.T) {
	for name, c := range map[string]struct{ text, want string }{
		"declared":   {"other: !vault |\n  x\nteamchat_site: desk\n", "desk"},
		"undeclared": {"other: 1\n", ""},
	} {
		got, err := HostVarsSite(hostVars(t, c.text))()
		if err != nil || got != c.want {
			t.Errorf("%s: %q, %v; want %q", name, got, err, c.want)
		}
	}
}

func TestHostVarsSiteRefuses(t *testing.T) {
	if _, err := HostVarsSite(filepath.Join(t.TempDir(), "none.yml"))(); err == nil {
		t.Error("a missing host_vars file was read")
	}
	_, err := HostVarsSite(hostVars(t, "teamchat_site: Not A Label\n"))()
	if err == nil || !strings.Contains(err.Error(), "teamchat_site") {
		t.Errorf("a bad site: %v", err)
	}
}
