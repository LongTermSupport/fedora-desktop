package proto

import (
	"slices"
	"strings"
	"testing"
)

// composer is a sender on the test roster, the probe unparked as in judge.
func composer(t *testing.T, self string) Composer {
	t.Helper()
	r := testRoster(t)
	r.Members[probeID] = RosterMember{Role: RoleProbe, HumanText: true}
	gate := NewSendGate(r.Limits, testNow)
	return Composer{
		Server: testServer, Self: self, Token: ownSample, Roster: r, Lookup: testLookup(self),
		Gate: &gate, Reports: &Window{}, Questions: NewQuestionIndex(testServer, 0), NowMs: testNow,
	}
}

// receive is the verdict of receiver over the composed event sent by sender just now.
func receive(t *testing.T, out Outgoing, sender, receiver string) Verdict {
	t.Helper()
	return judge(t, receiver, out.Purpose).Judge(mustParse(t, event(sender, 0, string(out.Content.Compact()))))
}

// mustSend fails the test on a refused composition: mustSend(t)(c.Ping(...)).
func mustSend(t *testing.T) func(Outgoing, error) Outgoing {
	t.Helper()
	return func(out Outgoing, err error) Outgoing {
		t.Helper()
		if err != nil {
			t.Fatalf("compose: %v", err)
		}
		return out
	}
}

func refusedWith(t *testing.T, name string, err error, reason string) {
	t.Helper()
	if got := refusalOf(err); got != reason {
		t.Errorf("%s: refused %q (%v), want %q", name, got, err, reason)
	}
}

func at(t *testing.T, v Value, path ...string) Value {
	t.Helper()
	for _, key := range path {
		next, ok := v.Get(key)
		if !ok {
			t.Fatalf("no %q in %s", key, v.Compact())
		}
		v = next
	}
	return v
}

func userIDs(t *testing.T, content Value) []string {
	t.Helper()
	var ids []string
	if list, ok := at(t, content, keyMentions).Get("user_ids"); ok {
		for _, item := range list.Items() {
			ids = append(ids, item.Str())
		}
	}
	return ids
}

func TestComposePingIsWhatReceiversDeliver(t *testing.T) {
	c := composer(t, workerID)
	out := mustSend(t)(c.Ping(Ping{Verb: "review", To: []string{coordID}, Ref: commitRef}, "", "review -> coordinator"))
	if out.Purpose != PurposeMain || out.Forge == nil || out.Forge.Ref.Form != RefCommit {
		t.Errorf("Ping = %+v", out)
	}
	if at(t, out.Content, "msgtype").Str() != MsgNotice || !slices.Equal(userIDs(t, out.Content), []string{coordID}) {
		t.Errorf("ping content = %s", out.Content.Compact())
	}
	if v := receive(t, out, workerID, coordID); v.Outcome != Deliver || v.Kind != KindPing {
		t.Errorf("the coordinator's verdict = %v %q", v.Outcome, v.Reason)
	}
}

func TestComposePingCopiesTheCoordinator(t *testing.T) {
	c := composer(t, workerID)
	out := mustSend(t)(c.Ping(Ping{Verb: VerbDone, To: []string{aliceID}, Ref: commitRef}, pingEvent, "done"))
	to := at(t, out.Content, KeyPing, "to")
	if len(to.Items()) != 2 || to.Items()[0].Str() != aliceID || to.Items()[1].Str() != coordID {
		t.Errorf("a worker's ping to a human goes to %s", to.Compact())
	}
	if !at(t, out.Content, keyRelatesTo).Equal(AnswerTo(pingEvent).Encode()) {
		t.Errorf("the answer form = %s", at(t, out.Content, keyRelatesTo).Compact())
	}
	coord := composer(t, coordID)
	out = mustSend(t)(coord.Ping(Ping{Verb: VerbDone, To: []string{aliceID}, Ref: commitRef}, "", "done"))
	if len(at(t, out.Content, KeyPing, "to").Items()) != 1 {
		t.Errorf("the coordinator copied itself")
	}
}

func TestComposePingRefuses(t *testing.T) {
	cases := map[string]struct {
		self, answers, display string
		ping                   Ping
		reason                 string
	}{
		"verb":       {workerID, "", "x", Ping{Verb: "deploy", To: []string{coordID}}, ReasonVerb},
		"role":       {workerID, "", "x", Ping{Verb: VerbHalt, To: []string{coordID}}, ReasonRole},
		"parked":     {parkedID, "", "x", Ping{Verb: VerbHalt, To: []string{coordID}}, ReasonSender},
		"human":      {aliceID, "", "x", Ping{Verb: VerbHalt, To: []string{coordID}}, ReasonSender},
		"admin":      {adminID, "", "x", Ping{Verb: VerbHalt, To: []string{coordID}}, ReasonSender},
		"target":     {coordID, "", "x", Ping{Verb: VerbHalt, To: []string{parkedID}}, ReasonTarget},
		"excluded":   {coordID, "", "x", Ping{Verb: "fetch", To: []string{workerID}, Ref: "commit:example-org/private-notes@" + testSHA}, ReasonExcluded},
		"schema":     {coordID, pingEvent, "x", Ping{Verb: "review", To: []string{workerID}, Ref: commitRef}, ReasonSchema},
		"no answer":  {coordID, "", "x", Ping{Verb: VerbAck, To: []string{workerID}}, ReasonSchema},
		"secret":     {coordID, "", "halt " + ownSample, Ping{Verb: VerbHalt, To: []string{workerID}}, ReasonSecret},
		"bad answer": {coordID, "not-an-id", "x", Ping{Verb: VerbAck, To: []string{workerID}}, ReasonSchema},
	}
	for name, tc := range cases {
		_, err := composer(t, tc.self).Ping(tc.ping, tc.answers, tc.display)
		refusedWith(t, name, err, tc.reason)
	}
}

func TestComposeProbeCheck(t *testing.T) {
	probe := composer(t, probeID)
	out := mustSend(t)(probe.Ping(Ping{Verb: VerbCheck, To: []string{coordID}}, "", "check"))
	if v := receive(t, out, probeID, coordID); v.Outcome != Deliver {
		t.Errorf("the probe's check: %v %q", v.Outcome, v.Reason)
	}
	_, err := composer(t, coordID).Ping(Ping{Verb: VerbAck, To: []string{probeID}}, checkEvent, "ack")
	if err != nil {
		t.Errorf("an answer to the probe's check: %v", err)
	}
	_, err = composer(t, coordID).Ping(Ping{Verb: VerbAck, To: []string{probeID}}, pingEvent, "ack")
	refusedWith(t, "an answer to the probe for another ping", err, ReasonTarget)
	_, err = probe.Text(Text{To: []string{coordID}, Text: "hi"}, "", "hi")
	refusedWith(t, "the probe's text", err, ReasonRole)
	_, err = probe.Notice(Notice{Event: EventBack, Subject: probeID, To: []string{coordID}}, "back")
	refusedWith(t, "the probe's notice", err, ReasonRole)
}

func TestComposeText(t *testing.T) {
	c := composer(t, workerID)
	out := mustSend(t)(c.Text(Text{To: []string{aliceID}, Text: "Branch pushed."}, otherEvent, "to alice"))
	if at(t, out.Content, "msgtype").Str() != MsgText || !slices.Equal(userIDs(t, out.Content), []string{aliceID, coordID}) {
		t.Errorf("text to a human = %s", out.Content.Compact())
	}
	if !at(t, out.Content, keyRelatesTo).Equal(ReplyTo(otherEvent).Encode()) {
		t.Errorf("the reply = %s", out.Content.Compact())
	}
	if v := receive(t, out, workerID, coordID); v.Outcome != Deliver || v.Kind != KindPeer {
		t.Errorf("the coordinator's copy: %v %q", v.Outcome, v.Reason)
	}
	out = mustSend(t)(c.Text(Text{To: []string{coordID}, Text: "Ready."}, "", "ready"))
	if at(t, out.Content, "msgtype").Str() != MsgNotice {
		t.Errorf("text to agents only is %s", at(t, out.Content, "msgtype").Compact())
	}
	_, err := c.Text(Text{To: []string{probeID}, Text: "x"}, "", "x")
	refusedWith(t, "text to the probe", err, ReasonTarget)
	_, err = c.Text(Text{To: []string{coordID}, Text: "token " + ownSample}, "", "x")
	refusedWith(t, "the own token", err, ReasonSecret)
	_, err = c.Text(Text{To: []string{coordID}, Text: ""}, "", "x")
	refusedWith(t, "empty text", err, ReasonSchema)
}

func TestComposeMemberNotice(t *testing.T) {
	c := composer(t, workerID)
	out := mustSend(t)(c.Notice(Notice{Event: EventLeft, Subject: workerID, Reason: "exit", To: []string{aliceID, coordID}}, "left"))
	if v := receive(t, out, workerID, coordID); v.Outcome != Deliver || v.Kind != KindNotice {
		t.Errorf("the coordinator's verdict: %v %q", v.Outcome, v.Reason)
	}
	cases := map[string]Notice{
		"another subject": {Event: EventLeft, Subject: coordID, Reason: "exit", To: []string{aliceID}},
		"admin's event":   {Event: EventAdded, Subject: workerID, Role: RoleWorker, To: []string{aliceID}},
		"to a worker":     {Event: EventBack, Subject: workerID, To: []string{lxcWorkerID}},
	}
	for name, n := range cases {
		_, err := c.Notice(n, "x")
		refusedWith(t, name, err, ReasonTarget)
	}
}

func TestComposeAdminNotice(t *testing.T) {
	c := composer(t, adminID)
	c.Gate = nil
	out := mustSend(t)(c.Notice(Notice{Event: EventAdded, Subject: workerID, Role: RoleWorker, To: []string{aliceID, coordID}}, "added"))
	if v := receive(t, out, adminID, coordID); v.Outcome != Deliver || v.Kind != KindNotice {
		t.Errorf("the coordinator's verdict: %v %q", v.Outcome, v.Reason)
	}
	if err := c.Admit(out); err != nil {
		t.Errorf("the admin's send is gated: %v", err)
	}
	agents := []string{coordID, lxcWorkerID, workerID}
	if _, err := c.Notice(Notice{Event: EventMoved, To: agents}, "moved"); err != nil {
		t.Errorf("moved: %v", err)
	}
	cases := map[string]Notice{
		"member's event": {Event: EventLeft, Subject: workerID, Reason: "exit", To: []string{aliceID}},
		"the probe":      {Event: EventParked, Subject: probeID, To: []string{aliceID}},
		"not a member":   {Event: EventParked, Subject: UserID("gone.9+desk.podman", testServer), To: []string{aliceID}},
	}
	for name, n := range cases {
		_, err := c.Notice(n, "x")
		refusedWith(t, name, err, ReasonTarget)
	}
}

func TestComposeCarried(t *testing.T) {
	c := composer(t, adminID)
	k := Carried{Kind: CarriedPing, Verb: "review", Ref: commitRef, From: workerID, SentMs: 1, V1Event: otherEvent, To: coordID, Text: "please review"}
	out := mustSend(t)(c.Carried(k, "carried from v1"))
	if v := receive(t, out, adminID, coordID); v.Outcome != Deliver || !v.Carried {
		t.Errorf("the coordinator's verdict: %v %q", v.Outcome, v.Reason)
	}
	_, err := composer(t, coordID).Carried(k, "x")
	refusedWith(t, "a member's carried item", err, ReasonSender)
	gone := k
	gone.To = UserID("gone.9+desk.podman", testServer)
	_, err = c.Carried(gone, "x")
	refusedWith(t, "to a former member", err, ReasonTarget)
	leak := k
	leak.Text = "key " + ownSample
	_, err = c.Carried(leak, "x")
	refusedWith(t, "a carried secret", err, ReasonSecret)
	bad := k
	bad.Verb = "deploy"
	_, err = c.Carried(bad, "x")
	refusedWith(t, "a carried unknown verb", err, ReasonVerb)
}

func TestComposeReport(t *testing.T) {
	c := composer(t, workerID)
	out := mustSend(t)(c.Report("Merged the parser fix\nPR 12 is in main."))
	if out.Purpose != PurposeReports || at(t, out.Content, "msgtype").Str() != MsgNotice || len(at(t, out.Content, keyMentions).Members()) != 0 {
		t.Errorf("report = %s", out.Content.Compact())
	}
	_, err := c.Report("\nno headline")
	refusedWith(t, "no headline", err, ReasonBody)
	_, err = c.Report("Leak\n" + ownSample)
	refusedWith(t, "a secret", err, ReasonSecret)
	_, err = composer(t, probeID).Report("Probe")
	refusedWith(t, "the probe", err, ReasonRole)
	_, err = composer(t, aliceID).Report("Human")
	refusedWith(t, "a human", err, ReasonSender)
}

func TestAdmitReportsPerHour(t *testing.T) {
	c := composer(t, workerID)
	c.Roster.Limits.ReportPerHour = 2
	for i := range 2 {
		out := mustSend(t)(c.Report("Report " + strings.Repeat("x", i+1)))
		if err := c.Admit(out); err != nil {
			t.Fatalf("report %d: %v", i, err)
		}
	}
	out := mustSend(t)(c.Report("Report three"))
	refusedWith(t, "the third report in an hour", c.Admit(out), ReasonRate)
	c.NowMs += ReportWindowMs
	if err := c.Admit(out); err != nil {
		t.Errorf("an hour later: %v", err)
	}
}

func TestAdmitDuplicatesAndRate(t *testing.T) {
	c := composer(t, workerID)
	text := mustSend(t)(c.Text(Text{To: []string{coordID}, Text: "same"}, "", "same"))
	if err := c.Admit(text); err != nil {
		t.Fatalf("first text: %v", err)
	}
	refusedWith(t, "the same text twice", c.Admit(text), ReasonDuplicate)
	notice := mustSend(t)(c.Notice(Notice{Event: EventBack, Subject: workerID, To: []string{coordID}}, "back"))
	for i := range 2 {
		if err := c.Admit(notice); err != nil {
			t.Fatalf("notice %d: %v", i, err)
		}
	}
	c.Gate.Bucket.Credit = 0
	other := mustSend(t)(c.Text(Text{To: []string{coordID}, Text: "other"}, "", "other"))
	refusedWith(t, "an empty bucket", c.Admit(other), ReasonRate)
}

// TestSendRefusalOrder pins the one order every composer refuses in (Composer): a text that
// fails two checks is refused by the first.
func TestSendRefusalOrder(t *testing.T) {
	gone := UserID("gone.9+desk.podman", testServer)
	carried := Carried{Kind: CarriedPing, Verb: "review", Ref: commitRef, From: workerID, SentMs: 1, V1Event: otherEvent, To: gone, Text: ownSample}
	cases := map[string]struct {
		send   func() (Outgoing, error)
		reason string
	}{
		"ping: target, then secret": {func() (Outgoing, error) {
			return composer(t, coordID).Ping(Ping{Verb: "review", To: []string{parkedID}, Ref: commitRef}, "", "review "+ownSample)
		}, ReasonTarget},
		"text: target, then secret": {func() (Outgoing, error) {
			return composer(t, coordID).Text(Text{To: []string{parkedID}, Text: ownSample}, "", "x")
		}, ReasonTarget},
		"notice: target, then secret": {func() (Outgoing, error) {
			return composer(t, adminID).Notice(Notice{Event: EventParked, Subject: gone, To: []string{aliceID}}, ownSample)
		}, ReasonTarget},
		"carried: target, then secret": {func() (Outgoing, error) { return composer(t, adminID).Carried(carried, "x") }, ReasonTarget},
		"report: form, then secret":    {func() (Outgoing, error) { return composer(t, workerID).Report("\n" + ownSample) }, ReasonBody},
		"question: form, then target": {func() (Outgoing, error) {
			return composer(t, coordID).Question(QuestionPost{Text: "Head\nQuestion: x", AskedBy: []string{aliceID}})
		}, ReasonBody},
		"question: target, then secret": {func() (Outgoing, error) {
			return composer(t, coordID).Question(QuestionPost{Text: "Head\nContext: " + ownSample + "\nQuestion: x", AskedBy: []string{aliceID}})
		}, ReasonTarget},
		"question: secret, then duplicate": {func() (Outgoing, error) {
			return asked(t).Question(QuestionPost{Text: "Again\nContext: " + ownSample + "\nQuestion: MIT  or\nCC-BY?"})
		}, ReasonSecret},
		"thread: target, then secret": {func() (Outgoing, error) { return asked(t).Thread(ThreadPost{N: 4, Text: ownSample}) }, ReasonTarget},
		"thread: role, then target":   {func() (Outgoing, error) { return composer(t, workerID).Thread(ThreadPost{N: 4, Text: "x"}) }, ReasonRole},
	}
	for name, tc := range cases {
		_, err := tc.send()
		refusedWith(t, name, err, tc.reason)
	}
}
