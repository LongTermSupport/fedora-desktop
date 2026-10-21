package matrix

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
)

const (
	msgType  = "m.room.message"
	nameType = "m.room.name"
	sendPath = "/send/"
)

var hello = json.RawMessage(`{"msgtype":"m.text","body":"hello"}`)

// roomFixture is a room alice made, with bob invited and joined.
type roomFixture struct {
	alice, bob member
	id         string
}

func room(t *testing.T, srv *fakehs.Server) roomFixture {
	t.Helper()
	f := roomFixture{alice: user(t, srv, "alice"), bob: user(t, srv, "bob")}
	id, err := f.alice.CreateRoom(t.Context(), CreateRoomRequest{Preset: "private_chat", RoomVersion: "12", Invite: []string{f.bob.uid}})
	if err != nil {
		t.Fatalf("createRoom: %v", err)
	}
	f.id = id
	if joined, joinErr := f.bob.Join(t.Context(), id); joinErr != nil || joined != id {
		t.Fatalf("join = %q, %v", joined, joinErr)
	}
	return f
}

func count(t *testing.T, m member, roomID, eventType string) int {
	t.Helper()
	page, err := m.Messages(t.Context(), roomID, MessagesRequest{Dir: Backward, Limit: 100, Types: []string{eventType}})
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	return len(page.Chunk)
}

func TestVersions(t *testing.T) {
	srv := newServer(t)
	m := user(t, srv, "alice")
	versions, err := m.Versions(t.Context())
	if err != nil || len(versions) == 0 {
		t.Fatalf("versions = %v, %v", versions, err)
	}
	inject(srv, http.MethodGet, "/versions", http.StatusOK, `{"versions":"v1.11"}`)
	_, err = m.Versions(t.Context())
	wantErr(t, err, ErrUnreachable, detailMalformed)
	inject(srv, http.MethodGet, "/versions", http.StatusOK, `{}`)
	_, err = m.Versions(t.Context())
	wantErr(t, err, ErrUnreachable, detailMalformed)
}

func TestLogin(t *testing.T) {
	srv := newServer(t)
	uid := srv.AddUser("carol", "right", false)
	c, err := NewMember(Config{BaseURL: srv.URL(), PlainHTTPHosts: []string{loopback}, Token: "unused", Clock: &fakeClock{}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	sess, err := c.Login(t.Context(), "carol", "right", "bus host")
	if err != nil || sess.UserID != uid || sess.DeviceID == "" || sess.AccessToken == "" {
		t.Fatalf("login = %+v, %v", sess, err)
	}
	if got := lastRequest(t, srv).Header.Get("Authorization"); got != "" {
		t.Errorf("login sent Authorization %q", got)
	}
	signed, err := c.WithToken(sess.AccessToken.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	who, err := signed.Whoami(t.Context())
	if err != nil || who.DeviceID != sess.DeviceID {
		t.Fatalf("whoami = %+v, %v", who, err)
	}
	_, err = c.Login(t.Context(), "carol", "wrong", "bus host")
	wantErr(t, err, ErrForbidden, "")
	inject(srv, http.MethodPost, "/login", http.StatusOK, `{"user_id":"@carol:example.org","access_token":"a b","device_id":"D"}`)
	_, err = c.Login(t.Context(), "carol", "right", "bus host")
	wantErr(t, err, ErrUnreachable, detailMalformed)
	inject(srv, http.MethodGet, whoamiPath, http.StatusOK, `{"device_id":"D"}`)
	_, err = signed.Whoami(t.Context())
	wantErr(t, err, ErrUnreachable, detailMalformed)
}

func TestSendGeneratesTransactionIDs(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	first, err := f.alice.Send(t.Context(), f.id, msgType, "", hello)
	if err != nil || !strings.HasPrefix(first, "$") {
		t.Fatalf("send = %q, %v", first, err)
	}
	path := lastRequest(t, srv).Path
	txn := path[strings.LastIndex(path, "/")+1:]
	if !regexp.MustCompile(`^pb[0-9a-f]{32}$`).MatchString(txn) {
		t.Errorf("generated transaction ID %q", txn)
	}
	again, err := f.alice.Send(t.Context(), f.id, msgType, txn, hello)
	if err != nil || again != first {
		t.Errorf("a reused transaction ID gave %q, %v; want %q", again, err, first)
	}
	if n := count(t, f.alice, f.id, msgType); n != 1 {
		t.Errorf("%d messages stored, want 1", n)
	}
	before := len(srv.Requests())
	for _, bad := range []string{"has space", strings.Repeat("a", 65), "a/b", "café"} {
		_, err = f.alice.Send(t.Context(), f.id, msgType, bad, hello)
		wantErr(t, err, ErrInvalid, "")
	}
	for _, content := range []string{`[]`, `"x"`, `{`, ``, `{} {}`} {
		_, err = f.alice.Send(t.Context(), f.id, msgType, "", json.RawMessage(content))
		wantErr(t, err, ErrInvalid, "")
	}
	_, err = f.alice.Send(t.Context(), "", msgType, "", hello)
	wantErr(t, err, ErrInvalid, detailBadSegment)
	if len(srv.Requests()) != before {
		t.Error("a refused send reached the server")
	}
}

func TestSendRetriesALostAnswer(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	srv.Inject(fakehs.Fault{Method: http.MethodPut, Path: sendPath, DropAfter: true})
	eventID, err := f.alice.Send(t.Context(), f.id, msgType, "txn-1", hello)
	if err != nil || !strings.HasPrefix(eventID, "$") {
		t.Fatalf("send after a lost answer = %q, %v", eventID, err)
	}
	if n := count(t, f.alice, f.id, msgType); n != 1 {
		t.Errorf("%d messages stored, want 1", n)
	}
	if got := f.alice.clk.waits(); !slices.Equal(got, []time.Duration{time.Second}) {
		t.Errorf("waits %v, want [1s]", got)
	}
	var paths []string
	for _, req := range srv.Requests() {
		if strings.Contains(req.Path, sendPath) {
			paths = append(paths, req.Path)
		}
	}
	if len(paths) != 2 || paths[0] != paths[1] {
		t.Errorf("send paths %v, want the same one twice", paths)
	}
}

func TestSendGivesUp(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	srv.Inject(fakehs.Fault{Method: http.MethodPut, Path: sendPath, Drop: true, Times: 3})
	_, err := f.alice.Send(t.Context(), f.id, msgType, "", hello)
	wantErr(t, err, ErrUnreachable, detailNoAnswer)
	if got := f.alice.clk.waits(); len(got) != 2 {
		t.Errorf("waits %v, want two", got)
	}
	for _, tc := range []struct {
		status int
		body   string
		kind   error
	}{
		{http.StatusForbidden, `{"errcode":"M_FORBIDDEN"}`, ErrForbidden},
		{http.StatusOK, `{"event_id":"nope"}`, ErrUnreachable},
		{http.StatusOK, `{"event_id":"$` + strings.Repeat("a", 255) + `"}`, ErrUnreachable},
		{http.StatusOK, `{}`, ErrUnreachable},
		{http.StatusInternalServerError, `{}`, ErrRefused},
	} {
		before := len(srv.Requests())
		inject(srv, http.MethodPut, sendPath, tc.status, tc.body)
		_, err = f.alice.Send(t.Context(), f.id, msgType, "", hello)
		wantErr(t, err, tc.kind, "")
		if n := len(srv.Requests()) - before; n != 1 {
			t.Errorf("%d %s: %d tries, want 1", tc.status, tc.body, n)
		}
	}
}

func TestSendCancelledBetweenTries(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.alice.clk.cancel = cancel
	srv.Inject(fakehs.Fault{Method: http.MethodPut, Path: sendPath, Drop: true, Times: 3})
	_, err := f.alice.Send(ctx, f.id, msgType, "", hello)
	wantErr(t, err, ErrUnreachable, detailCancelled)
}

func TestRedact(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	leaked, err := f.bob.Send(t.Context(), f.id, msgType, "", hello)
	if err != nil {
		t.Fatal(err)
	}
	srv.Inject(fakehs.Fault{Method: http.MethodPut, Path: "/redact/", DropAfter: true})
	redaction, err := f.alice.Redact(t.Context(), f.id, leaked, "", "secret")
	if err != nil || !strings.HasPrefix(redaction, "$") {
		t.Fatalf("redact = %q, %v", redaction, err)
	}
	if got := f.alice.clk.waits(); !slices.Equal(got, []time.Duration{time.Second}) {
		t.Errorf("waits %v, want [1s]", got)
	}
	if n := count(t, f.alice, f.id, "m.room.redaction"); n != 1 {
		t.Errorf("%d redactions stored, want 1", n)
	}
	raw, err := f.alice.GetEvent(t.Context(), f.id, redaction)
	if err != nil {
		t.Fatal(err)
	}
	var ev struct {
		Content map[string]string `json:"content"`
	}
	if jsonErr := json.Unmarshal(raw, &ev); jsonErr != nil || ev.Content["reason"] != "secret" || ev.Content["redacts"] != leaked {
		t.Fatalf("redaction event %s, %v", raw, jsonErr)
	}
	other, err := f.bob.Send(t.Context(), f.id, msgType, "", hello)
	if err != nil {
		t.Fatal(err)
	}
	unreasoned, err := f.alice.Redact(t.Context(), f.id, other, "txn-r", "")
	if err != nil {
		t.Fatal(err)
	}
	if raw, err = f.alice.GetEvent(t.Context(), f.id, unreasoned); err != nil || strings.Contains(string(raw), "reason") {
		t.Fatalf("a redaction with no reason = %s, %v", raw, err)
	}
	before := len(srv.Requests())
	_, err = f.alice.Redact(t.Context(), f.id, other, "has space", "")
	wantErr(t, err, ErrInvalid, "")
	_, err = f.alice.Redact(t.Context(), f.id, "", "", "")
	wantErr(t, err, ErrInvalid, detailBadSegment)
	if len(srv.Requests()) != before {
		t.Error("a refused redact reached the server")
	}
}

func TestState(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	eventID, err := f.alice.PutState(t.Context(), f.id, nameType, "", json.RawMessage(`{"name":"ops"}`))
	if err != nil || !strings.HasPrefix(eventID, "$") {
		t.Fatalf("putState = %q, %v", eventID, err)
	}
	content, err := f.bob.GetState(t.Context(), f.id, nameType, "")
	if err != nil || string(content) != `{"name":"ops"}` {
		t.Fatalf("getState = %s, %v", content, err)
	}
	whole, err := f.bob.GetStateEvent(t.Context(), f.id, nameType, "")
	if err != nil {
		t.Fatal(err)
	}
	var ev struct {
		Sender  string `json:"sender"`
		EventID string `json:"event_id"`
	}
	if jsonErr := json.Unmarshal(whole, &ev); jsonErr != nil || ev.Sender != f.alice.uid || ev.EventID != eventID {
		t.Fatalf("getStateEvent = %s, %v", whole, jsonErr)
	}
	if got := lastRequest(t, srv).Query.Get("format"); got != "event" {
		t.Errorf("format = %q", got)
	}
	all, err := f.bob.RoomState(t.Context(), f.id)
	if err != nil || len(all) < 5 {
		t.Fatalf("roomState = %d events, %v", len(all), err)
	}
	got, err := f.bob.GetEvent(t.Context(), f.id, eventID)
	if err != nil || !strings.Contains(string(got), eventID) {
		t.Fatalf("getEvent = %s, %v", got, err)
	}
}

func TestStateFailures(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	_, err := f.bob.GetState(t.Context(), f.id, "m.room.topic", "")
	wantErr(t, err, ErrNotFound, "")
	_, err = f.bob.PutState(t.Context(), f.id, nameType, "", json.RawMessage(`[1]`))
	wantErr(t, err, ErrInvalid, "")
	for _, bad := range []func() error{
		func() error { _, e := f.bob.PutState(t.Context(), "", nameType, "", json.RawMessage(`{}`)); return e },
		func() error { _, e := f.bob.GetStateEvent(t.Context(), "", nameType, ""); return e },
		func() error { _, e := f.bob.RoomState(t.Context(), ""); return e },
		func() error { _, e := f.bob.GetEvent(t.Context(), f.id, ""); return e },
		func() error { _, e := f.bob.Join(t.Context(), ""); return e },
		func() error { return f.bob.Leave(t.Context(), "") },
		func() error { return f.bob.ReadReceipt(t.Context(), f.id, "") },
		func() error { return f.bob.Invite(t.Context(), "", f.alice.uid) },
		func() error { return f.bob.Kick(t.Context(), "", f.alice.uid, "") },
		func() error { _, e := f.bob.Membership(t.Context(), f.id, ""); return e },
		func() error {
			_, e := f.bob.Messages(t.Context(), "", MessagesRequest{Dir: Forward, Limit: 1})
			return e
		},
	} {
		wantErr(t, bad(), ErrInvalid, detailBadSegment)
	}
	inject(srv, http.MethodGet, "/state", http.StatusOK, `[{"type":"x"},5]`)
	_, err = f.bob.RoomState(t.Context(), f.id)
	wantErr(t, err, ErrUnreachable, detailMalformed)
	inject(srv, http.MethodGet, "/state", http.StatusOK, `{"type":"x"}`)
	_, err = f.bob.RoomState(t.Context(), f.id)
	wantErr(t, err, ErrUnreachable, "the answer is not a JSON array")
	inject(srv, http.MethodGet, "/state", http.StatusForbidden, `{}`)
	_, err = f.bob.RoomState(t.Context(), f.id)
	wantErr(t, err, ErrForbidden, "")
	inject(srv, http.MethodPut, "/state/", http.StatusOK, `{"event_id":"bad"}`)
	_, err = f.alice.PutState(t.Context(), f.id, nameType, "", json.RawMessage(`{"name":"x"}`))
	wantErr(t, err, ErrUnreachable, detailMalformed)
}

func TestMembershipFlow(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	carol := user(t, srv, "carol")
	if err := f.alice.Invite(t.Context(), f.id, carol.uid); err != nil {
		t.Fatalf("invite: %v", err)
	}
	if got, err := f.alice.Membership(t.Context(), f.id, carol.uid); err != nil || got != "invite" {
		t.Fatalf("membership = %q, %v", got, err)
	}
	if got, err := f.alice.Membership(t.Context(), f.id, "@nobody:example.org"); err != nil || got != "" {
		t.Fatalf("membership of a stranger = %q, %v", got, err)
	}
	if err := f.alice.Kick(t.Context(), f.id, f.bob.uid, "rotated"); err != nil {
		t.Fatalf("kick: %v", err)
	}
	if got, err := f.alice.Membership(t.Context(), f.id, f.bob.uid); err != nil || got != "leave" {
		t.Fatalf("membership after kick = %q, %v", got, err)
	}
	if _, err := carol.Join(t.Context(), f.id); err != nil {
		t.Fatal(err)
	}
	if rooms, err := carol.JoinedRooms(t.Context()); err != nil || !slices.Equal(rooms, []string{f.id}) {
		t.Fatalf("joinedRooms = %v, %v", rooms, err)
	}
	if err := carol.Leave(t.Context(), f.id); err != nil {
		t.Fatalf("leave: %v", err)
	}
	if rooms, err := carol.JoinedRooms(t.Context()); err != nil || len(rooms) != 0 {
		t.Fatalf("joinedRooms after leave = %v, %v", rooms, err)
	}
}

func TestMembershipAnswers(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	for _, tc := range []struct {
		method, path, body string
		status             int
		kind               error
		run                func() error
	}{
		{http.MethodGet, "/joined_rooms", `{"joined_rooms":["#alias"]}`, http.StatusOK, ErrUnreachable, func() error { _, e := f.bob.JoinedRooms(t.Context()); return e }},
		{http.MethodGet, "/joined_rooms", `{}`, http.StatusOK, ErrUnreachable, func() error { _, e := f.bob.JoinedRooms(t.Context()); return e }},
		{http.MethodPost, "/join", `{"room_id":"nope"}`, http.StatusOK, ErrUnreachable, func() error { _, e := f.bob.Join(t.Context(), f.id); return e }},
		{http.MethodGet, "/state/m.room.member/", `{"membership":5}`, http.StatusOK, ErrUnreachable, func() error { _, e := f.alice.Membership(t.Context(), f.id, f.bob.uid); return e }},
		{http.MethodGet, "/state/m.room.member/", `{}`, http.StatusOK, ErrUnreachable, func() error { _, e := f.alice.Membership(t.Context(), f.id, f.bob.uid); return e }},
		{http.MethodGet, "/state/m.room.member/", `{}`, http.StatusForbidden, ErrForbidden, func() error { _, e := f.alice.Membership(t.Context(), f.id, f.bob.uid); return e }},
		{http.MethodGet, "/joined_rooms", `{}`, http.StatusUnauthorized, ErrAuthRefused, func() error { _, e := f.bob.JoinedRooms(t.Context()); return e }},
	} {
		inject(srv, tc.method, tc.path, tc.status, tc.body)
		wantErr(t, tc.run(), tc.kind, "")
	}
}

func TestCreateRoomBody(t *testing.T) {
	srv := newServer(t)
	alice, bob := user(t, srv, "alice"), user(t, srv, "bob")
	req := CreateRoomRequest{
		Preset: "private_chat", RoomVersion: "12", Visibility: "private", Name: "ops", Topic: "the bus",
		PowerLevelOverride: json.RawMessage(`{"users_default":0}`),
		InitialState:       []StateEvent{{Type: "m.room.join_rules", StateKey: "", Content: json.RawMessage(`{"join_rule":"invite"}`)}},
		Invite:             []string{bob.uid},
	}
	roomID, err := alice.CreateRoom(t.Context(), req)
	if err != nil || !strings.HasPrefix(roomID, "!") {
		t.Fatalf("createRoom = %q, %v", roomID, err)
	}
	if got, topicErr := alice.GetState(t.Context(), roomID, "m.room.topic", ""); topicErr != nil || !strings.Contains(string(got), `"topic":"the bus"`) {
		t.Errorf("topic = %s, %v", got, topicErr)
	}
	before := len(srv.Requests())
	for _, bad := range []CreateRoomRequest{
		{PowerLevelOverride: json.RawMessage(`[]`)},
		{InitialState: []StateEvent{{Type: "m.room.join_rules", Content: json.RawMessage(`1`)}}},
		{InitialState: []StateEvent{{Type: "", Content: json.RawMessage(`{}`)}}},
	} {
		_, err = alice.CreateRoom(t.Context(), bad)
		wantErr(t, err, ErrInvalid, "")
	}
	if len(srv.Requests()) != before {
		t.Error("a refused createRoom reached the server")
	}
	inject(srv, http.MethodPost, "/createRoom", http.StatusOK, `{"room_id":"$notaroom"}`)
	_, err = alice.CreateRoom(t.Context(), CreateRoomRequest{})
	wantErr(t, err, ErrUnreachable, detailMalformed)
}

func TestReadReceipt(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	eventID, err := f.alice.Send(t.Context(), f.id, msgType, "", hello)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.bob.ReadReceipt(t.Context(), f.id, eventID); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	want := fakehs.Receipt{UserID: f.bob.uid, RoomID: f.id, Type: "m.read", EventID: eventID}
	if got := srv.Receipts(); len(got) != 1 || got[0] != want {
		t.Errorf("receipts %+v", got)
	}
}

func TestMessagesPages(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	for range 3 {
		if _, err := f.alice.Send(t.Context(), f.id, msgType, "", hello); err != nil {
			t.Fatal(err)
		}
	}
	page, err := f.bob.Messages(t.Context(), f.id, MessagesRequest{Dir: Backward, Limit: 2, Types: []string{msgType}})
	if err != nil || len(page.Chunk) != 2 || page.End == "" {
		t.Fatalf("messages = %+v, %v", page, err)
	}
	q := lastRequest(t, srv).Query
	if q.Encode() != "dir=b&filter=%7B%22types%22%3A%5B%22m.room.message%22%5D%7D&limit=2" {
		t.Errorf("query %v", q)
	}
	next, err := f.bob.Messages(t.Context(), f.id, MessagesRequest{From: page.End, Dir: Backward, Limit: 2, Types: []string{msgType}})
	if err != nil || len(next.Chunk) != 1 {
		t.Fatalf("second page = %+v, %v", next, err)
	}
	forward, err := f.bob.Messages(t.Context(), f.id, MessagesRequest{From: "0", To: page.End, Dir: Forward, Limit: 10})
	if err != nil || len(forward.Chunk) == 0 {
		t.Fatalf("forward = %+v, %v", forward, err)
	}
	empty, err := f.bob.Messages(t.Context(), f.id, MessagesRequest{From: "0", To: "0", Dir: Forward, Limit: 10})
	if err != nil || len(empty.Chunk) != 0 || empty.End != "" {
		t.Fatalf("an empty page = %+v, %v", empty, err)
	}
}

func TestMessagesRefused(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	before := len(srv.Requests())
	for _, bad := range []MessagesRequest{
		{Dir: "x", Limit: 1},
		{Dir: Forward},
		{Dir: Forward, Limit: -1},
		{Dir: Forward, Limit: 1, From: "a b"},
		{Dir: Forward, Limit: 1, To: "\n"},
	} {
		_, err := f.bob.Messages(t.Context(), f.id, bad)
		wantErr(t, err, ErrInvalid, "")
	}
	if len(srv.Requests()) != before {
		t.Error("a refused /messages reached the server")
	}
	for _, body := range []string{`{"start":"1"}`, `{"chunk":{},"start":"1"}`, `{"chunk":[],"start":"1","end":5}`, `{"chunk":[7],"start":"1"}`} {
		inject(srv, http.MethodGet, "/messages", http.StatusOK, body)
		_, err := f.bob.Messages(t.Context(), f.id, MessagesRequest{Dir: Forward, Limit: 1})
		wantErr(t, err, ErrUnreachable, "")
	}
	inject(srv, http.MethodGet, "/messages", http.StatusForbidden, `{}`)
	_, err := f.bob.Messages(t.Context(), f.id, MessagesRequest{Dir: Forward, Limit: 1})
	wantErr(t, err, ErrForbidden, "")
}
