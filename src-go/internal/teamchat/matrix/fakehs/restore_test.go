package fakehs

import (
	"encoding/json"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// fakeHold is a held sync's timeout that ends only when the test releases it.
type fakeHold struct {
	armed   chan time.Duration
	release chan time.Time
}

func newFakeHold() *fakeHold {
	return &fakeHold{armed: make(chan time.Duration, 4), release: make(chan time.Time)}
}

func (h *fakeHold) hold(d time.Duration) (<-chan time.Time, func() bool) {
	h.armed <- d
	return h.release, func() bool { return true }
}

// withRoster puts a roster in the team room at the epoch given.
func (tm *team) withRoster(epoch any) {
	tm.t.Helper()
	tm.ok(tm.putState(tm.admin, rosterType, "", map[string]any{"v": 1, "team": "team-a", "restore_epoch": epoch}))
}

func (tm *team) rosterEvent() map[string]any {
	tm.t.Helper()
	return tm.ok(tm.call(tm.agent, "GET", tm.roomPath("state/"+rosterType+"/"), url.Values{"format": {"event"}}, nil))
}

func (tm *team) epoch() string {
	tm.t.Helper()
	return string(mustNumber(tm.t, field(tm.t, tm.rosterEvent(), "content", "restore_epoch")))
}

func mustNumber(t *testing.T, v any) json.Number {
	t.Helper()
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("not a number: %v", v)
	}
	return json.Number(strconv.FormatFloat(f, 'f', -1, 64))
}

func (tm *team) bodies() []any {
	tm.t.Helper()
	page := tm.ok(tm.call(tm.agent, "GET", tm.roomPath("messages"), url.Values{
		"dir": {"b"}, "limit": {"100"}, "filter": {`{"types":["m.room.message"]}`},
	}, nil))
	chunk := list(tm.t, page["chunk"])
	out := make([]any, 0, len(chunk))
	for _, e := range chunk {
		out = append(out, bodyOf(tm.t, e))
	}
	return out
}

// TestRestoreRollsTheDatabaseBack is gap 6: what landed after the snapshot is gone (events,
// tokens, room account data), and the roster's restore_epoch is one higher, sent by the
// roster's own sender, in the same step.
func TestRestoreRollsTheDatabaseBack(t *testing.T) {
	tm := newTeam(t)
	tm.withRoster(0)
	tm.ok(tm.send(tm.human, "before", map[string]any{"body": "before"}))
	snap := tm.s.Snapshot()

	tm.ok(tm.send(tm.human, "after", map[string]any{"body": "after"}))
	late, err := tm.s.MintToken(tm.agent)
	if err != nil {
		t.Fatal(err)
	}
	dataPath := "/_matrix/client/v3/user/" + q(tm.agent) + "/rooms/" + q(tm.room) + "/account_data/teamchat.floor"
	tm.ok(tm.call(tm.agent, "PUT", dataPath, nil, map[string]any{"v": 1}))
	if err := tm.s.RestoreTo(snap); err != nil {
		t.Fatal(err)
	}

	if got := tm.bodies(); len(got) != 1 || got[0] != "before" {
		t.Errorf("messages after the restore %v", got)
	}
	if got := tm.epoch(); got != "1" {
		t.Errorf("restore_epoch %s, want 1", got)
	}
	if sender := field(t, tm.rosterEvent(), "sender"); sender != tm.admin {
		t.Errorf("the raised roster was sent by %v", sender)
	}
	tm.tok["late"] = late
	tm.refused(tm.call("late", "GET", "/_matrix/client/v3/account/whoami", nil, nil), 401, "M_UNKNOWN_TOKEN", "Unknown access token")
	tm.refused(tm.call(tm.agent, "GET", dataPath, nil, nil), 404, "M_NOT_FOUND", "")

	// A second restore to the same snapshot still raises the epoch past the one in use.
	if err := tm.s.RestoreTo(snap); err != nil {
		t.Fatal(err)
	}
	if got := tm.epoch(); got != "2" {
		t.Errorf("restore_epoch after a second restore %s, want 2", got)
	}
}

// TestRestoreUnderAHeldSync is SY7's restore: a sync held from a position the restore takes
// away waits out its timeout and answers the server's own lower next_batch (H-C14), while a
// state read straight after the restore sees the raised epoch.
func TestRestoreUnderAHeldSync(t *testing.T) {
	hold := newFakeHold()
	tm := newTeamWith(t, Options{Hold: hold.hold})
	tm.withRoster(json.Number("4"))
	filter := syncFilter(t, nil, 10, false)
	snap := tm.s.Snapshot()
	for i := range 3 {
		tm.ok(tm.send(tm.human, "lost"+strconv.Itoa(i), map[string]any{"body": "lost"}))
	}
	ahead := text(t, tm.sync(tm.agent, "", filter, 0)["next_batch"])
	done := make(chan map[string]any, 1)
	go func() { done <- tm.sync(tm.agent, ahead, filter, 30000) }()
	if d := <-hold.armed; d != 30*time.Second {
		t.Fatalf("the sync held for %v", d)
	}
	if err := tm.s.RestoreTo(snap); err != nil {
		t.Fatal(err)
	}
	if got := tm.epoch(); got != "5" {
		t.Errorf("restore_epoch straight after the restore %s, want 5", got)
	}
	select {
	case got := <-done:
		t.Fatalf("the held sync ended before its timeout: %v", got)
	default:
	}
	hold.release <- time.Time{}
	got := <-done
	lower, err := strconv.Atoi(text(t, got["next_batch"]))
	was, wasErr := strconv.Atoi(ahead)
	if err != nil || wasErr != nil || lower >= was || got["rooms"] != nil {
		t.Fatalf("the held sync answered %v, from %s", got, ahead)
	}
}

func TestRestoreRefusesARosterWithoutAnIntegerEpoch(t *testing.T) {
	for name, epoch := range map[string]any{"none": nil, "text": "1", "fraction": 1.5, "huge": json.Number("1e30")} {
		t.Run(name, func(t *testing.T) {
			tm := newTeam(t)
			content := map[string]any{"v": 1}
			if epoch != nil {
				content["restore_epoch"] = epoch
			}
			tm.ok(tm.putState(tm.admin, rosterType, "", content))
			snap := tm.s.Snapshot()
			tm.ok(tm.send(tm.human, "kept", map[string]any{"body": "kept"}))
			if err := tm.s.RestoreTo(snap); err == nil {
				t.Fatal("restored over a roster with no integer epoch")
			}
			if got := tm.bodies(); len(got) != 1 {
				t.Errorf("a refused restore changed the database: %v", got)
			}
		})
	}
}

// The epoch in use counts as much as the snapshot's, so a roster in use with no integer epoch
// refuses the restore too.
func TestRestoreRefusesARosterInUseWithoutAnIntegerEpoch(t *testing.T) {
	tm := newTeam(t)
	tm.withRoster(0)
	snap := tm.s.Snapshot()
	tm.withRoster("later")
	if err := tm.s.RestoreTo(snap); err == nil {
		t.Fatal("restored over a roster in use with no integer epoch")
	}
	if got := field(t, tm.rosterEvent(), "content", "restore_epoch"); got != "later" {
		t.Errorf("a refused restore changed the roster: %v", got)
	}
}

func TestRestoreRefusesASnapshotWithNoRoster(t *testing.T) {
	tm := newTeam(t)
	if err := tm.s.RestoreTo(tm.s.Snapshot()); err == nil {
		t.Error("restored a database with no roster")
	}
	if err := tm.s.RestoreTo(Snapshot{}); err == nil {
		t.Error("restored the zero Snapshot")
	}
}

// TestASnapshotIsACopy: a redaction after the snapshot does not reach it, and the restored
// database does not share the snapshot's events, so a second restore starts from the same
// place.
func TestASnapshotIsACopy(t *testing.T) {
	tm := newTeam(t)
	tm.withRoster(0)
	sent := text(t, tm.ok(tm.send(tm.human, "kept", map[string]any{"body": "kept"}))["event_id"])
	snap := tm.s.Snapshot()
	tm.ok(tm.call(tm.admin, "PUT", tm.roomPath("redact/"+q(sent)+"/r1"), nil, map[string]any{}))
	if err := tm.s.RestoreTo(snap); err != nil {
		t.Fatal(err)
	}
	if got := tm.bodies(); len(got) != 1 || got[0] != "kept" {
		t.Fatalf("after the restore %v", got)
	}
	tm.ok(tm.call(tm.admin, "PUT", tm.roomPath("redact/"+q(sent)+"/r2"), nil, map[string]any{}))
	if err := tm.s.RestoreTo(snap); err != nil {
		t.Fatal(err)
	}
	if got := tm.bodies(); len(got) != 1 || got[0] != "kept" {
		t.Fatalf("after a second restore %v", got)
	}
}
