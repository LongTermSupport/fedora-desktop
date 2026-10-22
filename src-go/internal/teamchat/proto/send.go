package proto

import (
	"slices"
)

// Outgoing is one composed event: the room it goes to (a purpose), its content, and the forge
// check its reference needs, which the sender runs before it posts (A 9.3). Admit then spends
// its flood budget.
type Outgoing struct {
	Purpose string
	Content Value
	Forge   *ForgeCheck
	dupKey  string
	dedupe  bool
	report  bool
}

// QuestionPost is a question to compose (A 10.2): N is the admin's (a carried v1 question,
// D 5.6) and 0 for the coordinator, whose question takes the index's next number.
type QuestionPost struct {
	N            int64
	Text         string
	AskedBy, Re  []string
	RemindAfterS int64
}

// ThreadPost is a coordinator's post in questions (A 10.3). With N it goes in Q<N>'s thread,
// answering the attached human post Answers when one is named; without N it is the reply to
// the unattached human post Answers. Ask mentions the humans; Close closes Q<N>.
type ThreadPost struct {
	N          int64
	Text       string
	Close, Ask bool
	Answers    string
}

// Composer builds what a sender posts, and refuses what a receiver would drop. Every composer
// refuses in one order, the first failure winning: the sender's class and role; what the
// content is built from (the question index's number and relation, `schema` and `target`; the
// body form of A 10.1-10.3, `body`); the receiver's own rules over the built content (decode,
// then the roster: `schema`, `size`, `target` and the rest); the secret check (`secret`); an
// open question asking the same (`duplicate`). The sender then runs the forge check and Admit
// spends the flood budget (the duplicate window, `rate`). Token is the sender's own access
// token (R43). Gate and Reports are the sender's flood state; the admin has neither (its posts
// are C's and D's, bounded by the change they record). Questions is the questions room's
// index, for the coordinator.
type Composer struct {
	Server, Self, Token string
	Roster              Roster
	Lookup              Lookup
	Gate                *SendGate
	Reports             *Window
	Questions           *QuestionIndex
	NowMs               int64
}

// Ping composes a ping (A 7.2, 8.1); answers is the ping it answers, "" for none. A worker's
// ping that names a human copies the coordinator (P21).
func (c Composer) Ping(p Ping, answers, display string) (Outgoing, error) {
	class, err := c.agent()
	if err != nil {
		return Outgoing{}, err
	}
	p.To = c.copyCoordinator(class, p.To)
	rel := Relation{}
	if answers != "" {
		rel = AnswerTo(answers)
	}
	obj := []Member{M("v", Int(WireMajor)), M("verb", Str(p.Verb)), M("to", strArray(p.To))}
	if p.Ref != "" {
		obj = append(obj, M("ref", Str(p.Ref)))
	}
	if p.Branch != "" {
		obj = append(obj, M("branch", Str(p.Branch)))
	}
	content := agentContent(MsgNotice, display, p.To, rel, KeyPing, Obj(obj...))
	forge, err := c.checkMain(class, content)
	if err != nil {
		return Outgoing{}, err
	}
	if err := c.screen(display); err != nil {
		return Outgoing{}, err
	}
	key := DuplicateKey(p.Verb, p.Ref, answers, p.To, "")
	return Outgoing{Purpose: PurposeMain, Content: content, Forge: forge, dupKey: key, dedupe: true}, nil
}

// Text composes agent text (A 7.3): `m.text` when it is to a human, so Element notifies them,
// else `m.notice`; replyTo is the event it replies to, "" for none.
func (c Composer) Text(t Text, replyTo, display string) (Outgoing, error) {
	class, err := c.agent()
	if err != nil {
		return Outgoing{}, err
	}
	t.To = c.copyCoordinator(class, t.To)
	msgtype := MsgNotice
	if slices.ContainsFunc(t.To, c.Roster.IsHuman) {
		msgtype = MsgText
	}
	rel := Relation{}
	if replyTo != "" {
		rel = ReplyTo(replyTo)
	}
	obj := Obj(M("v", Int(WireMajor)), M("to", strArray(t.To)), M("text", Str(t.Text)))
	content := agentContent(msgtype, display, t.To, rel, KeyText, obj)
	if _, err := c.checkMain(class, content); err != nil {
		return Outgoing{}, err
	}
	if err := c.screen(t.Text, display); err != nil {
		return Outgoing{}, err
	}
	key := DuplicateKey("text", "", replyTo, t.To, t.Text)
	return Outgoing{Purpose: PurposeMain, Content: content, dupKey: key, dedupe: true}, nil
}

// Notice composes a lifecycle notice (A 7.4): a member's own `left`, `disconnected` or `back`,
// or the admin's `added`, `removed`, `parked`, `role` and `moved`, never about the probe. A
// notice is exempt from the duplicate window.
func (c Composer) Notice(n Notice, display string) (Outgoing, error) {
	obj := []Member{M("v", Int(WireMajor)), M("event", Str(n.Event))}
	if n.Subject != "" {
		obj = append(obj, M("subject", Str(n.Subject)))
	}
	if n.Reason != "" {
		obj = append(obj, M("reason", Str(n.Reason)))
	}
	if n.Role != "" {
		obj = append(obj, M("role", Str(n.Role)))
	}
	obj = append(obj, M("to", strArray(sortedStrs(n.To))))
	content := agentContent(MsgNotice, display, n.To, Relation{}, KeyNotice, Obj(obj...))
	if err := c.checkNotice(content); err != nil {
		return Outgoing{}, err
	}
	if err := c.screen(display); err != nil {
		return Outgoing{}, err
	}
	return Outgoing{Purpose: PurposeMain, Content: content, dupKey: DuplicateKey(KeyNotice, "", "", n.To, "")}, nil
}

// Carried composes the admin's carried item (A 7.7), to one current member; its text passes
// the secret check.
func (c Composer) Carried(k Carried, display string) (Outgoing, error) {
	if c.class() != SenderAdmin {
		return Outgoing{}, refused(ReasonSender, "only the admin carries")
	}
	obj := []Member{M("v", Int(WireMajor)), M("kind", Str(k.Kind))}
	if k.Kind == CarriedPing {
		obj = append(obj, M("verb", Str(k.Verb)))
		if k.Ref != "" {
			obj = append(obj, M("ref", Str(k.Ref)))
		}
	}
	obj = append(obj, M("from", Str(k.From)), M("sent_ms", Int(k.SentMs)), M("v1_event", Str(k.V1Event)),
		M("to", strArray([]string{k.To})), M("text", Str(k.Text)), M("possible_duplicate", Bool(k.PossibleDuplicate)))
	content := agentContent(MsgNotice, display, []string{k.To}, Relation{}, KeyCarried, Obj(obj...))
	dc, err := decodeSent(content, AdminMainKeys)
	if err != nil {
		return Outgoing{}, err
	}
	if reason := checkCarried(dc.Carried, c.Server); reason != "" {
		return Outgoing{}, refused(reason, "")
	}
	if _, member := c.Roster.Members[k.To]; !member {
		return Outgoing{}, refused(ReasonTarget, "a carried item goes to a current member")
	}
	if err := c.screen(k.Text, display); err != nil {
		return Outgoing{}, err
	}
	return Outgoing{Purpose: PurposeMain, Content: content}, nil
}

// Report composes a report (A 10.1): its text is the body, with no mentions, as `m.notice`.
func (c Composer) Report(text string) (Outgoing, error) {
	class, err := c.agent()
	switch {
	case err != nil:
		return Outgoing{}, err
	case class == SenderProbe:
		return Outgoing{}, refused(ReasonRole, "the probe sends pings only")
	}
	if _, err := ReportForm(text); err != nil {
		return Outgoing{}, err
	}
	content := Obj(M("msgtype", Str(MsgNotice)), M("body", Str(text)), M(keyMentions, Obj()), M(KeyReport, Obj(M("v", Int(WireMajor)))))
	if _, err := decodeSent(content, ReportKeys); err != nil {
		return Outgoing{}, err
	}
	if err := c.screen(text); err != nil {
		return Outgoing{}, err
	}
	key := DuplicateKey(KeyReport, "", "", nil, text)
	return Outgoing{Purpose: PurposeReports, Content: content, dupKey: key, dedupe: true, report: true}, nil
}

// Question composes a question (A 10.2) as `m.text` mentioning every human: the coordinator's
// numbered by the index and refused `duplicate` when an open question asks the same, the
// admin's with its own number.
func (c Composer) Question(q QuestionPost) (Outgoing, error) {
	n, err := c.questionNumber(q.N)
	if err != nil {
		return Outgoing{}, err
	}
	body := QuestionName(int(n)) + " " + q.Text
	_, question, err := QuestionForm(n, body)
	if err != nil {
		return Outgoing{}, err
	}
	obj := []Member{M("v", Int(WireMajor)), M("n", Int(n)), M("asked_by", strArray(q.AskedBy)), M("re", strArray(q.Re))}
	if q.RemindAfterS != 0 {
		obj = append(obj, M("remind_after_s", Int(q.RemindAfterS)))
	}
	content := agentContent(MsgText, body, c.Roster.Humans, Relation{}, KeyQuestion, Obj(obj...))
	if _, err := decodeSent(content, QuestionKeys); err != nil {
		return Outgoing{}, err
	}
	for _, id := range q.AskedBy {
		if _, member := c.Roster.Members[id]; !member {
			return Outgoing{}, refused(ReasonTarget, "asked_by names members")
		}
	}
	if err := c.screen(q.Text); err != nil {
		return Outgoing{}, err
	}
	if open, dup := c.Questions.OpenDuplicate(question); dup && c.class() != SenderAdmin {
		return Outgoing{}, refused(ReasonDuplicate, QuestionName(int(open))+" is open with the same question")
	}
	key := DuplicateKey(KeyQuestion, "", "", nil, body)
	return Outgoing{Purpose: PurposeQuestions, Content: content, dupKey: key, dedupe: true}, nil
}

// Thread composes the coordinator's post in questions (A 10.3): in a question's thread, its
// fallback reply the thread's latest event or the attached post it answers; or the reply to
// an unattached human post, which closes nothing.
func (c Composer) Thread(t ThreadPost) (Outgoing, error) {
	if class, err := c.agent(); err != nil || class != SenderCoordinator {
		return Outgoing{}, questionsRole(err)
	}
	rel, err := c.threadRelation(t)
	if err != nil {
		return Outgoing{}, err
	}
	body, err := ThreadBody(t.N, t.Text, t.Close)
	if err != nil {
		return Outgoing{}, err
	}
	obj := []Member{M("v", Int(WireMajor))}
	if t.N > 0 {
		obj = append(obj, M("n", Int(t.N)))
	}
	if t.Close {
		obj = append(obj, M("close", Bool(true)))
	}
	msgtype, mentioned := MsgNotice, []string(nil)
	if t.Ask {
		msgtype, mentioned = MsgText, c.Roster.Humans
	}
	content := agentContent(msgtype, body, mentioned, rel, KeyThread, Obj(obj...))
	if _, err := decodeSent(content, QuestionKeys); err != nil {
		return Outgoing{}, err
	}
	if err := c.screen(body); err != nil {
		return Outgoing{}, err
	}
	key := DuplicateKey(KeyThread, "", rel.InReplyTo, nil, body)
	return Outgoing{Purpose: PurposeQuestions, Content: content, dupKey: key, dedupe: true}, nil
}

// Admit spends a composed event's flood budget, or refuses it having spent nothing: the report
// window (`report_per_hour`), then the duplicate window and the bucket (A 11.1, 11.2). The
// admin's posts are not limited.
func (c Composer) Admit(out Outgoing) error {
	if c.class() == SenderAdmin {
		return nil
	}
	limit := c.Roster.Limits.ReportPerHour
	if out.report && int64(c.Reports.Count(c.Self, ReportWindowMs, c.NowMs)) >= limit {
		return refused(ReasonRate, "report_per_hour")
	}
	if reason := c.Gate.Admit(c.Roster.Limits, out.dupKey, out.dedupe, c.NowMs); reason != "" {
		return refused(reason, "")
	}
	if out.report {
		c.Reports.Admit(c.Self, limit, ReportWindowMs, c.NowMs)
	}
	return nil
}

func (c Composer) class() SenderClass { return c.Roster.Sender(c.Self, c.Server) }

// agent is the sender's class when it is a coordinator, a worker or the probe; else `sender`.
func (c Composer) agent() (SenderClass, error) {
	switch class := c.class(); class {
	case SenderCoordinator, SenderWorker, SenderProbe:
		return class, nil
	case SenderNone, SenderAdmin, SenderHuman:
	}
	return SenderNone, refused(ReasonSender, "not an active agent member of the team")
}

// copyCoordinator is a worker's addressees with the coordinator added when they hold a human
// (P21), sorted.
func (c Composer) copyCoordinator(class SenderClass, to []string) []string {
	to = sortedStrs(to)
	coordinator, ok := c.Roster.Coordinator()
	if class == SenderWorker && ok && slices.ContainsFunc(to, c.Roster.IsHuman) && !slices.Contains(to, coordinator) {
		to = sortedStrs(append(to, coordinator))
	}
	return to
}

// checkMain is a member's main-room content judged as its receivers judge it (A 7.6 rule 08):
// the probe sends pings only, then the roster's rules; it gives the forge check a ping needs.
func (c Composer) checkMain(class SenderClass, content Value) (*ForgeCheck, error) {
	if _, ping := content.Get(KeyPing); class == SenderProbe && !ping {
		return nil, refused(ReasonRole, "the probe sends pings only")
	}
	dc, err := decodeSent(content, MainKeys)
	if err != nil {
		return nil, err
	}
	_, reason, forge := c.Roster.checkAgent(c.Self, class, dc, probeCheckOf(c.Lookup, c.Roster, dc))
	if reason != "" {
		return nil, refused(reason, "")
	}
	return forge, nil
}

// checkNotice is a notice judged by the rules for its sender: the admin's under rule 06, never
// a member's own event and never about the probe; a member's under rule 08.
func (c Composer) checkNotice(content Value) error {
	if c.class() != SenderAdmin {
		class, err := c.agent()
		if err != nil {
			return err
		}
		_, err = c.checkMain(class, content)
		return err
	}
	dc, err := decodeSent(content, AdminMainKeys)
	if err != nil {
		return err
	}
	n := dc.Notice
	if MemberNotice(n.Event) || n.Subject != "" && c.Roster.Members[n.Subject].Role == RoleProbe {
		return refused(ReasonTarget, "the admin posts no notice about the probe or for a member's session")
	}
	if reason := c.Roster.checkAdminNotice(n); reason != "" {
		return refused(reason, "")
	}
	return nil
}

// questionNumber is the coordinator's next number (it names none) or the admin's own.
func (c Composer) questionNumber(n int64) (int64, error) {
	switch class := c.class(); {
	case class == SenderAdmin && n > 0:
		return n, nil
	case class == SenderAdmin || class == SenderCoordinator && n != 0:
		return 0, refused(ReasonSchema, "only the admin numbers a question")
	case class == SenderCoordinator:
		return c.Questions.Next(), nil
	}
	_, err := c.agent()
	return 0, questionsRole(err)
}

// threadRelation is a thread post's relation from the index: Q<N>'s thread, or the reply to an
// unattached human post; else `target`.
func (c Composer) threadRelation(t ThreadPost) (Relation, error) {
	attached, known := c.Questions.Attachment(t.Answers)
	if t.N == 0 {
		if t.Answers == "" || !known || attached != 0 || t.Close {
			return Relation{}, refused(ReasonTarget, "a reply without a question answers an unattached human post and closes nothing")
		}
		return ReplyTo(t.Answers), nil
	}
	q, ok := c.Questions.Question(t.N)
	switch {
	case !ok:
		return Relation{}, refused(ReasonTarget, QuestionName(int(t.N))+" is not a question")
	case t.Answers == "":
		return InThread(q.EventID, q.Latest, true), nil
	case !known || attached != t.N:
		return Relation{}, refused(ReasonTarget, "the post answered is not attached to "+QuestionName(int(t.N)))
	}
	return InThread(q.EventID, t.Answers, false), nil
}

// screen is A 7.3's secret check over each free text the member posts, its own token included.
func (c Composer) screen(texts ...string) error {
	for _, text := range texts {
		if name, found := SecretShape(text, c.Token); found {
			return refused(ReasonSecret, name)
		}
	}
	return nil
}

func questionsRole(err error) error {
	if err != nil {
		return err
	}
	return refused(ReasonRole, "only the coordinator posts in questions")
}

// decodeSent is DecodeContent as a refusal.
func decodeSent(content Value, keys []string) (Content, error) {
	c, reason := DecodeContent(content, keys)
	if reason != "" {
		return Content{}, refused(reason, "")
	}
	return c, nil
}

// agentContent is A 7.1's shape: msgtype, body, the mentions of the addressees sorted, the
// relation when there is one, and the typed object.
func agentContent(msgtype, body string, mentioned []string, rel Relation, key string, obj Value) Value {
	mentions := Obj()
	if len(mentioned) > 0 {
		mentions = Obj(M("user_ids", strArray(sortedStrs(mentioned))))
	}
	members := []Member{M("msgtype", Str(msgtype)), M("body", Str(body)), M(keyMentions, mentions)}
	if rel.Kind != RelNone {
		members = append(members, M(keyRelatesTo, rel.Encode()))
	}
	return Obj(append(members, M(key, obj))...)
}
