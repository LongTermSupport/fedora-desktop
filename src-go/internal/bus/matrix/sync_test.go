package matrix

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/matrix/fakehs"
)

const syncPath = "/sync"

func TestRoomFilterJSON(t *testing.T) {
	f := RoomFilter{
		Rooms: []string{"!a", "!b"}, StateTypes: []string{"m.room.member"}, LazyLoadMembers: true,
		TimelineTypes: []string{msgType}, TimelineLimit: 50,
	}
	want := `{"account_data":{"types":[]},"presence":{"types":[]},"room":{"account_data":{"types":[]},` +
		`"ephemeral":{"types":[]},"rooms":["!a","!b"],"state":{"lazy_load_members":true,"types":["m.room.member"]},` +
		`"timeline":{"limit":50,"types":["m.room.message"]}}}`
	if got := f.JSON(); got != want {
		t.Errorf("filter\n got %s\nwant %s", got, want)
	}
	open := `{"account_data":{"types":[]},"presence":{"types":[]},"room":{"account_data":{"types":[]},` +
		`"ephemeral":{"types":[]},"state":{"lazy_load_members":false},"timeline":{}}}`
	if got := (RoomFilter{}).JSON(); got != open {
		t.Errorf("an open filter\n got %s\nwant %s", got, open)
	}
}

func TestSyncFirstAndInvite(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	if _, err := f.alice.Send(t.Context(), f.id, msgType, "", hello); err != nil {
		t.Fatal(err)
	}
	filter := &RoomFilter{Rooms: []string{f.id}, TimelineTypes: []string{msgType}, TimelineLimit: 10}
	resp, err := f.bob.Sync(t.Context(), SyncRequest{Filter: filter})
	if err != nil || resp.NextBatch == "" {
		t.Fatalf("sync = %+v, %v", resp, err)
	}
	joined, ok := resp.Join[f.id]
	if !ok || len(joined.Timeline.Events) != 1 || len(joined.State) == 0 {
		t.Fatalf("joined room %+v", joined)
	}
	q := lastRequest(t, srv).Query
	if q.Get("timeout") != "0" || q.Get("filter") != filter.JSON() || q.Has("since") {
		t.Errorf("query %v", q)
	}
	carol := user(t, srv, "carol")
	if err = f.alice.Invite(t.Context(), f.id, carol.uid); err != nil {
		t.Fatal(err)
	}
	invited, err := carol.Sync(t.Context(), SyncRequest{})
	if err != nil || len(invited.Invite[f.id].InviteState) == 0 {
		t.Fatalf("invite sync = %+v, %v", invited, err)
	}
	if lastRequest(t, srv).Query.Has("filter") {
		t.Error("a sync without a filter sent one")
	}
}

func TestSyncHeldUntilAnEvent(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	first, err := f.bob.Sync(t.Context(), SyncRequest{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan SyncResponse, 1)
	fail := make(chan error, 1)
	go func() {
		resp, syncErr := f.bob.Sync(t.Context(), SyncRequest{Since: first.NextBatch, Timeout: 30 * time.Second})
		if syncErr != nil {
			fail <- syncErr
			return
		}
		done <- resp
	}()
	waitHeld(srv)
	eventID, err := f.alice.Send(t.Context(), f.id, msgType, "", hello)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case syncErr := <-fail:
		t.Fatalf("held sync: %v", syncErr)
	case resp := <-done:
		events := resp.Join[f.id].Timeline.Events
		if len(events) == 0 || !strings.Contains(string(events[len(events)-1]), eventID) {
			t.Fatalf("held sync gave %+v", resp)
		}
		if resp.NextBatch == first.NextBatch {
			t.Error("next_batch did not move")
		}
	}
	if !held(srv.Requests(), first.NextBatch) {
		t.Errorf("no sync was sent since %q", first.NextBatch)
	}
}

func TestSyncLimitedTimeline(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	first, err := f.bob.Sync(t.Context(), SyncRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if _, err = f.alice.Send(t.Context(), f.id, msgType, "", hello); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := f.bob.Sync(t.Context(), SyncRequest{Since: first.NextBatch, Filter: &RoomFilter{TimelineLimit: 2}})
	if err != nil {
		t.Fatal(err)
	}
	tl := resp.Join[f.id].Timeline
	if !tl.Limited || tl.PrevBatch == "" || len(tl.Events) != 2 {
		t.Errorf("timeline %+v", tl)
	}
}

func TestSyncRefusedBeforeARequest(t *testing.T) {
	srv := newServer(t)
	m := user(t, srv, "alice")
	before := len(srv.Requests())
	for _, req := range []SyncRequest{
		{Timeout: -time.Millisecond},
		{Timeout: 30*time.Second + time.Millisecond},
		{Timeout: 1500 * time.Microsecond},
		{Since: "a b"},
		{Since: strings.Repeat("s", 4097)},
	} {
		_, err := m.Sync(t.Context(), req)
		wantErr(t, err, ErrInvalid, "")
	}
	if len(srv.Requests()) != before {
		t.Error("a refused sync reached the server")
	}
	cl, err := m.syncCall(SyncRequest{Timeout: 30 * time.Second})
	if err != nil || cl.limit != 45*time.Second || cl.query.Get("timeout") != "30000" {
		t.Errorf("a 30 s sync: limit %v query %v, %v", cl.limit, cl.query, err)
	}
}

func TestSyncMalformed(t *testing.T) {
	srv := newServer(t)
	m := user(t, srv, "alice")
	for _, body := range []string{
		`{}`,
		`{"next_batch":"a b"}`,
		`{"next_batch":5}`,
		`{"next_batch":"s1","rooms":[]}`,
		`{"next_batch":"s1","rooms":{"join":[]}}`,
		`{"next_batch":"s1","rooms":{"join":{"!r":{"timeline":{"events":{}}}}}}`,
		`{"next_batch":"s1","rooms":{"join":{"!r":{"timeline":{"events":[5]}}}}}`,
		`{"next_batch":"s1","rooms":{"join":{"!r":{"timeline":{"events":[],"limited":"yes"}}}}}`,
		`{"next_batch":"s1","rooms":{"join":{"!r":{"timeline":{"events":[],"limited":true}}}}}`,
		`{"next_batch":"s1","rooms":{"join":{"!r":{"timeline":{"events":[],"limited":true,"prev_batch":"a b"}}}}}`,
		`{"next_batch":"s1","rooms":{"join":{"!r":{"state":{"events":[null]}}}}}`,
		`{"next_batch":"s1","rooms":{"join":{"#alias":{}}}}`,
		`{"next_batch":"s1","rooms":{"invite":{"!r":{"invite_state":{"events":"x"}}}}}`,
		`{"next_batch":"s1","rooms":{"invite":{"x":{}}}}`,
		`{"next_batch":"s1","rooms":{"leave":{"!r":{"timeline":{"events":[1]}}}}}`,
	} {
		inject(srv, http.MethodGet, syncPath, http.StatusOK, body)
		_, err := m.Sync(t.Context(), SyncRequest{})
		if e := wantErr(t, err, ErrUnreachable, detailMalformed); e.Transient() {
			t.Errorf("%s: transient", body)
		}
	}
	inject(srv, http.MethodGet, syncPath, http.StatusOK,
		`{"next_batch":"s1","rooms":{"join":{"!r":{"timeline":{"events":[{}],"limited":false}}},"leave":{"!l":{"state":{"events":[{}]}}}}}`)
	resp, err := m.Sync(t.Context(), SyncRequest{})
	if err != nil || len(resp.Join["!r"].Timeline.Events) != 1 || len(resp.Leave["!l"].State) != 1 {
		t.Fatalf("a well-formed answer: %+v, %v", resp, err)
	}
	inject(srv, http.MethodGet, syncPath, http.StatusUnauthorized, `{}`)
	_, err = m.Sync(t.Context(), SyncRequest{})
	wantErr(t, err, ErrAuthRefused, "")
}

func TestSyncDroppedByARestart(t *testing.T) {
	srv := newServer(t)
	m := user(t, srv, "alice")
	first, err := m.Sync(t.Context(), SyncRequest{})
	if err != nil {
		t.Fatal(err)
	}
	// The held sync must be on a new connection: net/http replays a GET, once, when a reused
	// keep-alive connection closes before any answer byte, which would carry it to the
	// restarted server.
	m.Close()
	fail := make(chan error, 1)
	go func() {
		_, syncErr := m.Sync(t.Context(), SyncRequest{Since: first.NextBatch, Timeout: 30 * time.Second})
		fail <- syncErr
	}()
	waitHeld(srv)
	if err = srv.Restart(); err != nil {
		t.Fatal(err)
	}
	if e := wantErr(t, <-fail, ErrUnreachable, ""); !e.Transient() {
		t.Errorf("a dropped sync is not transient: %+v", e)
	}
}

// waitHeld waits, a bounded while, for a sync with a since token to reach the server.
func waitHeld(srv *fakehs.Server) {
	for i := 0; i < 2000 && !held(srv.Requests(), ""); i++ {
		time.Sleep(time.Millisecond)
	}
}

func held(reqs []fakehs.Request, since string) bool {
	for _, r := range reqs {
		if strings.HasSuffix(r.Path, syncPath) && r.Query.Has("since") && (since == "" || r.Query.Get("since") == since) {
			return true
		}
	}
	return false
}
