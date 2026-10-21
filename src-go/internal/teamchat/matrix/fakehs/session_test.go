package fakehs

import (
	"net/url"
	"testing"
)

const (
	loginPath    = "/_matrix/client/v3/login"
	logoutPath   = "/_matrix/client/v3/logout"
	floorType    = "teamchat.floor"
	unknownToken = "Unknown access token."
)

// loginSeat signs the account "seat" in, as deviceID when it is not nil.
func (tm *team) loginSeat(password string, deviceID any) answer {
	body := map[string]any{
		"type": "m.login.password", "identifier": map[string]any{"type": "m.id.user", "user": "seat"},
		"password": password,
	}
	if deviceID != nil {
		body["device_id"] = deviceID
	}
	return tm.call("", "POST", loginPath, nil, body)
}

func (tm *team) accountDataPath(uid, room, eventType string) string {
	return "/_matrix/client/v3/user/" + q(uid) + "/rooms/" + q(room) + "/account_data/" + q(eventType)
}

func (tm *team) devicesOf(uid string) []any {
	tm.t.Helper()
	return list(tm.t, tm.ok(tm.call(tm.admin, "GET", adminUsers+q(uid)+"/devices", nil, nil))["devices"])
}

// A second login as a device leaves the device's earlier token valid, as Tuwunel 1.9.4 does
// (host check H-C13 recorded it).
func TestALoginWithAKnownDeviceIDKeepsItsEarlierToken(t *testing.T) {
	tm := newTeam(t)
	seat := tm.s.AddUser("seat", "pw", false)
	first := tm.ok(tm.loginSeat("pw", "TCSEAT"))
	if first["device_id"] != "TCSEAT" || first["user_id"] != seat {
		t.Fatalf("first login %v", first)
	}
	tm.tok[seat] = text(t, first["access_token"])
	if who := tm.ok(tm.whoami(seat)); who["device_id"] != "TCSEAT" {
		t.Fatalf("whoami %v", who)
	}
	held := tm.tok[seat]
	second := tm.ok(tm.loginSeat("pw", "TCSEAT"))
	if second["device_id"] != "TCSEAT" || second["access_token"] == held {
		t.Fatalf("second login %v", second)
	}
	if who := tm.ok(tm.whoami(seat)); who["device_id"] != "TCSEAT" {
		t.Fatalf("the first token after a second login: whoami %v", who)
	}
	tm.tok[seat] = text(t, second["access_token"])
	tm.ok(tm.whoami(seat))
	if devices := tm.devicesOf(seat); len(devices) != 1 || field(t, devices[0], "device_id") != "TCSEAT" {
		t.Fatalf("devices after two logins as one device %v", devices)
	}
	other := tm.ok(tm.loginSeat("pw", "TCOTHER"))
	if other["device_id"] != "TCOTHER" || len(tm.devicesOf(seat)) != 2 {
		t.Fatalf("a second device %v", other)
	}
	tm.ok(tm.whoami(seat))
	if tm.s.CheckModelled() != nil {
		t.Fatalf("a device login was reported unmodelled: %v", tm.s.CheckModelled())
	}
	if bad := tm.loginSeat("pw", 7); bad.status != 500 || tm.s.CheckModelled() == nil {
		t.Fatalf("a device_id that is not a string answered %d, want 500 and unmodelled", bad.status)
	}
}

// A logout ends every token of the caller's device, as H-C13 recorded, and deletes the device.
func TestLogoutEndsEveryTokenOfTheDeviceAndTheDevice(t *testing.T) {
	tm := newTeam(t)
	seat := tm.s.AddUser("seat", "pw", false)
	earlier := text(t, tm.ok(tm.loginSeat("pw", "TCSEAT"))["access_token"])
	tm.tok[seat] = text(t, tm.ok(tm.loginSeat("pw", "TCSEAT"))["access_token"])
	kept := text(t, tm.ok(tm.loginSeat("pw", "TCKEPT"))["access_token"])
	if got := tm.ok(tm.call(seat, "POST", logoutPath, nil, map[string]any{})); len(got) != 0 {
		t.Fatalf("logout answered %v", got)
	}
	tm.refused(tm.whoami(seat), 401, "M_UNKNOWN_TOKEN", unknownToken)
	tm.refused(tm.call(seat, "POST", logoutPath, nil, map[string]any{}), 401, "M_UNKNOWN_TOKEN", unknownToken)
	tm.tok[seat] = earlier
	tm.refused(tm.whoami(seat), 401, "M_UNKNOWN_TOKEN", unknownToken)
	devices := tm.devicesOf(seat)
	if len(devices) != 1 || field(t, devices[0], "device_id") != "TCKEPT" {
		t.Fatalf("devices after a logout %v", devices)
	}
	tm.tok[seat] = kept
	tm.ok(tm.whoami(seat))
}

// userAuth is a device delete's body answering its challenge with userName's password.
func userAuth(userName, password string, session any) map[string]any {
	return map[string]any{"auth": map[string]any{
		"type": "m.login.password", "identifier": map[string]any{"type": "m.id.user", "user": userName},
		"password": password, "session": session,
	}}
}

func passwordAuth(password string, session any) map[string]any {
	return userAuth("seat", password, session)
}

// DELETE /devices/{id} asks for user-interactive auth, then takes the password and ends every
// token of the device, as H-C13's fallback recorded; the deleting token lives on.
func TestDeletingADeviceByPasswordEndsItsTokens(t *testing.T) {
	tm := newTeam(t)
	seat := tm.s.AddUser("seat", "pw", false)
	earlier := text(t, tm.ok(tm.loginSeat("pw", "TCSEAT"))["access_token"])
	later := text(t, tm.ok(tm.loginSeat("pw", "TCSEAT"))["access_token"])
	tm.tok[seat] = text(t, tm.ok(tm.loginSeat("pw", "TCTAKER"))["access_token"])
	path := "/_matrix/client/v3/devices/TCSEAT"
	challenge := tm.call(seat, "DELETE", path, nil, map[string]any{})
	session, isText := challenge.obj["session"].(string)
	flows := list(t, challenge.obj["flows"])
	if challenge.status != 401 || !isText || session == "" || len(flows) != 1 ||
		mustJSON(t, field(t, flows[0], "stages")) != `["m.login.password"]` || challenge.obj["errcode"] != nil {
		t.Fatalf("the first answer %d %v", challenge.status, challenge.obj)
	}
	wrong := tm.call(seat, "DELETE", path, nil, passwordAuth("nope", session))
	if wrong.status != 401 || wrong.obj["errcode"] != "M_FORBIDDEN" || wrong.obj["session"] != session {
		t.Fatalf("a wrong password answered %d %v", wrong.status, wrong.obj)
	}
	if got := tm.ok(tm.call(seat, "DELETE", path, nil, passwordAuth("pw", session))); len(got) != 0 {
		t.Fatalf("the delete answered %v", got)
	}
	taker := tm.tok[seat]
	for _, token := range []string{earlier, later} {
		tm.tok[seat] = token
		tm.refused(tm.whoami(seat), 401, "M_UNKNOWN_TOKEN", unknownToken)
	}
	tm.tok[seat] = taker
	tm.ok(tm.whoami(seat))
	if devices := tm.devicesOf(seat); len(devices) != 1 || field(t, devices[0], "device_id") != "TCTAKER" {
		t.Fatalf("devices after the delete %v", devices)
	}
	if tm.s.CheckModelled() != nil {
		t.Fatalf("a device delete was reported unmodelled: %v", tm.s.CheckModelled())
	}
	own := tm.call(seat, "DELETE", "/_matrix/client/v3/devices/TCTAKER", nil, map[string]any{})
	tm.ok(tm.call(seat, "DELETE", "/_matrix/client/v3/devices/TCTAKER", nil, passwordAuth("pw", own.obj["session"])))
	tm.refused(tm.whoami(seat), 401, "M_UNKNOWN_TOKEN", unknownToken)
}

func TestADeviceDeleteBeyondTheModelIsUnmodelled(t *testing.T) {
	cases := map[string]func(tm *team, seat, session string) answer{
		"an unknown device": func(tm *team, seat, _ string) answer {
			return tm.call(seat, "DELETE", "/_matrix/client/v3/devices/NOSUCH", nil, map[string]any{})
		},
		"a session never given": func(tm *team, seat, _ string) answer {
			return tm.call(seat, "DELETE", "/_matrix/client/v3/devices/TCSEAT", nil, passwordAuth("pw", "made-up"))
		},
		"another account named": func(tm *team, seat, session string) answer {
			return tm.call(seat, "DELETE", "/_matrix/client/v3/devices/TCSEAT", nil, userAuth("alice", "pw", session))
		},
		"another auth type": func(tm *team, seat, session string) answer {
			return tm.call(seat, "DELETE", "/_matrix/client/v3/devices/TCSEAT", nil,
				map[string]any{"auth": map[string]any{"type": "m.login.dummy", "session": session}})
		},
		"auth that is not an object": func(tm *team, seat, _ string) answer {
			return tm.call(seat, "DELETE", "/_matrix/client/v3/devices/TCSEAT", nil, map[string]any{"auth": "pw"})
		},
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			tm := newTeam(t)
			seat := tm.s.AddUser("seat", "pw", false)
			tm.tok[seat] = text(t, tm.ok(tm.loginSeat("pw", "TCSEAT"))["access_token"])
			session := text(t, tm.call(seat, "DELETE", "/_matrix/client/v3/devices/TCSEAT", nil, map[string]any{}).obj["session"])
			if got := request(tm, seat, session); got.status != 500 || tm.s.CheckModelled() == nil {
				t.Fatalf("answered %d %v, want 500 and unmodelled", got.status, got.obj)
			}
		})
	}
}

func TestRoomAccountDataIsKeptPerAccountRoomAndType(t *testing.T) {
	tm := newTeam(t)
	path := tm.accountDataPath(tm.agent, tm.room, floorType)
	tm.refused(tm.call(tm.agent, "GET", path, nil, nil), 404, "M_NOT_FOUND", "")
	marker := map[string]any{"v": 1, "floor_ms": 1700, "restore_epoch": 0}
	if got := tm.ok(tm.call(tm.agent, "PUT", path, nil, marker)); len(got) != 0 {
		t.Fatalf("put answered %v", got)
	}
	got := tm.ok(tm.call(tm.agent, "GET", path, nil, nil))
	if got["floor_ms"] != 1700.0 || got["v"] != 1.0 || len(got) != 3 {
		t.Fatalf("read back %v", got)
	}
	tm.ok(tm.call(tm.agent, "PUT", path, nil, map[string]any{"v": 1, "floor_ms": 1800, "restore_epoch": 0}))
	if again := tm.ok(tm.call(tm.agent, "GET", path, nil, nil)); again["floor_ms"] != 1800.0 {
		t.Fatalf("a second put read back %v", again)
	}
	tm.refused(tm.call(tm.other, "GET", tm.accountDataPath(tm.other, tm.room, floorType), nil, nil), 404, "M_NOT_FOUND", "")
	tm.refused(tm.call(tm.agent, "GET", tm.accountDataPath(tm.agent, tm.room, "teamchat.other"), nil, nil), 404, "M_NOT_FOUND", "")
	if tm.s.CheckModelled() != nil {
		t.Fatalf("room account data was reported unmodelled: %v", tm.s.CheckModelled())
	}
}

func TestRoomAccountDataOutlivesLogoutResetAndRestart(t *testing.T) {
	tm := newTeam(t)
	seat := tm.s.AddUser("seat", "pw", false)
	tm.ok(tm.call(tm.admin, "POST", tm.roomPath("invite"), nil, map[string]any{"user_id": seat}))
	tm.tok[seat] = text(t, tm.ok(tm.loginSeat("pw", "TCSEAT"))["access_token"])
	tm.ok(tm.call(seat, "POST", tm.roomPath("join"), nil, map[string]any{}))
	path := tm.accountDataPath(seat, tm.room, floorType)
	tm.ok(tm.call(seat, "PUT", path, nil, map[string]any{"v": 1, "floor_ms": 5, "restore_epoch": 0}))
	tm.ok(tm.call(seat, "POST", logoutPath, nil, map[string]any{}))
	tm.ok(tm.call(tm.admin, "POST", "/_synapse/admin/v1/reset_password/"+q(seat), nil, map[string]any{"new_password": "pw2"}))
	tm.tok[seat] = text(t, tm.ok(tm.loginSeat("pw2", "TCSEAT"))["access_token"])
	if err := tm.s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tm.s.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := tm.s.Restart(); err != nil {
		t.Fatal(err)
	}
	if got := tm.ok(tm.call(seat, "GET", path, nil, nil)); got["floor_ms"] != 5.0 {
		t.Fatalf("the marker after a logout, a reset and a restart %v", got)
	}
}

func TestARedactionKeepsItsReason(t *testing.T) {
	tm := newTeam(t)
	sent := text(t, tm.ok(tm.send(tm.agent, "t1", map[string]any{"msgtype": "m.text", "body": "a secret"}))["event_id"])
	redaction := text(t, tm.ok(tm.call(tm.admin, "PUT", tm.roomPath("redact/"+q(sent)+"/r1"), nil, map[string]any{"reason": "secret"}))["event_id"])
	got := tm.ok(tm.call(tm.admin, "GET", tm.roomPath("event/"+q(redaction)), nil, nil))
	if field(t, got, "content", "reason") != "secret" || field(t, got, "content", "redacts") != sent {
		t.Fatalf("the redaction %v", got)
	}
	target := tm.ok(tm.call(tm.agent, "GET", tm.roomPath("event/"+q(sent)), nil, nil))
	if field(t, target, "unsigned", "redacted_because", "event_id") != redaction || mustJSON(t, field(t, target, "content")) != "{}" {
		t.Fatalf("the redacted event %v", target)
	}
	page := tm.ok(tm.call(tm.agent, "GET", tm.roomPath("messages"), url.Values{"dir": {"b"}, "limit": {"10"}}, nil))
	for _, ev := range list(t, page["chunk"]) {
		if field(t, ev, "event_id") == sent && field(t, ev, "unsigned", "redacted_because") == nil {
			t.Fatalf("/messages gave the redacted event without redacted_because: %v", ev)
		}
	}
}

func TestSendAndRedactTransactionIDsAreApart(t *testing.T) {
	tm := newTeam(t)
	sent := text(t, tm.ok(tm.send(tm.admin, "same", map[string]any{"msgtype": "m.text", "body": "x"}))["event_id"])
	redaction := text(t, tm.ok(tm.call(tm.admin, "PUT", tm.roomPath("redact/"+q(sent)+"/same"), nil, map[string]any{}))["event_id"])
	if redaction == sent {
		t.Fatal("a redact reusing a send's transaction ID was given the send's event")
	}
	replay := tm.call(tm.admin, "PUT", tm.roomPath("redact/"+q(sent)+"/same"), nil, map[string]any{})
	if replay.status != 500 || tm.s.CheckModelled() == nil {
		t.Fatalf("a replayed redact transaction ID answered %d, want 500 and unmodelled", replay.status)
	}
}

func TestPresenceIsKeptPerAccount(t *testing.T) {
	tm := newTeam(t)
	if got := tm.s.Presence(tm.agent); got != "" {
		t.Fatalf("presence before any = %q", got)
	}
	path := "/_matrix/client/v3/presence/" + q(tm.agent) + "/status"
	if got := tm.ok(tm.call(tm.agent, "PUT", path, nil, map[string]any{"presence": "offline"})); len(got) != 0 {
		t.Fatalf("put presence answered %v", got)
	}
	if got := tm.s.Presence(tm.agent); got != "offline" {
		t.Fatalf("presence = %q, want offline", got)
	}
	for _, step := range []struct {
		query url.Values
		want  string
	}{
		{url.Values{}, "online"},
		{url.Values{"set_presence": {"offline"}}, "offline"},
		{url.Values{"set_presence": {"unavailable"}}, "unavailable"},
		{url.Values{"set_presence": {"online"}}, "online"},
		{url.Values{"set_presence": {"offline"}}, "offline"},
	} {
		step.query.Set("timeout", "0")
		tm.ok(tm.call(tm.agent, "GET", "/_matrix/client/v3/sync", step.query, nil))
		if got := tm.s.Presence(tm.agent); got != step.want {
			t.Fatalf("after a sync with %v presence is %q, want %q", step.query, got, step.want)
		}
	}
	if bad := tm.call(tm.agent, "GET", "/_matrix/client/v3/sync", url.Values{"set_presence": {"away"}}, nil); bad.status != 500 || tm.s.CheckModelled() == nil {
		t.Fatalf("a sync's set_presence of away answered %d, want 500 and unmodelled", bad.status)
	}
	tm = newTeam(t)
	for name, body := range map[string]any{
		"a value beyond the three": map[string]any{"presence": "away"},
		"no presence":              map[string]any{},
		"a body that is not one":   []any{},
	} {
		t.Run(name, func(t *testing.T) {
			fresh := newTeam(t)
			own := "/_matrix/client/v3/presence/" + q(fresh.agent) + "/status"
			if got := fresh.call(fresh.agent, "PUT", own, nil, body); got.status != 500 || fresh.s.CheckModelled() == nil {
				t.Fatalf("answered %d, want 500 and unmodelled", got.status)
			}
		})
	}
	if got := tm.call(tm.other, "PUT", path, nil, map[string]any{"presence": "offline"}); got.status != 500 || tm.s.CheckModelled() == nil {
		t.Fatalf("another account's presence answered %d, want 500 and unmodelled", got.status)
	}
}

func TestRoomAccountDataBeyondTheModelIsUnmodelled(t *testing.T) {
	cases := map[string]func(tm *team) answer{
		"another account's data": func(tm *team) answer {
			return tm.call(tm.agent, "PUT", tm.accountDataPath(tm.other, tm.room, floorType), nil, map[string]any{"v": 1})
		},
		"another account's read": func(tm *team) answer {
			return tm.call(tm.agent, "GET", tm.accountDataPath(tm.other, tm.room, floorType), nil, nil)
		},
		"an unknown room": func(tm *team) answer {
			return tm.call(tm.agent, "PUT", tm.accountDataPath(tm.agent, "!nowhere:"+teamServerName, floorType), nil, map[string]any{"v": 1})
		},
		"content that is not an object": func(tm *team) answer {
			return tm.call(tm.agent, "PUT", tm.accountDataPath(tm.agent, tm.room, floorType), nil, []any{1})
		},
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			tm := newTeam(t)
			if got := request(tm); got.status != 500 || tm.s.CheckModelled() == nil {
				t.Fatalf("answered %d, want 500 and unmodelled", got.status)
			}
		})
	}
}
