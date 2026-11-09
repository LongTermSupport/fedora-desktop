package engine

import (
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// rosterContent is a valid roster content of team id at rev, naming this member.
func rosterContent(id paths.TeamID, rev int64) json.RawMessage {
	r := proto.Roster{
		Team: id.String(), Name: "Operations", Rev: rev, Humans: []string{alice(id)},
		Members: map[string]proto.RosterMember{agent(id, 1): {Role: proto.RoleWorker, HumanText: true}},
		Rooms:   proto.SideRooms{Questions: qRoom, Reports: "!" + strings.Repeat("r", 43)},
		Forge:   proto.Forge{Kind: proto.ForgeGitHub, Org: "example-org"}, Limits: proto.DefaultLimits(),
	}
	return r.Encode().Compact()
}

func (w *world) rosterCache(id paths.TeamID) (state.RosterCache, error) {
	return state.Read(w.root, w.layout.Seat(id).RosterCache(), state.MaxRosterCache, state.DecodeRosterCache)
}

// cached is the roster the engine holds for id, and its rev; 0 for none.
func (w *world) cached(id paths.TeamID) (proto.Roster, int64) {
	w.t.Helper()
	var (
		r   proto.Roster
		rev int64
	)
	w.local(func() {
		if h := w.e.teams[id]; h != nil && h.roster != nil {
			r, rev = *h.roster, h.rosterRev
		}
	})
	return r, rev
}

// n3d-D3: SaveRoster writes roster.json once per new rev, and holds the roster.
func TestN3dD3SaveRosterWritesOncePerRev(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	var eng team.Engine = w.e
	check(t, eng.SaveRoster(t.Context(), ops, 3, rosterContent(ops, 3)))
	c, err := w.rosterCache(ops)
	check(t, err)
	if c.Rev != 3 {
		t.Fatalf("roster.json = %+v", c)
	}
	if r, rev := w.cached(ops); rev != 3 || r.Name != "Operations" {
		t.Fatalf("the held roster = %+v at %d", r, rev)
	}
	check(t, w.root.Unlink(w.layout.Seat(ops).RosterCache()))
	check(t, eng.SaveRoster(t.Context(), ops, 3, rosterContent(ops, 3)))
	if _, err := w.rosterCache(ops); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the same rev was written again: %v", err)
	}
	check(t, eng.SaveRoster(t.Context(), ops, 4, rosterContent(ops, 4)))
	if c, err := w.rosterCache(ops); err != nil || c.Rev != 4 {
		t.Fatalf("rev 4 = %+v, %v", c, err)
	}
}

// n3d-D3: a roster that does not decode for this team, or whose rev is not the one given, is
// refused and written nowhere; so is a save for a team not held.
func TestN3dD3SaveRosterRefusesARosterThatDoesNotDecode(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	w.held(ops)
	for name, c := range map[string]json.RawMessage{
		"not json":       json.RawMessage(`{`),
		"no roster":      json.RawMessage(`{"x":1}`),
		"another team's": rosterContent(dev, 3),
		"another rev":    rosterContent(ops, 4),
	} {
		if err := w.e.SaveRoster(t.Context(), ops, 3, c); err == nil {
			t.Errorf("%s: saved", name)
		}
	}
	if _, err := w.rosterCache(ops); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a refused roster was written: %v", err)
	}
	if err := w.e.SaveRoster(t.Context(), dev, 3, rosterContent(dev, 3)); !errors.Is(err, team.ErrRemoved) {
		t.Fatalf("a save for a team not held = %v", err)
	}
}

// n3d-D3: Add reads the cache back; a missing or undecodable file is no cache, logged.
func TestN3dD3AddReadsTheRosterCache(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev, qa := tid(t, "ops"), tid(t, "dev"), tid(t, "qa")
	for _, id := range []paths.TeamID{ops, dev} {
		check(t, w.root.MkdirAll(w.layout.Seat(id).Dir()))
	}
	check(t, state.Write(w.root, w.layout.Seat(ops).RosterCache(), state.RosterCache{Rev: 5, Content: rosterContent(ops, 5)}))
	check(t, state.Write(w.root, w.layout.Seat(dev).RosterCache(), state.RosterCache{Rev: 5, Content: json.RawMessage(`{"x":1}`)}))
	for _, id := range []paths.TeamID{ops, dev, qa} {
		w.held(id)
	}
	if r, rev := w.cached(ops); rev != 5 || r.Rev != 5 {
		t.Fatalf("the read cache = %+v at %d", r, rev)
	}
	for _, id := range []paths.TeamID{dev, qa} {
		if _, rev := w.cached(id); rev != 0 {
			t.Fatalf("team %s holds a cache", id)
		}
	}
	log := w.log.String()
	if !strings.Contains(log, "team dev: the roster cache is not used") || !strings.Contains(log, "team qa: no roster cache") {
		t.Fatalf("link.log = %q", log)
	}
}

// n3d-D3: a cache with no seat.json to check it against is no cache.
func TestN3dD3NoSeatJSONIsNoCache(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	check(t, w.root.MkdirAll(w.layout.Seat(ops).Dir()))
	check(t, state.Write(w.root, w.layout.Seat(ops).RosterCache(), state.RosterCache{Rev: 5, Content: rosterContent(ops, 5)}))
	check(t, w.e.Add(t.Context(), ops, w.seat(ops, true, nil), nil))
	if _, rev := w.cached(ops); rev != 0 {
		t.Fatal("a cache was read with no seat.json")
	}
	if err := w.e.SaveRoster(t.Context(), ops, 5, rosterContent(ops, 5)); err == nil {
		t.Fatal("a roster was saved with no seat.json")
	}
}

// n3d-D3: Remove unlinks the cache with the store.
func TestN3dD3RemoveUnlinksTheRosterCache(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	w.held(ops)
	w.held(dev)
	check(t, w.e.SaveRoster(t.Context(), ops, 3, rosterContent(ops, 3)))
	for _, id := range []paths.TeamID{ops, dev} {
		if _, err := w.e.Remove(t.Context(), id); err != nil {
			t.Fatalf("remove %s: %v", id, err)
		}
	}
	if _, err := w.rosterCache(ops); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("roster.json after the removal: %v", err)
	}
}
