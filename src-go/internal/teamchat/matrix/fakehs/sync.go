package fakehs

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
)

// inviteStateTypes is the stripped state an invitee sees (Tuwunel's room_invite_state, as H4
// recorded it).
var inviteStateTypes = []string{createType, joinRulesType, "m.room.name", "m.room.topic"}

const heroesShown = 5

// filterSpec is the part of an inline filter the model reads.
type filterSpec struct {
	rooms         []string
	timelineTypes []string
	timelineLimit int
	stateTypes    []string
	lazyMembers   bool
}

type filterJSON struct {
	Room struct {
		Rooms    *[]string `json:"rooms"`
		Timeline struct {
			Types *[]string    `json:"types"`
			Limit *json.Number `json:"limit"`
		} `json:"timeline"`
		State struct {
			Types       *[]string `json:"types"`
			LazyMembers bool      `json:"lazy_load_members"`
		} `json:"state"`
	} `json:"room"`
}

func parseFilter(raw string, present bool) (filterSpec, error) {
	flt := filterSpec{timelineLimit: defaultTimelineLimit}
	if !present {
		return flt, nil
	}
	if !strings.HasPrefix(raw, "{") {
		return flt, unmodelled("stored filter IDs; the fake takes inline JSON filters only")
	}
	var parsed filterJSON
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.UseNumber()
	if err := dec.Decode(&parsed); err != nil {
		return flt, invalidParam("filter is not a valid filter")
	}
	room := parsed.Room
	if room.Rooms != nil {
		flt.rooms = *room.Rooms
	}
	if room.Timeline.Types != nil {
		flt.timelineTypes = *room.Timeline.Types
	}
	if room.Timeline.Limit != nil {
		limit, err := strconv.Atoi(room.Timeline.Limit.String())
		if err != nil {
			return flt, invalidParam("filter timeline limit is not an integer")
		}
		flt.timelineLimit = limit
	}
	if room.State.Types != nil {
		flt.stateTypes = *room.State.Types
	}
	flt.lazyMembers = room.State.LazyMembers
	return flt, nil
}

// sync is GET /sync: with a since token and a timeout it is held until an event for the caller
// lands, the timeout passes or the caller goes. It runs with the server's lock held and gives it
// up only while it waits.
func (s *Server) sync(r *request, _ []string) (any, error) {
	rawSince, hasSince := queryValue(r, "since")
	since, err := batchToken(rawSince, hasSince, -1)
	if err != nil {
		return nil, err
	}
	rawFilter, hasFilter := queryValue(r, "filter")
	flt, err := parseFilter(rawFilter, hasFilter)
	if err != nil {
		return nil, err
	}
	timeoutMS := 0
	if raw, ok := queryValue(r, "timeout"); ok {
		if timeoutMS, err = strconv.Atoi(raw); err != nil || timeoutMS < 0 {
			return nil, invalidParam("timeout must be a non-negative integer")
		}
	}
	if err := s.syncPresence(r); err != nil {
		return nil, err
	}
	timer := time.NewTimer(time.Duration(timeoutMS) * time.Millisecond)
	defer timer.Stop()
	for {
		answer := s.syncOnce(r, since, flt)
		if _, hasRooms := answer["rooms"]; hasRooms || since < 0 || timeoutMS == 0 {
			return answer, nil
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-changed:
			s.mu.Lock()
		case <-timer.C:
			s.mu.Lock()
			return s.syncOnce(r, since, flt), nil
		case <-r.done:
			s.mu.Lock()
			return s.syncOnce(r, since, flt), nil
		}
	}
}

func (s *Server) syncOnce(r *request, since int, flt filterSpec) map[string]any {
	joined := map[string]any{}
	invited := map[string]any{}
	for _, id := range s.roomOrder {
		rm := s.rooms[id]
		member := rm.state[stateKey{memberType, r.userID}]
		if member == nil || (flt.rooms != nil && !slices.Contains(flt.rooms, id)) {
			continue
		}
		switch stringAt(member.content, keyMembership) {
		case membershipInvite:
			if since < 0 || member.pos > since {
				invited[id] = map[string]any{"invite_state": map[string]any{keyEvents: inviteState(rm, member)}}
			}
		case membershipJoin:
			if entry := s.joinedEntry(rm, r, member, since, flt); entry != nil {
				joined[id] = entry
			}
		}
	}
	answer := map[string]any{
		"device_one_time_keys_count":       map[string]any{"signed_curve25519": 0},
		"device_unused_fallback_key_types": []any{},
		"next_batch":                       strconv.Itoa(s.pos),
	}
	rooms := map[string]any{}
	if len(joined) > 0 {
		rooms["join"] = joined
	}
	if len(invited) > 0 {
		rooms["invite"] = invited
	}
	if len(rooms) > 0 {
		answer["rooms"] = rooms
	}
	return answer
}

func inviteState(rm *room, invite *event) []any {
	var events []*event
	for _, typ := range inviteStateTypes {
		if e := rm.state[stateKey{typ, ""}]; e != nil {
			events = append(events, e)
		}
	}
	if inviter := rm.state[stateKey{memberType, invite.sender}]; inviter != nil {
		events = append(events, inviter)
	}
	events = append(events, invite)
	out := make([]any, 0, len(events))
	for _, e := range events {
		out = append(out, e.stripped())
	}
	return out
}

// window is the timeline a joined room's sync entry shows, and whether it is limited.
func (s *Server) window(rm *room, since int, newly bool, flt filterSpec) ([]*event, int, bool) {
	var candidates []*event
	for _, e := range rm.events {
		if (since < 0 || e.pos > since) && typeOK(e.typ, flt.timelineTypes) {
			candidates = append(candidates, e)
		}
	}
	var timeline []*event
	if flt.timelineLimit > 0 {
		timeline = candidates[max(0, len(candidates)-flt.timelineLimit):]
	}
	start := s.pos + 1
	if len(timeline) > 0 {
		start = timeline[0].pos
	}
	limited := len(candidates) > len(timeline) || (newly && since >= 0 && rm.events[0].pos <= since)
	return timeline, start, limited
}

func entryState(rm *room, timeline []*event, start, since int, newly bool, flt filterSpec) map[stateKey]*event {
	atStart := rm.stateBefore(start)
	state := map[stateKey]*event{}
	for k, e := range atStart {
		if (newly || e.pos > since) && typeOK(k.typ, flt.stateTypes) {
			state[k] = e
		}
	}
	if !flt.lazyMembers {
		return state
	}
	senders := map[string]bool{}
	for _, e := range timeline {
		senders[e.sender] = true
	}
	for k := range state {
		if k.typ == memberType && !senders[k.key] {
			delete(state, k)
		}
	}
	for sender := range senders {
		k := stateKey{memberType, sender}
		if e, ok := atStart[k]; ok && typeOK(memberType, flt.stateTypes) {
			state[k] = e
		}
	}
	return state
}

func (s *Server) joinedEntry(rm *room, r *request, member *event, since int, flt filterSpec) map[string]any {
	newly := since < 0 || member.pos > since
	timeline, start, limited := s.window(rm, since, newly, flt)
	state := entryState(rm, timeline, start, since, newly, flt)
	if !newly && len(timeline) == 0 && len(state) == 0 {
		return nil
	}
	timelineEvents := make([]any, 0, len(timeline))
	for _, e := range timeline {
		timelineEvents = append(timelineEvents, e.client(false, s.unsigned(e, r)))
	}
	byPos := slices.SortedFunc(maps.Values(state), func(a, b *event) int { return a.pos - b.pos })
	stateEvents := make([]any, 0, len(byPos))
	for _, e := range byPos {
		stateEvents = append(stateEvents, e.client(false, nil))
	}
	outTimeline := map[string]any{keyEvents: timelineEvents}
	// Tuwunel 1.9.4 gives a timeline limit of 0 no prev_batch (matrix's tagged test).
	if flt.timelineLimit != 0 {
		outTimeline["prev_batch"] = strconv.Itoa(start - 1)
	}
	if limited {
		outTimeline["limited"] = true
	}
	return map[string]any{
		"account_data":         map[string]any{keyEvents: []any{}},
		"ephemeral":            map[string]any{keyEvents: []any{}},
		"state":                map[string]any{keyEvents: stateEvents},
		"summary":              summary(rm, r.userID),
		"timeline":             outTimeline,
		"unread_notifications": map[string]any{"highlight_count": 0, "notification_count": 0},
	}
}

func summary(rm *room, self string) map[string]any {
	heroes := []string{}
	joined, invited := 0, 0
	for k, e := range rm.state {
		if k.typ != memberType {
			continue
		}
		switch stringAt(e.content, keyMembership) {
		case membershipJoin:
			joined++
		case membershipInvite:
			invited++
		default:
			continue
		}
		if k.key != self {
			heroes = append(heroes, k.key)
		}
	}
	slices.Sort(heroes)
	return map[string]any{
		"m.heroes":               heroes[:min(heroesShown, len(heroes))],
		"m.invited_member_count": invited,
		"m.joined_member_count":  joined,
	}
}
