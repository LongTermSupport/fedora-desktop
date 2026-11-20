package teamtest

import (
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// A v2 team put on the fake machine is read back as teamchat-admin and lxc apply wrote it, under
// the world's teamchat root, never the machine's own.
func TestAV2TeamIsWrittenUnderTheWorldsTeamchatRoot(t *testing.T) {
	w := New(t)
	if w.Host.TeamchatRoot == "" {
		t.Fatal("the world reads the machine's own v2 records")
	}
	w.WriteV2Status(t, V2Team, true, V2Member("proj.1+desk.lxc", status.KindLXC, true), V2Member("proj.2+local.podman", "ccy", false))
	w.WriteV2Lxc(t, V2Team, V2Seat(t, "box", "agent", "/srv/proj", 1, "proj.1+desk.lxc", lxcstate.StatusJoined, ""))
	team := mustTeam(t, V2Team)
	rec, found, err := status.Read(w.Host.TeamchatRoot+paths.StatusFile(team), team)
	if err != nil || !found || len(rec.Members) != 2 {
		t.Fatalf("status: %+v %v %v", rec, found, err)
	}
	if v := status.Shown(rec, found, nil, time.Unix(1_800_000_000, 0)); !v.Current {
		t.Fatalf("a current record is shown as expired: %+v", v.Problems)
	}
	lxc, found, err := lxcstate.ReadRecord(w.Host.TeamchatRoot, team)
	if err != nil || !found || len(lxc.Members) != 1 {
		t.Fatalf("lxc: %+v %v %v", lxc, found, err)
	}
	w.WriteV2Status(t, V2Team, false)
	rec, found, err = status.Read(w.Host.TeamchatRoot+paths.StatusFile(team), team)
	if v := status.Shown(rec, found, err, time.Unix(1_800_000_000, 0)); v.Current {
		t.Fatal("an expired record is shown as current")
	}
}
