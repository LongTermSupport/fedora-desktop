package fakehs

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"maps"
	"math"
	"slices"
	"strconv"
)

type stateKey struct {
	typ, key string
}

type txnRef struct {
	userID, deviceID, txn string
}

type event struct {
	pos      int
	id       string
	roomID   string
	typ      string
	sender   string
	content  map[string]any
	ts       int64
	stateKey *string
	txn      *txnRef
}

func (e *event) client(withRoom bool, unsigned map[string]any) map[string]any {
	out := map[string]any{
		"content": e.content, keyEventID: e.id, "origin_server_ts": e.ts, "sender": e.sender, "type": e.typ,
	}
	if e.stateKey != nil {
		out["state_key"] = *e.stateKey
	}
	if withRoom {
		out["room_id"] = e.roomID
	}
	if unsigned != nil {
		out["unsigned"] = unsigned
	}
	return out
}

func (e *event) stripped() map[string]any {
	out := map[string]any{"content": e.content, "sender": e.sender, "state_key": *e.stateKey, "type": e.typ}
	if e.typ == createType {
		out["origin_server_ts"] = e.ts
	}
	return out
}

type room struct {
	id      string
	creator string
	events  []*event
	state   map[stateKey]*event
}

func (r *room) membership(userID string) string {
	e := r.state[stateKey{memberType, userID}]
	if e == nil {
		return ""
	}
	return stringAt(e.content, "membership")
}

func (r *room) levels() map[string]any {
	e := r.state[stateKey{powerLevelsType, ""}]
	if e == nil {
		return map[string]any{}
	}
	return e.content
}

// level is a power level from levels, or def when it is absent.
func level(levels map[string]any, key string, def float64) (float64, error) {
	v, ok := levels[key]
	if !ok {
		return def, nil
	}
	return number(v, key)
}

func number(v any, what string) (float64, error) {
	switch n := v.(type) {
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, unmodelled("power level %s is %v", what, v)
		}
		return f, nil
	case int:
		return float64(n), nil
	default:
		return 0, unmodelled("power level %s is %v", what, v)
	}
}

// power is a member's power; a room-version-12 creator's is infinite and not in users.
func (r *room) power(userID string) (float64, error) {
	if userID == r.creator {
		return math.Inf(1), nil
	}
	levels := r.levels()
	if users, ok := levels["users"].(map[string]any); ok {
		if v, listed := users[userID]; listed {
			return number(v, "users."+userID)
		}
	}
	return level(levels, "users_default", 0)
}

// eventLevel is the power an event type needs.
func (r *room) eventLevel(eventType string, state bool) (float64, error) {
	levels := r.levels()
	def, err := level(levels, "events_default", 0)
	if state {
		def, err = level(levels, "state_default", 50)
	}
	if err != nil {
		return 0, err
	}
	if events, ok := levels[keyEvents].(map[string]any); ok {
		return level(events, eventType, def)
	}
	return def, nil
}

func (r *room) stateBefore(pos int) map[stateKey]*event {
	found := map[stateKey]*event{}
	for _, e := range r.events {
		if e.pos < pos && e.stateKey != nil {
			found[stateKey{e.typ, *e.stateKey}] = e
		}
	}
	return found
}

func formatLevel(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func (s *Server) newIDBody() string {
	s.ids++
	sum := sha256.Sum256(binary.BigEndian.AppendUint64(append([]byte(nil), s.salt...), s.ids))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (s *Server) appendEvent(r *room, typ, sender string, content map[string]any, key *string, id string, txn *txnRef) *event {
	s.pos++
	if id == "" {
		id = "$" + s.newIDBody()
	}
	e := &event{pos: s.pos, id: id, roomID: r.id, typ: typ, sender: sender, content: content, ts: s.nowMS(), stateKey: key, txn: txn}
	r.events = append(r.events, e)
	if key != nil {
		r.state[stateKey{typ, *key}] = e
	}
	close(s.changed)
	s.changed = make(chan struct{})
	return e
}

func (s *Server) findRoom(id string) (*room, error) {
	r, ok := s.rooms[id]
	if !ok {
		return nil, notFound("Room not found.")
	}
	return r, nil
}

func (s *Server) joinedRoom(id, userID string) (*room, error) {
	r, err := s.findRoom(id)
	if err != nil {
		return nil, err
	}
	if r.membership(userID) != membershipJoin {
		return nil, forbidden("You don't have permission to view this room.")
	}
	return r, nil
}

func (s *Server) memberContent(userID, membership string) map[string]any {
	return map[string]any{keyDisplayName: s.users[userID].displayName, "membership": membership}
}

var createRoomKeys = map[string]bool{
	"preset": true, "room_version": true, keyName: true, "topic": true, "initial_state": true,
	"power_level_content_override": true, "visibility": true, membershipInvite: true,
}

func defaultLevels() map[string]any {
	return map[string]any{
		"users": map[string]any{}, "users_default": 0, "events_default": 0, "state_default": 50,
		"ban": 50, "kick": 50, "redact": 50, membershipInvite: 0, "notifications": map[string]any{"room": 50},
		keyEvents: map[string]any{
			"m.room.name": 50, powerLevelsType: 100, "m.room.history_visibility": 100,
			"m.room.canonical_alias": 50, "m.room.avatar": 50, "m.room.tombstone": 150,
			"m.room.server_acl": 100, "m.room.encryption": 100,
		},
	}
}

type initialState struct {
	typ, key string
	content  map[string]any
}

// createRoomPlan is a createRoom body checked against the model before anything is made.
type createRoomPlan struct {
	override map[string]any
	initial  []initialState
	invite   []string
}

// privateV12 refuses a createRoom beyond a private room of version 12.
func privateV12(body map[string]any) error {
	for key := range body {
		if !createRoomKeys[key] {
			return unmodelled("createRoom beyond a private v12 room: %s", key)
		}
	}
	if v, ok := body["room_version"]; ok && v != roomVersion {
		return unmodelled("createRoom with room_version %v", v)
	}
	if v, ok := body["preset"]; ok && v != "private_chat" {
		return unmodelled("createRoom with preset %v", v)
	}
	return nil
}

func planCreateRoom(body map[string]any) (createRoomPlan, error) {
	var plan createRoomPlan
	if err := privateV12(body); err != nil {
		return plan, err
	}
	if v, ok := body["power_level_content_override"]; ok {
		if plan.override, ok = v.(map[string]any); !ok {
			return plan, unmodelled("createRoom with power_level_content_override %v", v)
		}
	}
	list, err := listAt(body, "initial_state")
	if err != nil {
		return plan, err
	}
	for _, item := range list {
		e, isObj := item.(map[string]any)
		typ, hasType := e["type"].(string)
		content, hasContent := e["content"].(map[string]any)
		if !isObj || !hasType || !hasContent {
			return plan, unmodelled("createRoom initial_state %v", item)
		}
		plan.initial = append(plan.initial, initialState{typ, stringAt(e, "state_key"), content})
	}
	invites, err := listAt(body, membershipInvite)
	if err != nil {
		return plan, err
	}
	for _, item := range invites {
		target, ok := item.(string)
		if !ok {
			return plan, unmodelled("createRoom invite %v", item)
		}
		plan.invite = append(plan.invite, target)
	}
	return plan, nil
}

func listAt(body map[string]any, key string) ([]any, error) {
	v, present := body[key]
	if !present {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, unmodelled("createRoom %s %v", key, v)
	}
	return list, nil
}

func (s *Server) createRoom(r *request, _ []string) (any, error) {
	body, err := r.obj()
	if err != nil {
		return nil, err
	}
	plan, err := planCreateRoom(body)
	if err != nil {
		return nil, err
	}
	for _, target := range plan.invite {
		if _, ok := s.users[target]; !ok {
			return nil, notFound("User not found.")
		}
	}
	idBody, creator := s.newIDBody(), r.userID
	rm := &room{id: "!" + idBody, creator: creator, state: map[stateKey]*event{}}
	s.rooms[rm.id] = rm
	s.roomOrder = append(s.roomOrder, rm.id)
	s.appendEvent(rm, createType, creator, map[string]any{"room_version": roomVersion}, new(""), "$"+idBody, nil)
	s.appendEvent(rm, memberType, creator, s.memberContent(creator, membershipJoin), new(creator), "", nil)
	levels := defaultLevels()
	maps.Copy(levels, plan.override)
	s.appendEvent(rm, powerLevelsType, creator, levels, new(""), "", nil)
	s.appendEvent(rm, joinRulesType, creator, map[string]any{"join_rule": membershipInvite}, new(""), "", nil)
	s.appendEvent(rm, "m.room.history_visibility", creator, map[string]any{"history_visibility": "shared"}, new(""), "", nil)
	s.appendEvent(rm, "m.room.guest_access", creator, map[string]any{"guest_access": "can_join"}, new(""), "", nil)
	for _, e := range plan.initial {
		s.appendEvent(rm, e.typ, creator, e.content, new(e.key), "", nil)
	}
	if name, ok := body[keyName]; ok {
		s.appendEvent(rm, "m.room.name", creator, map[string]any{keyName: name}, new(""), "", nil)
	}
	if topic, ok := body["topic"]; ok {
		content := map[string]any{"m.topic": map[string]any{"m.text": []any{map[string]any{"body": topic}}}, "topic": topic}
		s.appendEvent(rm, "m.room.topic", creator, content, new(""), "", nil)
	}
	for _, target := range plan.invite {
		if err := s.doInvite(rm, creator, target); err != nil {
			return nil, err
		}
	}
	return map[string]any{"room_id": rm.id}, nil
}

// authorise applies the room-version-12 auth rules pingbus relies on, in the spec's order.
func (s *Server) authorise(r *room, sender, eventType string, key *string) error {
	if r.membership(sender) != membershipJoin {
		return forbidden("Auth check failed: sender is not joined to the room")
	}
	required, err := r.eventLevel(eventType, key != nil)
	if err != nil {
		return err
	}
	have, err := r.power(sender)
	if err != nil {
		return err
	}
	if have < required {
		return forbidden("Auth check failed: sender does not have enough power (Int(" + formatLevel(have) +
			")) for `" + eventType + "` event type (" + formatLevel(required) + ")")
	}
	if key != nil && len(*key) > 0 && (*key)[0] == '@' && *key != sender {
		return forbidden("Auth check failed: sender cannot send event with `state_key` matching another user's ID")
	}
	return nil
}

// powerAtLeast reports whether sender has the power levels[key] asks, def when absent.
func powerAtLeast(r *room, sender, key string, def float64) (bool, error) {
	have, err := r.power(sender)
	if err != nil {
		return false, err
	}
	need, err := level(r.levels(), key, def)
	if err != nil {
		return false, err
	}
	return have >= need, nil
}

func (s *Server) doInvite(r *room, sender, target string) error {
	if r.membership(sender) != membershipJoin {
		return forbidden("Auth check failed: sender is not joined to the room")
	}
	if m := r.membership(target); m == membershipJoin || m == membershipBan {
		return forbidden("Auth check failed: cannot invite user that is joined or banned")
	}
	allowed, err := powerAtLeast(r, sender, membershipInvite, 0)
	if err != nil {
		return err
	}
	if !allowed {
		return forbidden("Auth check failed: sender does not have enough power to invite")
	}
	if _, ok := s.users[target]; !ok {
		return notFound("User not found.")
	}
	s.appendEvent(r, memberType, sender, s.memberContent(target, membershipInvite), new(target), "", nil)
	return nil
}

func (s *Server) invite(r *request, args []string) (any, error) {
	rm, err := s.findRoom(args[0])
	if err != nil {
		return nil, err
	}
	body, err := r.obj()
	if err != nil {
		return nil, err
	}
	if err := s.doInvite(rm, r.userID, stringAt(body, "user_id")); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func (s *Server) join(r *request, args []string) (any, error) {
	rm, err := s.findRoom(args[0])
	if err != nil {
		return nil, err
	}
	m := rm.membership(r.userID)
	if m != membershipInvite && m != membershipJoin && !s.restrictedAllows(rm, r.userID) {
		return nil, forbidden("You are not invited to this room.")
	}
	if m != membershipJoin {
		s.appendEvent(rm, memberType, r.userID, s.memberContent(r.userID, membershipJoin), new(r.userID), "", nil)
	}
	return map[string]any{"room_id": rm.id}, nil
}

// restrictedAllows is a v12 restricted join rule: a member of a room the rule's allow names may
// join unless banned.
func (s *Server) restrictedAllows(r *room, userID string) bool {
	rules := r.state[stateKey{joinRulesType, ""}]
	if rules == nil || rules.content["join_rule"] != "restricted" || r.membership(userID) == membershipBan {
		return false
	}
	allow, ok := rules.content["allow"].([]any)
	if !ok {
		return false
	}
	for _, item := range allow {
		entry, isObj := item.(map[string]any)
		if !isObj || entry["type"] != "m.room_membership" {
			continue
		}
		if other, found := s.rooms[stringAt(entry, "room_id")]; found && other.membership(userID) == membershipJoin {
			return true
		}
	}
	return false
}

// leave is the spec's POST /leave, modelled for rejecting an invite and leaving; H4 recorded no
// answer for it, so leaving a room the account is neither invited to nor joined is unmodelled.
func (s *Server) leave(r *request, args []string) (any, error) {
	rm, err := s.findRoom(args[0])
	if err != nil {
		return nil, err
	}
	if m := rm.membership(r.userID); m != membershipInvite && m != membershipJoin {
		return nil, unmodelled("leaving a room the account is neither invited to nor joined")
	}
	s.appendEvent(rm, memberType, r.userID, s.memberContent(r.userID, membershipLeave), new(r.userID), "", nil)
	return map[string]any{}, nil
}

// mayKick applies the kick rules: the kicker joined, the target invited or joined, and the
// kicker's power at least the kick level and above the target's.
func mayKick(rm *room, sender, target string) error {
	if rm.membership(sender) != membershipJoin {
		return forbidden("Auth check failed: sender is not joined to the room")
	}
	if m := rm.membership(target); m != membershipJoin && m != membershipInvite {
		return forbidden("Auth check failed: cannot kick a user who is not in the room")
	}
	allowed, err := powerAtLeast(rm, sender, "kick", 50)
	if err != nil {
		return err
	}
	have, err := rm.power(sender)
	if err != nil {
		return err
	}
	theirs, err := rm.power(target)
	if err != nil {
		return err
	}
	if !allowed || have <= theirs {
		return forbidden("Auth check failed: sender does not have enough power to kick")
	}
	return nil
}

func (s *Server) kick(r *request, args []string) (any, error) {
	rm, err := s.findRoom(args[0])
	if err != nil {
		return nil, err
	}
	body, err := r.obj()
	if err != nil {
		return nil, err
	}
	target := stringAt(body, "user_id")
	if err := mayKick(rm, r.userID, target); err != nil {
		return nil, err
	}
	content := map[string]any{"membership": membershipLeave}
	if reason, ok := body["reason"].(string); ok {
		content["reason"] = reason
	}
	s.appendEvent(rm, memberType, r.userID, content, new(target), "", nil)
	return map[string]any{}, nil
}

// txnEvent gives the event a transaction ID made: a transaction ID the same device used before
// in the room gives the first event's ID and sends nothing.
func (s *Server) txnEvent(r *request, rm *room, txn string, send func(*txnRef) (*event, error)) (any, error) {
	key := txnKey{r.userID, r.deviceID, rm.id, txn}
	if _, done := s.txns[key]; !done {
		e, err := send(&txnRef{r.userID, r.deviceID, txn})
		if err != nil {
			return nil, err
		}
		s.txns[key] = e.id
	}
	return map[string]any{keyEventID: s.txns[key]}, nil
}

func (s *Server) send(r *request, args []string) (any, error) {
	rm, err := s.findRoom(args[0])
	if err != nil {
		return nil, err
	}
	content, err := r.obj()
	if err != nil {
		return nil, err
	}
	eventType := args[1]
	return s.txnEvent(r, rm, args[2], func(ref *txnRef) (*event, error) {
		if err := s.authorise(rm, r.userID, eventType, nil); err != nil {
			return nil, err
		}
		return s.appendEvent(rm, eventType, r.userID, content, nil, "", ref), nil
	})
}

func (s *Server) putState(r *request, args []string) (any, error) {
	rm, err := s.findRoom(args[0])
	if err != nil {
		return nil, err
	}
	content, err := r.obj()
	if err != nil {
		return nil, err
	}
	key := new(args[2])
	if err := s.authorise(rm, r.userID, args[1], key); err != nil {
		return nil, err
	}
	return map[string]any{keyEventID: s.appendEvent(rm, args[1], r.userID, content, key, "", nil).id}, nil
}

func (s *Server) getState(r *request, args []string) (any, error) {
	rm, err := s.joinedRoom(args[0], r.userID)
	if err != nil {
		return nil, err
	}
	e := rm.state[stateKey{args[1], args[2]}]
	if e == nil {
		return nil, notFound("Event not found.")
	}
	if r.query.Get("format") == "event" {
		return e.client(true, map[string]any{}), nil
	}
	return e.content, nil
}

func (s *Server) fullState(r *request, args []string) (any, error) {
	rm, err := s.joinedRoom(args[0], r.userID)
	if err != nil {
		return nil, err
	}
	out := []any{}
	for _, e := range rm.events {
		if e.stateKey != nil && rm.state[stateKey{e.typ, *e.stateKey}] == e {
			out = append(out, e.client(true, map[string]any{}))
		}
	}
	return out, nil
}

func findEvent(r *room, id string) (*event, error) {
	for _, e := range r.events {
		if e.id == id {
			return e, nil
		}
	}
	return nil, notFound("Event not found.")
}

func (s *Server) redact(r *request, args []string) (any, error) {
	rm, err := s.findRoom(args[0])
	if err != nil {
		return nil, err
	}
	eventID := args[1]
	return s.txnEvent(r, rm, args[2], func(ref *txnRef) (*event, error) {
		if err := s.authorise(rm, r.userID, redactionType, nil); err != nil {
			return nil, err
		}
		target, err := findEvent(rm, eventID)
		if err != nil {
			return nil, err
		}
		allowed, err := powerAtLeast(rm, r.userID, "redact", 50)
		if err != nil {
			return nil, err
		}
		if target.sender != r.userID && !allowed {
			return nil, forbidden("Auth check failed: sender does not have enough power to redact")
		}
		if target.stateKey != nil {
			return nil, unmodelled("redacting a state event: the spec keeps per-type content keys")
		}
		target.content = map[string]any{}
		content := map[string]any{"redacts": eventID}
		if body, isObject := r.body.(map[string]any); isObject && stringAt(body, "reason") != "" {
			content["reason"] = stringAt(body, "reason")
		}
		return s.appendEvent(rm, redactionType, r.userID, content, nil, "", ref), nil
	})
}

func (s *Server) unsigned(e *event, r *request) map[string]any {
	out := map[string]any{"age": max(0, s.nowMS()-e.ts)}
	if e.txn != nil && e.txn.userID == r.userID && e.txn.deviceID == r.deviceID {
		out["transaction_id"] = e.txn.txn
	}
	return out
}

func (s *Server) getEvent(r *request, args []string) (any, error) {
	rm, err := s.joinedRoom(args[0], r.userID)
	if err != nil {
		return nil, err
	}
	e, err := findEvent(rm, args[1])
	if err != nil {
		return nil, err
	}
	return e.client(true, s.unsigned(e, r)), nil
}

var receiptTypes = []string{"m.read", "m.read.private", "m.fully_read"}

// receipt is the spec's POST /receipt/{type}/{eventId}; a receipt on an event the room does not
// hold is a 404, as an unknown event is everywhere else here.
func (s *Server) receipt(r *request, args []string) (any, error) {
	rm, err := s.joinedRoom(args[0], r.userID)
	if err != nil {
		return nil, err
	}
	if _, err := r.obj(); err != nil {
		return nil, err
	}
	if !slices.Contains(receiptTypes, args[1]) {
		return nil, invalidParam("Unknown receipt type.")
	}
	if _, err := findEvent(rm, args[2]); err != nil {
		return nil, err
	}
	s.receipts = append(s.receipts, Receipt{UserID: r.userID, RoomID: rm.id, Type: args[1], EventID: args[2]})
	return map[string]any{}, nil
}

func (s *Server) joinedRooms(r *request, _ []string) (any, error) {
	out := []any{}
	for _, id := range s.roomOrder {
		if s.rooms[id].membership(r.userID) == membershipJoin {
			out = append(out, id)
		}
	}
	return map[string]any{"joined_rooms": out}, nil
}

func batchToken(value string, present bool, def int) (int, error) {
	if !present {
		return def, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 || strconv.Itoa(n) != value {
		return 0, invalidParam("Invalid pagination token.")
	}
	return n, nil
}

func queryValue(r *request, key string) (string, bool) {
	values, ok := r.query[key]
	if !ok || len(values) == 0 {
		return "", false
	}
	return values[0], true
}

func typeOK(eventType string, types []string) bool {
	return types == nil || slices.Contains(types, eventType)
}

func (s *Server) messagesRange(r *request, direction string) (int, int, error) {
	from, hasFrom := queryValue(r, "from")
	to, hasTo := queryValue(r, "to")
	switch direction {
	case "f":
		low, err := batchToken(from, hasFrom, 0)
		if err != nil {
			return 0, 0, err
		}
		high, err := batchToken(to, hasTo, s.pos)
		return low, high, err
	case "b":
		high, err := batchToken(from, hasFrom, s.pos)
		if err != nil {
			return 0, 0, err
		}
		low, err := batchToken(to, hasTo, 0)
		return low, high, err
	default:
		return 0, 0, invalidParam("dir must be f or b")
	}
}

func messageTypes(r *request) ([]string, error) {
	raw, ok := queryValue(r, "filter")
	if !ok {
		return nil, nil
	}
	var filter struct {
		Types *[]string `json:"types"`
	}
	if err := json.Unmarshal([]byte(raw), &filter); err != nil {
		return nil, invalidParam("filter is not a valid filter")
	}
	if filter.Types == nil {
		return nil, nil
	}
	return *filter.Types, nil
}

func (s *Server) messages(r *request, args []string) (any, error) {
	rm, err := s.joinedRoom(args[0], r.userID)
	if err != nil {
		return nil, err
	}
	types, err := messageTypes(r)
	if err != nil {
		return nil, err
	}
	limit := defaultTimelineLimit
	if raw, ok := queryValue(r, "limit"); ok {
		if limit, err = strconv.Atoi(raw); err != nil {
			return nil, invalidParam("limit must be an integer")
		}
	}
	direction := r.query.Get("dir")
	low, high, err := s.messagesRange(r, direction)
	if err != nil {
		return nil, err
	}
	var picked []*event
	for _, e := range rm.events {
		if low < e.pos && e.pos <= high && typeOK(e.typ, types) {
			picked = append(picked, e)
		}
	}
	if direction == "b" {
		slices.Reverse(picked)
	}
	picked = picked[:min(max(limit, 0), len(picked))]
	chunk := make([]any, 0, len(picked))
	for _, e := range picked {
		chunk = append(chunk, e.client(true, s.unsigned(e, r)))
	}
	start, ok := queryValue(r, "from")
	if !ok {
		start = strconv.Itoa(s.pos)
	}
	out := map[string]any{"chunk": chunk, "start": start}
	if len(picked) > 0 {
		last := picked[len(picked)-1].pos
		if direction == "b" {
			last--
		}
		out["end"] = strconv.Itoa(last)
	}
	return out, nil
}
