package picker

import (
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

const v2Box = "proj.1+desk.lxc"

// withV2 puts teamtest.V2Team on the machine with one LXC seat, its session not online.
func (h rig) withV2(t *testing.T) {
	t.Helper()
	h.WriteV2Status(t, teamtest.V2Team, true, teamtest.V2Member(v2Box, status.KindLXC, false))
	h.WriteV2Lxc(t, teamtest.V2Team, teamtest.V2Seat(t, "box", "agent", "/srv/proj", 1, v2Box, lxcstate.StatusJoined, ""))
	h.On("systemctl is-active teamchat-server@"+teamtest.V2Team+".service", "active\n", 0)
	// Offers asks root's LXC without a password: none here, so the line is kept and its choice
	// says why.
	h.OnErr("sudo -n lxc-ls", "sudo: a password is required\n", 1)
}

// TC1, TC17: a v2 team's row offers only what reaches a v2 team, its Close every session among them; no
// v1 flow (apply, remove, restart, wind-down, Element, adding a member) is offered on it.
func TestAV2TeamsRowOffersOnlyWhatReachesIt(t *testing.T) {
	h := newRig(t)
	h.withV2(t)
	r := h.run1(t)
	var team board.Row
	for _, row := range r.snap.Rows {
		if row.Team == teamtest.V2Team && row.Kind == board.KindTeam {
			team = row
		}
	}
	want := []string{helpLabel, "Close every session of " + teamtest.V2Team + " (compact, then exit)"}
	if got := h.labels(r, team); !slices.Equal(got, want) {
		t.Fatalf("menu %q, want %q", got, want)
	}
}

// TC16: a v2 LXC seat's row offers starting its session where the start can work, and help.
func TestAV2LxcSeatsRowOffersItsStartWhereItCanWork(t *testing.T) {
	h := newRig(t)
	h.withV2(t)
	r := h.run1(t)
	row := h.row(t, r.snap, v2Box)
	r.lxcStarts[lxcStartKey(row)] = true
	want := []string{"Start its session in its LXC container", helpLabel}
	if got := h.labels(r, row); !slices.Equal(got, want) {
		t.Fatalf("menu %q, want %q", got, want)
	}
	r.lxcStarts[lxcStartKey(row)] = false
	if got := h.labels(r, row); !slices.Equal(got, []string{helpLabel}) {
		t.Fatalf("menu %q, want help alone", got)
	}
}

// The list's heading counts a v2 team as a team.
func TestTheListHeadingCountsAV2Team(t *testing.T) {
	h := newRig(t)
	h.withV2(t)
	r := h.run1(t)
	if got := r.listHeading().Context; got != "2 teams" {
		t.Fatalf("heading context %q", got)
	}
}
