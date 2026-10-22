package fakehs

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
)

// rosterType is the team's roster state event, whose restore_epoch a restore raises. The fake
// reads only that one field of it, generically, so it imports nothing of the protocol.
const rosterType = "teamchat.roster"

const keyRestoreEpoch = "restore_epoch"

// database is what a backup of the homeserver holds and a restore puts back: accounts,
// devices, tokens, rooms with their events and state, transaction IDs, receipts, room account
// data, presence and the batch position. The event ID counter is not in it, so an event made
// after a restore never reuses the ID of one the restore took away.
type database struct {
	users       map[string]*user
	devices     []*device
	tokens      map[string]session
	rooms       map[string]*room
	roomOrder   []string
	adminRoom   *room
	txns        map[txnKey]string
	receipts    []Receipt
	accountData map[accountDataKey]json.RawMessage
	presence    map[string]string
	pos         int
}

// Snapshot is a deep copy of the server's database at one moment, for RestoreTo.
type Snapshot struct {
	db *database
}

// Snapshot copies the database as it stands.
func (s *Server) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	db := s.current().clone()
	return Snapshot{db: &db}
}

// RestoreTo rolls the database back to snap and raises the restore_epoch of the roster in
// every room that has one, all under the server's lock in one step (gap 6), so no request
// sees the restored database with the old epoch. The listener and open connections are kept.
// The new epoch is one more than the higher of the snapshot's and the one in use, so a second
// restore to the same snapshot still raises it; the raised roster is a new state event by the
// roster's own sender. A sync held from a position the restore took away waits out its
// timeout and answers the server's own lower next_batch, as Tuwunel 1.9.4 does (H-C14). A
// snapshot with no roster, or a roster without an integer restore_epoch, is the calling
// test's fault: an error, and the database is left as it was (n3c-D1).
func (s *Server) RestoreTo(snap Snapshot) error {
	if snap.db == nil {
		return errors.New("fakehs: restore to the zero Snapshot")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db := snap.db.clone()
	raises, err := s.epochRaises(db)
	if err != nil {
		return err
	}
	s.install(db)
	for _, r := range raises {
		content := maps.Clone(r.roster.content)
		content[keyRestoreEpoch] = json.Number(strconv.FormatInt(r.epoch, 10))
		s.appendEvent(r.room, rosterType, r.roster.sender, content, new(*r.roster.stateKey), "", nil)
	}
	return nil
}

// epochRaise is one roster a restore rewrites, and the epoch it takes.
type epochRaise struct {
	room   *room
	roster *event
	epoch  int64
}

// epochRaises are the rosters of db's rooms, in room order, each with its new epoch.
func (s *Server) epochRaises(db database) ([]epochRaise, error) {
	var raises []epochRaise
	for _, id := range db.roomOrder {
		rm := db.rooms[id]
		keys := slices.SortedFunc(maps.Keys(rm.state), func(a, b stateKey) int {
			return cmp.Or(cmp.Compare(a.typ, b.typ), cmp.Compare(a.key, b.key))
		})
		for _, k := range keys {
			if k.typ != rosterType {
				continue
			}
			epoch, err := epochOf(rm.state[k], id)
			if err != nil {
				return nil, err
			}
			if live, ok := s.rooms[id]; ok && live.state[k] != nil {
				inUse, liveErr := epochOf(live.state[k], id)
				if liveErr != nil {
					return nil, liveErr
				}
				epoch = max(epoch, inUse)
			}
			raises = append(raises, epochRaise{room: rm, roster: rm.state[k], epoch: epoch + 1})
		}
	}
	if len(raises) == 0 {
		return nil, errors.New("fakehs: the snapshot holds no " + rosterType + " to raise the restore_epoch of")
	}
	return raises, nil
}

// epochOf is a roster's restore_epoch, which must be a non-negative integer below the
// largest one that can be raised. A request body's numbers are kept as written (decodeBody),
// so a roster's epoch is a json.Number.
func epochOf(e *event, roomID string) (int64, error) {
	number, isNumber := e.content[keyRestoreEpoch].(json.Number)
	epoch, err := number.Int64()
	if !isNumber || err != nil || epoch < 0 || epoch == math.MaxInt64 {
		return 0, fmt.Errorf("fakehs: the %s of %s has no integer %s", rosterType, roomID, keyRestoreEpoch)
	}
	return epoch, nil
}

func (s *Server) current() database {
	return database{
		users: s.users, devices: s.devices, tokens: s.tokens, rooms: s.rooms, roomOrder: s.roomOrder,
		adminRoom: s.adminRoom, txns: s.txns, receipts: s.receipts, accountData: s.accountData,
		presence: s.presence, pos: s.pos,
	}
}

func (s *Server) install(db database) {
	s.users, s.devices, s.tokens, s.rooms, s.roomOrder = db.users, db.devices, db.tokens, db.rooms, db.roomOrder
	s.adminRoom, s.txns, s.receipts, s.accountData = db.adminRoom, db.txns, db.receipts, db.accountData
	s.presence, s.pos = db.presence, db.pos
}

// clone is a deep copy: nothing in it is shared with db.
func (db database) clone() database {
	out := database{
		users:       make(map[string]*user, len(db.users)),
		devices:     make([]*device, 0, len(db.devices)),
		tokens:      maps.Clone(db.tokens),
		rooms:       make(map[string]*room, len(db.rooms)),
		roomOrder:   slices.Clone(db.roomOrder),
		txns:        maps.Clone(db.txns),
		receipts:    slices.Clone(db.receipts),
		accountData: make(map[accountDataKey]json.RawMessage, len(db.accountData)),
		presence:    maps.Clone(db.presence),
		pos:         db.pos,
	}
	for id, u := range db.users {
		c := *u
		c.lastSeen = cloneInt(u.lastSeen)
		out.users[id] = &c
	}
	for _, d := range db.devices {
		c := *d
		c.lastSeen = cloneInt(d.lastSeen)
		out.devices = append(out.devices, &c)
	}
	for id, rm := range db.rooms {
		out.rooms[id] = rm.clone()
	}
	if db.adminRoom != nil {
		out.adminRoom = out.rooms[db.adminRoom.id]
	}
	for k, raw := range db.accountData {
		out.accountData[k] = slices.Clone(raw)
	}
	return out
}

func cloneInt(p *int64) *int64 {
	if p == nil {
		return nil
	}
	return new(*p)
}

// clone copies a room's events, its state and the redactions between them, pointing into the
// copy.
func (r *room) clone() *room {
	out := &room{id: r.id, creator: r.creator, events: make([]*event, 0, len(r.events)), state: make(map[stateKey]*event, len(r.state))}
	copies := make(map[*event]*event, len(r.events))
	for _, e := range r.events {
		c := *e
		c.content = cloneObject(e.content)
		if e.stateKey != nil {
			c.stateKey = new(*e.stateKey)
		}
		if e.txn != nil {
			c.txn = new(*e.txn)
		}
		copies[e] = &c
		out.events = append(out.events, &c)
	}
	for _, c := range out.events {
		if c.redactedBy != nil {
			c.redactedBy = copies[c.redactedBy]
		}
	}
	for k, e := range r.state {
		out.state[k] = copies[e]
	}
	return out
}

func cloneObject(obj map[string]any) map[string]any {
	out := make(map[string]any, len(obj))
	for k, item := range obj {
		out[k] = cloneJSON(item)
	}
	return out
}

// cloneJSON copies a decoded JSON value: objects and arrays anew, scalars as they are.
func cloneJSON(v any) any {
	switch v := v.(type) {
	case map[string]any:
		return cloneObject(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneJSON(item)
		}
		return out
	}
	return v
}
