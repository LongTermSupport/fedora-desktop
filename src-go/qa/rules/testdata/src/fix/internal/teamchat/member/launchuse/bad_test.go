package launchuse

import (
	"testing"

	"fix/internal/teamchat/member/cli"
	"fix/internal/teamchat/member/state"
)

// A test names and compares the launch values freely: the rule judges no test file.
func TestLaunches(t *testing.T) {
	k := keeper{cfg: cli.Launch{Mode: cli.LaunchClaude}}
	if k.sessionLaunch() != state.LaunchClaude || k.cfg.Mode != cli.LaunchClaude {
		t.Fatal("not claude")
	}
}
