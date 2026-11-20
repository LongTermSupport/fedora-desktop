package teamtest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// V2Team is the v2 (teamchat) team the fake machine can hold.
const V2Team = "ops"

// currentMS is when a current record was written: far enough ahead that any clock a test sets
// finds it current. expiredMS is long past.
const (
	currentMS int64 = 4_102_444_800_000
	expiredMS int64 = 1_000
)

// V2Member is a status record's member entry; online is set for an LXC member only, as SR1 has it.
func V2Member(handle, kind string, online bool) status.Member {
	m := status.Member{
		Handle: handle, Kind: kind, Role: proto.RoleWorker, State: "enrolled",
		Rooms: map[string]string{proto.PurposeMain: "join"},
	}
	if kind == status.KindLXC {
		m.Online = &online
	}
	return m
}

// WriteV2Status writes team's status record under the world's teamchat root, as health writes
// it: current, or expired long ago.
func (w *World) WriteV2Status(t *testing.T, team string, current bool, members ...status.Member) {
	t.Helper()
	id := mustTeam(t, team)
	written := expiredMS
	if current {
		written = currentMS
	}
	rec := status.Record{
		V: 1, Team: id, Name: team, Origin: status.OriginV2, Seq: 1,
		WrittenMS: written, ExpiresMS: written + status.Lifetime.Milliseconds(),
		Homeserver: status.Homeserver{Unit: "active", Probe: status.ProbeOK},
		Members:    members, Backup: status.Backup{LastRun: status.RunNone},
	}
	path := w.Host.TeamchatRoot + paths.StatusFile(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := status.Write(path, rec, -1); err != nil {
		t.Fatal(err)
	}
}

// V2Seat is one entry of a team's LXC record.
func V2Seat(t *testing.T, container, user, checkout string, seat int, handle, st, why string) lxcstate.Entry {
	t.Helper()
	box, err := paths.ParseContainerName(container)
	if err != nil {
		t.Fatal(err)
	}
	guest, err := paths.ParseGuestUser(user)
	if err != nil {
		t.Fatal(err)
	}
	return lxcstate.Entry{
		Container: box, Checkout: checkout, Seat: seat, User: guest, Handle: handle, Status: st,
		SinceMS: 1_760_000_000_000, Why: why,
	}
}

// WriteV2Lxc writes team's LXC record under the world's teamchat root, as lxc apply writes it.
func (w *World) WriteV2Lxc(t *testing.T, team string, entries ...lxcstate.Entry) {
	t.Helper()
	id := mustTeam(t, team)
	if err := os.MkdirAll(w.Host.TeamchatRoot+paths.StatusRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := lxcstate.WriteRecord(w.Host.TeamchatRoot, lxcstate.Record{V: 1, Team: id, Members: entries}, -1); err != nil {
		t.Fatal(err)
	}
}

func mustTeam(t *testing.T, team string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(team)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
