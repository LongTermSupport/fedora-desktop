package proto

import (
	"strconv"
	"strings"
	"testing"
)

func TestReasonSetsAreA76(t *testing.T) {
	drop := "version schema size edit sender role target verb ref allowlist excluded stale rate forge-rate unresolved provenance"
	ignore := "type seen self admin not-addressed empty not-coordinator"
	if got := strings.Join(DropReasons, " "); got != drop {
		t.Fatalf("DropReasons = %s", got)
	}
	if got := strings.Join(IgnoreReasons, " "); got != ignore {
		t.Fatalf("IgnoreReasons = %s", got)
	}
	if got := strings.Join(SendRefusals, " "); got != drop+" secret body duplicate" {
		t.Fatalf("SendRefusals = %s", got)
	}
}

const testNow int64 = 1_800_000_000_000

var (
	judgedEvent = "$" + strings.Repeat("j", 43)
	selfEvent   = "$" + strings.Repeat("s", 43)
	checkEvent  = "$" + strings.Repeat("c", 43)
	adminID     = AdminID(testServer)
)

type fakeLookup struct {
	seen   map[string]bool
	events map[string]Related
	pings  map[string]OwnPing
}

func (f fakeLookup) Seen(id string) bool { return f.seen[id] }

func (f fakeLookup) Event(id string) (Related, bool) {
	r, ok := f.events[id]
	return r, ok
}

func (f fakeLookup) OwnPing(id string) (OwnPing, bool) {
	p, ok := f.pings[id]
	return p, ok
}

// testLookup knows: the coordinator's own ping to the worker (pingEvent), an event the self
// sent (selfEvent), a human's earlier message (otherEvent, addressed to the self), the probe's
// check (checkEvent), and one judged event.
func testLookup(self string) fakeLookup {
	return fakeLookup{
		seen: map[string]bool{judgedEvent: true},
		events: map[string]Related{
			selfEvent:  {Sender: self, RoomID: mainRoom},
			otherEvent: {Sender: aliceID, RoomID: mainRoom, AddressedToSelf: true},
			checkEvent: {Sender: probeID, RoomID: mainRoom, Verb: VerbCheck},
		},
		pings: map[string]OwnPing{pingEvent: {Verb: "review", To: []string{workerID}, OriginTs: testNow - 60_000}},
	}
}

func judge(t *testing.T, self, purpose string) Judge {
	t.Helper()
	room := map[string]string{PurposeMain: mainRoom, PurposeQuestions: questionsRoom, PurposeReports: reportsRoom}[purpose]
	r := testRoster(t)
	r.Members[probeID] = RosterMember{Role: RoleProbe, HumanText: true}
	return Judge{
		Server: testServer, Self: self, RoomID: room, Purpose: purpose, Roster: r,
		NowMs: testNow, Lookup: testLookup(self), Flood: &Window{},
	}
}

// event is an m.room.message from sender with this content, sent ageMs ago.
func event(sender string, ageMs int64, content string) string {
	return `{"type":"m.room.message","event_id":"` + replyEvent + `","sender":"` + sender + `","origin_server_ts":` +
		strconv.FormatInt(testNow-ageMs, 10) + `,"content":` + content + `}`
}

func pingContent(verb string, to []string, ref, rel string) string {
	s := `{"msgtype":"m.notice","body":"x","teamchat.ping":{"v":1,"verb":"` + verb + `","to":["` + strings.Join(to, `","`) + `"]`
	if ref != "" {
		s += `,"ref":"` + ref + `"`
	}
	s += `}`
	if rel != "" {
		s += `,"m.relates_to":` + rel
	}
	return s + `}`
}

func answerRel(root string) string {
	return `{"rel_type":"m.thread","event_id":"` + root + `","is_falling_back":true,"m.in_reply_to":{"event_id":"` + root + `"}}`
}

func textContent(to []string, rel string) string {
	s := `{"msgtype":"m.text","body":"x","teamchat.text":{"v":1,"to":["` + strings.Join(to, `","`) + `"],"text":"hello"}`
	if rel != "" {
		s += `,"m.relates_to":` + rel
	}
	return s + `}`
}

func noticeContent(event, subject string, to []string) string {
	reason := ""
	if event == EventLeft {
		reason = `,"reason":"exit"`
	}
	if event == EventAdded || event == EventRole {
		reason = `,"role":"worker"`
	}
	subj := ""
	if subject != "" {
		subj = `,"subject":"` + subject + `"`
	}
	return `{"msgtype":"m.notice","body":"x","teamchat.notice":{"v":1,"event":"` + event + `"` + subj + reason + `,"to":["` + strings.Join(to, `","`) + `"]}}`
}

func humanContent(extra string) string {
	return `{"msgtype":"m.text","body":"please stop"` + extra + `}`
}

func mention(id string) string { return `,"m.mentions":{"user_ids":["` + id + `"]}` }

func carriedContent(fields, to string) string {
	return `{"msgtype":"m.notice","body":"x","teamchat.carried":{"v":1,` + fields + `,"sent_ms":1,"v1_event":"` + otherEvent + `","to":["` + to + `"],"text":"x"}}`
}

var (
	commitRef   = "commit:example-org/myrepo@" + testSHA
	externalRef = "commit:someone/elsewhere@" + testSHA
)

type verdictCase struct {
	self, purpose, event string
	outcome              Outcome
	reason, kind         string
}

func checkVerdicts(t *testing.T, cases map[string]verdictCase) {
	t.Helper()
	for name, tc := range cases {
		purpose := tc.purpose
		if purpose == "" {
			purpose = PurposeMain
		}
		v := judge(t, tc.self, purpose).Judge(mustParse(t, tc.event))
		if v.Outcome != tc.outcome || v.Reason != tc.reason || v.Kind != tc.kind {
			t.Errorf("%s: verdict %v %q %q, want %v %q %q", name, v.Outcome, v.Reason, v.Kind, tc.outcome, tc.reason, tc.kind)
		}
	}
}

func TestVerdictRulesOneToFive(t *testing.T) {
	msg := humanContent(mention(workerID))
	checkVerdicts(t, map[string]verdictCase{
		"state":                 {workerID, "", `{"type":"m.room.topic","event_id":"` + replyEvent + `","sender":"` + adminID + `","state_key":"","content":{}}`, Ignore, IgnoreType, ""},
		"no type":               {workerID, "", `{"event_id":"` + replyEvent + `"}`, Ignore, IgnoreType, ""},
		"worker redaction":      {workerID, "", `{"type":"m.room.redaction","event_id":"` + replyEvent + `","sender":"` + coordID + `","content":{"redacts":"` + otherEvent + `"}}`, Ignore, IgnoreType, ""},
		"admin redaction bad":   {workerID, "", `{"type":"m.room.redaction","event_id":"` + replyEvent + `","sender":"` + adminID + `","content":{"redacts":"$x"}}`, Ignore, IgnoreType, ""},
		"event id":              {workerID, "", strings.Replace(event(aliceID, 0, msg), replyEvent, "$x", 1), Drop, ReasonSchema, ""},
		"state key":             {workerID, "", strings.Replace(event(aliceID, 0, msg), `"content"`, `"state_key":"","content"`, 1), Drop, ReasonSchema, ""},
		"redacted":              {workerID, "", strings.Replace(event(aliceID, 0, msg), `"content"`, `"unsigned":{"redacted_because":{}},"content"`, 1), Drop, ReasonSchema, ""},
		"content not object":    {workerID, "", `{"type":"m.room.message","event_id":"` + replyEvent + `","sender":"` + aliceID + `","origin_server_ts":1,"content":"x"}`, Drop, ReasonSchema, ""},
		"negative ts":           {workerID, "", `{"type":"m.room.message","event_id":"` + replyEvent + `","sender":"` + aliceID + `","origin_server_ts":-1,"content":{}}`, Drop, ReasonSchema, ""},
		"no sender":             {workerID, "", `{"type":"m.room.message","event_id":"` + replyEvent + `","origin_server_ts":1,"content":{}}`, Drop, ReasonSchema, ""},
		"seen":                  {workerID, "", strings.Replace(event(aliceID, 0, msg), replyEvent, judgedEvent, 1), Ignore, IgnoreSeen, ""},
		"self":                  {workerID, "", event(workerID, 0, msg), Ignore, IgnoreSelf, ""},
		"parked":                {workerID, "", event(parkedID, 0, msg), Drop, ReasonSender, ""},
		"unknown":               {workerID, "", event(UserID("bob", testServer), 0, msg), Drop, ReasonSender, ""},
		"reports":               {workerID, PurposeReports, event(coordID, 0, msg), Fault, "", ""},
		"questions, worker":     {workerID, PurposeQuestions, event(aliceID, 0, msg), Ignore, IgnoreNotCoordinator, ""},
		"questions, human":      {coordID, PurposeQuestions, event(aliceID, 0, humanContent("")), Deliver, "", KindHuman},
		"questions, human old":  {coordID, PurposeQuestions, event(aliceID, 90_000_000, humanContent("")), Deliver, "", KindHuman},
		"questions, agent text": {coordID, PurposeQuestions, event(workerID, 0, textContent([]string{coordID}, "")), Drop, ReasonSender, ""},
		"questions, admin":      {coordID, PurposeQuestions, event(adminID, 0, noticeContent(EventAdded, workerID, []string{aliceID})), Drop, ReasonSender, ""},
		"questions, carried":    {coordID, PurposeQuestions, event(adminID, 0, carriedContent(`"kind":"text","from":"`+aliceID+`"`, coordID)), Drop, ReasonSender, ""},
	})
}

func TestVerdictRedaction(t *testing.T) {
	v := judge(t, workerID, PurposeMain).Judge(mustParse(t, `{"type":"m.room.redaction","event_id":"`+replyEvent+`","sender":"`+adminID+`","content":{"redacts":"`+otherEvent+`"}}`))
	if v.Outcome != Redact || v.Redacts != otherEvent {
		t.Fatalf("admin redaction = %+v", v)
	}
}

func TestVerdictQuestionIndex(t *testing.T) {
	question := `{"msgtype":"m.text","body":"Q1 Which?","teamchat.question":{"v":1,"n":1}}`
	thread := `{"msgtype":"m.notice","body":"Q1 closed.","m.relates_to":` + answerRel(otherEvent) + `,"teamchat.thread":{"v":1,"n":1,"close":true}}`
	checkVerdicts(t, map[string]verdictCase{
		"own question":        {coordID, PurposeQuestions, event(coordID, 0, question), Index, "", ""},
		"former coordinator":  {coordID, PurposeQuestions, event(parkedID, 0, question), Index, "", ""},
		"thread":              {coordID, PurposeQuestions, event(workerID, 0, thread), Index, "", ""},
		"admin question":      {coordID, PurposeQuestions, event(adminID, 0, question), Index, "", ""},
		"admin thread":        {coordID, PurposeQuestions, event(adminID, 0, thread), Drop, ReasonSender, ""},
		"bad question":        {coordID, PurposeQuestions, event(workerID, 0, `{"body":"Q2 x","teamchat.question":{"v":1,"n":1}}`), Drop, ReasonSchema, ""},
		"human with question": {coordID, PurposeQuestions, event(aliceID, 0, question), Deliver, "", KindHuman},
		"seen first":          {coordID, PurposeQuestions, strings.Replace(event(coordID, 0, question), replyEvent, judgedEvent, 1), Ignore, IgnoreSeen, ""},
	})
	v := judge(t, coordID, PurposeQuestions).Judge(mustParse(t, event(coordID, 0, question)))
	if v.Content.Question.N != 1 || v.Content.Key != KeyQuestion || v.Event.Sender != coordID {
		t.Fatalf("index verdict = %+v", v)
	}
}

func TestVerdictAdmin(t *testing.T) {
	checkVerdicts(t, map[string]verdictCase{
		"added":         {coordID, "", event(adminID, 0, noticeContent(EventAdded, workerID, []string{aliceID, coordID})), Deliver, "", KindNotice},
		"moved":         {workerID, "", event(adminID, 0, noticeContent(EventMoved, "", []string{coordID, workerID})), Deliver, "", KindNotice},
		"moved human":   {workerID, "", event(adminID, 0, noticeContent(EventMoved, "", []string{aliceID, workerID})), Drop, ReasonTarget, ""},
		"added worker":  {workerID, "", event(adminID, 0, noticeContent(EventAdded, lxcWorkerID, []string{workerID})), Drop, ReasonTarget, ""},
		"added subject": {coordID, "", event(adminID, 0, noticeContent(EventRemoved, coordID, []string{coordID})), Drop, ReasonTarget, ""},
		"not addressed": {coordID, "", event(adminID, 0, noticeContent(EventParked, workerID, []string{aliceID})), Ignore, IgnoreNotAddressed, ""},
		"admin left":    {coordID, "", event(adminID, 0, noticeContent(EventLeft, workerID, []string{coordID})), Ignore, IgnoreAdmin, ""},
		"admin text":    {coordID, "", event(adminID, 0, textContent([]string{coordID}, "")), Ignore, IgnoreAdmin, ""},
		"admin display": {coordID, "", event(adminID, 0, humanContent("")), Ignore, IgnoreAdmin, ""},
		"admin two":     {coordID, "", event(adminID, 0, `{"body":"x","teamchat.notice":{"v":1},"teamchat.ping":{"v":1}}`), Drop, ReasonSchema, ""},
		"carried":       {coordID, "", event(adminID, 0, carriedContent(`"kind":"text","from":"`+UserID("gone.9+desk.podman", testServer)+`"`, coordID)), Deliver, "", KindNotice},
		"carried old":   {coordID, "", event(adminID, 900_000_000, carriedContent(`"kind":"ping","verb":"review","ref":"`+commitRef+`","from":"`+aliceID+`"`, coordID)), Deliver, "", KindNotice},
		"carried from":  {coordID, "", event(adminID, 0, carriedContent(`"kind":"text","from":"`+adminID+`"`, coordID)), Drop, ReasonSchema, ""},
		"carried verb":  {coordID, "", event(adminID, 0, carriedContent(`"kind":"ping","verb":"poke","from":"`+aliceID+`"`, coordID)), Drop, ReasonVerb, ""},
		"carried ref":   {coordID, "", event(adminID, 0, carriedContent(`"kind":"ping","verb":"review","from":"`+aliceID+`"`, coordID)), Drop, ReasonRef, ""},
		"carried other": {workerID, "", event(adminID, 0, carriedContent(`"kind":"text","from":"`+aliceID+`"`, coordID)), Ignore, IgnoreNotAddressed, ""},
	})
	v := judge(t, coordID, PurposeMain).Judge(mustParse(t, event(adminID, 0, carriedContent(`"kind":"text","from":"`+aliceID+`"`, coordID))))
	if !v.Carried || v.Class != SenderAdmin {
		t.Fatalf("carried verdict = %+v", v)
	}
}

func TestVerdictHuman(t *testing.T) {
	reply := func(id string) string { return `,"m.relates_to":{"m.in_reply_to":{"event_id":"` + id + `"}}` }
	thread := func(id string) string {
		return `,"m.relates_to":{"rel_type":"m.thread","event_id":"` + id + `","is_falling_back":true,"m.in_reply_to":{"event_id":"` + id + `"}}`
	}
	edit := func(orig, mentions string) string {
		return `{"msgtype":"m.text","body":"* fixed","m.new_content":{"msgtype":"m.text","body":"fixed"` + mentions + `},"m.relates_to":{"rel_type":"m.replace","event_id":"` + orig + `"}}`
	}
	checkVerdicts(t, map[string]verdictCase{
		"mention":          {workerID, "", event(aliceID, 0, humanContent(mention(workerID))), Deliver, "", KindHuman},
		"room":             {workerID, "", event(aliceID, 0, humanContent(`,"m.mentions":{"room":true}`)), Deliver, "", KindHuman},
		"other mention":    {workerID, "", event(aliceID, 0, humanContent(mention(coordID))), Ignore, IgnoreNotAddressed, ""},
		"reply to self":    {workerID, "", event(aliceID, 0, humanContent(reply(selfEvent))), Deliver, "", KindHuman},
		"reply to other":   {workerID, "", event(aliceID, 0, humanContent(reply(otherEvent))), Ignore, IgnoreNotAddressed, ""},
		"thread of self":   {workerID, "", event(aliceID, 0, humanContent(thread(selfEvent))), Deliver, "", KindHuman},
		"no human text":    {lxcWorkerID, "", event(aliceID, 0, humanContent(mention(lxcWorkerID))), Drop, ReasonSender, ""},
		"stale":            {workerID, "", event(aliceID, 86_400_001, humanContent(mention(workerID))), Drop, ReasonStale, ""},
		"emote":            {workerID, "", event(aliceID, 0, `{"msgtype":"m.emote","body":"x"}`), Ignore, IgnoreType, ""},
		"empty":            {workerID, "", event(aliceID, 0, `{"msgtype":"m.text","body":""}`), Ignore, IgnoreEmpty, ""},
		"too big":          {workerID, "", event(aliceID, 0, `{"msgtype":"m.text","body":"`+strings.Repeat("a", MaxHumanBody+1)+`"}`), Drop, ReasonSize, ""},
		"edit":             {workerID, "", event(aliceID, 0, edit(otherEvent, "")), Deliver, "", KindHuman},
		"edit of other":    {workerID, "", event(aliceID, 0, edit(checkEvent, "")), Drop, ReasonEdit, ""},
		"edit unknown":     {workerID, "", event(aliceID, 0, edit(pingEvent, "")), Drop, ReasonEdit, ""},
		"edit new mention": {coordID, "", event(aliceID, 0, edit(otherEvent, mention(coordID))), Deliver, "", KindHuman},
		"edit other room":  {coordID, PurposeQuestions, event(aliceID, 0, edit(otherEvent, "")), Drop, ReasonEdit, ""},
	})
}

func TestVerdictHumanEditAddressing(t *testing.T) {
	j := judge(t, coordID, PurposeMain)
	lookup := testLookup(coordID)
	lookup.events[otherEvent] = Related{Sender: aliceID, RoomID: mainRoom}
	j.Lookup = lookup
	edit := `{"msgtype":"m.text","body":"* fixed","m.new_content":{"msgtype":"m.text","body":"fixed"},"m.relates_to":{"rel_type":"m.replace","event_id":"` + otherEvent + `"}}`
	if v := j.Judge(mustParse(t, event(aliceID, 0, edit))); v.Outcome != Ignore || v.Reason != IgnoreNotAddressed {
		t.Fatalf("an edit of a message not to this member = %+v", v)
	}
}

func TestVerdictAgent(t *testing.T) {
	coordWorker := []string{coordID, workerID}
	checkVerdicts(t, map[string]verdictCase{
		"review":            {coordID, "", event(workerID, 0, pingContent("review", []string{coordID}, commitRef, "")), Deliver, "", KindPing},
		"halt":              {workerID, "", event(coordID, 0, pingContent(VerbHalt, []string{workerID}, "", "")), Deliver, "", KindPing},
		"carried key":       {coordID, "", event(workerID, 0, carriedContent(`"kind":"text","from":"`+aliceID+`"`, coordID)), Drop, ReasonSender, ""},
		"probe text":        {coordID, "", event(probeID, 0, textContent([]string{coordID}, "")), Drop, ReasonRole, ""},
		"probe halt":        {coordID, "", event(probeID, 0, pingContent(VerbHalt, []string{coordID}, "", "")), Drop, ReasonRole, ""},
		"probe check":       {coordID, "", event(probeID, 0, pingContent(VerbCheck, []string{coordID}, "", "")), Deliver, "", KindPing},
		"worker check":      {coordID, "", event(workerID, 0, pingContent(VerbCheck, []string{coordID}, "", "")), Drop, ReasonRole, ""},
		"check two":         {coordID, "", event(probeID, 0, pingContent(VerbCheck, coordWorker, "", "")), Drop, ReasonTarget, ""},
		"ack the check":     {coordID, "", event(workerID, 0, pingContent(VerbAck, []string{coordID, probeID}, "", answerRel(checkEvent))), Deliver, "", KindPing},
		"ack probe else":    {coordID, "", event(workerID, 0, pingContent(VerbAck, []string{coordID, probeID}, "", answerRel(otherEvent))), Drop, ReasonTarget, ""},
		"bad content":       {coordID, "", event(workerID, 0, `{"body":"x"}`), Drop, ReasonSchema, ""},
		"version":           {coordID, "", event(workerID, 0, `{"body":"x","teamchat.ping":{"v":2}}`), Drop, ReasonVersion, ""},
		"unknown verb":      {coordID, "", event(workerID, 0, pingContent("poke", []string{coordID}, "", "")), Drop, ReasonVerb, ""},
		"worker fetch":      {coordID, "", event(workerID, 0, pingContent("fetch", []string{coordID}, commitRef, "")), Drop, ReasonRole, ""},
		"ack no relation":   {coordID, "", event(workerID, 0, pingContent(VerbAck, []string{coordID}, "", "")), Drop, ReasonSchema, ""},
		"review relation":   {coordID, "", event(workerID, 0, pingContent("review", []string{coordID}, commitRef, answerRel(otherEvent))), Drop, ReasonSchema, ""},
		"done plain reply":  {coordID, "", event(workerID, 0, pingContent(VerbDone, []string{coordID}, commitRef, `{"m.in_reply_to":{"event_id":"`+otherEvent+`"}}`)), Drop, ReasonSchema, ""},
		"to parked":         {coordID, "", event(workerID, 0, pingContent("review", []string{coordID, parkedID}, commitRef, "")), Drop, ReasonTarget, ""},
		"to repeated":       {coordID, "", event(workerID, 0, pingContent("review", []string{coordID, coordID}, commitRef, "")), Drop, ReasonTarget, ""},
		"to human review":   {coordID, "", event(workerID, 0, pingContent("review", []string{aliceID, coordID}, commitRef, "")), Drop, ReasonTarget, ""},
		"no ref":            {coordID, "", event(workerID, 0, pingContent("review", []string{coordID}, "", "")), Drop, ReasonRef, ""},
		"halt ref":          {workerID, "", event(coordID, 0, pingContent(VerbHalt, []string{workerID}, commitRef, "")), Drop, ReasonRef, ""},
		"bad ref":           {coordID, "", event(workerID, 0, pingContent("review", []string{coordID}, "commit:Example/x@"+testSHA, "")), Drop, ReasonRef, ""},
		"issue for review":  {coordID, "", event(workerID, 0, pingContent("review", []string{coordID}, "issue:example-org/myrepo#1", "")), Drop, ReasonRef, ""},
		"excluded":          {coordID, "", event(workerID, 0, pingContent("review", []string{coordID}, "commit:example-org/private-notes@"+testSHA, "")), Drop, ReasonExcluded, ""},
		"external sync":     {workerID, "", event(coordID, 0, pingContent("sync", []string{workerID}, externalRef, "")), Drop, ReasonAllowlist, ""},
		"external review":   {coordID, "", event(workerID, 0, pingContent("review", []string{coordID}, externalRef, "")), Deliver, "", KindPing},
		"stale ping":        {coordID, "", event(workerID, 900_001, pingContent("review", []string{coordID}, commitRef, "")), Drop, ReasonStale, ""},
		"not addressed":     {lxcWorkerID, "", event(workerID, 0, pingContent("review", []string{coordID}, commitRef, "")), Ignore, IgnoreNotAddressed, ""},
		"text":              {coordID, "", event(workerID, 0, textContent([]string{coordID}, "")), Deliver, "", KindPeer},
		"text reply":        {coordID, "", event(workerID, 0, textContent([]string{aliceID, coordID}, `{"m.in_reply_to":{"event_id":"`+otherEvent+`"}}`)), Deliver, "", KindPeer},
		"text to parked":    {coordID, "", event(workerID, 0, textContent([]string{coordID, parkedID}, "")), Drop, ReasonTarget, ""},
		"text to probe":     {coordID, "", event(workerID, 0, textContent([]string{coordID, probeID}, "")), Drop, ReasonTarget, ""},
		"text no human":     {lxcWorkerID, "", event(workerID, 0, textContent([]string{lxcWorkerID}, "")), Drop, ReasonSender, ""},
		"text stale":        {coordID, "", event(workerID, 86_400_001, textContent([]string{coordID}, "")), Drop, ReasonStale, ""},
		"left":              {coordID, "", event(workerID, 0, noticeContent(EventLeft, workerID, []string{aliceID, coordID})), Deliver, "", KindNotice},
		"old back":          {coordID, "", event(workerID, 900_000_000, noticeContent(EventBack, workerID, []string{coordID})), Deliver, "", KindNotice},
		"left for another":  {coordID, "", event(workerID, 0, noticeContent(EventLeft, lxcWorkerID, []string{coordID})), Drop, ReasonTarget, ""},
		"agent added":       {coordID, "", event(workerID, 0, noticeContent(EventAdded, workerID, []string{coordID})), Drop, ReasonTarget, ""},
		"notice to subject": {coordID, "", event(workerID, 0, noticeContent(EventBack, workerID, []string{coordID, workerID})), Drop, ReasonTarget, ""},
		"notice to worker":  {coordID, "", event(workerID, 0, noticeContent(EventBack, workerID, []string{coordID, lxcWorkerID})), Drop, ReasonTarget, ""},
		"notice to none":    {coordID, "", event(workerID, 0, `{"body":"x","teamchat.notice":{"v":1,"event":"back","subject":"`+workerID+`","to":[]}}`), Drop, ReasonTarget, ""},
	})
}

func TestVerdictPingForgeCheck(t *testing.T) {
	v := judge(t, coordID, PurposeMain).Judge(mustParse(t, event(workerID, 0, pingContent("review", []string{coordID}, commitRef, ""))))
	if v.Forge == nil || v.Forge.Class != RefListed || v.Forge.Ref.Text != commitRef || len(v.Forge.Branches) != 1 || v.Forge.Trust() != TrustTrusted {
		t.Fatalf("forge check = %+v", v.Forge)
	}
	v = judge(t, coordID, PurposeMain).Judge(mustParse(t, event(workerID, 0, pingContent("review", []string{coordID}, externalRef, ""))))
	if v.Forge == nil || v.Forge.Trust() != TrustExternal {
		t.Fatalf("external forge check = %+v", v.Forge)
	}
	if v = judge(t, workerID, PurposeMain).Judge(mustParse(t, event(coordID, 0, pingContent(VerbHalt, []string{workerID}, "", "")))); v.Forge != nil {
		t.Fatalf("halt has a forge check: %+v", v.Forge)
	}
}

func TestVerdictBranchAndPathRules(t *testing.T) {
	r := testRoster(t)
	review := Verbs["review"]
	path := "path:example-org/myrepo@" + testSHA + ":docs/a.md"
	cases := map[string]struct {
		ref, branch, prefix, want string
	}{
		"branch beside commit": {commitRef, "main", "", ""},
		"branch beside pr":     {"pr:example-org/myrepo#1@" + testSHA, "main", "", ReasonRef},
		"bad branch":           {commitRef, "a b", "", ReasonRef},
		"branch without ref":   {"", "main", "", ReasonRef},
		"path in prefix":       {path, "", "docs/", ""},
		"path outside prefix":  {path, "", "src/", ReasonAllowlist},
		"external path":        {"path:someone/x@" + testSHA + ":docs/a.md", "", "src/", ""},
	}
	for name, tc := range cases {
		r.Forge.PathPrefixes = nil
		if tc.prefix != "" {
			r.Forge.PathPrefixes = []string{tc.prefix}
		}
		reason, forge := r.checkRef(review, tc.ref, tc.branch)
		if reason != tc.want || (forge == nil) != (reason != "") {
			t.Errorf("%s: %q %+v, want %q", name, reason, forge, tc.want)
		}
	}
}

func TestVerdictAnswers(t *testing.T) {
	answer := func(verb string, sender string, ageMs int64) Verdict {
		ref := ""
		if verb == VerbDone {
			ref = commitRef
		}
		return judge(t, coordID, PurposeMain).Judge(mustParse(t, event(sender, ageMs, pingContent(verb, []string{coordID}, ref, answerRel(pingEvent)))))
	}
	v := answer(VerbAck, workerID, 0)
	if v.Outcome != Record || v.Answer == nil || v.Answer.Ping != pingEvent || v.Answer.Verb != VerbAck || !v.Answer.OnTime {
		t.Fatalf("ack = %+v", v)
	}
	if v = answer(VerbDone, workerID, 0); v.Outcome != Deliver || v.Kind != KindPing || v.Answer == nil || !v.Answer.OnTime {
		t.Fatalf("done = %+v", v)
	}
	if v = answer(VerbDone, workerID, 900_001); v.Outcome != Record || v.Answer == nil {
		t.Fatalf("a stale done = %+v", v)
	}
	if v = answer(VerbAck, lxcWorkerID, 0); v.Outcome != Deliver || v.Answer != nil {
		t.Fatalf("an ack from a non-addressee = %+v", v)
	}
}

func TestVerdictFloodedAnswers(t *testing.T) {
	var v Verdict
	j := judge(t, coordID, PurposeMain)
	j.Flood = &Window{Times: map[string][]int64{workerID: make([]int64, 60)}}
	for i := range j.Flood.Times[workerID] {
		j.Flood.Times[workerID][i] = testNow
	}
	if v = j.Judge(mustParse(t, event(workerID, 0, pingContent(VerbDone, []string{coordID}, commitRef, answerRel(pingEvent))))); v.Reason != ReasonRate || v.Answer != nil {
		t.Fatalf("a flooded done = %+v", v)
	}
	if v = j.Judge(mustParse(t, event(workerID, 0, pingContent(VerbAck, []string{coordID}, "", answerRel(pingEvent))))); v.Outcome != Record {
		t.Fatalf("a flooded ack is still recorded: %+v", v)
	}
}

func TestVerdictReceiveFlood(t *testing.T) {
	j := judge(t, coordID, PurposeMain)
	content := mustParse(t, event(workerID, 0, textContent([]string{coordID}, "")))
	for i := range 60 {
		if v := j.Judge(content); v.Outcome != Deliver {
			t.Fatalf("event %d = %+v", i, v)
		}
	}
	if v := j.Judge(content); v.Reason != ReasonRate {
		t.Fatalf("event 61 = %+v", v)
	}
	if v := j.Judge(mustParse(t, event(adminID, 0, noticeContent(EventAdded, workerID, []string{coordID})))); v.Outcome != Deliver {
		t.Fatalf("the admin is not flood-limited: %+v", v)
	}
}
