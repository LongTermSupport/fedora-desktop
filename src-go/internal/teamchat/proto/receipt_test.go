package proto

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// receiptContent is a receipt event's content with this typed object.
func receiptContent(obj string) string {
	return `{"msgtype":"m.notice","body":"receipt","m.mentions":{},"teamchat.receipt":` + obj + `}`
}

// receiptObj is a receipt of pingEvent to the worker, with these fields after `to`.
func receiptObj(fields string) string {
	return `{"v":1,"of":"` + pingEvent + `","to":["` + workerID + `"]` + fields + `}`
}

func TestReceiptReasonSets(t *testing.T) {
	bounce := strings.Join(DropReasons, " ") + " render gone removed redacted unheard type empty role-changed expired"
	if got := strings.Join(BounceReasons, " "); got != bounce {
		t.Fatalf("BounceReasons = %s", got)
	}
	if got := strings.Join(HeldReasons, " "); got != "absent missing rate budget full forge-rate unresolved" {
		t.Fatalf("HeldReasons = %s", got)
	}
	if got := strings.Join(ReceiptStates, " "); got != "delivered held bounced recorded" {
		t.Fatalf("ReceiptStates = %s", got)
	}
	for _, r := range slices.Concat(BounceReasons, HeldReasons) {
		if !slices.Contains(ReceiptReasons, r) {
			t.Errorf("%q is not in ReceiptReasons", r)
		}
	}
	if got := strings.Join(ReceiptReasons, " "); got != bounce+" absent missing budget full" {
		t.Errorf("ReceiptReasons = %v: the union of the bounce and held reasons, each once", ReceiptReasons)
	}
}

func TestDecodeReceipt(t *testing.T) {
	c := mustDecode(t, ReportKeys, receiptContent(receiptObj(`,"state":"bounced","reason":"render","at":17,"kit":1`)))
	r := c.Receipt
	if c.Key != KeyReceipt || r.Of != pingEvent || !slices.Equal(r.To, []string{workerID}) || r.State != ReceiptBounced ||
		r.Reason != ReasonRender || r.At != 17 || c.Kit != 1 {
		t.Fatalf("receipt = %+v", c)
	}
	accepted := map[string]string{
		"delivered":     `,"state":"delivered","at":0`,
		"recorded":      `,"state":"recorded","at":1`,
		"held rate":     `,"state":"held","reason":"rate","at":1`,
		"held budget":   `,"state":"held","reason":"budget","at":1`,
		"held absent":   `,"state":"held","reason":"absent","at":1`,
		"held missing":  `,"state":"held","reason":"missing","at":1`,
		"bounced stale": `,"state":"bounced","reason":"stale","at":1`,
		"expired":       `,"state":"bounced","reason":"expired","at":1`,
		"unknown field": `,"state":"delivered","at":1,"later":true`,
		"no kit":        `,"state":"delivered","at":1`,
	}
	for _, reason := range BounceReasons {
		accepted["bounced "+reason] = `,"state":"bounced","reason":"` + reason + `","at":1`
	}
	for name, fields := range accepted {
		if _, reason := decode(t, ReportKeys, receiptContent(receiptObj(fields))); reason != "" {
			t.Errorf("%s: refused %q", name, reason)
		}
	}
	two := `{"v":1,"of":"` + pingEvent + `","to":["` + workerID + `","` + coordID + `"],"state":"delivered","at":1}`
	if c := mustDecode(t, ReportKeys, receiptContent(two)); !slices.Equal(c.Receipt.To, []string{coordID, workerID}) {
		t.Errorf("a receipt's to is not sorted: %v", c.Receipt.To)
	}
}

func TestDecodeReceiptRefuses(t *testing.T) {
	to := func(list string) string {
		return `{"v":1,"of":"` + pingEvent + `","to":` + list + `,"state":"delivered","at":1}`
	}
	cases := map[string]struct{ obj, want string }{
		"of absent":           {`{"v":1,"to":["` + workerID + `"],"state":"delivered","at":1}`, ReasonSchema},
		"of not an event":     {`{"v":1,"of":"$x","to":["` + workerID + `"],"state":"delivered","at":1}`, ReasonSchema},
		"of not a string":     {`{"v":1,"of":1,"to":["` + workerID + `"],"state":"delivered","at":1}`, ReasonSchema},
		"to absent":           {`{"v":1,"of":"` + pingEvent + `","state":"delivered","at":1}`, ReasonSchema},
		"to empty":            {to(`[]`), ReasonSchema},
		"to duplicate":        {to(`["` + workerID + `","` + workerID + `"]`), ReasonSchema},
		"to too many":         {to(idList(MaxReceiptTo + 1)), ReasonSchema},
		"to not a user":       {to(`["alice"]`), ReasonSchema},
		"to no server":        {to(`["@alice:"]`), ReasonSchema},
		"to not strings":      {to(`[1]`), ReasonSchema},
		"state absent":        {receiptObj(`,"at":1`), ReasonSchema},
		"state unknown":       {receiptObj(`,"state":"read","at":1`), ReasonSchema},
		"held no reason":      {receiptObj(`,"state":"held","at":1`), ReasonSchema},
		"bounced no reason":   {receiptObj(`,"state":"bounced","at":1`), ReasonSchema},
		"delivered reason":    {receiptObj(`,"state":"delivered","reason":"stale","at":1`), ReasonSchema},
		"recorded reason":     {receiptObj(`,"state":"recorded","reason":"stale","at":1`), ReasonSchema},
		"bounced unknown":     {receiptObj(`,"state":"bounced","reason":"lost","at":1`), ReasonSchema},
		"bounced held reason": {receiptObj(`,"state":"bounced","reason":"budget","at":1`), ReasonSchema},
		"held bounce reason":  {receiptObj(`,"state":"held","reason":"render","at":1`), ReasonSchema},
		"reason not string":   {receiptObj(`,"state":"bounced","reason":1,"at":1`), ReasonSchema},
		"at absent":           {receiptObj(`,"state":"delivered"`), ReasonSchema},
		"at negative":         {receiptObj(`,"state":"delivered","at":-1`), ReasonSchema},
		"at not integer":      {receiptObj(`,"state":"delivered","at":"1"`), ReasonSchema},
		"kit zero":            {receiptObj(`,"state":"delivered","at":1,"kit":0`), ReasonSchema},
		"kit not integer":     {receiptObj(`,"state":"delivered","at":1,"kit":"1"`), ReasonSchema},
		"kit huge":            {receiptObj(`,"state":"delivered","at":1,"kit":` + strconv.Itoa(MaxKit+1)), ReasonSchema},
		"v two":               {`{"v":2,"of":"` + pingEvent + `"}`, ReasonVersion},
		"features":            {`{"v":1,"features":["x"]}`, ReasonVersion},
	}
	for name, tc := range cases {
		if _, reason := decode(t, ReportKeys, receiptContent(tc.obj)); reason != tc.want {
			t.Errorf("%s: reason %q, want %q", name, reason, tc.want)
		}
	}
	rel := `{"msgtype":"m.notice","body":"x","m.relates_to":{"m.in_reply_to":{"event_id":"` + replyEvent + `"}},"teamchat.receipt":` +
		receiptObj(`,"state":"delivered","at":1`) + `}`
	if _, reason := decode(t, ReportKeys, rel); reason != ReasonSchema {
		t.Errorf("a receipt with a relation: %q", reason)
	}
	if _, reason := decode(t, MainKeys, receiptContent(receiptObj(`,"state":"delivered","at":1`))); reason != ReasonSchema {
		t.Errorf("a receipt in main: %q, want schema", reason)
	}
	both := `{"body":"x","teamchat.report":{"v":1},"teamchat.receipt":` + receiptObj(`,"state":"delivered","at":1`) + `}`
	if _, reason := decode(t, ReportKeys, both); reason != ReasonSchema {
		t.Errorf("a report and a receipt in one event: %q, want schema", reason)
	}
}

// TestKitIsReadOptionally: a typed object's `kit` is the sender kit's protocol level; absent is
// an old kit (0), and a present one is a positive integer.
func TestKitIsReadOptionally(t *testing.T) {
	ping := func(kit string) string {
		return `{"body":"x","teamchat.ping":{"v":1,"verb":"halt","to":["` + workerID + `"]` + kit + `}}`
	}
	if c := mustDecode(t, MainKeys, ping("")); c.Kit != 0 {
		t.Errorf("an old kit's ping has kit %d", c.Kit)
	}
	if c := mustDecode(t, MainKeys, ping(`,"kit":3`)); c.Kit != 3 {
		t.Errorf("kit = %d, want 3", c.Kit)
	}
	for name, kit := range map[string]string{"zero": `,"kit":0`, "negative": `,"kit":-1`, "string": `,"kit":"1"`, "null": `,"kit":null`} {
		if _, reason := decode(t, MainKeys, ping(kit)); reason != ReasonSchema {
			t.Errorf("kit %s: reason %q, want schema", name, reason)
		}
	}
}

// kitOf is the `kit` of out's typed object under key.
func kitOf(t *testing.T, out Outgoing, key string) int64 {
	t.Helper()
	k := at(t, out.Content, key, "kit")
	if k.Kind() != KindInt {
		t.Fatalf("%s kit is not an integer: %s", key, out.Content.Compact())
	}
	return k.Int()
}

// TestEveryTypedSendCarriesTheKit is C7: each typed object a kit sends says its protocol level,
// and the receiver reads it back.
func TestEveryTypedSendCarriesTheKit(t *testing.T) {
	r := linked(t)
	linker := composer(t, coordID)
	linker.Questions = r.index
	worker, coord, send := composer(t, workerID), composer(t, coordID), mustSend(t)
	link := LinkPost{N: 1, Post: qid('t'), Link: qid('c'), To: aliceID, Permalink: Permalink(questionsRoom, qid('c'))}
	sends := map[string]struct {
		key string
		out Outgoing
	}{
		"ping":      {KeyPing, send(worker.Ping(Ping{Verb: "review", To: []string{coordID}, Ref: commitRef}, "", "review"))},
		"text":      {KeyText, send(worker.Text(Text{To: []string{coordID}, Text: "hi"}, "", "hi"))},
		"notice":    {KeyNotice, send(worker.Notice(Notice{Event: EventBack, Subject: workerID, To: []string{coordID}}, "back"))},
		"report":    {KeyReport, send(worker.Report("Merged\nPR 12."))},
		"question":  {KeyQuestion, send(coord.Question(QuestionPost{Text: goodQuestion, AskedBy: []string{workerID}}))},
		"thread":    {KeyThread, send(asked(t).Thread(ThreadPost{N: 1, Text: "Relayed."}))},
		"link-back": {KeyThread, send(linker.LinkBack(link))},
	}
	for name, s := range sends {
		out := s.out
		if got := kitOf(t, out, s.key); got != KitLevel {
			t.Errorf("%s: kit %d, want %d", name, got, KitLevel)
		}
		keys := map[string][]string{PurposeMain: MainKeys, PurposeReports: ReportKeys, PurposeQuestions: QuestionKeys}[out.Purpose]
		if c := mustDecode(t, keys, string(out.Content.Compact())); c.Kit != KitLevel {
			t.Errorf("%s: decoded kit %d", name, c.Kit)
		}
	}
}

func TestReceiptOwed(t *testing.T) {
	owed := map[string]bool{
		"receipt":           false,
		"malformed receipt": false,
		"ping":              true,
		"text":              true,
		"report":            true,
		"human":             true,
	}
	contents := map[string]string{
		"receipt":           receiptContent(receiptObj(`,"state":"delivered","at":1`)),
		"malformed receipt": receiptContent(`{"v":7}`),
		"ping":              pingContent("halt", []string{workerID}, "", ""),
		"text":              textContent([]string{workerID}, ""),
		"report":            `{"body":"x","teamchat.report":{"v":1}}`,
		"human":             humanContent(""),
	}
	for name, want := range owed {
		if got := ReceiptOwed(mustParse(t, contents[name])); got != want {
			t.Errorf("%s: ReceiptOwed %v, want %v", name, got, want)
		}
	}
}

// TestVerdictReports is the reports room (rule 05): every rostered sender's event is a fault no
// member reads, as before receipts; a coordinator's or worker's receipt is decoded on the fault,
// and a malformed one is dropped with its reason.
func TestVerdictReports(t *testing.T) {
	good := receiptContent(receiptObj(`,"state":"bounced","reason":"stale","at":1,"kit":1`))
	checkVerdicts(t, map[string]verdictCase{
		"report":            {workerID, PurposeReports, event(coordID, 0, `{"body":"x","teamchat.report":{"v":1}}`), Fault, "", ""},
		"receipt":           {workerID, PurposeReports, event(coordID, 0, good), Fault, "", ""},
		"receipt bad state": {workerID, PurposeReports, event(coordID, 0, receiptContent(receiptObj(`,"state":"read","at":1`))), Drop, ReasonSchema, ""},
		"receipt v two":     {coordID, PurposeReports, event(workerID, 0, receiptContent(`{"v":2}`)), Drop, ReasonVersion, ""},
		"receipt by human":  {workerID, PurposeReports, event(aliceID, 0, receiptContent(`{"v":2}`)), Fault, "", ""},
		"receipt by probe":  {workerID, PurposeReports, event(probeID, 0, receiptContent(`{"v":2}`)), Fault, "", ""},
		"receipt by admin":  {workerID, PurposeReports, event(adminID, 0, receiptContent(`{"v":2}`)), Fault, "", ""},
		"receipt stranger":  {workerID, PurposeReports, event(UserID("bob", testServer), 0, good), Drop, ReasonSender, ""},
		"receipt in main":   {workerID, PurposeMain, event(coordID, 0, good), Drop, ReasonSchema, ""},
	})
	v := judge(t, workerID, PurposeReports).Judge(mustParse(t, event(coordID, 0, good)))
	if v.Content.Key != KeyReceipt || v.Content.Receipt.Of != pingEvent || v.Content.Receipt.Reason != ReasonStale || v.Content.Kit != 1 {
		t.Errorf("the receipt on the fault = %+v", v.Content)
	}
}

// TestComposedReceiptIsReadInTheReports is the agreement for receipts: what ReceiptContent builds
// the reports room's judge decodes as the same receipt, and what it refuses no judge would take.
func TestComposedReceiptIsReadInTheReports(t *testing.T) {
	for _, r := range []Receipt{
		{Of: pingEvent, To: []string{workerID}, State: ReceiptDelivered, At: 5},
		{Of: pingEvent, To: []string{workerID, coordID}, State: ReceiptRecorded, At: 5},
		{Of: pingEvent, To: []string{aliceID}, State: ReceiptHeld, Reason: ReasonBudget, At: 5},
		{Of: pingEvent, To: []string{workerID}, State: ReceiptBounced, Reason: ReasonExpired, At: 5},
	} {
		content, err := ReceiptContent(r)
		if err != nil {
			t.Fatalf("%+v: %v", r, err)
		}
		if at(t, content, "msgtype").Str() != MsgNotice || len(at(t, content, keyMentions).Members()) != 0 {
			t.Errorf("a receipt notifies: %s", content.Compact())
		}
		v := judge(t, workerID, PurposeReports).Judge(mustParse(t, event(coordID, 0, string(content.Compact()))))
		got := v.Content.Receipt
		if v.Outcome != Fault || v.Content.Key != KeyReceipt || got.Of != r.Of || !slices.Equal(got.To, sortedStrs(r.To)) ||
			got.State != r.State || got.Reason != r.Reason || got.At != r.At || v.Content.Kit != KitLevel {
			t.Errorf("%+v was judged %v %q as %+v", r, v.Outcome, v.Reason, v.Content)
		}
		if ReceiptOwed(content) {
			t.Errorf("a receipt is owed a receipt: %s", content.Compact())
		}
	}
	for name, r := range map[string]Receipt{
		"no to":            {Of: pingEvent, State: ReceiptDelivered, At: 1},
		"bad of":           {Of: "$x", To: []string{workerID}, State: ReceiptDelivered, At: 1},
		"held no reason":   {Of: pingEvent, To: []string{workerID}, State: ReceiptHeld, At: 1},
		"delivered reason": {Of: pingEvent, To: []string{workerID}, State: ReceiptDelivered, Reason: ReasonStale, At: 1},
		"negative at":      {Of: pingEvent, To: []string{workerID}, State: ReceiptDelivered, At: -1},
	} {
		if _, err := ReceiptContent(r); refusalOf(err) != ReasonSchema {
			t.Errorf("%s: %v, want a schema refusal", name, err)
		}
	}
}
