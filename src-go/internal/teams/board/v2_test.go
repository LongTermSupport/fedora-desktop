package board

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// v2Box and v2Host are the handles of the v2 team's LXC seat and its host member.
const (
	v2Box  = "proj.1+desk.lxc"
	v2Host = "proj.9+local.podman"
)

// withV2Team puts teamtest.V2Team on the machine: a current status record with an LXC member
// online and a host member, the LXC record of its seat, and its homeserver unit active.
func withV2Team(t *testing.T, h rig, current bool) {
	t.Helper()
	h.WriteV2Status(t, teamtest.V2Team, current, teamtest.V2Member(v2Box, status.KindLXC, true), teamtest.V2Member(v2Host, "ccy", false))
	h.WriteV2Lxc(t, teamtest.V2Team, teamtest.V2Seat(t, "box", "agent", "/srv/proj", 1, v2Box, lxcstate.StatusJoined, ""))
	h.On("systemctl is-active teamchat-server@"+teamtest.V2Team+".service", "active\n", 0)
}

// TC15: a v2 team is on the board, apart from the v1 teams: its row from its status record, its
// state its homeserver unit's, its members the record's, an LXC member with its container and
// whether its session is online. No v1 flow reads it from Teams.
func TestAV2TeamIsOnTheBoardFromItsStatusRecord(t *testing.T) {
	h := populated(t)
	withV2Team(t, h, true)
	s, err := h.c.Collect(t.Context())
	if err != nil {
		t.Fatalf("%v: %s", err, h.ErrOut.String())
	}
	if slices.Contains(s.Teams, teamtest.V2Team) || !slices.Equal(s.V2Teams, []string{teamtest.V2Team}) {
		t.Fatalf("teams %q, v2 teams %q", s.Teams, s.V2Teams)
	}
	var team Row
	for _, r := range s.Rows {
		if r.Team == teamtest.V2Team && r.Kind == KindTeam {
			team = r
		}
	}
	if !team.V2 || team.State != "up" || !strings.Contains(team.Where, "teamchat (v2)") || strings.Contains(team.Where, "health") {
		t.Fatalf("team row %+v", team)
	}
	box := onlyRowOf(t, s.Rows, v2Box)
	if !box.V2 || box.Kind != "lxc" || box.Team != teamtest.V2Team || box.Role != "worker" ||
		box.State != "enrolled, on the bus at the last health check" || box.Where != "container box, its session on the bus at the last health check" {
		t.Fatalf("LXC row %+v", box)
	}
	host := onlyRowOf(t, s.Rows, v2Host)
	if !host.V2 || host.Kind != "ccy" || host.State != "enrolled" || host.Dir != "-" {
		t.Fatalf("host row %+v", host)
	}
}

// TC15: an expired record is said in WHERE, never shown as current; an LXC seat that is not
// online says why the LXC record gives.
func TestAV2TeamsExpiredRecordIsSaid(t *testing.T) {
	h := populated(t)
	h.WriteV2Status(t, teamtest.V2Team, false, teamtest.V2Member(v2Box, status.KindLXC, false))
	h.WriteV2Lxc(t, teamtest.V2Team, teamtest.V2Seat(t, "box", "agent", "/srv/proj", 1, v2Box, lxcstate.StatusOffline, "offline: container stopped"))
	h.On("systemctl is-active teamchat-server@"+teamtest.V2Team+".service", "inactive\n", 3)
	rows := collectRows(t, h)
	var team Row
	for _, r := range rows {
		if r.Team == teamtest.V2Team && r.Kind == KindTeam {
			team = r
		}
	}
	if team.State != "down (inactive)" || !strings.Contains(team.Where, "health has not run since 1970-01-01T00:00:01Z (systemctl status teamchat-health.timer)") {
		t.Fatalf("team row %+v", team)
	}
	box := onlyRowOf(t, rows, v2Box)
	if box.State != "enrolled, off the bus at the last health check" ||
		box.Where != "container box, its session off the bus at the last health check (offline: container stopped)" {
		t.Fatalf("LXC row %+v", box)
	}
}

// A machine whose only team is a v2 one still has a board.
func TestAMachineWithOnlyAV2TeamHasABoard(t *testing.T) {
	h := newRig(t)
	h.WriteHostVars(t, "agent_bus_address: 192.0.2.10\n")
	h.On("systemctl list-units", "", 0)
	h.OnDeclare("status", "")
	h.On("tmux -L ccy list-sessions", "", 0)
	h.OnNotARepository()
	withV2Team(t, h, true)
	rows := collectRows(t, h)
	if len(rowsOf(rows, v2Box)) != 1 {
		t.Fatalf("rows %+v\n%s", rows, h.ErrOut.String())
	}
}

// A v2 team whose name is a v1 team's is not a second team: the v1 one is shown, saying so
// (ccy-teams system refuses such a name).
func TestAV2NameThatIsAV1TeamsIsSaidOnTheV1Row(t *testing.T) {
	h := populated(t)
	h.WriteV2Status(t, "alpha", true)
	s, err := h.c.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.V2Teams) != 0 {
		t.Fatalf("v2 teams %q", s.V2Teams)
	}
	for _, r := range s.Rows {
		if r.Team == "alpha" && r.Kind == KindTeam && !strings.Contains(r.Where, "also a teamchat (v2) team of this name") {
			t.Fatalf("alpha row %+v", r)
		}
	}
}

// The graph draws every row --list shows, a v2 team's too.
func TestMermaidDrawsAV2Team(t *testing.T) {
	h := populated(t)
	withV2Team(t, h, true)
	s, err := h.c.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := Mermaid(s); !strings.Contains(got, "team ops: teamchat (v2)") || !strings.Contains(got, v2Box) {
		t.Fatalf("graph lacks the v2 team:\n%s", got)
	}
}
