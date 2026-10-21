package matrix

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const maxSyncTimeout = 30 * time.Second

// RoomFilter is an inline sync filter that asks for rooms and nothing else: presence and
// account data empty, ephemeral and room account data empty (SPEC M21). A nil list is not
// sent (every room, every type); an empty one asks for none. A zero TimelineLimit is not sent.
type RoomFilter struct {
	Rooms           []string
	StateTypes      []string
	LazyLoadMembers bool
	TimelineTypes   []string
	TimelineLimit   int
}

// JSON is the filter as sent: compact, keys sorted.
func (f RoomFilter) JSON() string {
	none := map[string]any{"types": []string{}}
	state := map[string]any{"lazy_load_members": f.LazyLoadMembers}
	if f.StateTypes != nil {
		state["types"] = f.StateTypes
	}
	timeline := map[string]any{}
	if f.TimelineTypes != nil {
		timeline["types"] = f.TimelineTypes
	}
	if f.TimelineLimit != 0 {
		timeline["limit"] = f.TimelineLimit
	}
	rooms := map[string]any{"account_data": none, "ephemeral": none, "state": state, "timeline": timeline}
	if f.Rooms != nil {
		rooms["rooms"] = f.Rooms
	}
	return string(jsonBody(map[string]any{"account_data": none, "presence": none, "room": rooms}))
}

// SyncRequest is one /sync: Since empty for a first sync, Timeout the long-poll (0 to 30 s,
// whole milliseconds), Filter nil for none, SetPresence empty for the server's default.
type SyncRequest struct {
	Since       string
	Timeout     time.Duration
	Filter      *RoomFilter
	SetPresence string
}

// presenceOK says p is a presence the specification names.
func presenceOK(p string) bool { return p == "online" || p == "offline" || p == "unavailable" }

const detailBadPresence = "a presence is online, offline or unavailable"

// SetPresence sets the account's presence (SPEC M50): a link sets offline at a clean end.
func (c *Client) SetPresence(ctx context.Context, userID, presence string) error {
	if !presenceOK(presence) {
		return invalid(detailBadPresence)
	}
	path, err := pathf(clientPrefix+"/presence/%s/status", userID)
	if err != nil {
		return err
	}
	return c.empty(ctx, call{method: http.MethodPut, path: path, body: jsonBody(map[string]string{"presence": presence})})
}

// SyncResponse is a sync answer: the batch to sync from next, and per room the joined, invited
// and left rooms' updates. Events are raw JSON objects, for the caller to judge.
type SyncResponse struct {
	NextBatch string
	Join      map[string]RoomUpdate
	Invite    map[string]InvitedRoom
	Leave     map[string]RoomUpdate
}

// RoomUpdate is a joined or left room's state and timeline.
type RoomUpdate struct {
	State    []json.RawMessage
	Timeline Timeline
}

// Timeline is a room's new events; when Limited, events were left out and PrevBatch reads them.
type Timeline struct {
	Events    []json.RawMessage
	Limited   bool
	PrevBatch string
}

// InvitedRoom is the stripped state an invite carries.
type InvitedRoom struct {
	InviteState []json.RawMessage
}

type wireEvents struct {
	Events []json.RawMessage `json:"events"`
}

type wireRoom struct {
	State    *wireEvents `json:"state"`
	Timeline *struct {
		Events    []json.RawMessage `json:"events"`
		Limited   *bool             `json:"limited"`
		PrevBatch *string           `json:"prev_batch"`
	} `json:"timeline"`
}

type wireSync struct {
	NextBatch string `json:"next_batch"`
	Rooms     *struct {
		Join   map[string]wireRoom `json:"join"`
		Invite map[string]struct {
			InviteState *wireEvents `json:"invite_state"`
		} `json:"invite"`
		Leave map[string]wireRoom `json:"leave"`
	} `json:"rooms"`
}

// Sync is GET /sync (SPEC M20). Its time limit is the long-poll plus 15 s.
func (c *Client) Sync(ctx context.Context, req SyncRequest) (SyncResponse, error) {
	cl, err := c.syncCall(req)
	if err != nil {
		return SyncResponse{}, err
	}
	var out wireSync
	if err := c.object(ctx, cl, &out); err != nil {
		return SyncResponse{}, err
	}
	resp, ok := out.response()
	if !ok {
		return SyncResponse{}, cl.malformed()
	}
	return resp, nil
}

func (c *Client) syncCall(req SyncRequest) (call, error) {
	if req.Timeout < 0 || req.Timeout > maxSyncTimeout || req.Timeout%time.Millisecond != 0 {
		return call{}, invalid("a sync timeout is 0 to 30000 whole milliseconds")
	}
	query := url.Values{"timeout": {strconv.FormatInt(req.Timeout.Milliseconds(), 10)}}
	if req.Since != "" {
		if !isToken(req.Since) {
			return call{}, invalid("a batch token is not printable ASCII")
		}
		query.Set("since", req.Since)
	}
	if req.Filter != nil {
		query.Set("filter", req.Filter.JSON())
	}
	if req.SetPresence != "" {
		if !presenceOK(req.SetPresence) {
			return call{}, invalid(detailBadPresence)
		}
		query.Set("set_presence", req.SetPresence)
	}
	return call{method: http.MethodGet, path: clientPrefix + "/sync", query: query, limit: req.Timeout + syncGrace}, nil
}

func (w wireSync) response() (SyncResponse, bool) {
	resp := SyncResponse{NextBatch: w.NextBatch}
	if !isToken(w.NextBatch) {
		return resp, false
	}
	if w.Rooms == nil {
		return resp, true
	}
	var ok bool
	if resp.Join, ok = updates(w.Rooms.Join); !ok {
		return resp, false
	}
	if resp.Leave, ok = updates(w.Rooms.Leave); !ok {
		return resp, false
	}
	resp.Invite = make(map[string]InvitedRoom, len(w.Rooms.Invite))
	for id, room := range w.Rooms.Invite {
		var events []json.RawMessage
		if room.InviteState != nil {
			events = room.InviteState.Events
		}
		if !roomIDRE.MatchString(id) || !allObjects(events) {
			return resp, false
		}
		resp.Invite[id] = InvitedRoom{InviteState: events}
	}
	return resp, true
}

func updates(rooms map[string]wireRoom) (map[string]RoomUpdate, bool) {
	out := make(map[string]RoomUpdate, len(rooms))
	for id, room := range rooms {
		if !roomIDRE.MatchString(id) {
			return nil, false
		}
		update, ok := room.update()
		if !ok {
			return nil, false
		}
		out[id] = update
	}
	return out, true
}

func (w wireRoom) update() (RoomUpdate, bool) {
	var out RoomUpdate
	if w.State != nil {
		out.State = w.State.Events
	}
	if tl := w.Timeline; tl != nil {
		out.Timeline.Events = tl.Events
		out.Timeline.Limited = tl.Limited != nil && *tl.Limited
		if tl.PrevBatch != nil {
			if !isToken(*tl.PrevBatch) {
				return out, false
			}
			out.Timeline.PrevBatch = *tl.PrevBatch
		}
	}
	if out.Timeline.Limited && out.Timeline.PrevBatch == "" {
		return out, false
	}
	return out, allObjects(out.State) && allObjects(out.Timeline.Events)
}
