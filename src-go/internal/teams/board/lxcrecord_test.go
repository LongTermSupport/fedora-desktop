package board

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// The LXC member record play-agent-bus.yml writes (SPEC-lxc-stopped-members.md L8, L10): the
// joined member proj.box+role.lxc's container was stopped at the last apply, a new member in
// container-c was deferred, and a member leaving from container-d is deferred too.
const lxcRecordAlpha = `{"version": 1, "team": "alpha", "members": [
 {"container": "box", "user": "agent", "state": "present", "status": "stopped", "handle": "proj.box+role.lxc",
  "checkout": "/srv/box", "why": "LXC container box is STOPPED: kept", "since": 1792000000},
 {"container": "container-c", "user": "agent", "state": "present", "status": "deferred", "handle": null,
  "checkout": "/srv/c", "why": "LXC container container-c is STOPPED: start it", "since": 1792000000},
 {"container": "container-d", "user": "agent", "state": "absent", "status": "deferred", "handle": "proj.2+local.podman",
  "checkout": "/srv/d", "why": "LXC container container-d is STOPPED: parked", "since": 1792000000}]}`

// writeLxcRecord is team alpha's record.
func writeLxcRecord(t *testing.T, h rig, body string) {
	t.Helper()
	teamtest.MustWrite(t, filepath.Join(h.c.AppliedDir, "lxc", "alpha.json"), body)
}

func collectRows(t *testing.T, h rig) []Row {
	t.Helper()
	s, err := h.c.Collect(t.Context())
	if err != nil {
		t.Fatalf("%v: %s", err, h.ErrOut.String())
	}
	return s.Rows
}

// rowsOf is every row whose Member is member.
func rowsOf(rows []Row, member string) []Row {
	var found []Row
	for _, r := range rows {
		if r.Member == member {
			found = append(found, r)
		}
	}
	return found
}

// onlyRowOf is the one row whose Member is member.
func onlyRowOf(t *testing.T, rows []Row, member string) Row {
	t.Helper()
	found := rowsOf(rows, member)
	if len(found) != 1 {
		t.Fatalf("%d rows for %s, want 1: %+v", len(found), member, found)
	}
	return found[0]
}

func TestAJoinedMemberWhoseContainerWasStoppedSaysSo(t *testing.T) {
	h := populated(t)
	writeLxcRecord(t, h, lxcRecordAlpha)
	row := onlyRowOf(t, collectRows(t, h), "proj.box+role.lxc")
	if row.State != "active" || row.Where != "ops, container stopped at the last apply" {
		t.Fatalf("row %+v", row)
	}
}

func TestADeferredNewMemberHasARowOfItsOwn(t *testing.T) {
	h := populated(t)
	writeLxcRecord(t, h, lxcRecordAlpha)
	row := onlyRowOf(t, collectRows(t, h), "agent@container-c")
	want := Row{
		Team: "alpha", Member: "agent@container-c", Kind: "lxc", State: "deferred", Applied: "-", Session: "-",
		Restore: "-", Where: "deferred: LXC container container-c is STOPPED: start it", Handle: "-", Dir: "-",
	}
	if row != want {
		t.Fatalf("row\n%+v\nwant\n%+v", row, want)
	}
}

func TestADeferredLeavingMemberSaysItIsLeavingOnItsOwnRow(t *testing.T) {
	h := populated(t)
	writeLxcRecord(t, h, lxcRecordAlpha)
	rows := collectRows(t, h)
	row := onlyRowOf(t, rows, "proj.2+local.podman")
	if row.State != "deferred" || row.Where != "deferred (leaving): LXC container container-d is STOPPED: parked" {
		t.Fatalf("row %+v", row)
	}
	if again := rowsOf(rows, "agent@container-d"); len(again) != 0 {
		t.Fatalf("a member the roster lists is not given a second row: %+v", again)
	}
}

func TestATeamWithNoLxcRecordShowsNoLxcWords(t *testing.T) {
	h := populated(t)
	rows := collectRows(t, h)
	if row := onlyRowOf(t, rows, "proj.box+role.lxc"); row.Where != "ops" {
		t.Fatalf("row %+v", row)
	}
	if strings.Contains(rows[0].Where, "LXC") {
		t.Fatalf("team where %q", rows[0].Where)
	}
}

func TestAnUnreadableLxcRecordIsSaidOnTheTeamRow(t *testing.T) {
	h := populated(t)
	writeLxcRecord(t, h, "{")
	rows := collectRows(t, h)
	if !strings.Contains(rows[0].Where, "the LXC member record "+filepath.Join(h.c.AppliedDir, "lxc", "alpha.json")) {
		t.Fatalf("team where %q", rows[0].Where)
	}
}

func TestDeferredIsShownYellow(t *testing.T) {
	if got := wordColour("deferred"); got != cYellow {
		t.Fatalf("colour %q", got)
	}
}
