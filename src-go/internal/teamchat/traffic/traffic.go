// Package traffic is the read-only traffic probe of a team's rooms (Plan 00268 Task 1.3): over
// a window of at most a week, each room's events counted by hour (UTC), event type, typed
// teamchat key, relation (thread, reply, edit) and membership change. It reads each room's
// history backwards through /messages as the team's admin, a bounded number of pages, and
// keeps no content: an event's body is parsed in the reading process and dropped there, and
// only names of a fixed form and counts leave it. Read receipts, typing and presence are
// ephemeral (not in room history), so they cannot be counted here; a `teamchat.receipt` event,
// which is in history, is counted by its typed key.
package traffic

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The probe's bounds.
const (
	// HourMS is one bucket, an hour in milliseconds.
	HourMS = int64(time.Hour / time.Millisecond)
	// MaxWindow is the longest window read.
	MaxWindow = 7 * 24 * time.Hour
	// DefaultPage is the events asked for per /messages page.
	DefaultPage = 500
	// DefaultMaxPages is the pages read per room before the read gives up.
	DefaultMaxPages = 1000
	// MaxRows is the most distinct rows an answer holds.
	MaxRows = 4096
)

// The catch-all names: an event type of another form, an unknown or second typed key, an
// unknown relation or membership. Brackets are outside every form a name may take, so a
// catch-all is never a real name.
const (
	Other   = "(other)"
	Several = "(several)"
)

// The relations counted.
const (
	RelThread = "thread"
	RelReply  = "reply"
	RelEdit   = "edit"
)

const memberType = "m.room.member"

// typeForm is the form an event type is counted under; any other type counts as Other.
var typeForm = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

var (
	// typedKeys are the typed keys of A 7.1 and 7.7 (proto's).
	typedKeys   = []string{proto.KeyPing, proto.KeyText, proto.KeyNotice, proto.KeyReport, proto.KeyQuestion, proto.KeyThread, proto.KeyCarried, proto.KeyReceipt}
	rels        = []string{"", RelThread, RelReply, RelEdit, Other}
	memberships = []string{"join", "leave", "invite", "ban", "knock", Other}
	purposes    = []string{proto.PurposeMain, proto.PurposeReports, proto.PurposeQuestions}
)

// Window is the time read, [SinceMS, UntilMS) in milliseconds.
type Window struct {
	SinceMS, UntilMS int64
}

// NewWindow is the window from since to until, which must be after it by at most MaxWindow.
func NewWindow(since, until time.Time) (Window, error) {
	w := Window{SinceMS: since.UnixMilli(), UntilMS: until.UnixMilli()}
	return w, w.Valid()
}

// Valid is the window's rule: since at 0 or after, until after it by at most MaxWindow.
func (w Window) Valid() error {
	span := w.UntilMS - w.SinceMS
	if w.SinceMS < 0 || span < 1 || span > MaxWindow.Milliseconds() {
		return fmt.Errorf("the window is not a start before its end by at most %s", MaxWindow)
	}
	return nil
}

// firstHour is the start of the hour since falls in.
func (w Window) firstHour() int64 { return w.SinceMS - w.SinceMS%HourMS }

// Room is one of the team's rooms: its purpose and its ID.
type Room struct {
	Purpose, ID string
}

// Key is what an event is counted by. Membership is set for a membership event only.
type Key struct {
	Room       string
	HourMS     int64
	Type       string
	Typed      string
	Rel        string
	Membership string
}

// Row is a key and the events counted under it, at least one.
type Row struct {
	Key

	Count int64
}

// RoomRead is one room's read: the events counted in the window and the pages read.
type RoomRead struct {
	Room          string
	Events, Pages int64
}

// Counts is a probe's answer: the window, each room's read in the order read, and the rows in
// room, hour and name order.
type Counts struct {
	Window Window
	Rooms  []RoomRead
	Rows   []Row
}

// Reader reads a page of a room's events; *matrix.Admin is one.
type Reader interface {
	Messages(ctx context.Context, roomID string, req matrix.MessagesRequest) (matrix.MessagesPage, error)
}

// Options are the read's page size and page bound; zero is the default.
type Options struct {
	Page, MaxPages int
}

// Read counts each room's events in the window, each read backwards from its newest event
// until a page holds one older than the window's start or the history ends. A room that reaches
// neither within MaxPages fails the read: a part is never answered as the whole.
func Read(ctx context.Context, r Reader, rooms []Room, w Window, o Options) (Counts, error) {
	if err := w.Valid(); err != nil {
		return Counts{}, err
	}
	page, most := cmp.Or(o.Page, DefaultPage), cmp.Or(o.MaxPages, DefaultMaxPages)
	counts := map[Key]int64{}
	c := Counts{Window: w}
	for _, room := range rooms {
		read, err := readRoom(ctx, r, room, w, page, most, counts)
		if err != nil {
			return Counts{}, fmt.Errorf("room %s: %w", room.Purpose, err)
		}
		c.Rooms = append(c.Rooms, read)
	}
	if len(counts) > MaxRows {
		return Counts{}, fmt.Errorf("the window holds %d distinct rows, over %d: narrow the window", len(counts), MaxRows)
	}
	for k, n := range counts {
		c.Rows = append(c.Rows, Row{Key: k, Count: n})
	}
	c.sortRows()
	return c, nil
}

func readRoom(ctx context.Context, r Reader, room Room, w Window, page, most int, counts map[Key]int64) (RoomRead, error) {
	read := RoomRead{Room: room.Purpose}
	from := ""
	for {
		if read.Pages >= int64(most) {
			return RoomRead{}, fmt.Errorf("%d pages read (%d events in the window) without reaching the window's start: narrow the window", read.Pages, read.Events)
		}
		p, err := r.Messages(ctx, room.ID, matrix.MessagesRequest{From: from, Dir: matrix.Backward, Limit: page})
		if err != nil {
			return RoomRead{}, err
		}
		read.Pages++
		older := false
		for _, raw := range p.Chunk {
			ts, k, err := classify(raw)
			switch {
			case err != nil:
				return RoomRead{}, err
			case ts < w.SinceMS:
				older = true
			case ts < w.UntilMS:
				k.Room, k.HourMS = room.Purpose, ts-ts%HourMS
				counts[k]++
				read.Events++
			}
		}
		if older || p.End == "" || len(p.Chunk) == 0 {
			return read, nil
		}
		from = p.End
	}
}

// event is the fields of an event the probe reads; the content's values other than the
// relation and the membership are never decoded.
type event struct {
	Type    string                     `json:"type"`
	TS      *int64                     `json:"origin_server_ts"`
	Content map[string]json.RawMessage `json:"content"`
}

type relatesTo struct {
	RelType   *string         `json:"rel_type"`
	InReplyTo json.RawMessage `json:"m.in_reply_to"`
}

// classify is an event's time and its key's names.
func classify(raw []byte) (int64, Key, error) {
	var e event
	if err := json.Unmarshal(raw, &e); err != nil {
		return 0, Key{}, fmt.Errorf("an event is not JSON: %w", err)
	}
	if e.TS == nil || *e.TS < 0 {
		return 0, Key{}, errors.New("an event has no origin_server_ts")
	}
	k := Key{Type: Other, Typed: typedOf(e.Content), Rel: relOf(e.Content)}
	if typeForm.MatchString(e.Type) {
		k.Type = e.Type
	}
	if k.Type == memberType {
		k.Membership = membershipOf(e.Content)
	}
	return *e.TS, k, nil
}

func typedOf(content map[string]json.RawMessage) string {
	found := ""
	for key := range content {
		switch {
		case slices.Contains(typedKeys, key) && found == "":
			found = key
		case slices.Contains(typedKeys, key):
			return Several
		}
	}
	return found
}

func relOf(content map[string]json.RawMessage) string {
	raw, given := content["m.relates_to"]
	if !given {
		return ""
	}
	var rel relatesTo
	if err := json.Unmarshal(raw, &rel); err != nil {
		return Other
	}
	switch {
	case rel.RelType == nil && rel.InReplyTo != nil:
		return RelReply
	case rel.RelType == nil:
		return Other
	case *rel.RelType == "m.thread":
		return RelThread
	case *rel.RelType == "m.replace":
		return RelEdit
	}
	return Other
}

func membershipOf(content map[string]json.RawMessage) string {
	var m string
	if err := json.Unmarshal(content["membership"], &m); err != nil || !slices.Contains(memberships, m) {
		return Other
	}
	return m
}

func (c *Counts) sortRows() {
	order := func(room string) int {
		return slices.IndexFunc(c.Rooms, func(r RoomRead) bool { return r.Room == room })
	}
	slices.SortFunc(c.Rows, func(a, b Row) int {
		return cmp.Or(cmp.Compare(order(a.Room), order(b.Room)), cmp.Compare(a.HourMS, b.HourMS), cmp.Compare(a.Type, b.Type),
			cmp.Compare(a.Typed, b.Typed), cmp.Compare(a.Rel, b.Rel), cmp.Compare(a.Membership, b.Membership))
	})
}

// Validate is the rule every answer keeps, which root checks of the child's: a valid window,
// distinct rooms of the team's purposes each read at least one page, at most MaxRows distinct
// rows each in a room read and an hour of the window, every name of its form, and each room's
// events the sum of its rows.
func (c Counts) Validate() error {
	if err := c.Window.Valid(); err != nil {
		return err
	}
	events := map[string]int64{}
	for _, r := range c.Rooms {
		if _, seen := events[r.Room]; seen || !slices.Contains(purposes, r.Room) || r.Pages < 1 || r.Events < 0 {
			return fmt.Errorf("room %q is not a distinct purpose read at least one page", r.Room)
		}
		events[r.Room] = r.Events
	}
	if len(c.Rows) > MaxRows {
		return fmt.Errorf("more than %d rows", MaxRows)
	}
	seen := map[Key]bool{}
	for _, r := range c.Rows {
		if err := c.validRow(r, seen); err != nil {
			return err
		}
		events[r.Room] -= r.Count
	}
	for room, left := range events {
		if left != 0 {
			return fmt.Errorf("room %s's events are not the sum of its rows", room)
		}
	}
	return nil
}

func (c Counts) validRow(r Row, seen map[Key]bool) error {
	_, read := slices.BinarySearch(sortedRooms(c.Rooms), r.Room)
	switch {
	case !read || seen[r.Key] || r.Count < 1:
		return fmt.Errorf("a row of room %q is not a distinct count of a room read", r.Room)
	case r.HourMS%HourMS != 0 || r.HourMS < c.Window.firstHour() || r.HourMS >= c.Window.UntilMS:
		return errors.New("a row's hour is not an hour of the window")
	case r.Type != Other && !typeForm.MatchString(r.Type):
		return errors.New("a row's type is not of the type form")
	case r.Typed != "" && r.Typed != Several && !slices.Contains(typedKeys, r.Typed):
		return errors.New("a row's typed key is not a teamchat typed key")
	case !slices.Contains(rels, r.Rel):
		return errors.New("a row's relation is not one counted")
	case (r.Type == memberType) != (r.Membership != "") || r.Membership != "" && !slices.Contains(memberships, r.Membership):
		return errors.New("a row's membership is not a membership event's")
	}
	seen[r.Key] = true
	return nil
}

func sortedRooms(rooms []RoomRead) []string {
	out := make([]string, 0, len(rooms))
	for _, r := range rooms {
		out = append(out, r.Room)
	}
	slices.Sort(out)
	return out
}
