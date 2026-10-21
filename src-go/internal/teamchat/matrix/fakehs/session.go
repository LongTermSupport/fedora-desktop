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

// signIn signs userID in: on a new device when deviceID is empty, else as that device, whose
// earlier tokens end, as the Matrix specification has a login with an existing device ID do.
func (s *Server) signIn(userID, deviceID, displayName string) session {
	if deviceID == "" {
		return s.newToken(userID, displayName)
	}
	s.endTokens(userID, deviceID)
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

// logout ends the caller's token and deletes its device. Room account data stays.
func (s *Server) logout(r *request, _ []string) (any, error) {
	s.endTokens(r.userID, r.deviceID)
	kept := s.devices[:0]
	for _, d := range s.devices {
		if d.userID != r.userID || d.id != r.deviceID {
			kept = append(kept, d)
		}
	}
	s.devices = kept
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

// syncPresence applies a sync's set_presence as the specification says: absent or online marks
// the caller online, unavailable marks it so, offline leaves it as it was. Whether Tuwunel does
// the same is host check HC-A1's question; a value the specification does not name is not guessed.
func (s *Server) syncPresence(r *request) error {
	value, given := queryValue(r, "set_presence")
	if !given {
		value = presenceOnline
	}
	switch value {
	case presenceOnline, presenceUnavailable:
		s.presence[r.userID] = value
	case presenceOffline:
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
