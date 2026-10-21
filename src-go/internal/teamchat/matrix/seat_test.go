package matrix

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
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

// A second login as the device leaves the first token valid, as Tuwunel 1.9.4 does (host check
// H-C13); a logout with either token ends both (SPEC M45, M46).
func TestLoginDeviceKeepsThePreviousHoldersTokenUntilALogout(t *testing.T) {
	srv := newServer(t)
	uid := srv.AddUser("seat", "pw", false)
	c := signedOut(t, srv.URL())
	first := take(t, c)
	second := take(t, c)
	for _, held := range []*Client{first, second} {
		if who, err := held.Whoami(t.Context()); err != nil || who.UserID != uid || who.DeviceID != seatDevice {
			t.Fatalf("whoami after two takes = %+v, %v", who, err)
		}
	}
	if err := second.Logout(t.Context()); err != nil {
		t.Fatalf("logout: %v", err)
	}
	for _, held := range []*Client{first, second} {
		_, err := held.Whoami(t.Context())
		if e := wantErr(t, err, ErrAuthRefused, ""); e.Errcode != "M_UNKNOWN_TOKEN" {
			t.Errorf("a token of the logged-out device answered %q", e.Errcode)
		}
	}
	_, err := c.LoginDevice(t.Context(), "seat", "wrong", seatDevice)
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
	inject(srv, http.MethodGet, "/account_data/", http.StatusNotFound, `{"errcode":"M_UNRECOGNIZED","error":"Unrecognized request"}`)
	_, found, err := f.bob.RoomAccountData(t.Context(), f.bob.uid, f.id, floorType)
	if e := wantErr(t, err, ErrNotFound, ""); found || e.Errcode != "M_UNRECOGNIZED" {
		t.Errorf("a 404 that is not M_NOT_FOUND = %v, %v", found, err)
	}
	inject(srv, http.MethodGet, "/account_data/", http.StatusNotFound, `{}`)
	_, _, err = f.bob.RoomAccountData(t.Context(), f.bob.uid, f.id, floorType)
	wantErr(t, err, ErrNotFound, "")
	inject(srv, http.MethodGet, "/account_data/", http.StatusForbidden, `{}`)
	_, _, err = f.bob.RoomAccountData(t.Context(), f.bob.uid, f.id, floorType)
	wantErr(t, err, ErrForbidden, "")
	inject(srv, http.MethodPut, "/account_data/", http.StatusOK, `"done"`)
	err = f.bob.PutRoomAccountData(t.Context(), f.bob.uid, f.id, floorType, json.RawMessage(`{"v":1}`))
	wantErr(t, err, ErrUnreachable, detailNotObject)
}

const devicesPath = "/_matrix/client/v3/devices/"

// signedAs is c signed in as deviceID of the account "seat".
func signedAs(t *testing.T, c *Client, deviceID string) *Client {
	t.Helper()
	sess, err := c.LoginDevice(t.Context(), "seat", "pw", deviceID)
	if err != nil {
		t.Fatalf("loginDevice %s: %v", deviceID, err)
	}
	signed, err := c.WithToken(sess.AccessToken.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signed.Close)
	return signed
}

// A device delete answers the challenge with the password and ends every token of the device
// (SPEC M52), the take's way to end a previous holder's token (host check H-C13's fallback).
func TestDeleteDeviceEndsEveryTokenOfTheDevice(t *testing.T) {
	srv := newServer(t)
	srv.AddUser("seat", "pw", false)
	c := signedOut(t, srv.URL())
	first, second := take(t, c), take(t, c)
	taker := signedAs(t, c, "TCTAKER")
	before := len(srv.Requests())
	if err := taker.DeleteDevice(t.Context(), "seat", "pw", seatDevice); err != nil {
		t.Fatalf("deleteDevice: %v", err)
	}
	reqs := srv.Requests()[before:]
	if len(reqs) != 2 {
		t.Fatalf("the delete sent %d requests, want the challenge and its answer", len(reqs))
	}
	for _, req := range reqs {
		if req.Method != http.MethodDelete || req.Path != devicesPath+seatDevice || req.Header.Get("Authorization") == "" {
			t.Errorf("the delete sent %s %s", req.Method, req.Path)
		}
	}
	for _, held := range []*Client{first, second} {
		_, err := held.Whoami(t.Context())
		if e := wantErr(t, err, ErrAuthRefused, ""); e.Errcode != "M_UNKNOWN_TOKEN" {
			t.Errorf("a token of the deleted device answered %q", e.Errcode)
		}
	}
	if _, err := taker.Whoami(t.Context()); err != nil {
		t.Fatalf("the deleting token: %v", err)
	}
	err := taker.DeleteDevice(t.Context(), "seat", "wrong", "TCTAKER")
	if e := wantErr(t, err, ErrForbidden, detailPasswordRefused); e.Errcode != "M_FORBIDDEN" || e.Status != http.StatusUnauthorized {
		t.Errorf("a wrong password = %v", err)
	}
	if err = taker.DeleteDevice(t.Context(), "seat", "pw", "TCTAKER"); err != nil {
		t.Fatalf("deleting its own device: %v", err)
	}
	err = taker.DeleteDevice(t.Context(), "seat", "pw", "TCTAKER")
	if e := wantErr(t, err, ErrAuthRefused, ""); e.Errcode != "M_UNKNOWN_TOKEN" {
		t.Errorf("a delete with a dead token answered %q", e.Errcode)
	}
}

func TestDeleteDeviceBody(t *testing.T) {
	bodies := make(chan map[string]any, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		bodies <- body
		answer := `{}`
		if _, hasAuth := body["auth"]; !hasAuth {
			w.WriteHeader(http.StatusUnauthorized)
			answer = `{"session":"S1","flows":[{"stages":["m.login.dummy"]},{"stages":["m.login.password"]}],"params":{}}`
		}
		if _, err := io.WriteString(w, answer); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	held, err := NewMember(Config{BaseURL: srv.URL, PlainHTTPHosts: []string{loopback}, Token: "tok", Clock: &fakeClock{}})
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err = held.DeleteDevice(t.Context(), "seat", "pw", seatDevice); err != nil {
		t.Fatal(err)
	}
	if challenge := <-bodies; len(challenge) != 0 {
		t.Errorf("the first request's body %v, want {}", challenge)
	}
	auth, isObject := (<-bodies)["auth"].(map[string]any)
	ident, isIdent := auth["identifier"].(map[string]any)
	if !isObject || auth["type"] != "m.login.password" || auth["password"] != "pw" || auth["session"] != "S1" ||
		!isIdent || ident["type"] != "m.id.user" || ident["user"] != "seat" || len(auth) != 4 {
		t.Errorf("the answer's auth %v", auth)
	}
}

func TestDeleteDeviceRefusals(t *testing.T) {
	srv := newServer(t)
	srv.AddUser("seat", "pw", false)
	held := signedAs(t, signedOut(t, srv.URL()), seatDevice)
	before := len(srv.Requests())
	for _, bad := range []string{"", "has space", strings.Repeat("A", 256), "café"} {
		wantErr(t, held.DeleteDevice(t.Context(), "seat", "pw", bad), ErrInvalid, "")
	}
	if len(srv.Requests()) != before {
		t.Error("a refused device ID reached the server")
	}
	const challenge = `{"session":"S1","flows":[{"stages":["m.login.password"]}]}`
	for _, step := range []struct {
		name    string
		answers []string
		kind    error
		detail  string
	}{
		{"no session", []string{`{"flows":[{"stages":["m.login.password"]}]}`}, ErrUnreachable, detailMalformed},
		{"no password flow", []string{`{"session":"S1","flows":[{"stages":["m.login.email.identity"]}]}`}, ErrRefused, detailNoPasswordFlow},
		{"password among stages", []string{`{"session":"S1","flows":[{"stages":["m.login.password","m.login.dummy"]}]}`}, ErrRefused, detailNoPasswordFlow},
		{"a challenge that is not an object", []string{`[1]`}, ErrUnreachable, detailMalformed},
		{"another stage asked", []string{challenge, `{"session":"S1","flows":[{"stages":["m.login.password"]}],"completed":["m.login.password"]}`}, ErrAuthRefused, ""},
	} {
		t.Run(step.name, func(t *testing.T) {
			for _, body := range step.answers {
				inject(srv, http.MethodDelete, devicesPath, http.StatusUnauthorized, body)
			}
			wantErr(t, held.DeleteDevice(t.Context(), "seat", "pw", seatDevice), step.kind, step.detail)
		})
	}
	inject(srv, http.MethodDelete, devicesPath, http.StatusUnauthorized, challenge)
	inject(srv, http.MethodDelete, devicesPath, http.StatusOK, `[]`)
	wantErr(t, held.DeleteDevice(t.Context(), "seat", "pw", seatDevice), ErrUnreachable, detailNotObject)
	srv.Inject(fakehs.Fault{Method: http.MethodDelete, Path: devicesPath, Drop: true})
	wantErr(t, held.DeleteDevice(t.Context(), "seat", "pw", seatDevice), ErrUnreachable, detailNoAnswer)
	inject(srv, http.MethodDelete, devicesPath, http.StatusUnauthorized, challenge)
	srv.Inject(fakehs.Fault{Method: http.MethodDelete, Path: devicesPath, Drop: true})
	wantErr(t, held.DeleteDevice(t.Context(), "seat", "pw", seatDevice), ErrUnreachable, detailNoAnswer)
	inject(srv, http.MethodDelete, devicesPath, http.StatusNotFound, `{"errcode":"M_NOT_FOUND"}`)
	wantErr(t, held.DeleteDevice(t.Context(), "seat", "pw", seatDevice), ErrNotFound, "")
	before = len(srv.Requests())
	inject(srv, http.MethodDelete, devicesPath, http.StatusOK, `{}`)
	if err := held.DeleteDevice(t.Context(), "seat", "pw", seatDevice); err != nil || len(srv.Requests()) != before+1 {
		t.Fatalf("a delete the server needed no auth for = %v after %d requests", err, len(srv.Requests())-before)
	}
}
