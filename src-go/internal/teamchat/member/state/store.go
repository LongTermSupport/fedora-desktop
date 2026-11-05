package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// MaxStore is the seat store's file cap (B 2.3.2): the list caps' sum, rounded up to the next
// 256 KiB (n3d-D1), so a list cap always fires first. The sum is n3d's lists, about 3.0 MB, and
// the questions room's, about 1.1 MB (SPEC-n3f 12.6, QX6): 4 104 600 bytes, so 4 MiB.
const MaxStore = 16 * 256 << 10

// B 2.3.2's caps: entries per list, and bytes per entry as compact JSON.
const (
	capPending = 2048
	// sizePending and sizeLedger hold a TIMEOUT item keyed on the longest localpart (432 bytes)
	// and a ping item with its verb (n3d-D30); B's 320 holds neither.
	sizePending    = 448
	pendingReserve = 256 // the last entries of pending, which only HUMAN and TIMEOUT items use
	capLedger      = 1024
	sizeLedger     = 448
	sizePing       = 3 << 10
	capReceipts    = 1024
	sizeReceipt    = 80
	capAwaiting    = 32
	sizeAwaiting   = 2252 // 2.2 KiB
	capMissed      = 16
	sizeMissed     = 640
	// n3d-D1: a send in flight carries its content, so B's 64 of 128 bytes are 32 of 20 KiB.
	sizeSending = 20 << 10
	// maxSendContent is A 11.2's cap on a composed content.
	maxSendContent = 16384
	// n3d-D24: one 60 s window at the limits' maxima allows 90 sends.
	capGateKeys = 96
	sizeGateKey = 96

	// capReports is the report limit's upper bound (n3f-D6); capRemindLate the cap on questions
	// not closed, since each number is an open question (n3f-D11).
	capReports    = 60
	capRemindLate = proto.MaxOpenQuestions

	maxNextBatch      = 1024
	maxTranscriptPath = 1024
	// maxUserID is A4's bound on a user id, which the entry sizes assume.
	maxUserID = 128
)

// The send caps the store holds, which the engine refuses a send at before it reaches the store:
// pings awaiting answers in the outbox (B 2.3.2), a ping's addressees (n3d-D5) and sends in
// flight (n3d-D1).
const (
	MaxPings       = 256
	MaxPingTargets = 16
	MaxSending     = 32
)

const (
	storeFile = "store.json"
	keyOutbox = "outbox"
)

// listCap is one list's cap: its name, where it is, how many entries and how large each.
type listCap struct {
	name        string
	path        []string
	count, size int
}

// listCaps are every capped list of the store.
var listCaps = []listCap{
	{"pending", []string{"pending"}, capPending, sizePending},
	{"awaiting", []string{"awaiting"}, capAwaiting, sizeAwaiting},
	{"missed", []string{"missed"}, capMissed, sizeMissed},
	{"ledger", []string{"ledger"}, capLedger, sizeLedger},
	{"receipts", []string{"receipts"}, capReceipts, sizeReceipt},
	{"pings", []string{keyOutbox, "pings"}, MaxPings, sizePing},
	{"sending", []string{keyOutbox, "sending"}, MaxSending, sizeSending},
	{"gate keys", []string{keyOutbox, "gate", "recent"}, capGateKeys, sizeGateKey},
}

// The closed sets of the store's string fields.
const (
	WrittenUnknown = "unknown"
	WrittenYes     = "yes"

	SendText   = "text"
	SendPing   = "ping"
	SendNotice = "notice"
	SendReport = "report"

	NoticeLeft         = "left"
	NoticeDisconnected = "disconnected"
	NoticeNone         = "none"

	StateAttempted = "attempted"
	StateSent      = "sent"
	StateSkipped   = "skipped"

	ReasonRestart      = "restart"
	ReasonLaunchFailed = "launch-failed"
)

var (
	storeRooms  = []string{proto.PurposeMain, proto.PurposeReports, proto.PurposeQuestions}
	storeItems  = []string{records.KindHuman, records.KindPeer, records.KindPing, records.KindNotice, records.KindTimeout}
	sendKinds   = []string{SendText, SendPing, SendNotice, SendReport}
	endReasons  = []string{"exit", "logout", "other"}
	noneReasons = []string{ReasonRestart, ReasonLaunchFailed}
	nonceRe     = regexp.MustCompile(`^[0-9a-f]{8}$`)
	txnRe       = regexp.MustCompile(`^tc[0-9a-f]{32}$`)
	sha256Re    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	syncTokenRe = regexp.MustCompile(`^[\x21-\x7e]+$`)
)

// Store is a seat's store.json (B 2.3.2), the link's one snapshot of the seat's state.
type Store struct {
	V        int          `json:"v"`
	Team     paths.TeamID `json:"team"`
	Seat     string       `json:"seat"`
	UserID   string       `json:"user_id"`
	Sync     Sync         `json:"sync"`
	Pending  []Pending    `json:"pending"`
	Awaiting []Awaiting   `json:"awaiting"`
	Missed   []Missed     `json:"missed"`
	Ledger   []Ledger     `json:"ledger"`
	Receipts []Receipt    `json:"receipts"`
	Outbox   Outbox       `json:"outbox"`
	// Questions is the room's question index, null for a seat with none (SPEC-n3f QX5).
	Questions *proto.QuestionsState `json:"questions"`
	// RemindedMS is the latest due time this seat has reminded; RemindLate the open questions a
	// build or catch-up found below it (n3f-D11).
	RemindedMS int64    `json:"reminded_ms"`
	RemindLate []int64  `json:"remind_late"`
	Run        StoreRun `json:"run"`
}

// Sync is the sync position, its restore epoch and the room floors.
type Sync struct {
	NextBatch    string           `json:"next_batch"`
	Fresh        bool             `json:"fresh"`
	RestoreEpoch int64            `json:"restore_epoch"`
	FloorMS      map[string]int64 `json:"floor_ms"`
}

// Pending is an item the store owes.
type Pending struct {
	ID       string  `json:"id"`
	Room     string  `json:"room"`
	Item     string  `json:"item"`
	SentMS   int64   `json:"sent_ms"`
	From     string  `json:"from"`
	Repeat   bool    `json:"repeat"`
	Held     bool    `json:"held"`
	Restored bool    `json:"restored"`
	Handed   *string `json:"handed"`
	// Verb is the ping's verb for an item "ping", null otherwise (n3d-D30).
	Verb *string `json:"verb"`
}

// Awaiting is a delivery whose proof is not settled.
type Awaiting struct {
	Nonce      string         `json:"nonce"`
	IDs        []string       `json:"ids"`
	Transcript TranscriptMark `json:"transcript"`
	AtMS       int64          `json:"at_ms"`
	Written    string         `json:"written"`
	StopMS     *int64         `json:"stop_ms"`
}

// TranscriptMark is a transcript and its size before a delivery was written.
type TranscriptMark struct {
	Path   string `json:"path"`
	Offset int64  `json:"offset"`
}

// Missed is a delivery declared missing, watched for a late find.
type Missed struct {
	Nonce  string   `json:"nonce"`
	IDs    []string `json:"ids"`
	Offset int64    `json:"offset"`
	AtMS   int64    `json:"at_ms"`
}

// Ledger is a delivered item.
type Ledger struct {
	ID     string  `json:"id"`
	Room   string  `json:"room"`
	Item   string  `json:"item"`
	Thread *string `json:"thread"`
	From   string  `json:"from"`
	AtMS   int64   `json:"at_ms"`
	// Verb is the ping's verb for an item "ping", null otherwise (n3d-D30).
	Verb *string `json:"verb"`
}

// Receipt is a read receipt still to send.
type Receipt struct {
	ID   string `json:"id"`
	Room string `json:"room"`
}

// Outbox is the sends: pings awaiting answers, sends in flight, the send gate, and the event
// id of the last send recorded as posted (null for none), which bounds a recovery's search
// (n3d-D4).
type Outbox struct {
	Pings    []Ping    `json:"pings"`
	Sending  []Sending `json:"sending"`
	Gate     Gate      `json:"gate"`
	LastSent *string   `json:"last_sent"`
	// Reports are the times of the reports of the last 3600 s (n3f-D6).
	Reports []int64 `json:"reports"`
	// QuestionsUnechoed is the event id of the coordinator's last questions post that is posted
	// and not yet folded, or null (QX3).
	QuestionsUnechoed *string `json:"questions_unechoed"`
	// QuestionsUncounted counts the own questions posts this term whose echo was not counted and
	// the hook lines said about them (QX3).
	QuestionsUncounted Uncounted `json:"questions_uncounted"`
	// QuestionsHandover is true while the line that a promotion's questions index is ready is
	// owed (QX1).
	QuestionsHandover bool `json:"questions_handover"`
}

// Uncounted is the own questions posts whose echo was not counted, and how many of them have
// had their hook line said.
type Uncounted struct {
	Count int64 `json:"count"`
	Said  int64 `json:"said"`
}

// Ping is a sent ping awaiting its answers; Answered and Reported index into To.
type Ping struct {
	ID       string   `json:"id"`
	Verb     string   `json:"verb"`
	Ref      *string  `json:"ref"`
	To       []string `json:"to"`
	SentMS   int64    `json:"sent_ms"`
	DueMS    int64    `json:"due_ms"`
	Answered []int    `json:"answered"`
	Reported []int    `json:"reported"`
}

// Sending is a send whose transaction id is chosen and whose answer has not arrived, with what
// a restarted link re-sends it with (n3d-D1).
type Sending struct {
	Txn  string `json:"txn"`
	Room string `json:"room"`
	Kind string `json:"kind"`
	// SinceMS is the wall time of the charge.
	SinceMS int64 `json:"since_ms"`
	// Type is the event type, m.room.message.
	Type string `json:"type"`
	// Content is the composed content: a strict object of at most 16384 bytes (A 11.2).
	Content json.RawMessage `json:"content"`
	// Tried is a PUT that may have reached the server (n3d-D4).
	Tried bool `json:"tried"`
	// Ping is set for a ping whose verb expects an answer, else null.
	Ping *SendingPing `json:"ping"`
}

// SendingPing is what the outbox needs of an answer-expecting ping once it is posted.
type SendingPing struct {
	Verb string   `json:"verb"`
	Ref  *string  `json:"ref"`
	To   []string `json:"to"`
	// TimeoutMS is ack_timeout_s or halt_ack_timeout_s at the charge.
	TimeoutMS int64 `json:"timeout_ms"`
}

// Gate is the send gate: proto.Bucket's integer credit and the recent duplicate keys.
type Gate struct {
	Credit int64     `json:"credit"`
	AtMS   int64     `json:"at_ms"`
	Recent []GateKey `json:"recent"`
}

// GateKey is the SHA-256 of a duplicate key sent at AtMS.
type GateKey struct {
	Key  string `json:"key"`
	AtMS int64  `json:"at_ms"`
}

// StoreRun is the run that holds or last held the seat; Ended is null while it holds it.
type StoreRun struct {
	ID        paths.RunID `json:"id"`
	StartedMS int64       `json:"started_ms"`
	Ended     *RunEnd     `json:"ended"`
}

// RunEnd is the run's end notice (B 2.11).
type RunEnd struct {
	Notice string  `json:"notice"`
	Reason *string `json:"reason"`
	State  string  `json:"state"`
	Txn    *string `json:"txn"`
	AtMS   int64   `json:"at_ms"`
}

// NewStore is a fresh store for userID's seat of team: no position yet, every list empty. The
// seat is the handle's; a user id that is no agent's leaves it empty, which no write accepts.
func NewStore(team paths.TeamID, userID string, run StoreRun, gate Gate) Store {
	seat := ""
	if h, ok := proto.AgentHandle(userID, paths.ServerName(team)); ok {
		seat = h.Seat
	}
	gate.Recent = []GateKey{}
	return Store{
		V: Version, Team: team, Seat: seat, UserID: userID,
		Sync:    Sync{Fresh: true, FloorMS: map[string]int64{}},
		Pending: []Pending{}, Awaiting: []Awaiting{}, Missed: []Missed{}, Ledger: []Ledger{}, Receipts: []Receipt{},
		Outbox:     Outbox{Pings: []Ping{}, Sending: []Sending{}, Gate: gate, Reports: []int64{}},
		RemindLate: []int64{},
		Run:        run,
	}
}

// DecodeStore reads a store strictly, with every cap (MS1).
func DecodeStore(data []byte) (Store, error) {
	var s Store
	parsed, err := decodeStrict(storeFile, data, MaxStore, &s, func(c *rules) { s.check(c) })
	if err != nil {
		return Store{}, err
	}
	c := &rules{}
	for _, l := range listCaps {
		for _, item := range at(parsed, l.path).Items() {
			c.need(item.Size() <= l.size, fmt.Sprintf("an entry of %s is over %d bytes", l.name, l.size))
		}
	}
	if c.rule != "" {
		return Store{}, &InvalidError{File: storeFile, Code: c.code, Rule: c.rule}
	}
	return s, nil
}

// at is the value at path below v; null when a key is missing.
func at(v proto.Value, path []string) proto.Value {
	for _, key := range path {
		next, found := v.Get(key)
		if !found {
			return proto.Null()
		}
		v = next
	}
	return v
}

func (s *Sync) check(c *rules) {
	c.need(s.Fresh == (s.NextBatch == ""), "next_batch is set exactly when the store is not fresh")
	c.need(s.NextBatch == "" || len(s.NextBatch) <= maxNextBatch && syncTokenRe.MatchString(s.NextBatch), "next_batch is not a sync token")
	c.need(s.RestoreEpoch >= 0 && s.FloorMS != nil, "restore_epoch is negative, or floor_ms is not an object")
	for room, floor := range s.FloorMS {
		c.need(slices.Contains(storeRooms, room) && validMS(floor), "floor_ms holds an unknown room or a floor that is not a time")
	}
}

func (a *Awaiting) check(c *rules) {
	c.need(nonceRe.MatchString(a.Nonce) && itemIDs(a.IDs) && validMS(a.AtMS), "an awaiting entry is not well formed")
	c.need(validTranscript(a.Transcript.Path) && a.Transcript.Offset >= 0, "an awaiting entry's transcript is not an absolute .jsonl path and an offset")
	c.need((a.Written == WrittenUnknown || a.Written == WrittenYes) && (a.StopMS == nil || validMS(*a.StopMS)), "an awaiting entry's written or stop_ms is not well formed")
}

func (o *Outbox) check(c *rules, server string) {
	c.need(o.Pings != nil && o.Sending != nil && o.Gate.Recent != nil && o.Reports != nil, "an outbox list is not a list")
	ids := make([]string, 0, len(o.Pings))
	for _, p := range o.Pings {
		p.check(c, server)
		ids = append(ids, p.ID)
	}
	c.need(distinct(ids), "the outbox holds a ping id twice")
	c.need(o.LastSent == nil || proto.ValidEventID(*o.LastSent), "last_sent is not an event id")
	for _, s := range o.Sending {
		s.check(c, server)
	}
	c.need(o.Gate.Credit >= 0 && validMS(o.Gate.AtMS), "the gate's credit is negative or its time is not a time")
	for _, k := range o.Gate.Recent {
		c.need(sha256Re.MatchString(k.Key) && validMS(k.AtMS), "a gate key is not a SHA-256 and a time")
	}
	o.checkQuestionsRoom(c)
}

// checkQuestionsRoom holds the outbox's keys of the questions room well formed: the report times
// (n3f-D6), the last questions post not yet folded and the own questions posts not counted
// (QX3). said counts the lines said about them, so it is never past count.
func (o *Outbox) checkQuestionsRoom(c *rules) {
	c.need(len(o.Reports) <= capReports, fmt.Sprintf("reports holds more than %d entries", capReports))
	for _, ms := range o.Reports {
		c.need(validMS(ms), "a report time is not a time")
	}
	c.need(o.QuestionsUnechoed == nil || proto.ValidEventID(*o.QuestionsUnechoed), "questions_unechoed is not an event id")
	u := o.QuestionsUncounted
	c.need(u.Said >= 0 && u.Said <= u.Count, "questions_uncounted is not counts with said at most count")
}

func (s *Sending) check(c *rules, server string) {
	c.need(txnRe.MatchString(s.Txn) && slices.Contains(storeRooms, s.Room) && slices.Contains(sendKinds, s.Kind) &&
		validMS(s.SinceMS) && s.Type == proto.MessageType, "a send in flight is not well formed")
	content, err := proto.Parse(s.Content)
	c.need(err == nil && content.Kind() == proto.KindObject && content.Size() <= maxSendContent,
		fmt.Sprintf("a send in flight's content is not an object of at most %d bytes", maxSendContent))
	if s.Ping != nil {
		c.need(s.Kind == SendPing, "a send in flight that is not a ping has a ping")
		s.Ping.check(c, server)
	}
}

func (p *SendingPing) check(c *rules, server string) {
	rule, known := proto.Verbs[p.Verb]
	c.need(known && rule.ExpectsAnswer && p.TimeoutMS > 0, "a send in flight's ping is not an answer-expecting ping")
	ref := p.Ref != nil && validRef(*p.Ref)
	c.need(ref && rule.Ref != proto.Forbidden || p.Ref == nil && rule.Ref != proto.Required, "a send in flight's ref is not as its verb needs")
	c.need(len(p.To) >= 1 && len(p.To) <= MaxPingTargets && distinct(p.To) && !slices.ContainsFunc(p.To, func(id string) bool { return !validUser(id, server) }),
		fmt.Sprintf("a send in flight's ping is not to 1 to %d distinct users of the team's server", MaxPingTargets))
}

// validVerb is a verb exactly for an item "ping", and then one of A 8.1's (n3d-D30).
func validVerb(item string, verb *string) bool {
	if verb == nil {
		return item != records.KindPing
	}
	_, known := proto.Verbs[*verb]
	return item == records.KindPing && known
}

func (p *Ping) check(c *rules, server string) {
	rule, known := proto.Verbs[p.Verb]
	c.need(proto.ValidEventID(p.ID) && known && rule.ExpectsAnswer && validMS(p.SentMS) && p.SentMS <= p.DueMS && validMS(p.DueMS), "a ping is not well formed")
	ref := p.Ref != nil && validRef(*p.Ref)
	c.need(ref && rule.Ref != proto.Forbidden || p.Ref == nil && rule.Ref != proto.Required, "a ping's ref is not as its verb needs")
	c.need(len(p.To) >= 1 && len(p.To) <= MaxPingTargets && distinct(p.To), fmt.Sprintf("a ping is not to 1 to %d distinct ids", MaxPingTargets))
	for _, id := range p.To {
		c.need(validUser(id, server), "a ping's target is not a user of the team's server")
	}
	c.need(indexes(p.Answered, len(p.To)) && indexes(p.Reported, len(p.To)), "a ping's answered or reported are not ascending indexes into to")
}

func (r *StoreRun) check(c *rules) {
	c.need(validMS(r.StartedMS), "run.started_ms is not a time")
	e := r.Ended
	if e == nil {
		return
	}
	c.need(validMS(e.AtMS), "run.ended.at_ms is not a time")
	if e.Notice == NoticeNone {
		c.need(e.Reason != nil && slices.Contains(noneReasons, *e.Reason) && e.State == StateSkipped && e.Txn == nil, "a run that posts nothing is a skipped restart or launch failure with no txn")
		return
	}
	c.need(e.Notice == NoticeLeft || e.Notice == NoticeDisconnected, "run.ended.notice is not left, disconnected or none")
	c.need(e.Reason == nil || slices.Contains(endReasons, *e.Reason), "run.ended.reason is not exit, logout, other or null")
	c.need((e.State == StateAttempted || e.State == StateSent) && e.Txn != nil && txnRe.MatchString(*e.Txn), "run.ended is not attempted or sent with its txn")
}

// reserved is an item kind that may use the reserve of pending.
func reserved(item string) bool { return item == records.KindHuman || item == records.KindTimeout }

func validRef(s string) bool {
	_, ok := proto.ParseRef(s)
	return ok
}

// validUser is a user id on server (an agent, a human, the admin or a reserved account),
// within A4's bound.
func validUser(id, server string) bool {
	return len(id) <= maxUserID && proto.ClassifyUserID(id, server) != proto.IDNone
}

const reminderRule = "a reminder's key, `~humans`, is on a TIMEOUT in the questions room alone"

// reminderPlaced is whether an item with this id, room and kind keeps to A 10.5: a key that ends
// `~humans` (proto.ReminderKey) belongs to a TIMEOUT in the questions room, and only to it.
func reminderPlaced(id, room, item string) bool {
	if !strings.HasSuffix(id, "~"+proto.HumansWord) {
		return true
	}
	return room == proto.PurposeQuestions && item == records.KindTimeout
}

// validItemID is an item's id: an event id, or a TIMEOUT's key (proto.TimeoutKey) naming the
// silent addressee, or humans for a reminder (proto.ReminderKey).
func validItemID(id string) bool {
	event, local, timeout := strings.Cut(id, "~")
	if !timeout {
		return proto.ValidEventID(id)
	}
	_, handle := proto.ParseHandle(local)
	return proto.ValidEventID(event) && (handle || proto.ValidHumanName(local) || local == proto.HumansWord)
}

// validPendingID is a pending item's id: a TIMEOUT's key exactly for a TIMEOUT.
func validPendingID(id, item string) bool {
	return validItemID(id) && strings.Contains(id, "~") == (item == records.KindTimeout)
}

func itemIDs(ids []string) bool {
	return len(ids) > 0 && distinct(ids) && !slices.ContainsFunc(ids, func(id string) bool { return !validItemID(id) })
}

func distinct(ss []string) bool {
	sorted := slices.Clone(ss)
	slices.Sort(sorted)
	return len(slices.Compact(sorted)) == len(ss)
}

// indexes is a non-null, strictly ascending list of indexes below n.
func indexes(ix []int, n int) bool {
	if ix == nil {
		return false
	}
	for i, x := range ix {
		if x < 0 || x >= n || i > 0 && x <= ix[i-1] {
			return false
		}
	}
	return true
}

// ErrFull is a store list with no room for the entry an operation would add (B 2.3.2: the
// operation is refused loudly, never dropped).
var ErrFull = errors.New("state: a store list is full")

// FullError names the full list.
type FullError struct {
	List string
}

func (e *FullError) Error() string { return "state: the store's " + e.List + " list is full" }

// Is makes errors.Is(err, ErrFull) match.
func (e *FullError) Is(target error) bool { return target == ErrFull }

// grow appends item to *list unless it already holds limit entries.
func grow[T any](list *[]T, item T, limit int, name string) error {
	if len(*list) >= limit {
		return &FullError{List: name}
	}
	*list = append(*list, item)
	return nil
}

// roll appends item, dropping the oldest entries past limit.
func roll[T any](list []T, item T, limit int) []T {
	list = append(list, item)
	if len(list) > limit {
		list = slices.Delete(list, 0, len(list)-limit)
	}
	return list
}

// AddPending owes p. Agent items stop at the cap less the reserve, so a burst of agent
// traffic never stops human delivery; HUMAN and TIMEOUT items may use the whole cap.
func (s *Store) AddPending(p Pending) error {
	limit := capPending - pendingReserve
	if reserved(p.Item) {
		limit = capPending
	}
	return grow(&s.Pending, p, limit, "pending")
}

// AddAwaiting records a delivery being written.
func (s *Store) AddAwaiting(a Awaiting) error {
	return grow(&s.Awaiting, a, capAwaiting, "awaiting")
}

// AddReceipt queues a read receipt.
func (s *Store) AddReceipt(r Receipt) error { return grow(&s.Receipts, r, capReceipts, "receipts") }

// AddPing records a sent ping.
func (s *Store) AddPing(p Ping) error { return grow(&s.Outbox.Pings, p, MaxPings, "pings") }

// AddSending records a send in flight.
func (s *Store) AddSending(x Sending) error {
	return grow(&s.Outbox.Sending, x, MaxSending, "sending")
}

// AddGateKey records a duplicate key in the gate.
func (s *Store) AddGateKey(k GateKey) error {
	return grow(&s.Outbox.Gate.Recent, k, capGateKeys, "gate keys")
}

// AddLedger records a delivered item; the oldest is dropped past the cap.
func (s *Store) AddLedger(l Ledger) { s.Ledger = roll(s.Ledger, l, capLedger) }

// AddMissed records a delivery declared missing; the oldest is dropped past the cap.
func (s *Store) AddMissed(m Missed) { s.Missed = roll(s.Missed, m, capMissed) }

// encode is the store's bytes, for the StoreWriter alone.
func (s *Store) encode() ([]byte, error) {
	out := *s
	out.V = Version
	return encode(storeFile, out, DecodeStore)
}

func (s *Store) check(c *rules) {
	server := paths.ServerName(s.Team)
	h, agent := proto.AgentHandle(s.UserID, server)
	c.need(agent && h.Seat == s.Seat && len(s.UserID) <= maxUserID, "user_id is not the agent of this seat on the team's server")
	s.Sync.check(c)
	c.need(s.Pending != nil && s.Awaiting != nil && s.Missed != nil && s.Ledger != nil && s.Receipts != nil, "a list is not a list")
	s.checkPending(c, server)
	s.checkDelivered(c, server)
	s.Outbox.check(c, server)
	s.checkQuestionsRoom(c, server)
	s.Run.check(c)
	counts := []int{len(s.Pending), len(s.Awaiting), len(s.Missed), len(s.Ledger), len(s.Receipts), len(s.Outbox.Pings), len(s.Outbox.Sending), len(s.Outbox.Gate.Recent)}
	for i, l := range listCaps {
		c.need(counts[i] <= l.count, fmt.Sprintf("%s holds more than %d entries", l.name, l.count))
	}
}

// checkQuestionsRoom holds the top-level keys of the questions room well formed: the room's
// index by the codec's own rules (the store gives it the team's server), the reminder watermark
// and the numbers of the open questions found below it (n3f-D11). The index key's size follows
// from its caps (QX6), so no byte rule of its own is needed.
func (s *Store) checkQuestionsRoom(c *rules, server string) {
	if s.Questions != nil {
		if err := s.Questions.Check(server); err != nil {
			rule := "the key is not valid"
			if qe, ok := errors.AsType[*proto.QuestionsStateError](err); ok {
				rule = qe.Rule
			}
			c.need(false, "questions: "+rule)
		}
	}
	c.need(s.RemindedMS >= 0, "reminded_ms is not a time")
	c.need(s.RemindLate != nil, "remind_late is not a list")
	c.need(len(s.RemindLate) <= capRemindLate, fmt.Sprintf("remind_late holds more than %d entries", capRemindLate))
	c.need(questionNumbers(s.RemindLate), "remind_late is not distinct question numbers")
}

// questionNumbers is distinct question numbers: from 1, each once.
func questionNumbers(ns []int64) bool {
	seen := make(map[int64]bool, len(ns))
	for _, n := range ns {
		if n < 1 || seen[n] {
			return false
		}
		seen[n] = true
	}
	return true
}

// checkPending holds every pending entry well formed, each id once, and agent items within
// the cap less the reserve.
func (s *Store) checkPending(c *rules, server string) {
	seen := map[string]bool{}
	agent := 0
	for _, p := range s.Pending {
		c.need(validPendingID(p.ID, p.Item) && slices.Contains(storeRooms, p.Room) && slices.Contains(storeItems, p.Item) &&
			validMS(p.SentMS) && validUser(p.From, server) && (p.Handed == nil || nonceRe.MatchString(*p.Handed)) && validVerb(p.Item, p.Verb), "a pending entry is not well formed")
		c.need(reminderPlaced(p.ID, p.Room, p.Item), reminderRule)
		c.need(!seen[p.ID], "pending holds an id twice")
		seen[p.ID] = true
		if !reserved(p.Item) {
			agent++
		}
	}
	c.need(agent <= capPending-pendingReserve, fmt.Sprintf("pending holds more than %d agent items", capPending-pendingReserve))
}

// checkDelivered holds the awaiting, missed, ledger and receipt entries well formed.
func (s *Store) checkDelivered(c *rules, server string) {
	for _, a := range s.Awaiting {
		a.check(c)
	}
	for _, m := range s.Missed {
		c.need(nonceRe.MatchString(m.Nonce) && itemIDs(m.IDs) && m.Offset >= 0 && validMS(m.AtMS), "a missed entry is not well formed")
	}
	for _, l := range s.Ledger {
		l.check(c, server)
	}
	for _, r := range s.Receipts {
		c.need(proto.ValidEventID(r.ID) && slices.Contains(storeRooms, r.Room), "a receipt is not well formed")
	}
}

func (l *Ledger) check(c *rules, server string) {
	c.need(validItemID(l.ID) && slices.Contains(storeRooms, l.Room) && slices.Contains(storeItems, l.Item) &&
		(l.Thread == nil || proto.ValidEventID(*l.Thread)) && validUser(l.From, server) && validMS(l.AtMS) && validVerb(l.Item, l.Verb), "a ledger entry is not well formed")
	c.need(reminderPlaced(l.ID, l.Room, l.Item), reminderRule)
}
