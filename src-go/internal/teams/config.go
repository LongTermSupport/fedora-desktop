package teams

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
	// TrustPlays lets Play and ElementPlay name a program outside the checkout (the test
	// suites' stubs). Off, a play runs only from inside RootDir: it runs under Ansible with the
	// user's sudo, so an environment variable must not be able to substitute another program.
	TrustPlays bool
}

// LoadConfig resolves the configuration from root and the environment (getenv).
func LoadConfig(root string, getenv func(string) string) Config {
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
		TrustPlays:  getenv("CCY_TEAMS_TRUST_PLAYS") == "1",
	}
}
