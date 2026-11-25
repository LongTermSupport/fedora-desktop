package proto

import (
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/richtext"
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

// DuplicateKey is the event's duplicate identity (A 11.2), for a sender that keeps its window
// by digest; "" for an event exempt from the window.
func (o Outgoing) DuplicateKey() string {
	if !o.dedupe {
		return ""
	}
	return o.dupKey
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
// the unattached human post Answers. A post in the thread that answers nothing waits on the
// humans, `m.text` mentioning them all, unless it is a Note or a Close (quiet); a reply
// mentions only To, the human it answers ("" for none, quiet). Close closes Q<N>. Root is the
// thread an unattached post is in ("" for a top-level post, which becomes a new thread's root):
// a reply without N stays in the human's thread, never a plain reply in the room.
type ThreadPost struct {
	N                 int64
	Text              string
	Close, Note       bool
	Answers, To, Root string
}

// LinkPost is the link-back (A 10.3): the top-level reply to Post, a human post attached to Q<N>
// outside its thread, naming Link, the thread answer, by Permalink; it mentions To, Post's
// sender.
type LinkPost struct {
	N                         int64
	Post, Link, To, Permalink string
}

// RetitlePost is a coordinator's retitle of its own question Q<N> (A 10.4): Original is the
// question's root event as fetched, and Headline the new headline, which a closed question
// prefixes with ClosedTitle.
type RetitlePost struct {
	N        int64
	Headline string
	Original Event
}

// ClosedTitle is what a closed question's retitled headline starts with (the owner's v1 form).
const ClosedTitle = "✅ CLOSED — "

// editMarker starts an edit's fallback body, as Matrix clients write it.
const editMarker = "* "

// Permalink is the matrix.to link to an event of a room, which a link-back names.
func Permalink(roomID, eventID string) string { return "https://matrix.to/#/" + roomID + "/" + eventID }

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
	members := []Member{M("v", Int(WireMajor)), M("to", strArray(t.To)), M("text", Str(t.Text))}
	if r := t.Relay; r != nil {
		members = append(members, M("relay", Obj(M("room", Str(r.Room)), M("id", Str(r.ID)), M("q", Int(r.Q)))))
	}
	content := formatted(agentContent(msgtype, display, t.To, rel, KeyText, Obj(members...)))
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
	content := formatted(Obj(M("msgtype", Str(MsgNotice)), M("body", Str(text)), M(keyMentions, Obj()), M(KeyReport, Obj(M("v", Int(WireMajor))))))
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
	if c.class() == SenderCoordinator {
		why, err := WhyOf(q.Text)
		if err != nil {
			return Outgoing{}, err
		}
		obj = append(obj, M("why", Str(why)))
	}
	if q.RemindAfterS != 0 {
		obj = append(obj, M("remind_after_s", Int(q.RemindAfterS)))
	}
	content := formatted(agentContent(MsgText, body, c.Roster.Humans, Relation{}, KeyQuestion, Obj(obj...)))
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
	switch {
	case t.Answers != "" && t.To != "":
		if !c.Roster.IsHuman(t.To) {
			return Outgoing{}, refused(ReasonTarget, "a reply mentions the human it answers")
		}
		msgtype, mentioned = MsgText, []string{t.To}
	case t.Answers == "" && t.N > 0 && !t.Note && !t.Close:
		msgtype, mentioned = MsgText, c.Roster.Humans
		obj = append(obj, M("wait", Bool(true)))
	}
	content := formatted(agentContent(msgtype, body, mentioned, rel, KeyThread, Obj(obj...)))
	if _, err := decodeSent(content, QuestionKeys); err != nil {
		return Outgoing{}, err
	}
	if err := c.screen(body); err != nil {
		return Outgoing{}, err
	}
	key := DuplicateKey(KeyThread, "", rel.InReplyTo, nil, body)
	return Outgoing{Purpose: PurposeQuestions, Content: content, dupKey: key, dedupe: true}, nil
}

// LinkBack composes the link-back (A 10.3): after the coordinator answered, in Q<N>'s thread,
// a human post attached to Q<N> outside it, one top-level reply to that post naming the
// answer, `m.text` mentioning the post's sender. It neither waits nor closes.
func (c Composer) LinkBack(l LinkPost) (Outgoing, error) {
	if class, err := c.agent(); err != nil || class != SenderCoordinator {
		return Outgoing{}, questionsRole(err)
	}
	if _, ok := c.Questions.Question(l.N); !ok {
		return Outgoing{}, refused(ReasonTarget, QuestionName(int(l.N))+" is not a question")
	}
	if attached, known := c.Questions.Attachment(l.Post); !known || attached != l.N {
		return Outgoing{}, refused(ReasonTarget, "the post linked back to is not attached to "+QuestionName(int(l.N)))
	}
	if !c.Roster.IsHuman(l.To) {
		return Outgoing{}, refused(ReasonTarget, "a link-back mentions the human whose post it answers")
	}
	body := QuestionName(int(l.N)) + ": answered in its thread: " + l.Permalink
	obj := Obj(M("v", Int(WireMajor)), M("n", Int(l.N)), M("link", Str(l.Link)))
	content := formatted(agentContent(MsgText, body, []string{l.To}, ReplyTo(l.Post), KeyThread, obj))
	if _, err := decodeSent(content, QuestionKeys); err != nil {
		return Outgoing{}, err
	}
	if err := c.screen(body); err != nil {
		return Outgoing{}, err
	}
	key := DuplicateKey(KeyThread, "", l.Post, nil, body)
	return Outgoing{Purpose: PurposeQuestions, Content: content, dupKey: key, dedupe: true}, nil
}

// Retitle composes the coordinator's retitle of its own question (A 10.4): a quiet `m.replace`
// edit of the root whose new content is the root's own `teamchat.question` and sections under
// the new headline, `Q<n> ✅ CLOSED — <headline>` once the question is closed.
func (c Composer) Retitle(r RetitlePost) (Outgoing, error) {
	if class, err := c.agent(); err != nil || class != SenderCoordinator {
		return Outgoing{}, questionsRole(err)
	}
	q, ok := c.Questions.Question(r.N)
	switch {
	case !ok || q.Redacted() || q.Provisional:
		return Outgoing{}, refused(ReasonTarget, QuestionName(int(r.N))+" is not a question this index keeps")
	case r.Original.ID != q.EventID || r.Original.Sender != c.Self || q.From != c.Self:
		return Outgoing{}, refused(ReasonTarget, QuestionName(int(r.N))+" was asked by another seat; only its sender can edit it")
	}
	original, err := decodeSent(r.Original.Content, []string{KeyQuestion})
	if err != nil {
		return Outgoing{}, err
	}
	headline := strings.TrimPrefix(strings.TrimSpace(r.Headline), ClosedTitle)
	if strings.Contains(headline, "\n") {
		return Outgoing{}, refused(ReasonBody, "the headline is one line")
	}
	if q.Closed {
		headline = ClosedTitle + headline
	}
	body := QuestionName(int(r.N)) + " " + headline
	if _, rest, found := strings.Cut(original.Body, "\n"); found {
		body += "\n" + rest
	}
	if _, _, err := QuestionForm(r.N, body); err != nil {
		return Outgoing{}, err
	}
	obj := typedObject(r.Original.Content, KeyQuestion)
	newContent := formatted(agentContent(MsgText, body, c.Roster.Humans, Relation{}, KeyQuestion, obj))
	content := Obj(M("msgtype", Str(MsgNotice)), M("body", Str(editMarker+body)), M(keyMentions, Obj()),
		M(keyNewContent, newContent), M(keyRelatesTo, Obj(M("rel_type", Str(relReplace)), M("event_id", Str(q.EventID)))))
	if _, reason := DecodeRetitle(content); reason != "" {
		return Outgoing{}, refused(reason, "")
	}
	if err := c.screen(headline); err != nil {
		return Outgoing{}, err
	}
	key := DuplicateKey(relReplace, "", q.EventID, nil, body)
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
		return refused(ReasonRate, DetailReportPerHour)
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
	switch {
	case reason == "":
	case dc.Key == KeyPing && (reason == ReasonRef || reason == ReasonAllowlist || reason == ReasonExcluded):
		return nil, refused(reason, c.Roster.RefCause(dc.Ping))
	default:
		return nil, refused(reason, "")
	}
	if err := c.addresseesTake(dc); err != nil {
		return nil, err
	}
	return forge, nil
}

// addresseesTake refuses the addressees a receiver would not take (issue #422): the sender
// itself, whose own event its receiver ignores, and, for a relay (a human's post passed on), an
// agent whose roster entry says `human_text: false`, which takes no human text (rule 08).
// Either would be "sent" and never seen.
func (c Composer) addresseesTake(dc Content) error {
	to := addressees(dc)
	if slices.Contains(to, c.Self) {
		return refused(ReasonTarget, "the addressees name this member itself, which never receives its own message")
	}
	if dc.Key != KeyText || dc.Text.Relay == nil {
		return nil
	}
	for _, id := range to {
		if _, agent := c.Roster.Members[id]; agent && !c.Roster.HumanText(id) {
			return refused(ReasonTarget, id+" takes no human text (its roster entry says human_text false), so it would drop this relayed human post")
		}
	}
	return nil
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
// unattached human post in its thread (Root's, or a new one rooted at the post); else `target`.
func (c Composer) threadRelation(t ThreadPost) (Relation, error) {
	attached, known := c.Questions.Attachment(t.Answers)
	if t.N == 0 {
		switch {
		case t.Answers == "" || !known || attached != 0 || t.Close:
			return Relation{}, refused(ReasonTarget, "a reply without a question answers an unattached human post and closes nothing")
		case t.Root == "":
			return InThread(t.Answers, t.Answers, true), nil
		case t.Root == t.Answers || !ValidEventID(t.Root):
			return Relation{}, refused(ReasonTarget, "the thread root is another event than the post answered")
		}
		return InThread(t.Root, t.Answers, false), nil
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

// formatted is content with its body's HTML added beside the body (A 7.1 `format`, Plan 00241
// Phase 3), for the posts a person reads as an agent wrote them: reports, questions, thread posts
// and agent text. The HTML is display only, so when it would take the content past MaxContent
// the content goes as it was, its plain body complete, rather than being refused for it.
func formatted(content Value) Value {
	members := make([]Member, 0, len(content.Members())+2)
	for _, m := range content.Members() {
		members = append(members, m)
		if m.Key == "body" {
			members = append(members, M(keyFormat, Str(richtext.Format)), M(keyFormattedBody, Str(richtext.HTML(m.Val.Str()))))
		}
	}
	if with := Obj(members...); with.Size() <= MaxContent {
		return with
	}
	return content
}

// typedObject is content's typed object under key, which the caller has decoded.
func typedObject(content Value, key string) Value {
	for _, m := range content.Members() {
		if m.Key == key {
			return m.Val
		}
	}
	return Obj()
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
