package fakehs

import (
	"encoding/json"
	"fmt"
)

// The presences the specification names.
const (
	presenceOnline      = "online"
	presenceOffline     = "offline"
	presenceUnavailable = "unavailable"
)

// accountDataKey names one room account data entry: an account's, in a room, of a type.
type accountDataKey struct {
	userID, roomID, eventType string
}

// deviceIDOf is the device a login asks for; empty when it names none.
func deviceIDOf(body map[string]any) (string, error) {
	raw, present := body[keyDeviceID]
	if !present {
		return "", nil
	}
	id, isText := raw.(string)
	if !isText || id == "" {
		return "", unmodelled("a login device_id %v", raw)
	}
	return id, nil
}

// signIn signs userID in: on a new device when deviceID is empty, else as that device. The
// device's earlier tokens stay valid: Tuwunel 1.9.4 keeps them until a logout or a delete of
// the device (host check H-C13), where the Matrix specification would end them.
func (s *Server) signIn(userID, deviceID, displayName string) session {
	if deviceID == "" {
		return s.newToken(userID, displayName)
	}
	if s.device(userID, deviceID) == nil {
		s.devices = append(s.devices, &device{userID: userID, id: deviceID, displayName: displayName})
	}
	sess := session{userID: userID, deviceID: deviceID, token: newTokenText()}
	s.tokens[sess.token] = sess
	return sess
}

// endTokens revokes every token of one device.
func (s *Server) endTokens(userID, deviceID string) {
	for token, sess := range s.tokens {
		if sess.userID == userID && sess.deviceID == deviceID {
			delete(s.tokens, token)
		}
	}
}

// removeDevice ends every token of one device and deletes it. Room account data stays.
func (s *Server) removeDevice(userID, deviceID string) {
	s.endTokens(userID, deviceID)
	kept := s.devices[:0]
	for _, d := range s.devices {
		if d.userID != userID || d.id != deviceID {
			kept = append(kept, d)
		}
	}
	s.devices = kept
}

// logout ends every token of the caller's device, not only its own, and deletes the device, as
// H-C13 recorded.
func (s *Server) logout(r *request, _ []string) (any, error) {
	s.removeDevice(r.userID, r.deviceID)
	return map[string]any{}, nil
}

// uiaError is a 401 asking for user-interactive auth by password, in the specification's
// shape (H-C13 recorded only the status); failed adds the refusal of a wrong password.
type uiaError struct {
	session string
	failed  bool
}

func (c *uiaError) Error() string { return "user-interactive auth " + c.session }

func (c *uiaError) answer() map[string]any {
	out := map[string]any{
		"session": c.session, "flows": []any{map[string]any{"stages": []any{loginPassword}}},
		"params": map[string]any{}, "completed": []any{},
	}
	if c.failed {
		out["errcode"], out["error"] = "M_FORBIDDEN", "Invalid username or password."
	}
	return out
}

// deleteDevice is DELETE /devices/{id}: the first request is challenged, the second answers
// with the account's password and the challenge's session, and then the device and every
// token of it end (the H-C13 fallback). An unknown device, a session not given to the caller,
// or an auth beyond a password for the caller's own account is not guessed at.
func (s *Server) deleteDevice(r *request, args []string) (any, error) {
	if s.device(r.userID, args[0]) == nil {
		return nil, unmodelled("deleting the unknown device %s", args[0])
	}
	body, err := r.obj()
	if err != nil {
		return nil, err
	}
	raw, given := body["auth"]
	if !given {
		session := newTokenText()
		s.uiaSessions[session] = r.userID
		return nil, &uiaError{session: session}
	}
	auth, isObject := raw.(map[string]any)
	if !isObject || auth["type"] != loginPassword {
		return nil, unmodelled("a device delete's auth %v", raw)
	}
	session := stringAt(auth, "session")
	if owner, issued := s.uiaSessions[session]; !issued || owner != r.userID {
		return nil, unmodelled("a device delete's session that was not given to %s", r.userID)
	}
	ident, isIdent := auth["identifier"].(map[string]any)
	name := ""
	if isIdent {
		name = stringAt(ident, "user")
	}
	if name != r.userID && s.userIDOf(name) != r.userID {
		return nil, unmodelled("a device delete authenticated as %q by %s", name, r.userID)
	}
	if password, isText := auth["password"].(string); !isText || password != s.users[r.userID].password {
		return nil, &uiaError{session: session, failed: true}
	}
	delete(s.uiaSessions, session)
	s.removeDevice(r.userID, args[0])
	return map[string]any{}, nil
}

// accountDataAt is the entry a room account data path names. H4 recorded no request for another
// account's data or in a room the server does not know, so what Tuwunel answers is not guessed.
func (s *Server) accountDataAt(r *request, args []string) (accountDataKey, error) {
	key := accountDataKey{userID: args[0], roomID: args[1], eventType: args[2]}
	if key.userID != r.userID {
		return key, unmodelled("room account data of %s asked by %s", key.userID, r.userID)
	}
	if _, known := s.rooms[key.roomID]; !known {
		return key, unmodelled("room account data in the unknown room %s", key.roomID)
	}
	return key, nil
}

// getAccountData is one entry's content. The errcode of none is the specification's; H4
// recorded no such answer, so its text is not Tuwunel's.
func (s *Server) getAccountData(r *request, args []string) (any, error) {
	key, err := s.accountDataAt(r, args)
	if err != nil {
		return nil, err
	}
	content, found := s.accountData[key]
	if !found {
		return nil, notFound("Data not found.")
	}
	return content, nil
}

// Presence is the account's presence, empty while neither a sync nor PUT /presence has set it.
func (s *Server) Presence(userID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.presence[userID]
}

// syncPresence applies a sync's set_presence as Tuwunel 1.9.4 does (host check HC-A1): absent
// or online marks the caller online, and unavailable and offline each set themselves, where the
// specification has offline change nothing. A value the specification does not name is not
// guessed at.
func (s *Server) syncPresence(r *request) error {
	value, given := queryValue(r, "set_presence")
	if !given {
		value = presenceOnline
	}
	switch value {
	case presenceOnline, presenceUnavailable, presenceOffline:
		s.presence[r.userID] = value
	default:
		return unmodelled("a sync's set_presence of %q", value)
	}
	return nil
}

// putPresence sets the caller's own presence. H4 recorded no presence request, so another
// account's, or a value the specification does not name, is not guessed at.
func (s *Server) putPresence(r *request, args []string) (any, error) {
	if args[0] != r.userID {
		return nil, unmodelled("the presence of %s set by %s", args[0], r.userID)
	}
	body, isObject := r.body.(map[string]any)
	value := ""
	if isObject {
		value = stringAt(body, "presence")
	}
	switch value {
	case presenceOnline, presenceOffline, presenceUnavailable:
		s.presence[r.userID] = value
		return map[string]any{}, nil
	}
	return nil, unmodelled("a presence of %v", r.body)
}

func (s *Server) putAccountData(r *request, args []string) (any, error) {
	key, err := s.accountDataAt(r, args)
	if err != nil {
		return nil, err
	}
	body, isObject := r.body.(map[string]any)
	if !isObject {
		return nil, unmodelled("room account data that is not an object")
	}
	content, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("fakehs: room account data: %w", err)
	}
	s.accountData[key] = content
	return map[string]any{}, nil
}
