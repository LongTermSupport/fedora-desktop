package state

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// MaxStore is the seat store's file cap (B 2.3.2): the list caps' sum, rounded up.
const MaxStore = 2 << 20

// B 2.3.2's caps: entries per list, and bytes per entry as compact JSON.
const (
	capPending     = 2048
	sizePending    = 320
	pendingReserve = 256 // the last entries of pending, which only HUMAN and TIMEOUT items use
	capLedger      = 1024
	sizeLedger     = 320
	capPings       = 256
	sizePing       = 3 << 10
	capPingTargets = 16
	capReceipts    = 1024
	sizeReceipt    = 80
	capAwaiting    = 32
	sizeAwaiting   = 2252 // 2.2 KiB
	capMissed      = 16
	sizeMissed     = 640
	capSending     = 64
	sizeSending    = 128
	capGateKeys    = 64
	sizeGateKey    = 96

	maxNextBatch      = 1024
	maxTranscriptPath = 1024
	// maxUserID is A4's bound on a user id, which the entry sizes assume.
	maxUserID = 128
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
	{"pings", []string{keyOutbox, "pings"}, capPings, sizePing},
	{"sending", []string{keyOutbox, "sending"}, capSending, sizeSending},
	{"gate keys", []string{keyOutbox, "gate", "recent"}, capGateKeys, sizeGateKey},
}

// The closed sets of the store's string fields.
const (
	WrittenUnknown = "unknown"
	WrittenYes     = "yes"

	SendText   = "text"
	SendPing   = "ping"
	SendNotice = "notice"

	NoticeLeft         = "left"
	NoticeDisconnected = "disconnected"
	NoticeNone         = "none"

	StateAttempted = "attempted"
	StateSent      = "sent"
	StateSkipped   = "skipped"

	ReasonRestart = "restart"
)

var (
	storeRooms  = []string{proto.PurposeMain, proto.PurposeReports, proto.PurposeQuestions}
	storeItems  = []string{records.KindHuman, records.KindPeer, records.KindPing, records.KindNotice, records.KindTimeout}
	sendKinds   = []string{SendText, SendPing, SendNotice}
	endReasons  = []string{"exit", "logout", "other"}
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
	Run      StoreRun     `json:"run"`
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
}

// Receipt is a read receipt still to send.
type Receipt struct {
	ID   string `json:"id"`
	Room string `json:"room"`
}

// Outbox is the sends: pings awaiting answers, sends in flight, and the send gate.
type Outbox struct {
	Pings   []Ping    `json:"pings"`
	Sending []Sending `json:"sending"`
	Gate    Gate      `json:"gate"`
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

// Sending is a send whose transaction id is chosen and whose answer has not arrived.
type Sending struct {
	Txn     string `json:"txn"`
	Room    string `json:"room"`
	Kind    string `json:"kind"`
	SinceMS int64  `json:"since_ms"`
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
		Outbox: Outbox{Pings: []Ping{}, Sending: []Sending{}, Gate: gate},
		Run:    run,
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
			c.need(item.Size() <= l.size, fmt.Sprintf("a %s entry is over %d bytes", l.name, l.size))
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
	c.need(o.Pings != nil && o.Sending != nil && o.Gate.Recent != nil, "an outbox list is not a list")
	for _, p := range o.Pings {
		p.check(c, server)
	}
	for _, s := range o.Sending {
		c.need(txnRe.MatchString(s.Txn) && slices.Contains(storeRooms, s.Room) && slices.Contains(sendKinds, s.Kind) && validMS(s.SinceMS), "a send in flight is not well formed")
	}
	c.need(o.Gate.Credit >= 0 && validMS(o.Gate.AtMS), "the gate's credit is negative or its time is not a time")
	for _, k := range o.Gate.Recent {
		c.need(sha256Re.MatchString(k.Key) && validMS(k.AtMS), "a gate key is not a SHA-256 and a time")
	}
}

func (p *Ping) check(c *rules, server string) {
	rule, known := proto.Verbs[p.Verb]
	c.need(proto.ValidEventID(p.ID) && known && rule.ExpectsAnswer && validMS(p.SentMS) && p.SentMS <= p.DueMS && validMS(p.DueMS), "a ping is not well formed")
	ref := p.Ref != nil && validRef(*p.Ref)
	c.need(ref && rule.Ref != proto.Forbidden || p.Ref == nil && rule.Ref != proto.Required, "a ping's ref is not as its verb needs")
	c.need(len(p.To) >= 1 && len(p.To) <= capPingTargets && distinct(p.To), fmt.Sprintf("a ping is not to 1 to %d distinct ids", capPingTargets))
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
		c.need(e.Reason != nil && *e.Reason == ReasonRestart && e.State == StateSkipped && e.Txn == nil, "a run that posts nothing is a skipped restart with no txn")
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

// validItemID is an item's id: an event id, or a TIMEOUT's key (proto.TimeoutKey).
func validItemID(id string) bool {
	event, local, timeout := strings.Cut(id, "~")
	if !timeout {
		return proto.ValidEventID(id)
	}
	_, handle := proto.ParseHandle(local)
	return proto.ValidEventID(event) && (handle || proto.ValidHumanName(local))
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
func (s *Store) AddPing(p Ping) error { return grow(&s.Outbox.Pings, p, capPings, "pings") }

// AddSending records a send in flight.
func (s *Store) AddSending(x Sending) error {
	return grow(&s.Outbox.Sending, x, capSending, "sending")
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
	s.Run.check(c)
	counts := []int{len(s.Pending), len(s.Awaiting), len(s.Missed), len(s.Ledger), len(s.Receipts), len(s.Outbox.Pings), len(s.Outbox.Sending), len(s.Outbox.Gate.Recent)}
	for i, l := range listCaps {
		c.need(counts[i] <= l.count, fmt.Sprintf("%s holds more than %d entries", l.name, l.count))
	}
}

// checkPending holds every pending entry well formed, each id once, and agent items within
// the cap less the reserve.
func (s *Store) checkPending(c *rules, server string) {
	seen := map[string]bool{}
	agent := 0
	for _, p := range s.Pending {
		c.need(validPendingID(p.ID, p.Item) && slices.Contains(storeRooms, p.Room) && slices.Contains(storeItems, p.Item) &&
			validMS(p.SentMS) && validUser(p.From, server) && (p.Handed == nil || nonceRe.MatchString(*p.Handed)), "a pending entry is not well formed")
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
		c.need(validItemID(l.ID) && slices.Contains(storeRooms, l.Room) && slices.Contains(storeItems, l.Item) &&
			(l.Thread == nil || proto.ValidEventID(*l.Thread)) && validUser(l.From, server) && validMS(l.AtMS), "a ledger entry is not well formed")
	}
	for _, r := range s.Receipts {
		c.need(proto.ValidEventID(r.ID) && slices.Contains(storeRooms, r.Room), "a receipt is not well formed")
	}
}
