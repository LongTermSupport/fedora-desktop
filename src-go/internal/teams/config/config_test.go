package config

import "testing"

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
