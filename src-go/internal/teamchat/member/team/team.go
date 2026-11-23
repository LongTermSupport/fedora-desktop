// Package team is the seams between the link's three halves (Plan 00209 SPEC-n3, n3c): the
// engine, the link's state goroutine, which makes no network call (B 2.4.3); a team goroutine,
// which owns every homeserver request of its team (the sync engine); and the ends, which post
// a run's lifecycle notices on a team goroutine's client (EN1-EN5). Each half sees the others
// only through these interfaces, so each is tested against fakes of the rest.
package team

import (
	"context"
	"encoding/json"
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
	Items        []Item
	// Floors are the floors this batch moved, keyed by room purpose (proto.PurposeMain,
	// PurposeQuestions), as the store's floor_ms and Pending.Room are; never a room id.
	Floors map[string]state.Floor
	// Answered are the answer-form events to this member's own pings (SY4).
	Answered []Answer
	// Echoes are this member's own pings come back in sync (n3d-D9).
	Echoes []Echo
	// Redacts are admin redactions of pending or handed items.
	Redacts []string
	Counts  Counts
	// HeldBack is B 2.7.3's delivery budget: agent sender user id -> the items over its
	// budget in this batch, which never entered pending (n3e-D1).
	HeldBack map[string]int64
	// Questions is the `questions` key as the questions room's fold leaves it (QX5, n3f-D19): an
	// answer's SY5 write whose filter held the room and whose questions timeline was folded, or a
	// QX1 page's write. Nil leaves the key as it was, which is every other write's.
	Questions *proto.QuestionsState
	// Fresh is false once SY1's step 01 has persisted.
	Fresh bool
	// Gap is SY3's bound hit.
	Gap bool
	// DropQuestions is QS9's demotion: the questions room's pending and handed items, the index
	// key, `reminded_ms`, `remind_late` and the three questions keys of the outbox dropped in
	// this one write, and a `sending` questions post with them. It excludes Questions.
	DropQuestions bool
	// Page is a QX1 page's write, made beside the team's sync: its items, floors and key are
	// written, and the store's position (next_batch, restore epoch, fresh) stays as the sync left
	// it, whatever NextBatch holds.
	Page bool
	// QuestionsFolded are the questions room's events this batch folded, and QuestionsUncounted
	// the own posts among them whose echo was not counted (QX3): the write clears
	// `outbox.questions_unechoed` when it names that event, and raises the uncounted count by
	// their number; the engine keeps them for the hook line that names each (QX3).
	QuestionsFolded    []string
	QuestionsUncounted []proto.Dropped
	// Handover sets `outbox.questions_handover`, the ready line owed (QX1): the first page write of
	// a build begun with no `questions` key.
	Handover bool
	// Relays are the coordinator's relays this batch owes (A 10.6): one per human post the sync
	// folded onto a question with `asked_by` and per asking seat. The write queues each in
	// `sending`, under its derived transaction ID, unless that ID is there already.
	Relays []Relay
}

// Relay is one relay to send: its transaction ID (proto.RelayTxn) and its composed main-room
// content.
type Relay struct {
	Txn     string
	Content json.RawMessage
}

// Answer is an answer-form event to an own ping: From answered Ping with Verb in event ID, at
// its origin_server_ts (A 8.2).
type Answer struct {
	Ping, From, Verb, ID string
	OriginMS             int64
}

// Echo is an own ping come back in sync, with the origin_server_ts its deadline is counted from
// (A 8.2, rule 3 of A 7.6).
type Echo struct {
	ID       string
	OriginMS int64
}

// ForgeProven is a forge check that proved its reference; the other outcomes are proto's
// ReasonProvenance, ReasonUnresolved and ReasonForgeRate (A 9.3).
const ForgeProven = "proven"

// ForgeResult is one forge check's outcome. A proven check's trust is ForgeCheck.Trust().
type ForgeResult struct {
	Outcome string
}

var (
	// ErrForgeLogin is a member with no gh login (78 forge-login on a send).
	ErrForgeLogin = errors.New("team: no forge login")
	// ErrForgeDown is a forge with no answer, or one found so within the last 60 s (n3d-D29).
	ErrForgeDown = errors.New("team: the forge did not answer")
)

// Forge checks a ping's reference at the forge with the member's own login (A 9.3); safe for
// concurrent use, one per link (n3d-D20).
type Forge interface {
	Check(ctx context.Context, need proto.ForgeCheck, f proto.Forge) (ForgeResult, error)
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
	// Verified is whether Role and the facts above come from a verification of main this team
	// goroutine made since it last stopped: written with them in one SetHealth, false from a
	// stop until the next verification (issue 192). A reader states the role or the
	// coordinator only while it holds; a server that cannot be reached does not clear it.
	Verified bool
	Down     bool
	// Questions is the questions room as this member stands to it (QS2, QS8), the zero value
	// for a member that is not the coordinator.
	Questions QuestionsHealth
}

// The states of the questions room for a coordinator (the `room` record's `state` and `check`).
const (
	// QuestionsAwaitingInvite is a join refused with 403: not invited yet (QS2).
	QuestionsAwaitingInvite = "awaiting-invite"
	// QuestionsLeft is this member's own leave or a kick, the roster still naming it (QS8).
	QuestionsLeft = "left"
	// QuestionsBuilding is a room joined and verified whose index is not at the head (QX1).
	QuestionsBuilding = "building"
	// QuestionsReady is a room joined, verified and its index at the head.
	QuestionsReady = "ready"
	// QuestionsUntrusted is a room that fails its check, or an invite still absent after 300 s.
	QuestionsUntrusted = "untrusted"
	// CheckInvite is the check an invite absent for 300 s fails (QS2).
	CheckInvite = "invite"
)

// QuestionsHealth is the questions room's state on the team goroutine: State is one of the
// Questions constants, "" while this member is not the roster's non-parked coordinator, and Check
// the check an untrusted room fails.
type QuestionsHealth struct {
	State, Check string
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
	Persist(ctx context.Context, b Batch) (mainFloor state.Floor, err error)
	// ResetPosition is SY6: drop next_batch and store the new epoch; fresh stays as it is.
	ResetPosition(ctx context.Context, t paths.TeamID, epoch int64) error
	// SetEnded records the store's run's end (EN1, EN3).
	SetEnded(ctx context.Context, t paths.TeamID, e state.RunEnd) error
	// BeginRun makes the store's run this link's run, with ended null; a no-op when it is
	// already. The team calls it after Ends.Close returns nil.
	BeginRun(ctx context.Context, t paths.TeamID) error
	SetHealth(t paths.TeamID, h Health)
	// SaveRoster keeps the roster teamsync verified at a new rev in the seat's roster cache
	// (n3d-D3).
	SaveRoster(ctx context.Context, t paths.TeamID, rev int64, content json.RawMessage) error
	// RecoverSends finds the tried sends of a restarted link among its own events, before the
	// team's first long-poll (n3d-D4).
	RecoverSends(ctx context.Context, t paths.TeamID, n Notices) error
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
	// Verify is LN6 in the room named (main or questions), or main then questions for nil (R10).
	Verify(ctx context.Context, id string, room *string) (records.Verified, error)
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
	FloorRaised(floor state.Floor)
	// VerifyReports is RP4, at a `report` and again at the sender before a queued report is
	// posted: the join tried when this member is not in the reports room (RP1), then the room's
	// state read and judged against main's verified roster. Nil is a room that verifies; an
	// *exits.Error of code forbidden or untrusted is a refusal, which the room's state records;
	// any other error is transient (the homeserver did not answer, or main has not been
	// verified yet), and a send queues. It makes the team's requests, so it is not for the
	// engine's state goroutine.
	VerifyReports(ctx context.Context) error
	// ReportsRoom is the reports room's state as the team last found it, from memory, for status.
	ReportsRoom() ReportsRoom
	// Questions runs compose with the questions index, one compose at a time per team, on the
	// hold the team's judging and commits take (QX4): once the room is ready and no own post of
	// an earlier run waits for its echo (QX3), else at ctx's end 20 not-ready; a room that is
	// not this member's is 11 role, one that fails its check 22 untrusted. compose gets a copy
	// of the index; the placements it returns (after its SD3 write) are placed on the live
	// index before the hold is let go, and the view returned is the index after them.
	Questions(ctx context.Context, compose func(QuestionsView) ([]Placement, error)) (QuestionsView, error)
	// QuestionPosted is the sender's word that the own questions post txn was posted as id, so
	// a compose does not wait for that echo (QX3). It must not block.
	QuestionPosted(txn, id string)
	// QuestionsPostable is the sender's check before a queued questions post is put (QX3):
	// ErrQuestionsNotReady while the index is not ready (the send waits), a refusal when a
	// question's number is counted by a folded event, nil otherwise.
	QuestionsPostable(content proto.Value) error
	// FetchEvent fetches one event of the questions room by id (a retitle's root, A 10.4).
	FetchEvent(ctx context.Context, id string) (json.RawMessage, error)
}

// ErrQuestionsNotReady is a questions post the sender holds: the index is not at the head.
var ErrQuestionsNotReady = errors.New("team: the question index is not ready")

// QuestionsView is the questions room as a compose reads it: its id, and the index at the head
// (a copy, which the compose may read but whose changes are not kept).
type QuestionsView struct {
	Room  string
	Index *proto.QuestionIndex
}

// Placement is an own questions post placed provisionally once its SD3 write answered (QX3):
// a question with its body, or a thread post.
type Placement struct {
	Txn      string
	Question *proto.Question
	Body     string
	Thread   *proto.PlacedThread
}

// PlacementOf is the placement of an own questions content sent as txn (QX3), false for one
// that places nothing (a link-back, a retitle, or content that does not decode).
func PlacementOf(txn string, content json.RawMessage) (Placement, bool) {
	v, err := proto.Parse(content)
	if err != nil {
		return Placement{}, false
	}
	dc, reason := proto.DecodeContent(v, proto.QuestionKeys)
	switch {
	case reason != "":
		return Placement{}, false
	case dc.Key == proto.KeyQuestion:
		q := dc.Question
		return Placement{Txn: txn, Question: &q, Body: dc.Body}, true
	case dc.Key == proto.KeyThread && dc.Thread.Link == "":
		th := proto.PlacedThread{N: dc.Thread.N, Close: dc.Thread.Close}
		if th.N == 0 {
			th.ReplyTo = dc.Relation.InReplyTo
		}
		return Placement{Txn: txn, Thread: &th}, true
	}
	return Placement{}, false
}

// The states of a reports room (SPEC-n3f RP1, RP4): its join is not answered yet, it is joined
// and verified or not yet checked, its join was refused, or it fails a check.
const (
	ReportsJoining   = "joining"
	ReportsJoined    = "joined"
	ReportsRefused   = "refused"
	ReportsUntrusted = "untrusted"
)

// ReportsRoom is a reports room's state. Check is the errcode of a refused join, or the check an
// untrusted room failed; "" for any other state.
type ReportsRoom struct {
	State string
	Check string
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

type receiveKey struct{}

// WithReceive marks ctx as a receive check (n3d-D31). spend is the senders' budget: asked once,
// before the check's first request; false ends the check forge-rate with no request
// (n3d-D34). A receive check also stops asking while the forge's rate window is down to the
// reserve kept for sends.
func WithReceive(ctx context.Context, spend func() bool) context.Context {
	return context.WithValue(ctx, receiveKey{}, spend)
}

// ReceiveSpend is the spend a receive check's ctx carries; false for a send's.
func ReceiveSpend(ctx context.Context) (func() bool, bool) {
	spend, ok := ctx.Value(receiveKey{}).(func() bool)
	return spend, ok
}
