package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A play reads the inventory's host_vars itself, so a run with no override is given nothing and
// runs as before; an override is given to every play as an extra-vars file, absolute (run.bash
// starts the play in the checkout, not where ccy-teams was started), and named to the play as
// agent_bus_host_vars_file, so its messages name the file it read.
func TestPlayVarsNameTheOverrideOnly(t *testing.T) {
	for _, hv := range []string{"", "/r/environment/localhost/host_vars/localhost.yml", "/r//environment/./localhost/host_vars/localhost.yml"} {
		c := Load("/r", func(k string) string {
			return map[string]string{"HOME": "/h", "CCY_TEAMS_HOST_VARS": hv}[k]
		})
		got, err := c.PlayVars()
		if err != nil || len(got) != 0 {
			t.Errorf("CCY_TEAMS_HOST_VARS=%q: got %q, %v; want none", hv, got, err)
		}
	}
	c := Load("/r", func(k string) string {
		return map[string]string{"HOME": "/h", "CCY_TEAMS_HOST_VARS": "/run/hv.yml"}[k]
	})
	want := []string{"-e", "@/run/hv.yml", "-e", `{"agent_bus_host_vars_file":"/run/hv.yml"}`}
	if got, err := c.PlayVars(); err != nil || !slices.Equal(got, want) {
		t.Errorf("override: got %q, %v", got, err)
	}
	t.Chdir(t.TempDir())
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	c.HostVars = `h"v.yml`
	abs := filepath.Join(wd, `h"v.yml`)
	want = []string{"-e", "@" + abs, "-e", `{"agent_bus_host_vars_file":"` + strings.ReplaceAll(abs, `"`, `\"`) + `"}`}
	if got, err := c.PlayVars(); err != nil || !slices.Equal(got, want) {
		t.Errorf("relative override: got %q, %v", got, err)
	}
}

func TestLoadDefaultsAndOverrides(t *testing.T) {
	c := Load("/r", func(k string) string {
		return map[string]string{"HOME": "/h"}[k]
	})
	want := Config{
		RootDir: "/r", CcyLib: "/var/local/claude-yolo/lib",
		HostVars:    "/r/environment/localhost/host_vars/localhost.yml",
		Launcher:    "/var/local/claude-yolo/claude-yolo",
		Play:        "/r/playbooks/imports/play-agent-bus.yml",
		ElementPlay: "/r/playbooks/imports/optional/common/play-agent-bus-element.yml",
		AppliedDir:  "/var/lib/agent-bus-applied",
		StateDir:    "/h/.local/state/ccy-teams",
		Home:        "/h",
		Pingbus:     "/usr/local/share/agent-bus/kit/pingbus",
	}
	if c != want {
		t.Fatalf("got %+v\nwant %+v", c, want)
	}
	c = Load("/r", func(k string) string {
		return map[string]string{
			"HOME": "/h", "XDG_STATE_HOME": "/s", "CCY_LIB": "/l", "CCY_TEAMS_HOST_VARS": "/hv",
			"CCY_TEAMS_LAUNCHER": "/x", "CCY_TEAMS_PLAY": "/p", "CCY_TEAMS_ELEMENT_PLAY": "/e",
			"CCY_TEAMS_APPLIED_DIR": "/a", "CCY_TEAMS_PINGBUS": "/pb",
		}[k]
	})
	if c.StateDir != "/s/ccy-teams" || c.CcyLib != "/l" || c.HostVars != "/hv" || c.Launcher != "/x" ||
		c.Play != "/p" || c.ElementPlay != "/e" || c.AppliedDir != "/a" || c.Pingbus != "/pb" {
		t.Fatalf("overrides %+v", c)
	}
}
