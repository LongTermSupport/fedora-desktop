package fakehs

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"
)

const adminPrefix = `/_synapse/admin`

var (
	registerKeys = []string{"nonce", "username", "password", keyAdmin, "mac", keyDisplayName}
	putUserKeys  = []string{"password", keyDisplayName, keyAdmin, "logout_devices", "deactivated", "locked"}
	resetKeys    = []string{"new_password", "logout_devices"}
	// listOmits are the keys of the single-user object the list endpoint's entries lack (H4,
	// fixtures 022 and 023).
	listOmits = []string{"consent_ts", "suspended"}
)

// registrationMAC is Synapse's shared-secret registration MAC, which the protocol makes
// HMAC-SHA1.
func registrationMAC(key, nonce, username, password string, admin bool) string {
	kind := "notadmin"
	if admin {
		kind = keyAdmin
	}
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write([]byte(strings.Join([]string{nonce, username, password, kind}, "\x00")))
	return hex.EncodeToString(mac.Sum(nil))
}

func only(body map[string]any, allowed []string, what string) error {
	for key := range body {
		if !slices.Contains(allowed, key) {
			return unmodelled("%s with %s", what, key)
		}
	}
	return nil
}

func (s *Server) userObject(u *user) map[string]any {
	var lastSeen any
	if u.lastSeen != nil {
		lastSeen = *u.lastSeen
	}
	return map[string]any{
		keyAdmin: u.admin, "consent_ts": nil, "creation_ts": 0, "deactivated": u.deactivated,
		keyDisplayName: u.displayName, "erased": false, "is_guest": false, "last_seen_ts": lastSeen,
		"locked": u.locked, keyName: u.id, "shadow_banned": false, "suspended": false,
	}
}

func (s *Server) existing(userID string) (*user, error) {
	u, ok := s.users[userID]
	if !ok {
		return nil, notFound("User not found.")
	}
	return u, nil
}

func (s *Server) localPart(userID string) (string, error) {
	suffix := ":" + s.name
	if !strings.HasPrefix(userID, "@") || !strings.HasSuffix(userID, suffix) {
		return "", invalidParam("User ID is not local to this server.")
	}
	return strings.TrimSuffix(userID[1:], suffix), nil
}

func (s *Server) registerNonce(*request, []string) (any, error) {
	nonce := hex.EncodeToString(randomBytes(16))
	s.nonces[nonce] = true
	return map[string]any{"nonce": nonce}, nil
}

func (s *Server) register(r *request, _ []string) (any, error) {
	body, err := r.obj()
	if err != nil {
		return nil, err
	}
	if err = only(body, registerKeys, "shared-secret registration"); err != nil {
		return nil, err
	}
	nonce := stringAt(body, "nonce")
	if !s.nonces[nonce] {
		return nil, &apiError{status: http.StatusBadRequest, errcode: errUnknown, text: "Unrecognised nonce."}
	}
	delete(s.nonces, nonce)
	username, nameOK := body["username"].(string)
	password, passOK := body["password"].(string)
	if !nameOK || !passOK {
		return nil, &apiError{status: http.StatusBadRequest, errcode: "M_BAD_JSON", text: "username and password must be strings"}
	}
	admin := body[keyAdmin] == true
	expected := registrationMAC(s.regKey, nonce, username, password, admin)
	if !hmac.Equal([]byte(expected), []byte(stringAt(body, "mac"))) {
		return nil, forbidden("M_FORBIDDEN: HMAC check failed")
	}
	u, err := s.createUser(username, password, true, admin, stringAt(body, keyDisplayName))
	if err != nil {
		return nil, err
	}
	sess := s.newToken(u.id, "")
	return s.signedIn(sess), nil
}

func (s *Server) listUsers(r *request, _ []string) (any, error) {
	for key := range r.query {
		if key != "admins" {
			return nil, unmodelled("user list with %s", key)
		}
	}
	onlyAdmins := r.query.Get("admins") == "true"
	ids := make([]string, 0, len(s.users))
	for id := range s.users {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	entries := []any{}
	for _, id := range ids {
		u := s.users[id]
		if onlyAdmins && !u.admin {
			continue
		}
		entry := s.userObject(u)
		for _, key := range listOmits {
			delete(entry, key)
		}
		entries = append(entries, entry)
	}
	return map[string]any{"next_token": nil, "total": len(entries), "users": entries}, nil
}

func (s *Server) getUser(_ *request, args []string) (any, error) {
	u, err := s.existing(args[0])
	if err != nil {
		return nil, err
	}
	return s.userObject(u), nil
}

func boolAt(body map[string]any, key string) (bool, bool, error) {
	v, present := body[key]
	if !present {
		return false, false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, false, unmodelled("PUT v2/users with %s %v", key, v)
	}
	return b, true, nil
}

func (s *Server) putUser(r *request, args []string) (any, error) {
	body, err := r.obj()
	if err != nil {
		return nil, err
	}
	if err = only(body, putUserKeys, "PUT v2/users"); err != nil {
		return nil, err
	}
	userID := args[0]
	u := s.users[userID]
	if s.quirks.AdminFalseIs500 && body[keyAdmin] == false && (u == nil || !u.admin) {
		return nil, &apiError{status: http.StatusInternalServerError, errcode: errUnknown, text: userID + " was never an admin."}
	}
	if u == nil {
		return s.createAccount(userID, body)
	}
	if err := s.updateAccount(u, body); err != nil {
		return nil, err
	}
	return s.userObject(u), nil
}

func (s *Server) createAccount(userID string, body map[string]any) (any, error) {
	if _, ok := body["deactivated"]; ok {
		return nil, unmodelled("creating an account with deactivated")
	}
	if _, ok := body["locked"]; ok {
		return nil, unmodelled("creating an account with locked")
	}
	local, err := s.localPart(userID)
	if err != nil {
		return nil, err
	}
	password, hasPassword := body["password"].(string)
	u, err := s.createUser(local, password, hasPassword, body[keyAdmin] == true, stringAt(body, keyDisplayName))
	if err != nil {
		return nil, err
	}
	return s.userObject(u), nil
}

func (s *Server) updateAccount(u *user, body map[string]any) error {
	deactivate, hasDeactivated, err := boolAt(body, "deactivated")
	if err != nil {
		return err
	}
	lock, hasLocked, err := boolAt(body, "locked")
	if err != nil {
		return err
	}
	if hasDeactivated && !deactivate && u.deactivated {
		return unmodelled("reactivating a deactivated account")
	}
	if name, ok := body[keyDisplayName].(string); ok {
		u.displayName = name
	}
	if admin, ok := body[keyAdmin].(bool); ok {
		u.admin = admin
	}
	if password, ok := body["password"].(string); ok {
		u.password, u.hasPassword = password, true
		if body["logout_devices"] != false {
			s.logoutAll(u.id)
		}
	}
	if hasLocked {
		u.locked = lock
	}
	if hasDeactivated && deactivate {
		u.deactivated = true
		s.logoutAll(u.id)
	}
	return nil
}

func (s *Server) listDevices(_ *request, args []string) (any, error) {
	u, err := s.existing(args[0])
	if err != nil {
		return nil, err
	}
	out := []any{}
	for _, d := range s.devices {
		if d.userID != u.id {
			continue
		}
		var lastSeen, name any
		if d.lastSeen != nil {
			lastSeen = *d.lastSeen
		}
		if d.displayName != "" {
			name = d.displayName
		}
		out = append(out, map[string]any{keyDeviceID: d.id, "display_name": name, "last_seen_ts": lastSeen, keyUserID: d.userID})
	}
	return map[string]any{"devices": out, "total": len(out)}, nil
}

func (s *Server) mint(r *request, args []string) (any, error) {
	body, err := r.obj()
	if err != nil {
		return nil, err
	}
	if err = only(body, nil, "token mint"); err != nil {
		return nil, err
	}
	u, err := s.existing(args[0])
	if err != nil {
		return nil, err
	}
	return map[string]any{keyAccessToken: s.newToken(u.id, "").token}, nil
}

func (s *Server) resetPassword(r *request, args []string) (any, error) {
	body, err := r.obj()
	if err != nil {
		return nil, err
	}
	if err = only(body, resetKeys, "reset_password"); err != nil {
		return nil, err
	}
	u, err := s.existing(args[0])
	if err != nil {
		return nil, err
	}
	password, ok := body["new_password"].(string)
	if !ok {
		return nil, unmodelled("reset_password without a new_password")
	}
	u.password, u.hasPassword = password, true
	if body["logout_devices"] != false {
		s.logoutAll(u.id)
	}
	return map[string]any{}, nil
}

// resetLoginFailures empties every account's failed-login count, as a restart does.
func (s *Server) resetLoginFailures() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loginFailures = map[string]int{}
}
