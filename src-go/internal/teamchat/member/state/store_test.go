package state

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const (
	server  = "alpha.teamchat.internal"
	agentID = "@repo.2+site.podman:" + server
	otherID = "@repo.3+site.lxc:" + server
	aliceID = "@alice:" + server
	sha     = "0123456789abcdef0123456789abcdef01234567"
	txn     = "tc0123456789abcdef0123456789abcdef"
)

// eventID is the n-th distinct event id.
func eventID(n int) string {
	s := fmt.Sprintf("%043d", n)
	return "$" + s
}

// sampleStore is a store holding one entry of each kind.
func sampleStore(t *testing.T) Store {
	t.Helper()
	s := NewStore(teamID(t), agentID, StoreRun{ID: runID(t), StartedMS: 1}, Gate{Credit: 540_000, AtMS: 1})
	s.Sync = Sync{NextBatch: "s72_1", RestoreEpoch: 2, FloorMS: map[string]int64{proto.PurposeMain: 10, proto.PurposeQuestions: 9}}
	s.Pending = []Pending{
		{ID: eventID(1), Room: proto.PurposeMain, Item: records.KindHuman, SentMS: 11, From: aliceID},
		{ID: eventID(2) + "~repo.3+site.lxc", Room: proto.PurposeMain, Item: records.KindTimeout, SentMS: 12, From: otherID, Repeat: true, Held: true, Restored: true, Handed: new("0badf00d")},
	}
	s.Awaiting = []Awaiting{{Nonce: "3f9c2a71", IDs: []string{eventID(3)}, Transcript: TranscriptMark{Path: "/home/u/.claude/projects/p/x.jsonl", Offset: 81920}, AtMS: 13, Written: WrittenYes}}
	s.Missed = []Missed{{Nonce: "3f9c2a72", IDs: []string{eventID(4)}, Offset: 90112, AtMS: 14}}
	s.Ledger = []Ledger{{ID: eventID(5), Room: proto.PurposeMain, Item: records.KindPeer, Thread: new(eventID(6)), From: otherID, AtMS: 15}}
	s.Receipts = []Receipt{{ID: eventID(5), Room: proto.PurposeMain}}
	s.Outbox.Pings = []Ping{{ID: eventID(7), Verb: "review", Ref: new("commit:owner/repo@" + sha), To: []string{otherID, aliceID}, SentMS: 16, DueMS: 17, Answered: []int{1}, Reported: []int{}}}
	s.Outbox.Sending = []Sending{{Txn: txn, Room: proto.PurposeMain, Kind: SendText, SinceMS: 18}}
	s.Outbox.Gate.Recent = []GateKey{{Key: strings.Repeat("ab", 32), AtMS: 19}}
	s.Run.Ended = &RunEnd{Notice: NoticeLeft, Reason: new("exit"), State: StateAttempted, Txn: new(txn), AtMS: 20}
	return s
}

func TestStoreRoundTrip(t *testing.T) {
	s := sampleStore(t)
	data, err := s.encode()
	check(t, err)
	got, err := DecodeStore(data)
	check(t, err)
	again, err := got.encode()
	check(t, err)
	if string(again) != string(data) {
		t.Fatalf("round trip changed the store:\n%s\n%s", data, again)
	}
	fresh := NewStore(teamID(t), agentID, StoreRun{ID: runID(t), StartedMS: 1}, Gate{})
	if !fresh.Sync.Fresh || fresh.Seat != "2" {
		t.Fatalf("NewStore = %+v", fresh)
	}
	if _, err := fresh.encode(); err != nil {
		t.Fatalf("a new store does not encode: %v", err)
	}
	if NewStore(teamID(t), "@alice:"+server, StoreRun{ID: runID(t)}, Gate{}).Seat != "" {
		t.Fatal("a human's id gave a seat")
	}
}

func TestStoreRules(t *testing.T) {
	for name, mutate := range map[string]func(*Store){
		"seat not the handle's":     func(s *Store) { s.Seat = "3" },
		"user id a human":           func(s *Store) { s.UserID = aliceID },
		"fresh with a position":     func(s *Store) { s.Sync.Fresh = true },
		"no position, not fresh":    func(s *Store) { s.Sync.NextBatch = "" },
		"position with a space":     func(s *Store) { s.Sync.NextBatch = "a b" },
		"long position":             func(s *Store) { s.Sync.NextBatch = strings.Repeat("a", maxNextBatch+1) },
		"negative epoch":            func(s *Store) { s.Sync.RestoreEpoch = -1 },
		"floor of an unknown room":  func(s *Store) { s.Sync.FloorMS["side"] = 1 },
		"negative floor":            func(s *Store) { s.Sync.FloorMS[proto.PurposeMain] = -1 },
		"null floors":               func(s *Store) { s.Sync.FloorMS = nil },
		"null pending":              func(s *Store) { s.Pending = nil },
		"pending bad id":            func(s *Store) { s.Pending[0].ID = "$short" },
		"pending timeout key":       func(s *Store) { s.Pending[0].ID = eventID(9) + "~alice" },
		"pending bad room":          func(s *Store) { s.Pending[0].Room = "lobby" },
		"pending bad item":          func(s *Store) { s.Pending[0].Item = "fact" },
		"pending bad from":          func(s *Store) { s.Pending[0].From = "@alice:other" },
		"pending bad handed":        func(s *Store) { s.Pending[0].Handed = new("XYZ") },
		"pending bad time":          func(s *Store) { s.Pending[0].SentMS = -1 },
		"pending same id twice":     func(s *Store) { s.Pending[1].ID = s.Pending[0].ID },
		"awaiting bad nonce":        func(s *Store) { s.Awaiting[0].Nonce = "3f9c2a7" },
		"awaiting no ids":           func(s *Store) { s.Awaiting[0].IDs = []string{} },
		"awaiting repeated id":      func(s *Store) { s.Awaiting[0].IDs = []string{eventID(3), eventID(3)} },
		"awaiting relative path":    func(s *Store) { s.Awaiting[0].Transcript.Path = "x.jsonl" },
		"awaiting unclean path":     func(s *Store) { s.Awaiting[0].Transcript.Path = "/a/../x.jsonl" },
		"awaiting not a transcript": func(s *Store) { s.Awaiting[0].Transcript.Path = "/a/x.json" },
		"awaiting negative offset":  func(s *Store) { s.Awaiting[0].Transcript.Offset = -1 },
		"awaiting bad written":      func(s *Store) { s.Awaiting[0].Written = "no" },
		"awaiting bad stop":         func(s *Store) { s.Awaiting[0].StopMS = new(int64(-1)) },
		"missed bad nonce":          func(s *Store) { s.Missed[0].Nonce = "" },
		"missed negative offset":    func(s *Store) { s.Missed[0].Offset = -1 },
		"ledger bad thread":         func(s *Store) { s.Ledger[0].Thread = new("t") },
		"ledger bad item":           func(s *Store) { s.Ledger[0].Item = "more" },
		"receipt bad id":            func(s *Store) { s.Receipts[0].ID = eventID(1) + "~alice" },
		"ping unknown verb":         func(s *Store) { s.Outbox.Pings[0].Verb = "poke" },
		"ping answer verb":          func(s *Store) { s.Outbox.Pings[0].Verb = proto.VerbAck },
		"ping no ref":               func(s *Store) { s.Outbox.Pings[0].Ref = nil },
		"ping ref on halt":          func(s *Store) { s.Outbox.Pings[0].Verb = proto.VerbHalt },
		"ping bad ref":              func(s *Store) { s.Outbox.Pings[0].Ref = new("nothing") },
		"ping no target":            func(s *Store) { s.Outbox.Pings[0].To = []string{} },
		"ping repeated target":      func(s *Store) { s.Outbox.Pings[0].To = []string{otherID, otherID} },
		"ping due before sent":      func(s *Store) { s.Outbox.Pings[0].DueMS = 1 },
		"ping index out of range":   func(s *Store) { s.Outbox.Pings[0].Answered = []int{2} },
		"ping indexes not sorted":   func(s *Store) { s.Outbox.Pings[0].Reported = []int{1, 0} },
		"ping null reported":        func(s *Store) { s.Outbox.Pings[0].Reported = nil },
		"sending bad txn":           func(s *Store) { s.Outbox.Sending[0].Txn = "tc1" },
		"sending bad kind":          func(s *Store) { s.Outbox.Sending[0].Kind = "file" },
		"gate negative credit":      func(s *Store) { s.Outbox.Gate.Credit = -1 },
		"gate bad key":              func(s *Store) { s.Outbox.Gate.Recent[0].Key = "AB" },
		"gate null recent":          func(s *Store) { s.Outbox.Gate.Recent = nil },
		"run ended bad notice":      func(s *Store) { s.Run.Ended.Notice = "bye" },
		"run ended bad reason":      func(s *Store) { s.Run.Ended.Reason = new("restart") },
		"run ended bad state":       func(s *Store) { s.Run.Ended.State = StateSkipped },
		"run ended no txn":          func(s *Store) { s.Run.Ended.Txn = nil },
		"run none with a txn": func(s *Store) {
			s.Run.Ended = &RunEnd{Notice: NoticeNone, Reason: new(ReasonRestart), State: StateSkipped, Txn: new(txn)}
		},
		"run none not skipped": func(s *Store) {
			s.Run.Ended = &RunEnd{Notice: NoticeNone, Reason: new(ReasonRestart), State: StateSent}
		},
	} {
		s := sampleStore(t)
		mutate(&s)
		if _, err := s.encode(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: encode = %v, want ErrInvalid", name, err)
		}
	}
	s := sampleStore(t)
	s.Run.Ended = &RunEnd{Notice: NoticeNone, Reason: new(ReasonRestart), State: StateSkipped}
	if _, err := s.encode(); err != nil {
		t.Fatalf("a relaunch's run end: %v", err)
	}
	s.Run.Ended = &RunEnd{Notice: NoticeDisconnected, State: StateSent, Txn: new(txn)}
	if _, err := s.encode(); err != nil {
		t.Fatalf("a disconnected run end with no reason: %v", err)
	}
}

// TestStoreGateCreditIsInteger holds the integer credit of proto.Bucket: B's 9.5 is refused.
func TestStoreGateCreditIsInteger(t *testing.T) {
	s := sampleStore(t)
	data, err := s.encode()
	check(t, err)
	bad := strings.Replace(string(data), `"credit":540000`, `"credit":9.5`, 1)
	var inv *InvalidError
	if _, err := DecodeStore([]byte(bad)); !errors.As(err, &inv) || inv.Code != CodeJSON {
		t.Fatalf("a fractional credit = %v", err)
	}
}

func TestStoreRawRefusals(t *testing.T) {
	s := sampleStore(t)
	data, err := s.encode()
	check(t, err)
	for name, edit := range map[string][2]string{
		"null list":    {`"receipts":[`, `"receipts":null,"x":[`},
		"null handed":  {`"handed":"0badf00d"`, `"handed":7`},
		"bool for int": {`"offset":90112`, `"offset":true`},
		"upper key":    {`"seat":"2"`, `"SEAT":"2"`},
		"null run":     {`"run":{`, `"run":null,"y":{`},
	} {
		bad := strings.Replace(string(data), edit[0], edit[1], 1)
		if bad == string(data) {
			t.Fatalf("%s: the edit did not apply", name)
		}
		if _, err := DecodeStore([]byte(bad)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: DecodeStore = %v, want ErrInvalid", name, err)
		}
	}
}

// farMS is the last 13-digit millisecond time (the year 2286): B 2.3.2's entry sizes assume
// times of 13 digits, as every time until then is.
const farMS = 9_999_999_999_999

// fullStore is a store with every list at its cap, each entry as large as its grammar lets
// a real one be.
func fullStore(t *testing.T) Store {
	t.Helper()
	s := sampleStore(t)
	longID := "@" + strings.Repeat("r", 40) + ".999999+" + strings.Repeat("s", 48) + ".podman:" + server // A4's 128 bytes
	s.Pending, s.Ledger, s.Receipts = nil, nil, nil
	s.Pending = []Pending{}
	for i := range capPending {
		item := records.KindHuman
		if i < capPending-pendingReserve {
			item = records.KindPeer
		}
		s.Pending = append(s.Pending, Pending{ID: eventID(i), Room: proto.PurposeQuestions, Item: item, SentMS: farMS, From: longID, Handed: new("0badf00d")})
	}
	for i := range capLedger {
		s.AddLedger(Ledger{ID: eventID(i), Room: proto.PurposeQuestions, Item: records.KindTimeout, Thread: new(eventID(i)), From: longID, AtMS: farMS})
	}
	for i := range capReceipts {
		s.Receipts = append(s.Receipts, Receipt{ID: eventID(i), Room: proto.PurposeQuestions})
	}
	s.Outbox.Pings = []Ping{}
	for i := range capPings {
		to := make([]string, 0, capPingTargets)
		for j := range capPingTargets {
			to = append(to, fmt.Sprintf("@%s.%d+%s.podman:%s", strings.Repeat("r", 40), j+1, strings.Repeat("s", 40), server))
		}
		s.Outbox.Pings = append(s.Outbox.Pings, Ping{ID: eventID(i), Verb: "review", Ref: new("commit:owner/repo@" + sha), To: to, SentMS: 1, DueMS: farMS, Answered: []int{0, 1}, Reported: []int{2}})
	}
	s.Awaiting = []Awaiting{}
	for i := range capAwaiting {
		s.Awaiting = append(s.Awaiting, Awaiting{Nonce: fmt.Sprintf("%08x", i), IDs: []string{eventID(1), eventID(2)}, Transcript: TranscriptMark{Path: "/" + strings.Repeat("p", maxTranscriptPath-7) + ".jsonl", Offset: 1 << 40}, AtMS: farMS, Written: WrittenUnknown, StopMS: new(int64(farMS))})
	}
	for i := range capMissed {
		s.AddMissed(Missed{Nonce: fmt.Sprintf("%08x", i), IDs: []string{eventID(1), eventID(2), eventID(3)}, Offset: 1 << 40, AtMS: farMS})
	}
	s.Outbox.Sending = []Sending{}
	for range capSending {
		s.Outbox.Sending = append(s.Outbox.Sending, Sending{Txn: txn, Room: proto.PurposeQuestions, Kind: SendNotice, SinceMS: farMS})
	}
	s.Outbox.Gate.Recent = []GateKey{}
	for range capGateKeys {
		s.Outbox.Gate.Recent = append(s.Outbox.Gate.Recent, GateKey{Key: strings.Repeat("ab", 32), AtMS: farMS})
	}
	return s
}

// TestStoreFullFitsTheFileCap holds B 2.3.2: a store with every list full is under the file
// cap, by construction and by the caps' arithmetic, so a list cap is always the one that fires.
func TestStoreFullFitsTheFileCap(t *testing.T) {
	s := fullStore(t)
	data, err := s.encode()
	check(t, err)
	if len(data) > MaxStore {
		t.Fatalf("a full store is %d bytes, over the file cap", len(data))
	}
	bound := 4096 + maxNextBatch // every fixed field, the position at its longest
	for _, l := range listCaps {
		bound += l.count * (l.size + 1)
	}
	if bound > MaxStore {
		t.Fatalf("the list caps sum to %d bytes, over the file cap of %d", bound, MaxStore)
	}
}

// TestStoreListCapsFireFirst holds each list's cap, entry count and entry size, firing before
// the file cap.
func TestStoreListCapsFireFirst(t *testing.T) {
	for name, grow := range map[string]func(*Store){
		"pending": func(s *Store) {
			p := s.Pending[len(s.Pending)-1]
			p.ID = eventID(capPending)
			s.Pending = append(s.Pending, p)
		},
		"pending agent": func(s *Store) { s.Pending[len(s.Pending)-1].Item = records.KindPing },
		"ledger":        func(s *Store) { s.Ledger = append(s.Ledger, s.Ledger[0]) },
		"receipts":      func(s *Store) { s.Receipts = append(s.Receipts, s.Receipts[0]) },
		"pings":         func(s *Store) { s.Outbox.Pings = append(s.Outbox.Pings, s.Outbox.Pings[0]) },
		"ping targets":  func(s *Store) { s.Outbox.Pings[0].To = append(s.Outbox.Pings[0].To, otherID) },
		"awaiting":      func(s *Store) { s.Awaiting = append(s.Awaiting, s.Awaiting[0]) },
		"awaiting size": func(s *Store) { s.Awaiting[0].IDs = append(s.Awaiting[0].IDs, manyIDs(40)...) },
		"missed":        func(s *Store) { s.Missed = append(s.Missed, s.Missed[0]) },
		"missed size":   func(s *Store) { s.Missed[0].IDs = manyIDs(14) },
		"sending":       func(s *Store) { s.Outbox.Sending = append(s.Outbox.Sending, s.Outbox.Sending[0]) },
		"gate keys":     func(s *Store) { s.Outbox.Gate.Recent = append(s.Outbox.Gate.Recent, s.Outbox.Gate.Recent[0]) },
		"transcript path": func(s *Store) {
			s.Awaiting[0].Transcript.Path = "/" + strings.Repeat("p", maxTranscriptPath) + ".jsonl"
		},
		"pending repeated": func(s *Store) { s.Pending[1].ID = s.Pending[0].ID },
	} {
		s := fullStore(t)
		grow(&s)
		_, err := s.encode()
		var inv *InvalidError
		if !errors.As(err, &inv) || strings.Contains(inv.Rule, "larger than its cap") {
			t.Errorf("%s: encode = %v, want its list's own refusal", name, err)
		}
	}
}

func manyIDs(n int) []string {
	ids := make([]string, 0, n)
	for i := range n {
		ids = append(ids, eventID(100+i))
	}
	return ids
}

func TestStoreFileCap(t *testing.T) {
	data := `{"v":1,"pad":"` + strings.Repeat("x", MaxStore) + `"}`
	var inv *InvalidError
	if _, err := DecodeStore([]byte(data)); !errors.As(err, &inv) || !strings.Contains(inv.Rule, "larger than its cap") {
		t.Fatalf("an over-cap store = %v", err)
	}
}

// TestAddPendingKeepsHumanRoom holds MS2: agent items stop at the cap less 256; HUMAN and
// TIMEOUT items may use the rest.
func TestAddPendingKeepsHumanRoom(t *testing.T) {
	s := NewStore(teamID(t), agentID, StoreRun{ID: runID(t)}, Gate{})
	for i := range capPending - pendingReserve {
		check(t, s.AddPending(Pending{ID: eventID(i), Item: records.KindPeer}))
	}
	for _, item := range []string{records.KindPeer, records.KindPing, records.KindNotice} {
		if err := s.AddPending(Pending{ID: eventID(9000), Item: item}); !errors.Is(err, ErrFull) {
			t.Fatalf("an agent %s past the agent cap = %v", item, err)
		}
	}
	for i := range pendingReserve {
		item := records.KindHuman
		if i%2 == 1 {
			item = records.KindTimeout
		}
		check(t, s.AddPending(Pending{ID: eventID(5000 + i), Item: item}))
	}
	err := s.AddPending(Pending{ID: eventID(9001), Item: records.KindHuman})
	var full *FullError
	if !errors.As(err, &full) || full.List != "pending" || err.Error() != "state: the store's pending list is full" {
		t.Fatalf("a human past the cap = %v", err)
	}
}

func TestStoreListsRefuseOrRoll(t *testing.T) {
	s := NewStore(teamID(t), agentID, StoreRun{ID: runID(t)}, Gate{})
	for name, c := range map[string]struct {
		limit int
		add   func() error
	}{
		"awaiting": {capAwaiting, func() error { return s.AddAwaiting(Awaiting{}) }},
		"receipts": {capReceipts, func() error { return s.AddReceipt(Receipt{}) }},
		"pings":    {capPings, func() error { return s.AddPing(Ping{}) }},
		"sending":  {capSending, func() error { return s.AddSending(Sending{}) }},
		"gate":     {capGateKeys, func() error { return s.AddGateKey(GateKey{}) }},
	} {
		for range c.limit {
			check(t, c.add())
		}
		if err := c.add(); !errors.Is(err, ErrFull) {
			t.Errorf("%s past its cap = %v", name, err)
		}
	}
	for i := range capLedger + 2 {
		s.AddLedger(Ledger{ID: eventID(i)})
	}
	if len(s.Ledger) != capLedger || s.Ledger[0].ID != eventID(2) {
		t.Fatalf("the ledger did not roll: %d entries from %s", len(s.Ledger), s.Ledger[0].ID)
	}
	for i := range capMissed + 1 {
		s.AddMissed(Missed{Nonce: fmt.Sprintf("%08x", i)})
	}
	if len(s.Missed) != capMissed || s.Missed[0].Nonce != "00000001" {
		t.Fatalf("missed did not roll: %+v", s.Missed[0])
	}
}
