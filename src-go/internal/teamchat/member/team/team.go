// Package team is the seams between the link's three halves (Plan 00209 SPEC-n3, n3c): the
// engine, the link's state goroutine, which makes no network call (B 2.4.3); a team goroutine,
// which owns every homeserver request of its team (the sync engine); and the ends, which post
// a run's lifecycle notices on a team goroutine's client (EN1-EN5). Each half sees the others
// only through these interfaces, so each is tested against fakes of the rest.
package team

import (
	"context"
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/notice"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// Batch is one judged sync answer, after SY6's check (SY5): what the engine persists in one
// store write.
type Batch struct {
	Team         paths.TeamID
	NextBatch    string
	RestoreEpoch int64
	// Fresh is false once SY1's step 01 has persisted.
	Fresh bool
	Items []Item
	// Floors are the floor_ms this batch moved, keyed by room purpose (proto.PurposeMain,
	// PurposeQuestions), as the store's floor_ms and Pending.Room are; never a room id.
	Floors map[string]int64
	// Answered is n3d's; n3c passes nil.
	Answered []proto.Answered
	// Redacts are admin redactions of pending or handed items.
	Redacts []string
	// Gap is SY3's bound hit.
	Gap    bool
	Counts Counts
	// HeldBack is B 2.7.3's delivery budget: agent sender user id -> the items over its
	// budget in this batch, which never entered pending (n3e-D1).
	HeldBack map[string]int64
}

// Item is one judged event: its pending entry, with its content held in memory (B 2.7.3
// "Content"), and its verdict.
type Item struct {
	Pending state.Pending
	Verdict proto.Verdict
}

// Counts are a batch's counters (UH1), counted once per event.
type Counts struct {
	Ignored, Dropped, Unheard, Gaps, Redacted int64
}

// View is what Judge's Lookup reads, from the engine's memory (gap 14).
type View struct {
	// SeenIDs are the ids in pending or the ledger.
	SeenIDs map[string]bool
	// Events are ledger and outbox entries, filled by SY4a's rule.
	Events   map[string]proto.Related
	OwnPings map[string]proto.OwnPing
}

// Seen is whether id is in pending or the ledger.
func (v View) Seen(id string) bool { return v.SeenIDs[id] }

// Event is what the store knows of the earlier event id.
func (v View) Event(id string) (proto.Related, bool) {
	r, ok := v.Events[id]
	return r, ok
}

// OwnPing is this member's own ping id.
func (v View) OwnPing(id string) (proto.OwnPing, bool) {
	p, ok := v.OwnPings[id]
	return p, ok
}

// Health is a team goroutine's state, for status, status.json and report.json.
type Health struct {
	Down bool
	// Stopped is "" while the team runs, else the exits code that stopped it (token,
	// untrusted, store, ...; SY8).
	Stopped string
	Role    string
	// LastSyncOKMS is the wall time of the last sync answered, 0 before the first: an empty
	// answer is never persisted, so the store cannot hold it.
	LastSyncOKMS int64
	// Colour, Description and Coordinator are the main room's facts at the last verification,
	// nil for none: the admin's colour override (stored hex, n3e-D4), the admin's topic when it
	// is a team description (n3e-D5), and the roster's non-parked coordinator's handle. A hook
	// op reads them here and never from Roster(), which may make a network call.
	Colour      *string
	Description *string
	Coordinator *string
}

// ErrRemoved is a write for a team the engine has removed.
var ErrRemoved = errors.New("team: the team was removed")

// ErrItemGone is a Refetch whose event the server will never serve again (404, 403, a room the
// member does not read): the engine drops the restored item with its reason (B 2.7.3), where a
// transient failure keeps it pending.
var ErrItemGone = errors.New("team: the item can no longer be fetched")

// Engine is the link's state goroutine as a team goroutine and the ends see it.
type Engine interface {
	// Load is the team's store as loaded, or created for userID (the pair's user id); a copy.
	Load(t paths.TeamID, userID string) (state.Store, error)
	// View is what Judge needs of ids and relations; r is the roster an outbox entry's
	// AddressedToRosterAgent reads.
	View(t paths.TeamID, ids, relations []string, r proto.Roster) (View, error)
	// Persist is one store write of b, then a wake of delivery; ErrRemoved after a remove. It
	// answers the main room's floor the store then keeps (B 2.3.2), which the marker carries.
	Persist(ctx context.Context, b Batch) (mainFloor int64, err error)
	// ResetPosition is SY6: drop next_batch and store the new epoch; fresh stays as it is.
	ResetPosition(ctx context.Context, t paths.TeamID, epoch int64) error
	// SetEnded records the store's run's end (EN1, EN3).
	SetEnded(ctx context.Context, t paths.TeamID, e state.RunEnd) error
	// BeginRun makes the store's run this link's run, with ended null; a no-op when it is
	// already. The team calls it after Ends.Close returns nil.
	BeginRun(ctx context.Context, t paths.TeamID) error
	SetHealth(t paths.TeamID, h Health)
}

// Receipts is the engine's side of B 2.7.5's read receipts, as a team goroutine sends them.
type Receipts interface {
	// PendingReceipts is the team's receipts not yet sent, and a channel closed when they
	// next change.
	PendingReceipts(t paths.TeamID) (rs []state.Receipt, changed <-chan struct{})
	// ReceiptSent drops the receipt for id once the homeserver has it.
	ReceiptSent(ctx context.Context, t paths.TeamID, id string) error
}

// Team is one team goroutine, as the engine and the link see it.
type Team interface {
	// Run runs until ctx ends or a permanent stop.
	Run(ctx context.Context) error
	// Refetch judges a restored item again (B 2.7.3); ErrItemGone when it never can be.
	Refetch(ctx context.Context, room, id string) (proto.Verdict, error)
	// Verify is LN6.
	Verify(ctx context.Context, id string) (records.Verified, error)
	History(ctx context.Context, room string, limit int) ([]records.History, error)
	// Notices is for the ends; it works before Run and after it returns.
	Notices() Notices
	// Wake retries a stopped team after a bundle or generation change (SY8).
	Wake()
	// FlushFloor puts the teamchat.floor marker, at a removal before the store goes (SY10).
	FlushFloor(ctx context.Context) error
	// FloorRaised is the store's main floor raised outside a sync answer (an ack or a drop
	// settled what it owed, B 2.3.2), once written; the marker follows. It must not block: the
	// engine's state goroutine calls it.
	FloorRaised(floor int64)
}

// Notices is the matrix calls the ends make, on the team's client.
type Notices interface {
	notice.Client
	SetPresence(ctx context.Context, userID, presence string) error
	Anchors() proto.Anchors
	// Roster is false until a verification.
	Roster() (proto.Roster, bool)
	// RecordOwnSend adds to the resolver's own-sends map.
	RecordOwnSend(id string, r proto.Related)
	// Token is the held pair's token, for proto.Composer.Token and notice.Notice.Token; it is
	// never logged or formatted.
	Token() string
}

// Ends is the run's lifecycle notices.
type Ends interface {
	// Close closes the store's previous run (EN3).
	Close(ctx context.Context, t paths.TeamID, n Notices, prev state.StoreRun) error
	// Back posts back after a disconnect (EN4).
	Back(ctx context.Context, t paths.TeamID, n Notices, run state.StoreRun) error
	// End is EN1, for every team in parallel; one error per team.
	End(ctx context.Context, how End, teams []EndTeam) []error
	// Leave is LN8's left, released.
	Leave(ctx context.Context, t paths.TeamID, n Notices, txn string) error
}

// End is how the session ended: How is one of wire's End kinds, or "keeper-died" (LN1).
type End struct {
	How              string
	SessionEndReason *string
	Kind             string
}

// EndTeam is one team an End posts for.
type EndTeam struct {
	Team    paths.TeamID
	Notices Notices
	Run     state.StoreRun
}
