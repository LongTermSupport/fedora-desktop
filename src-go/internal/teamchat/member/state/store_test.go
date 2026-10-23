package state

import (
	"encoding/json"
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
	txn2    = "tc0123456789abcdef0123456789abcde0"
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
	s.Pending = append(s.Pending, Pending{ID: eventID(8), Room: proto.PurposeMain, Item: records.KindPing, SentMS: 12, From: otherID, Verb: new("review")})
	s.Ledger = []Ledger{
		{ID: eventID(5), Room: proto.PurposeMain, Item: records.KindPeer, Thread: new(eventID(6)), From: otherID, AtMS: 15},
		{ID: eventID(9), Room: proto.PurposeMain, Item: records.KindPing, From: otherID, AtMS: 15, Verb: new(proto.VerbDone)},
	}
	s.Receipts = []Receipt{{ID: eventID(5), Room: proto.PurposeMain}}
	s.Outbox.Pings = []Ping{{ID: eventID(7), Verb: "review", Ref: new("commit:owner/repo@" + sha), To: []string{otherID, aliceID}, SentMS: 16, DueMS: 17, Answered: []int{1}, Reported: []int{}}}
	s.Outbox.Sending = []Sending{
		{Txn: txn, Room: proto.PurposeMain, Kind: SendText, SinceMS: 18, Type: proto.MessageType, Content: json.RawMessage(`{"body":"hi","msgtype":"m.text"}`)},
		{
			Txn: txn2, Room: proto.PurposeMain, Kind: SendPing, SinceMS: 18, Type: proto.MessageType, Content: json.RawMessage(`{"body":"review"}`), Tried: true,
			Ping: &SendingPing{Verb: "review", Ref: new("commit:owner/repo@" + sha), To: []string{otherID}, TimeoutMS: 900_000},
		},
	}
	s.Outbox.Gate.Recent = []GateKey{{Key: strings.Repeat("ab", 32), AtMS: 19}}
	s.Outbox.LastSent = new(eventID(10))
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
		"ping same id twice":        func(s *Store) { s.Outbox.Pings = append(s.Outbox.Pings, s.Outbox.Pings[0]) },
		"last sent bad id":          func(s *Store) { s.Outbox.LastSent = new("$short") },
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

// n3d-D1, n3d-D30: a send in flight carries a strict object of content and, for an
// answer-expecting ping, its outbox part; a ping item carries its verb, no other item does.
func TestN3dD1SendingAndVerbRules(t *testing.T) {
	for name, mutate := range map[string]func(*Store){
		"sending bad type":          func(s *Store) { s.Outbox.Sending[0].Type = "m.reaction" },
		"sending content a list":    func(s *Store) { s.Outbox.Sending[0].Content = json.RawMessage(`["hi"]`) },
		"sending content a string":  func(s *Store) { s.Outbox.Sending[0].Content = json.RawMessage(`"hi"`) },
		"sending content null":      func(s *Store) { s.Outbox.Sending[0].Content = nil },
		"sending content float":     func(s *Store) { s.Outbox.Sending[0].Content = json.RawMessage(`{"n":1.5}`) },
		"sending content twice key": func(s *Store) { s.Outbox.Sending[0].Content = json.RawMessage(`{"n":1,"n":2}`) },
		"sending content over cap": func(s *Store) {
			s.Outbox.Sending[0].Content = json.RawMessage(`{"body":"` + strings.Repeat("x", maxSendContent-10) + `"}`)
		},
		"sending ping on a text":    func(s *Store) { s.Outbox.Sending[0].Ping = s.Outbox.Sending[1].Ping },
		"sending ping answer verb":  func(s *Store) { s.Outbox.Sending[1].Ping.Verb, s.Outbox.Sending[1].Ping.Ref = proto.VerbDone, nil },
		"sending ping unknown verb": func(s *Store) { s.Outbox.Sending[1].Ping.Verb = "poke" },
		"sending ping no ref":       func(s *Store) { s.Outbox.Sending[1].Ping.Ref = nil },
		"sending ping bad ref":      func(s *Store) { s.Outbox.Sending[1].Ping.Ref = new("nothing") },
		"sending ping no target":    func(s *Store) { s.Outbox.Sending[1].Ping.To = []string{} },
		"sending ping null targets": func(s *Store) { s.Outbox.Sending[1].Ping.To = nil },
		"sending ping twice target": func(s *Store) { s.Outbox.Sending[1].Ping.To = []string{otherID, otherID} },
		"sending ping foreign user": func(s *Store) { s.Outbox.Sending[1].Ping.To = []string{"@alice:other"} },
		"sending ping 17 targets":   func(s *Store) { s.Outbox.Sending[1].Ping.To = targets(MaxPingTargets + 1) },
		"sending ping no timeout":   func(s *Store) { s.Outbox.Sending[1].Ping.TimeoutMS = 0 },
		"pending ping no verb":      func(s *Store) { s.Pending[2].Verb = nil },
		"pending ping bad verb":     func(s *Store) { s.Pending[2].Verb = new("poke") },
		"pending human with verb":   func(s *Store) { s.Pending[0].Verb = new("review") },
		"ledger ping no verb":       func(s *Store) { s.Ledger[1].Verb = nil },
		"ledger peer with verb":     func(s *Store) { s.Ledger[0].Verb = new(proto.VerbAck) },
	} {
		s := sampleStore(t)
		mutate(&s)
		if _, err := s.encode(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: encode = %v, want ErrInvalid", name, err)
		}
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
	longLocal := longID[1 : len(longID)-len(server)-1]
	s.Pending, s.Ledger, s.Receipts = nil, nil, nil
	s.Pending = []Pending{}
	for i := range capPending {
		// The largest entries: an agent's ping with its verb, and a TIMEOUT keyed on the
		// longest localpart in the reserve.
		id, item, verb := eventID(i)+"~"+longLocal, records.KindTimeout, (*string)(nil)
		if i < capPending-pendingReserve {
			id, item, verb = eventID(i), records.KindPing, new(proto.VerbBlocked)
		}
		s.Pending = append(s.Pending, Pending{ID: id, Room: proto.PurposeQuestions, Item: item, SentMS: farMS, From: longID, Handed: new("0badf00d"), Verb: verb})
	}
	for i := range capLedger {
		s.AddLedger(Ledger{ID: eventID(i) + "~" + longLocal, Room: proto.PurposeQuestions, Item: records.KindTimeout, Thread: new(eventID(i)), From: longID, AtMS: farMS})
	}
	for i := range capReceipts {
		s.Receipts = append(s.Receipts, Receipt{ID: eventID(i), Room: proto.PurposeQuestions})
	}
	s.Outbox.Pings = []Ping{}
	for i := range MaxPings {
		to := make([]string, 0, MaxPingTargets)
		for j := range MaxPingTargets {
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
	for range MaxSending {
		s.Outbox.Sending = append(s.Outbox.Sending, largestSending(t))
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
	// n3d-D1: the file cap is that sum rounded up to the next 256 KiB, no more.
	const step = 256 << 10
	if want := (bound + step - 1) / step * step; MaxStore != want {
		t.Fatalf("the file cap is %d, want the sum %d rounded up to %d", MaxStore, bound, want)
	}
}

// n3d-D1, n3d-D24: 32 sends in flight of at most 20 KiB each, and 96 gate keys; the largest
// send its grammar allows is within its entry size, and a full sending list round-trips.
func TestN3dD1SendingAndGateCaps(t *testing.T) {
	if MaxSending != 32 || sizeSending != 20<<10 || capGateKeys != 96 || maxSendContent != 16384 {
		t.Fatalf("caps: sending %d of %d bytes, gate keys %d, content %d", MaxSending, sizeSending, capGateKeys, maxSendContent)
	}
	entry, err := json.Marshal(largestSending(t))
	check(t, err)
	if len(entry) > sizeSending {
		t.Fatalf("the largest send in flight is %d bytes, over %d", len(entry), sizeSending)
	}
	s := fullStore(t)
	data, err := s.encode()
	check(t, err)
	back, err := DecodeStore(data)
	check(t, err)
	if len(back.Outbox.Sending) != MaxSending || string(back.Outbox.Sending[0].Content) != string(s.Outbox.Sending[0].Content) {
		t.Fatalf("a full sending list did not round-trip: %d entries", len(back.Outbox.Sending))
	}
	if err := back.AddSending(largestSending(t)); !errors.Is(err, ErrFull) {
		t.Fatalf("a 33rd send in flight = %v", err)
	}
	if err := back.AddGateKey(GateKey{}); !errors.Is(err, ErrFull) {
		t.Fatalf("a 97th gate key = %v", err)
	}
}

// n3d-D1: a ping that expects no answer is sent with no outbox part; a sample store
// round-trips its sends with their content and ping.
func TestN3dD1SendingRoundTrips(t *testing.T) {
	s := sampleStore(t)
	s.Outbox.Sending = append(s.Outbox.Sending, Sending{Txn: txn, Room: proto.PurposeMain, Kind: SendPing, SinceMS: 1, Type: proto.MessageType, Content: json.RawMessage(`{"body":"done"}`)})
	data, err := s.encode()
	check(t, err)
	back, err := DecodeStore(data)
	check(t, err)
	got := back.Outbox.Sending[1]
	if !got.Tried || got.Ping == nil || got.Ping.TimeoutMS != 900_000 || *got.Ping.Ref != "commit:owner/repo@"+sha || string(got.Content) != `{"body":"review"}` {
		t.Fatalf("the ping in flight read back as %+v", got)
	}
	if back.Outbox.Sending[2].Ping != nil || *back.Pending[2].Verb != "review" || *back.Ledger[1].Verb != proto.VerbDone || back.Pending[0].Verb != nil {
		t.Fatalf("read back %+v", back)
	}
}

// TestStoreListCapsFireFirst holds each list's cap, entry count and entry size, firing before
// the file cap: each grown store is refused by its own rule.
func TestStoreListCapsFireFirst(t *testing.T) {
	count := func(list string, limit int) string { return fmt.Sprintf("%s holds more than %d entries", list, limit) }
	for name, c := range map[string]struct {
		grow func(*Store)
		rule string
	}{
		"pending": {func(s *Store) {
			p := s.Pending[len(s.Pending)-1]
			p.ID = eventID(capPending) + p.ID[len(eventID(0)):]
			s.Pending = append(s.Pending, p)
		}, count("pending", capPending)},
		"pending agent": {func(s *Store) {
			last := &s.Pending[len(s.Pending)-1]
			last.ID, last.Item, last.Verb = eventID(capPending), records.KindPing, new(proto.VerbDone)
		}, "agent items"},
		"ledger":   {func(s *Store) { s.Ledger = append(s.Ledger, s.Ledger[0]) }, count("ledger", capLedger)},
		"receipts": {func(s *Store) { s.Receipts = append(s.Receipts, s.Receipts[0]) }, count("receipts", capReceipts)},
		"pings": {func(s *Store) {
			p := s.Outbox.Pings[0]
			p.ID = eventID(MaxPings)
			s.Outbox.Pings = append(s.Outbox.Pings, p)
		}, count("pings", MaxPings)},
		"ping targets":  {func(s *Store) { s.Outbox.Pings[0].To = append(s.Outbox.Pings[0].To, otherID) }, "a ping is not to 1 to 16"},
		"awaiting":      {func(s *Store) { s.Awaiting = append(s.Awaiting, s.Awaiting[0]) }, count("awaiting", capAwaiting)},
		"awaiting size": {func(s *Store) { s.Awaiting[0].IDs = append(s.Awaiting[0].IDs, manyIDs(40)...) }, "an entry of awaiting is over"},
		"missed":        {func(s *Store) { s.Missed = append(s.Missed, s.Missed[0]) }, count("missed", capMissed)},
		"missed size":   {func(s *Store) { s.Missed[0].IDs = manyIDs(14) }, "an entry of missed is over"},
		"sending":       {func(s *Store) { s.Outbox.Sending = append(s.Outbox.Sending, s.Outbox.Sending[0]) }, count("sending", MaxSending)},
		"gate keys": {func(s *Store) {
			s.Outbox.Gate.Recent = append(s.Outbox.Gate.Recent, s.Outbox.Gate.Recent[0])
		}, count("gate keys", capGateKeys)},
		"transcript path": {func(s *Store) {
			s.Awaiting[0].Transcript.Path = "/" + strings.Repeat("p", maxTranscriptPath) + ".jsonl"
		}, "transcript is not"},
		"pending repeated": {func(s *Store) { s.Pending[1].ID = s.Pending[0].ID }, "holds an id twice"},
	} {
		s := fullStore(t)
		c.grow(&s)
		_, err := s.encode()
		var inv *InvalidError
		if !errors.As(err, &inv) || !strings.Contains(inv.Rule, c.rule) {
			t.Errorf("%s: encode = %v, want the rule %q", name, err, c.rule)
		}
	}
}

// longRef is a path reference of A 9.1's longest, 700 characters.
func longRef(t *testing.T) string {
	t.Helper()
	segs := make([]string, 0, 8)
	for range 7 {
		segs = append(segs, strings.Repeat("p", 64))
	}
	segs = append(segs, strings.Repeat("q", 58))
	ref := "path:" + strings.Repeat("o", 39) + "/" + strings.Repeat("r", 100) + "@" + sha + ":" + strings.Join(segs, "/")
	if len(ref) != proto.MaxRefLen || !validRef(ref) {
		t.Fatalf("the long reference is %d characters, valid %v", len(ref), validRef(ref))
	}
	return ref
}

// largestSending is a send in flight as large as its grammar lets one be: the content at its
// cap, a review ping to 16 of the longest user ids with the longest reference.
func largestSending(t *testing.T) Sending {
	t.Helper()
	to := make([]string, 0, MaxPingTargets)
	for j := range MaxPingTargets {
		id := fmt.Sprintf("@%s.%d+%s.podman:%s", strings.Repeat("r", 40), 999984+j, strings.Repeat("s", 48), server)
		if len(id) != maxUserID {
			t.Fatalf("a long user id is %d bytes", len(id))
		}
		to = append(to, id)
	}
	content := `{"body":"` + strings.Repeat("x", maxSendContent-len(`{"body":""}`)) + `"}`
	return Sending{
		Txn: txn, Room: proto.PurposeQuestions, Kind: SendPing, SinceMS: farMS, Type: proto.MessageType,
		Content: json.RawMessage(content), Tried: true,
		Ping: &SendingPing{Verb: "review", Ref: new(longRef(t)), To: to, TimeoutMS: farMS},
	}
}

// targets is n distinct agents of the team's server.
func targets(n int) []string {
	ids := make([]string, 0, n)
	for i := range n {
		ids = append(ids, fmt.Sprintf("@r.%d+s.podman:%s", i+1, server))
	}
	return ids
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
		"pings":    {MaxPings, func() error { return s.AddPing(Ping{}) }},
		"sending":  {MaxSending, func() error { return s.AddSending(Sending{}) }},
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

// A run that posts nothing is a skipped restart or launch failure (n3c-D20), never an exit.
func TestRunEndedNone(t *testing.T) {
	s := sampleStore(t)
	s.Run.Ended = &RunEnd{Notice: NoticeNone, Reason: new(ReasonLaunchFailed), State: StateSkipped}
	if _, err := s.encode(); err != nil {
		t.Fatalf("a launch failure's run end: %v", err)
	}
	s.Run.Ended = &RunEnd{Notice: NoticeNone, Reason: new("exit"), State: StateSkipped}
	if _, err := s.encode(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an exit that posts nothing: %v", err)
	}
}
