package forge_test

import (
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/forge"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
)

// n3d W: a Checker is the engine's and teamsync's team.Forge, so its outcomes and errors are
// team's own, one type and one value each, matched by errors.Is on either side.
func TestTheCheckerIsTeamsForge(t *testing.T) {
	forges := []team.Forge{forge.New(forge.Config{Login: forge.NewLogin(nil, nil)})}
	if _, ok := forges[0].(*forge.Checker); !ok {
		t.Fatalf("%T", forges[0])
	}
	if forge.Proven != team.ForgeProven {
		t.Errorf("Proven = %q", forge.Proven)
	}
	// A *forge.Result is a *team.ForgeResult only when the two are one type.
	rs := []*team.ForgeResult{&forge.Result{Outcome: forge.Proven}}
	if rs[0].Outcome != team.ForgeProven {
		t.Errorf("Result %+v", rs[0])
	}
	if !errors.Is(forge.ErrDown, team.ErrForgeDown) || !errors.Is(forge.ErrLogin, team.ErrForgeLogin) {
		t.Error("forge's errors are not team's")
	}
}
