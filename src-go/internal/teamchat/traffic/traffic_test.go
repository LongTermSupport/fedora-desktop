package traffic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// body is the text every test event carries, which no output may hold.
const body = "SECRET-BODY-never-printed"

// t0 is the start of the tests' first hour.
var t0 = time.Date(2026, 11, 25, 16, 0, 0, 0, time.UTC)

// fakeClock is the homeserver's and the client's clock: set by the test, a sleep passes no time.
type fakeClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *fakeClock) set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = at
}

func (c *fakeClock) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// team is a homeserver with a team's three rooms on fakehs, and the admin client of it.
type team struct {
	t     *testing.T
	clock *fakeClock
	admin *matrix.Admin
	rooms []Room
	txn   int
}

func newTeam(t *testing.T) *team {
	t.Helper()
	clock := &fakeClock{at: t0.Add(-time.Hour)}
	srv := fakehs.New(fakehs.Options{ServerName: "alpha.teamchat.internal", Now: clock.Now})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	srv.AddUser(proto.AdminLocalpart, "pw", true)
	srv.AddUser("someone", "pw", false)
	token, err := srv.MintToken(proto.AdminID("alpha.teamchat.internal"))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := matrix.NewAdmin(matrix.Config{BaseURL: srv.URL(), Token: token, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	tm := &team{t: t, clock: clock, admin: admin}
	for _, purpose := range []string{proto.PurposeMain, proto.PurposeReports, proto.PurposeQuestions} {
		id, err := admin.CreateRoom(t.Context(), matrix.CreateRoomRequest{Preset: "private_chat", RoomVersion: proto.RoomVersion})
		if err != nil {
			t.Fatal(err)
		}
		tm.rooms = append(tm.rooms, Room{Purpose: purpose, ID: id})
	}
	return tm
}

// send posts content into the room of purpose at the time given, and answers its event ID.
func (tm *team) send(purpose string, at time.Time, content map[string]any) string {
	tm.t.Helper()
	tm.clock.set(at)
	raw, err := json.Marshal(content)
	if err != nil {
		tm.t.Fatal(err)
	}
	tm.txn++
	id, err := tm.admin.Send(tm.t.Context(), tm.room(purpose), "m.room.message", "txn"+strconv.Itoa(tm.txn), raw)
	if err != nil {
		tm.t.Fatal(err)
	}
	return id
}

func (tm *team) room(purpose string) string {
	for _, r := range tm.rooms {
		if r.Purpose == purpose {
			return r.ID
		}
	}
	tm.t.Fatalf("no room %s", purpose)
	return ""
}

func typed(key string) map[string]any {
	return map[string]any{"msgtype": "m.text", "body": body, key: map[string]any{"v": 1, "text": body}}
}

func relates(c map[string]any, rel map[string]any) map[string]any {
	c["m.relates_to"] = rel
	return c
}

// traffic is the tests' shapes: in main, a ping root and a thread answer to it in the first
// hour, a reply and an edit in the second; in reports a receipt; in questions a question; and
// an invite in main (a membership change); one event in main before the window and one after.
// They are sent in time order, as a homeserver orders them.
func (tm *team) traffic() {
	tm.t.Helper()
	tm.send(proto.PurposeMain, t0.Add(-time.Minute), typed(proto.KeyText))
	ping := tm.send(proto.PurposeMain, t0.Add(5*time.Minute), typed(proto.KeyPing))
	tm.send(proto.PurposeMain, t0.Add(6*time.Minute), relates(typed(proto.KeyText), map[string]any{
		"rel_type": "m.thread", "event_id": ping, "is_falling_back": true, "m.in_reply_to": map[string]any{"event_id": ping},
	}))
	tm.send(proto.PurposeQuestions, t0.Add(10*time.Minute), typed(proto.KeyQuestion))
	tm.clock.set(t0.Add(30 * time.Minute))
	if err := tm.admin.Invite(tm.t.Context(), tm.room(proto.PurposeMain), "@someone:alpha.teamchat.internal"); err != nil {
		tm.t.Fatal(err)
	}
	tm.send(proto.PurposeMain, t0.Add(65*time.Minute), relates(typed(proto.KeyText), map[string]any{"m.in_reply_to": map[string]any{"event_id": ping}}))
	tm.send(proto.PurposeMain, t0.Add(66*time.Minute), relates(map[string]any{"msgtype": "m.text", "body": body, "m.new_content": map[string]any{"body": body}},
		map[string]any{"rel_type": "m.replace", "event_id": ping}))
	tm.send(proto.PurposeReports, t0.Add(70*time.Minute), typed(proto.KeyReceipt))
	tm.send(proto.PurposeMain, t0.Add(2*time.Hour), typed(proto.KeyText))
}

func window(t *testing.T) Window {
	t.Helper()
	w, err := NewWindow(t0, t0.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func count(c Counts, k Key) int64 {
	for _, r := range c.Rows {
		if r.Key == k {
			return r.Count
		}
	}
	return 0
}

// h1 is the first hour's start in milliseconds.
var h1 = t0.UnixMilli()

// T1: the counts by room, hour, type, typed key, relation and membership, only inside the
// window, every room read to before its start.
func TestReadCountsTheWindowsShapes(t *testing.T) {
	tm := newTeam(t)
	tm.traffic()
	c, err := Read(t.Context(), tm.admin, tm.rooms, window(t), Options{Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	h2 := h1 + HourMS
	msg := "m.room.message"
	for k, want := range map[Key]int64{
		{Room: proto.PurposeMain, HourMS: h1, Type: msg, Typed: proto.KeyPing}:                 1,
		{Room: proto.PurposeMain, HourMS: h1, Type: msg, Typed: proto.KeyText, Rel: RelThread}: 1,
		{Room: proto.PurposeMain, HourMS: h1, Type: "m.room.member", Membership: "invite"}:     1,
		{Room: proto.PurposeMain, HourMS: h2, Type: msg, Typed: proto.KeyText, Rel: RelReply}:  1,
		{Room: proto.PurposeMain, HourMS: h2, Type: msg, Rel: RelEdit}:                         1,
		{Room: proto.PurposeReports, HourMS: h2, Type: msg, Typed: proto.KeyReceipt}:           1,
		{Room: proto.PurposeQuestions, HourMS: h1, Type: msg, Typed: proto.KeyQuestion}:        1,
	} {
		if got := count(c, k); got != want {
			t.Errorf("%+v = %d, want %d", k, got, want)
		}
	}
	if len(c.Rows) != 7 {
		t.Errorf("rows = %+v", c.Rows)
	}
	wantRead := map[string]int64{proto.PurposeMain: 5, proto.PurposeReports: 1, proto.PurposeQuestions: 1}
	for _, r := range c.Rooms {
		if r.Events != wantRead[r.Room] || r.Pages < 1 {
			t.Errorf("room %+v", r)
		}
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

// T2: the page bound: a room that does not reach the window's start within MaxPages fails,
// naming the room, rather than answering a part as the whole.
func TestReadFailsAtThePageBound(t *testing.T) {
	tm := newTeam(t)
	tm.traffic()
	_, err := Read(t.Context(), tm.admin, tm.rooms, window(t), Options{Page: 1, MaxPages: 3})
	if err == nil || !strings.Contains(err.Error(), "room main") || !strings.Contains(err.Error(), "narrow the window") {
		t.Fatalf("err = %v", err)
	}
}

// T3: a read the homeserver fails is the failure, wrapped with the room.
func TestReadFailsOnTheHomeserversFailure(t *testing.T) {
	tm := newTeam(t)
	_, err := Read(t.Context(), failing{}, tm.rooms, window(t), Options{})
	if !errors.Is(err, errBroken) || !strings.Contains(err.Error(), "room main") {
		t.Fatalf("err = %v", err)
	}
}

var errBroken = errors.New("broken")

type failing struct{}

func (failing) Messages(context.Context, string, matrix.MessagesRequest) (matrix.MessagesPage, error) {
	return matrix.MessagesPage{}, errBroken
}

// pages is a reader that answers canned chunks in turn.
type pages struct{ chunks [][]string }

func (p *pages) Messages(_ context.Context, _ string, req matrix.MessagesRequest) (matrix.MessagesPage, error) {
	if len(p.chunks) == 0 {
		return matrix.MessagesPage{}, nil
	}
	page := matrix.MessagesPage{End: "next"}
	for _, raw := range p.chunks[0] {
		page.Chunk = append(page.Chunk, json.RawMessage(raw))
	}
	p.chunks = p.chunks[1:]
	if req.Dir != matrix.Backward {
		return matrix.MessagesPage{}, errors.New("not backwards")
	}
	return page, nil
}

func ts(at time.Time) string { return `"origin_server_ts":` + jsonInt(at.UnixMilli()) }

func jsonInt(n int64) string {
	raw, err := json.Marshal(n)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// T4: names that are not a plain event type, an unknown typed key, two typed keys, an unknown
// relation or membership are counted under the bracketed catch-alls, never as given.
func TestClassifyKeepsNamesToTheirForms(t *testing.T) {
	at := t0.Add(time.Minute)
	r := &pages{chunks: [][]string{{
		`{"type":"x y` + body + `",` + ts(at) + `,"content":{}}`,
		`{"type":"m.room.message",` + ts(at) + `,"content":{"teamchat.made-up":{},"body":"` + body + `"}}`,
		`{"type":"m.room.message",` + ts(at) + `,"content":{"teamchat.ping":{},"teamchat.text":{}}}`,
		`{"type":"m.room.message",` + ts(at) + `,"content":{"m.relates_to":{"rel_type":"m.annotation"}}}`,
		`{"type":"m.room.member",` + ts(at) + `,"content":{"membership":"` + body + `"}}`,
		`{"type":"m.room.redaction",` + ts(at) + `,"content":{}}`,
		`{"type":"m.room.message",` + ts(t0.Add(-time.Second)) + `,"content":{}}`,
	}}}
	c, err := Read(t.Context(), r, []Room{{Purpose: proto.PurposeMain, ID: "!r:x"}}, window(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []Key{
		{Room: proto.PurposeMain, HourMS: h1, Type: Other},
		{Room: proto.PurposeMain, HourMS: h1, Type: "m.room.message"},
		{Room: proto.PurposeMain, HourMS: h1, Type: "m.room.message", Typed: Several},
		{Room: proto.PurposeMain, HourMS: h1, Type: "m.room.message", Rel: Other},
		{Room: proto.PurposeMain, HourMS: h1, Type: "m.room.member", Membership: Other},
		{Room: proto.PurposeMain, HourMS: h1, Type: "m.room.redaction"},
	} {
		if count(c, k) != 1 {
			t.Errorf("%+v not counted once: %+v", k, c.Rows)
		}
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

// T5: an event with no time, or one that is not JSON, fails the read.
func TestReadFailsOnAMalformedEvent(t *testing.T) {
	for _, raw := range []string{`{"type":"m.room.message","content":{}}`, `{"type":`} {
		r := &pages{chunks: [][]string{{raw}}}
		if _, err := Read(t.Context(), r, []Room{{Purpose: proto.PurposeMain, ID: "!r:x"}}, window(t), Options{}); err == nil {
			t.Errorf("%s read", raw)
		}
	}
}

// T6: the window: since before until, at most MaxWindow.
func TestNewWindowBounds(t *testing.T) {
	for name, w := range map[string][2]time.Time{
		"empty":    {t0, t0},
		"reversed": {t0, t0.Add(-time.Hour)},
		"too long": {t0, t0.Add(MaxWindow + time.Hour)},
	} {
		if _, err := NewWindow(w[0], w[1]); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := NewWindow(t0, t0.Add(MaxWindow)); err != nil {
		t.Errorf("the longest window: %v", err)
	}
}

// T7: the wire form round-trips, and Decode refuses what Validate refuses.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	tm := newTeam(t)
	tm.traffic()
	c, err := Read(t.Context(), tm.admin, tm.rooms, window(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(Encode(c))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(Encode(got).Compact(), Encode(c).Compact()) {
		t.Fatalf("round trip\n%s\n%s", Encode(got).Compact(), Encode(c).Compact())
	}
	bad := c
	bad.Rows = append([]Row{}, c.Rows...)
	bad.Rows[0].Count = 0
	if _, err := Decode(Encode(bad)); err == nil {
		t.Error("a zero count decoded")
	}
	for _, raw := range []string{`[]`, `{"since_ms":1}`, `{"since_ms":1,"until_ms":2,"rooms":[],"rows":[],"extra":1}`} {
		v, err := proto.Parse([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Decode(v); err == nil {
			t.Errorf("%s decoded", raw)
		}
	}
}

// T8: Validate refuses rows the window, the rooms or the forms do not allow.
func TestValidateRefusesWhatTheReadCannotGive(t *testing.T) {
	w := window(t)
	good := Counts{
		Window: w, Rooms: []RoomRead{{Room: proto.PurposeMain, Events: 1, Pages: 1}},
		Rows: []Row{{Key: Key{Room: proto.PurposeMain, HourMS: h1, Type: "m.room.message"}, Count: 1}},
	}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Counts){
		"room unread":      func(c *Counts) { c.Rows[0].Room = proto.PurposeReports },
		"hour unaligned":   func(c *Counts) { c.Rows[0].HourMS++ },
		"hour after":       func(c *Counts) { c.Rows[0].HourMS = h1 + 2*HourMS },
		"bad type":         func(c *Counts) { c.Rows[0].Type = "a b" },
		"bad typed":        func(c *Counts) { c.Rows[0].Typed = "teamchat.nothing" },
		"bad rel":          func(c *Counts) { c.Rows[0].Rel = "m.thread" },
		"bad membership":   func(c *Counts) { c.Rows[0].Membership = "joined" },
		"sum":              func(c *Counts) { c.Rooms[0].Events = 2 },
		"duplicate row":    func(c *Counts) { c.Rows = append(c.Rows, c.Rows[0]); c.Rooms[0].Events = 2 },
		"no pages":         func(c *Counts) { c.Rooms[0].Pages = 0 },
		"duplicate room":   func(c *Counts) { c.Rooms = append(c.Rooms, c.Rooms[0]) },
		"unknown purpose":  func(c *Counts) { c.Rooms[0].Room = "side" },
		"window too long":  func(c *Counts) { c.Window.UntilMS = c.Window.SinceMS + MaxWindow.Milliseconds() + 1 },
		"membership typed": func(c *Counts) { c.Rows[0].Membership = "join" },
	} {
		c := good
		c.Rooms = append([]RoomRead{}, good.Rooms...)
		c.Rows = append([]Row{}, good.Rows...)
		change(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: valid", name)
		}
	}
}

// T9: the table and the JSON hold the counts and never a body; every hour of the window is
// listed, the quiet ones with 0.
func TestTableAndJSONHoldNoBody(t *testing.T) {
	tm := newTeam(t)
	tm.traffic()
	c, err := Read(t.Context(), tm.admin, tm.rooms, window(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var table bytes.Buffer
	if err := WriteTable(&table, "alpha", c); err != nil {
		t.Fatal(err)
	}
	out := table.String()
	for _, want := range []string{
		"team alpha", "2026-11-25T16:00:00Z", "2026-11-25T18:00:00Z", "2026-11-25T16:00Z", "2026-11-25T17:00Z",
		"teamchat.receipt", "thread", "reply", "edit", "invite", "not kept in room history",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	doc, err := JSON("alpha", c)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		V     int    `json:"v"`
		Team  string `json:"team"`
		Since string `json:"since"`
		Until string `json:"until"`
		Rooms []struct {
			Room   string `json:"room"`
			Events int64  `json:"events"`
			Pages  int64  `json:"pages"`
		} `json:"rooms"`
		Hours []struct {
			Hour  string           `json:"hour"`
			Rooms map[string]int64 `json:"rooms"`
			Total int64            `json:"total"`
		} `json:"hours"`
		Rows []struct {
			Room       string `json:"room"`
			Hour       string `json:"hour"`
			Type       string `json:"type"`
			Typed      string `json:"typed"`
			Rel        string `json:"rel"`
			Membership string `json:"membership"`
			Count      int64  `json:"count"`
		} `json:"rows"`
		NotCounted []string `json:"not_counted"`
	}
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&parsed); err != nil {
		t.Fatalf("%s: %v", doc, err)
	}
	if parsed.V != 1 || parsed.Team != "alpha" || parsed.Since != "2026-11-25T16:00:00Z" || len(parsed.Rows) != 7 || len(parsed.Hours) != 2 ||
		parsed.Hours[0].Total != 4 || parsed.Hours[1].Rooms[proto.PurposeReports] != 1 || len(parsed.NotCounted) == 0 {
		t.Errorf("json %s", doc)
	}
	for _, s := range []string{out, string(doc)} {
		if strings.Contains(s, body) {
			t.Errorf("output holds a body:\n%s", s)
		}
	}
}
