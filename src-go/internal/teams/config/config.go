// Package config is where ccy-teams finds everything: the checkout it was built from and the
// paths derived from it, each overridable by the variable the parity suite and the bash
// original honour.
package config

import (
	"encoding/json"
	"path/filepath"
)

// inventoryHostVars is the host_vars file the plays load themselves, from the inventory
// ansible.cfg names (environment/localhost), relative to the checkout.
const inventoryHostVars = "environment/localhost/host_vars/localhost.yml"

// PlayVars is what every play ccy-teams runs is given first, so that the play applies the
// host_vars ccy-teams reads and writes: nothing when HostVars is the inventory's own file,
// which the play loads itself; otherwise "-e @<HostVars>", made absolute (run.bash starts the
// play in the checkout). An extra-vars file outranks the inventory, so each key it sets
// replaces the inventory's, and a key it leaves out is still read from the inventory's file.
// The file is also named to the play as agent_bus_host_vars_file, so a refusal names the file
// the play read.
func (c Config) PlayVars() ([]string, error) {
	if filepath.Clean(c.HostVars) == filepath.Join(c.RootDir, inventoryHostVars) {
		return nil, nil
	}
	file, err := filepath.Abs(c.HostVars)
	if err != nil {
		return nil, err
	}
	named, err := json.Marshal(map[string]string{"agent_bus_host_vars_file": file})
	if err != nil {
		return nil, err
	}
	return []string{"-e", "@" + file, "-e", string(named)}, nil
}

// Config is where ccy-teams finds everything: the fedora-desktop checkout it was built from
// (RootDir, injected at build time) and the paths derived from it, each overridable by the
// environment variable the test suite and the bash version honour.
type Config struct {
	RootDir string
	CcyLib  string
	// HostVars is the host_vars file ccy-teams reads and writes. Its override,
	// CCY_TEAMS_HOST_VARS, is for test suites and acceptance harnesses that declare teams of
	// their own without touching the owner's file; the plays are handed it too (PlayVars).
	HostVars    string
	Launcher    string
	Play        string
	ElementPlay string
	AppliedDir  string
	StateDir    string // <XDG_STATE_HOME or ~/.local/state>/ccy-teams
	Home        string
	// Pingbus is the host's pingbus, the agent team bus kit's (a hot remove re-baselines a seat
	// and posts its goodbye with it).
	Pingbus string
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
		HostVars:    or("CCY_TEAMS_HOST_VARS", filepath.Join(root, inventoryHostVars)),
		Launcher:    or("CCY_TEAMS_LAUNCHER", "/var/local/claude-yolo/claude-yolo"),
		Play:        or("CCY_TEAMS_PLAY", filepath.Join(root, "playbooks/imports/play-agent-bus.yml")),
		ElementPlay: or("CCY_TEAMS_ELEMENT_PLAY", filepath.Join(root, "playbooks/imports/optional/common/play-agent-bus-element.yml")),
		AppliedDir:  or("CCY_TEAMS_APPLIED_DIR", "/var/lib/agent-bus-applied"),
		StateDir:    filepath.Join(or("XDG_STATE_HOME", filepath.Join(home, ".local/state")), "ccy-teams"),
		Home:        home,
		Pingbus:     or("CCY_TEAMS_PINGBUS", "/usr/local/share/agent-bus/kit/pingbus"),
	}
}
