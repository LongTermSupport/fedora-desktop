package proto

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

var (
	workerEvent = "$" + strings.Repeat("w", 43)
	aliceEvent  = "$" + strings.Repeat("a", 43)
	editedEvent = "$" + strings.Repeat("e", 43)
	quietEvent  = "$" + strings.Repeat("q", 43)
	lostEvent   = "$" + strings.Repeat("l", 43)
)

// reachLookup knows an event the worker sent, one alice sent, an earlier human message that
// was addressed to a roster agent and one that was not; lostEvent is unknown to it.
func reachLookup() fakeLookup {
	return fakeLookup{events: map[string]Related{
		workerEvent: {Sender: workerID, RoomID: mainRoom},
		aliceEvent:  {Sender: aliceID, RoomID: mainRoom},
		editedEvent: {Sender: aliceID, RoomID: mainRoom, AddressedToRosterAgent: true},
		quietEvent:  {Sender: aliceID, RoomID: mainRoom},
	}}
}

func replyTo(id string) string { return `,"m.relates_to":{"m.in_reply_to":{"event_id":"` + id + `"}}` }

func inThread(id string) string {
	return `,"m.relates_to":{"rel_type":"m.thread","event_id":"` + id + `","is_falling_back":true,"m.in_reply_to":{"event_id":"` + id + `"}}`
}

func editOf(id, newExtra string) string {
	return `{"msgtype":"m.text","body":"* fixed","m.new_content":{"msgtype":"m.text","body":"fixed"` + newExtra +
		`},"m.relates_to":{"rel_type":"m.replace","event_id":"` + id + `"}}`
}

func TestHumanReaderIsAnAgentThatHearsHumans(t *testing.T) {
	r := testRoster(t)
	for id, want := range map[string]bool{
		coordID: true, workerID: true, lxcWorkerID: false, parkedID: false, probeID: false, aliceID: false, adminID: false,
	} {
		if got := r.HumanReader(id); got != want {
			t.Errorf("HumanReader(%s) = %v, want %v", id, got, want)
		}
	}
}

// TestReachedNoAgent is gap 11's predicate, a row per clause, each with the related event known
// to the lookup and unknown to it (n3-D20: a miss reaches no agent).
func TestReachedNoAgent(t *testing.T) {
	cases := map[string]struct {
		content string
		want    bool
	}{
		"unaddressed":                       {humanContent(""), true},
		"mentions a worker":                 {humanContent(mention(workerID)), false},
		"mentions the coordinator":          {humanContent(mention(coordID)), false},
		"mentions a worker deaf to humans":  {humanContent(mention(lxcWorkerID)), true},
		"mentions a parked member":          {humanContent(mention(parkedID)), true},
		"mentions the probe":                {humanContent(mention(probeID)), true},
		"mentions a human only":             {humanContent(mention(aliceID)), true},
		"@room":                             {humanContent(`,"m.mentions":{"room":true}`), false},
		"reply to an agent's event":         {humanContent(replyTo(workerEvent)), false},
		"reply to a human's event":          {humanContent(replyTo(aliceEvent)), true},
		"reply to an unknown event":         {humanContent(replyTo(lostEvent)), true},
		"in an agent's thread":              {humanContent(inThread(workerEvent)), false},
		"in a human's thread":               {humanContent(inThread(aliceEvent)), true},
		"in an unknown thread":              {humanContent(inThread(lostEvent)), true},
		"edit of an addressed message":      {editOf(editedEvent, ""), false},
		"edit of an unaddressed message":    {editOf(quietEvent, ""), true},
		"edit of an unknown message":        {editOf(lostEvent, ""), true},
		"edit that now mentions the worker": {editOf(quietEvent, mention(workerID)), false},
	}
	r := testRoster(t)
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h, reason := DecodeHuman(mustParse(t, tc.content))
			if reason != "" {
				t.Fatalf("decode: %s", reason)
			}
			if got := ReachedNoAgent(h, r, reachLookup()); got != tc.want {
				t.Fatalf("ReachedNoAgent = %v, want %v", got, tc.want)
			}
		})
	}
}

// A reply to agent X's event is reached at every member alike: the predicate has no self, so
// the member Y that ignores it as not addressed to Y does not count it unheard.
func TestAReplyToAnotherAgentIsReachedAtEveryMember(t *testing.T) {
	r := testRoster(t)
	content := mustParse(t, event(aliceID, 0, humanContent(replyTo(workerEvent))))
	for _, self := range []string{coordID, workerID} {
		j := judge(t, self, PurposeMain)
		j.Lookup = reachLookup()
		v := j.Judge(content)
		if ReachedNoAgent(v.Human, r, j.Lookup) {
			t.Errorf("at %s (%v %s) the reply reached no agent", self, v.Outcome, v.Reason)
		}
	}
	if v := judge(t, coordID, PurposeMain); v.Judge(content).Reason != IgnoreNotAddressed {
		t.Error("the coordinator does not ignore a reply to the worker as not addressed")
	}
}

// An m.emote never reaches the predicate: DecodeHuman refuses it, so its addressing is never
// computed (UH1 counts it at every member).
func TestAnEmoteNeverReachesThePredicate(t *testing.T) {
	_, reason := DecodeHuman(mustParse(t, `{"msgtype":"m.emote","body":"waves"`+mention(workerID)+`}`))
	if reason != IgnoreType {
		t.Fatalf("an emote decoded with %q", reason)
	}
}

func TestRelationIDs(t *testing.T) {
	r := testRoster(t)
	threadReply := `,"m.relates_to":{"rel_type":"m.thread","event_id":"` + workerEvent + `","m.in_reply_to":{"event_id":"` + aliceEvent + `"}}`
	cases := map[string]struct {
		raw  string
		want []string
	}{
		"reply":                       {event(aliceID, 0, humanContent(replyTo(workerEvent))), []string{workerEvent}},
		"thread, falling back":        {event(aliceID, 0, humanContent(inThread(workerEvent))), []string{workerEvent}},
		"reply inside a thread":       {event(aliceID, 0, humanContent(threadReply)), []string{aliceEvent, workerEvent}},
		"thread reply to its root":    {event(aliceID, 0, humanContent(strings.Replace(threadReply, aliceEvent, workerEvent, 1))), []string{workerEvent}},
		"edit":                        {event(aliceID, 0, editOf(quietEvent, "")), []string{quietEvent}},
		"no relation":                 {event(aliceID, 0, humanContent(mention(workerID))), nil},
		"an agent's reply":            {event(workerID, 0, humanContent(replyTo(aliceEvent))), nil},
		"an emote":                    {event(aliceID, 0, `{"msgtype":"m.emote","body":"x"`+replyTo(workerEvent)+`}`), nil},
		"a fallback-only reply":       {event(aliceID, 0, `{"msgtype":"m.text","body":"> <@x:y> hi"`+replyTo(workerEvent)+`}`), nil},
		"a carried item":              {event(aliceID, 0, humanContent(humanCarried+replyTo(workerEvent))), nil},
		"not a message":               {`{"type":"m.room.redaction","event_id":"` + replyEvent + `","sender":"` + aliceID + `","content":{}}`, nil},
		"a malformed event":           {`{"type":"m.room.message","event_id":"bad","sender":"` + aliceID + `","origin_server_ts":1,"content":{}}`, nil},
		"a reply with a bad event id": {event(aliceID, 0, humanContent(replyTo("bad"))), nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := RelationIDs(mustParse(t, tc.raw), r); !slices.Equal(got, tc.want) {
				t.Fatalf("RelationIDs = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSyncFilterReAddDiffersOnlyInTheLimit is SY1 (c)'s first sync filter (n3-D5).
func TestSyncFilterReAddDiffersOnlyInTheLimit(t *testing.T) {
	r := testRoster(t)
	for _, self := range []string{workerID, coordID} {
		normal, readd := SyncFilter(r, anchors(self)).Compact(), SyncFilterReAdd(r, anchors(self)).Compact()
		if bytes.Count(normal, []byte(`"limit":100`)) != 1 {
			t.Fatalf("the filter is %s", normal)
		}
		if want := bytes.Replace(normal, []byte(`"limit":100`), []byte(`"limit":0`), 1); !bytes.Equal(readd, want) {
			t.Errorf("re-add filter\n%s\nwant\n%s", readd, want)
		}
	}
}
