// Package config is where ccy-teams finds everything: the checkout it was built from and the
// paths derived from it, each overridable by the variable the parity suite and the bash
// original honour.
package config

import "path/filepath"

// Config is where ccy-teams finds everything: the fedora-desktop checkout it was built from
// (RootDir, injected at build time) and the paths derived from it, each overridable by the
// environment variable the test suite and the bash version honour.
type Config struct {
	RootDir     string
	CcyLib      string
	HostVars    string
	Launcher    string
	Play        string
	ElementPlay string
	AppliedDir  string
	StateDir    string // <XDG_STATE_HOME or ~/.local/state>/ccy-teams
	Home        string
}

// Load resolves the configuration from root and the environment (getenv).
func Load(root string, getenv func(string) string) Config {
	or := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}
	home := getenv("HOME")
	return Config{
		RootDir:     root,
		CcyLib:      or("CCY_LIB", "/var/local/claude-yolo/lib"),
		HostVars:    or("CCY_TEAMS_HOST_VARS", filepath.Join(root, "environment/localhost/host_vars/localhost.yml")),
		Launcher:    or("CCY_TEAMS_LAUNCHER", "/var/local/claude-yolo/claude-yolo"),
		Play:        or("CCY_TEAMS_PLAY", filepath.Join(root, "playbooks/imports/play-agent-bus.yml")),
		ElementPlay: or("CCY_TEAMS_ELEMENT_PLAY", filepath.Join(root, "playbooks/imports/optional/common/play-agent-bus-element.yml")),
		AppliedDir:  or("CCY_TEAMS_APPLIED_DIR", "/var/lib/agent-bus-applied"),
		StateDir:    filepath.Join(or("XDG_STATE_HOME", filepath.Join(home, ".local/state")), "ccy-teams"),
		Home:        home,
	}
}
