package matrix

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	seatDevice = "TCSEAT"
	floorType  = "teamchat.floor"
	logoutPath = "/_matrix/client/v3/logout"
)

// signedOut is a member client with no token, for a login.
func signedOut(t *testing.T, base string) *Client {
	t.Helper()
	c, err := NewMember(Config{BaseURL: base, PlainHTTPHosts: []string{loopback}, Token: "unused", Clock: &fakeClock{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

// take signs the seat in as its fixed device, as a take does (C 3.7.3 step 3).
func take(t *testing.T, c *Client) *Client {
	t.Helper()
	sess, err := c.LoginDevice(t.Context(), "seat", "pw", seatDevice)
	if err != nil || sess.DeviceID != seatDevice || sess.AccessToken == "" {
		t.Fatalf("loginDevice = %+v, %v", sess, err)
	}
	signed, err := c.WithToken(sess.AccessToken.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signed.Close)
	return signed
}

func TestLoginDeviceBody(t *testing.T) {
	bodies := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/_matrix/client/v3/login" || r.Header.Get("Authorization") != "" {
			t.Errorf("%s %s, Authorization %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		bodies <- body
		if _, err := io.WriteString(w, `{"user_id":"@seat:example.org","access_token":"tok","device_id":"TCSEAT"}`); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	if _, err := signedOut(t, srv.URL).LoginDevice(t.Context(), "seat", "pw", seatDevice); err != nil {
		t.Fatal(err)
	}
	body := <-bodies
	ident, isObject := body["identifier"].(map[string]any)
	if body["type"] != "m.login.password" || body["password"] != "pw" || body["device_id"] != seatDevice ||
		!isObject || ident["type"] != "m.id.user" || ident["user"] != "seat" || len(body) != 4 {
		t.Errorf("login body %v", body)
	}
}

func TestLoginDeviceReplacesThePreviousHoldersToken(t *testing.T) {
	srv := newServer(t)
	uid := srv.AddUser("seat", "pw", false)
	c := signedOut(t, srv.URL())
	first := take(t, c)
	if who, err := first.Whoami(t.Context()); err != nil || who.UserID != uid || who.DeviceID != seatDevice {
		t.Fatalf("whoami = %+v, %v", who, err)
	}
	second := take(t, c)
	_, err := first.Whoami(t.Context())
	if e := wantErr(t, err, ErrAuthRefused, ""); e.Errcode != "M_UNKNOWN_TOKEN" {
		t.Errorf("the previous holder's token answered %q", e.Errcode)
	}
	if _, err = second.Whoami(t.Context()); err != nil {
		t.Fatalf("the new holder's whoami: %v", err)
	}
	_, err = c.LoginDevice(t.Context(), "seat", "wrong", seatDevice)
	wantErr(t, err, ErrForbidden, "")
}

func TestLoginDeviceRefusals(t *testing.T) {
	srv := newServer(t)
	srv.AddUser("seat", "pw", false)
	c := signedOut(t, srv.URL())
	before := len(srv.Requests())
	for _, bad := range []string{"", "has space", strings.Repeat("A", 256), "café", "TC\n"} {
		_, err := c.LoginDevice(t.Context(), "seat", "pw", bad)
		wantErr(t, err, ErrInvalid, "")
	}
	if len(srv.Requests()) != before {
		t.Error("a refused device ID reached the server")
	}
	inject(srv, http.MethodPost, "/login", http.StatusOK, `{"user_id":"@seat:example.org","access_token":"tok","device_id":"OTHER"}`)
	_, err := c.LoginDevice(t.Context(), "seat", "pw", seatDevice)
	wantErr(t, err, ErrUnreachable, detailMalformed)
	inject(srv, http.MethodPost, "/login", http.StatusOK, `{"user_id":"@seat:example.org","device_id":"TCSEAT"}`)
	_, err = c.LoginDevice(t.Context(), "seat", "pw", seatDevice)
	wantErr(t, err, ErrUnreachable, detailMalformed)
}

func TestLogout(t *testing.T) {
	srv := newServer(t)
	srv.AddUser("seat", "pw", false)
	held := take(t, signedOut(t, srv.URL()))
	if err := held.Logout(t.Context()); err != nil {
		t.Fatalf("logout: %v", err)
	}
	req := lastRequest(t, srv)
	if req.Method != http.MethodPost || req.Path != logoutPath || req.Header.Get("Authorization") == "" {
		t.Errorf("logout sent %s %s", req.Method, req.Path)
	}
	_, err := held.Whoami(t.Context())
	wantErr(t, err, ErrAuthRefused, "")
	again := held.Logout(t.Context())
	if e := wantErr(t, again, ErrAuthRefused, ""); e.Errcode != "M_UNKNOWN_TOKEN" {
		t.Errorf("a second logout answered %q, want M_UNKNOWN_TOKEN", e.Errcode)
	}
	inject(srv, http.MethodPost, "/logout", http.StatusOK, `[]`)
	wantErr(t, take(t, signedOut(t, srv.URL())).Logout(t.Context()), ErrUnreachable, detailNotObject)
}

func TestRoomAccountData(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	content, found, err := f.bob.RoomAccountData(t.Context(), f.bob.uid, f.id, floorType)
	if err != nil || found || content != nil {
		t.Fatalf("an absent marker = %s, %v, %v", content, found, err)
	}
	marker := json.RawMessage(`{"floor_ms":1700,"restore_epoch":0,"v":1}`)
	if err = f.bob.PutRoomAccountData(t.Context(), f.bob.uid, f.id, floorType, marker); err != nil {
		t.Fatalf("put: %v", err)
	}
	req := lastRequest(t, srv)
	if want := "/_matrix/client/v3/user/%40bob%3Aexample.org/rooms/" + escape(f.id) + "/account_data/teamchat.floor"; req.Method != http.MethodPut || req.Path != want {
		t.Errorf("put sent %s %s, want PUT %s", req.Method, req.Path, want)
	}
	content, found, err = f.bob.RoomAccountData(t.Context(), f.bob.uid, f.id, floorType)
	if err != nil || !found || string(content) != string(marker) {
		t.Fatalf("read back = %s, %v, %v", content, found, err)
	}
	if _, found, err := f.alice.RoomAccountData(t.Context(), f.alice.uid, f.id, floorType); err != nil || found {
		t.Fatalf("another account's marker = %v, %v", found, err)
	}
}

func TestRoomAccountDataOutlivesTheHoldersLogout(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	uid := srv.AddUser("seat", "pw", false)
	if err := f.alice.Invite(t.Context(), f.id, uid); err != nil {
		t.Fatal(err)
	}
	c := signedOut(t, srv.URL())
	held := take(t, c)
	if _, err := held.Join(t.Context(), f.id); err != nil {
		t.Fatal(err)
	}
	marker := json.RawMessage(`{"floor_ms":5,"restore_epoch":0,"v":1}`)
	if err := held.PutRoomAccountData(t.Context(), uid, f.id, floorType, marker); err != nil {
		t.Fatal(err)
	}
	if err := held.Logout(t.Context()); err != nil {
		t.Fatal(err)
	}
	content, found, err := take(t, c).RoomAccountData(t.Context(), uid, f.id, floorType)
	if err != nil || !found || string(content) != string(marker) {
		t.Fatalf("the next holder's marker = %s, %v, %v", content, found, err)
	}
}

func TestRoomAccountDataRefusals(t *testing.T) {
	srv := newServer(t)
	f := room(t, srv)
	before := len(srv.Requests())
	for _, bad := range []func() error{
		func() error { _, _, e := f.bob.RoomAccountData(t.Context(), "", f.id, floorType); return e },
		func() error { _, _, e := f.bob.RoomAccountData(t.Context(), f.bob.uid, "", floorType); return e },
		func() error { _, _, e := f.bob.RoomAccountData(t.Context(), f.bob.uid, f.id, ""); return e },
		func() error {
			return f.bob.PutRoomAccountData(t.Context(), f.bob.uid, f.id, "", json.RawMessage(`{}`))
		},
	} {
		wantErr(t, bad(), ErrInvalid, detailBadSegment)
	}
	for _, content := range []string{`[1]`, `"x"`, `{`, ``} {
		err := f.bob.PutRoomAccountData(t.Context(), f.bob.uid, f.id, floorType, json.RawMessage(content))
		wantErr(t, err, ErrInvalid, "")
	}
	if len(srv.Requests()) != before {
		t.Error("a refused account data call reached the server")
	}
	inject(srv, http.MethodGet, "/account_data/", http.StatusOK, `[1]`)
	_, _, err := f.bob.RoomAccountData(t.Context(), f.bob.uid, f.id, floorType)
	wantErr(t, err, ErrUnreachable, detailNotObject)
	inject(srv, http.MethodGet, "/account_data/", http.StatusForbidden, `{}`)
	_, _, err = f.bob.RoomAccountData(t.Context(), f.bob.uid, f.id, floorType)
	wantErr(t, err, ErrForbidden, "")
	inject(srv, http.MethodPut, "/account_data/", http.StatusOK, `"done"`)
	err = f.bob.PutRoomAccountData(t.Context(), f.bob.uid, f.id, floorType, json.RawMessage(`{"v":1}`))
	wantErr(t, err, ErrUnreachable, detailNotObject)
}
