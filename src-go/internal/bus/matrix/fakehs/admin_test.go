package fakehs

import (
	"net/url"
	"testing"
)

const adminUsers = "/_synapse/admin/v2/users/"

func (tm *team) putUser(uid string, body map[string]any) answer {
	return tm.call(tm.admin, "PUT", adminUsers+q(uid), nil, body)
}

func (tm *team) whoami(uid string) answer {
	return tm.call(uid, "GET", "/_matrix/client/v3/account/whoami", nil, nil)
}

func TestRegistrationNeedsAFreshNonceAndTheMAC(t *testing.T) {
	tm := newTeam(t)
	nonce := text(t, tm.ok(tm.call("", "GET", "/_synapse/admin/v1/register", nil, nil))["nonce"])
	body := map[string]any{
		"nonce": nonce, "username": "bot", "password": "pw", "admin": false,
		"mac": synapseMAC(tm.s.RegistrationKey(), nonce, "bot", "pw", false),
	}
	got := tm.ok(tm.call("", "POST", "/_synapse/admin/v1/register", nil, body))
	if got["user_id"] != "@bot:"+teamServerName || got["access_token"] == nil {
		t.Fatalf("registered %v", got)
	}
	tm.refused(tm.call("", "POST", "/_synapse/admin/v1/register", nil, body), 400, "M_UNKNOWN", "Unrecognised nonce.")
	body["displayname"], body["valid_until_ms"] = "x", 1
	if got := tm.call("", "POST", "/_synapse/admin/v1/register", nil, body); got.status != 500 {
		t.Fatalf("an unmodelled registration key answered %d", got.status)
	}
	nonce = text(t, tm.ok(tm.call("", "GET", "/_synapse/admin/v1/register", nil, nil))["nonce"])
	tm.refused(tm.call("", "POST", "/_synapse/admin/v1/register", nil, map[string]any{
		"nonce": nonce, "username": "bot2", "password": 7, "mac": "00",
	}), 400, "M_BAD_JSON", "username and password")
}

func TestDeactivationRevokesAndRefusesLogin(t *testing.T) {
	tm := newTeam(t)
	user := tm.s.AddUser("bob", "pw", false)
	tm.tok[user] = text(t, tm.ok(tm.call("", "POST", "/_matrix/client/v3/login", nil, map[string]any{
		"type": "m.login.password", "identifier": map[string]any{"type": "m.id.user", "user": "bob"}, "password": "pw",
	}))["access_token"])
	got := tm.ok(tm.putUser(user, map[string]any{"deactivated": true}))
	if got["deactivated"] != true {
		t.Fatalf("deactivated %v", got)
	}
	tm.refused(tm.whoami(user), 401, "M_UNKNOWN_TOKEN", "Unknown access token.")
	tm.refused(tm.call("", "POST", "/_matrix/client/v3/login", nil, map[string]any{
		"type": "m.login.password", "user": user, "password": "pw",
	}), 403, "M_USER_DEACTIVATED", "deactivated")
	tm.ok(tm.putUser(user, map[string]any{"deactivated": true}))
	if again := tm.putUser(user, map[string]any{"deactivated": false}); again.status != 500 || tm.s.CheckModelled() == nil {
		t.Fatalf("reactivation answered %d", again.status)
	}
}

func TestALockedAccountsTokenIsRefused(t *testing.T) {
	tm := newTeam(t)
	got := tm.ok(tm.putUser(tm.agent, map[string]any{"locked": true}))
	if got["locked"] != true {
		t.Fatalf("locked %v", got)
	}
	tm.refused(tm.whoami(tm.agent), 401, "M_USER_LOCKED", "locked")
	tm.ok(tm.putUser(tm.agent, map[string]any{"locked": false}))
	tm.ok(tm.whoami(tm.agent))
	if bad := tm.putUser(tm.agent, map[string]any{"locked": "yes"}); bad.status != 500 {
		t.Fatalf("a locked that is not a boolean answered %d", bad.status)
	}
}

func TestDevicesListsEachSignIn(t *testing.T) {
	tm := newTeam(t)
	tm.ok(tm.whoami(tm.agent))
	got := tm.ok(tm.call(tm.admin, "GET", adminUsers+q(tm.agent)+"/devices", nil, nil))
	devices := list(t, got["devices"])
	if len(devices) != 1 || field(t, devices[0], "last_seen_ts") == nil || field(t, devices[0], "user_id") != tm.agent ||
		got["total"] != 1.0 {
		t.Fatalf("devices %v", got)
	}
	tm.ok(tm.call(tm.admin, "POST", "/_synapse/admin/v1/reset_password/"+q(tm.agent), nil, map[string]any{"new_password": "n"}))
	if after := list(t, tm.ok(tm.call(tm.admin, "GET", adminUsers+q(tm.agent)+"/devices", nil, nil))["devices"]); len(after) != 0 {
		t.Fatalf("a reset kept devices %v", after)
	}
	tm.refused(tm.call(tm.admin, "GET", adminUsers+q("@ghost:"+teamServerName)+"/devices", nil, nil), 404, "M_NOT_FOUND", "User not found.")
}

func TestLoginFailuresAreLimitedUntilRestart(t *testing.T) {
	tm := newTeam(t)
	user := tm.s.AddUser("carol", "right", false)
	wrong := map[string]any{"type": "m.login.password", "user": "carol", "password": "wrong"}
	for range 3 {
		tm.refused(tm.call("", "POST", "/_matrix/client/v3/login", nil, wrong), 403, "M_FORBIDDEN", "Wrong username or password.")
	}
	right := map[string]any{"type": "m.login.password", "user": user, "password": "right"}
	limited := tm.call("", "POST", "/_matrix/client/v3/login", nil, right)
	if limited.status != 429 || limited.obj["retry_after_ms"] == nil {
		t.Fatalf("a fourth try answered %d %v", limited.status, limited.obj)
	}
	tm.s.resetLoginFailures()
	tm.ok(tm.call("", "POST", "/_matrix/client/v3/login", nil, right))
}

func TestPutUserCreatesAndUpdates(t *testing.T) {
	tm := newTeam(t)
	uid := "@dave:" + teamServerName
	got := tm.ok(tm.putUser(uid, map[string]any{"password": "p", "displayname": "Dave"}))
	if got["name"] != uid || got["admin"] != false || got["displayname"] != "Dave" || got["last_seen_ts"] != nil {
		t.Fatalf("created %v", got)
	}
	tm.ok(tm.putUser(uid, map[string]any{"displayname": "D", "admin": true}))
	if got := tm.ok(tm.call(tm.admin, "GET", adminUsers+q(uid), nil, nil)); got["admin"] != true || got["displayname"] != "D" {
		t.Fatalf("updated %v", got)
	}
	tm.refused(tm.putUser("@x:elsewhere", map[string]any{"password": "p"}), 400, "M_INVALID_PARAM", "not local")
	tm.refused(tm.putUser("@new:"+teamServerName, map[string]any{"admin": false}), 500, "M_UNKNOWN", "was never an admin.")
	tm.s.SetQuirks(Quirks{})
	if got := tm.ok(tm.putUser("@new:"+teamServerName, map[string]any{"admin": false})); got["admin"] != false {
		t.Fatalf("without the quirk %v", got)
	}
	if bad := tm.putUser(uid, map[string]any{"user_type": "bot"}); bad.status != 500 {
		t.Fatalf("an unmodelled key answered %d", bad.status)
	}
	if bad := tm.putUser("@fresh:"+teamServerName, map[string]any{"deactivated": true}); bad.status != 500 {
		t.Fatalf("creating a deactivated account answered %d", bad.status)
	}
	tm.refused(tm.call(tm.admin, "GET", adminUsers+q("@ghost:"+teamServerName), nil, nil), 404, "M_NOT_FOUND", "User not found.")
}

func TestUserListAndMint(t *testing.T) {
	tm := newTeam(t)
	got := tm.ok(tm.call(tm.admin, "GET", "/_synapse/admin/v2/users", url.Values{"admins": {"true"}}, nil))
	users := list(t, got["users"])
	if len(users) != 1 || field(t, users[0], "name") != tm.admin || field(t, users[0], "suspended") != nil || got["next_token"] != nil {
		t.Fatalf("admins %v", got)
	}
	all := tm.ok(tm.call(tm.admin, "GET", "/_synapse/admin/v2/users", nil, nil))
	if len(list(t, all["users"])) != 5 {
		t.Fatalf("users %v", all)
	}
	if bad := tm.call(tm.admin, "GET", "/_synapse/admin/v2/users", url.Values{"from": {"0"}}, nil); bad.status != 500 {
		t.Fatalf("an unmodelled query answered %d", bad.status)
	}
	if bad := tm.call(tm.admin, "POST", "/_synapse/admin/v1/users/"+q(tm.agent)+"/login", nil, map[string]any{"valid_until_ms": 1}); bad.status != 500 {
		t.Fatalf("an unmodelled mint answered %d", bad.status)
	}
	tm.refused(tm.call(tm.admin, "POST", "/_synapse/admin/v1/users/"+q("@ghost:"+teamServerName)+"/login", nil, map[string]any{}),
		404, "M_NOT_FOUND", "User not found.")
	if _, err := tm.s.MintToken("@ghost:" + teamServerName); err == nil {
		t.Fatal("a token was minted for no account")
	}
	tm.refused(tm.call(tm.agent, "GET", "/_synapse/admin/v2/users", nil, nil), 403, "M_FORBIDDEN", "Only server administrators")
}
