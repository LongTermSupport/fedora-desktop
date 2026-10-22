package proto

import "slices"

// The drop reasons of A 7.6, a closed set: a new reason is a feature.
const (
	ReasonVersion    = "version"
	ReasonSchema     = "schema"
	ReasonSize       = "size"
	ReasonEdit       = "edit"
	ReasonSender     = "sender"
	ReasonRole       = "role"
	ReasonTarget     = "target"
	ReasonVerb       = "verb"
	ReasonRef        = "ref"
	ReasonAllowlist  = "allowlist"
	ReasonExcluded   = "excluded"
	ReasonStale      = "stale"
	ReasonRate       = "rate"
	ReasonForgeRate  = "forge-rate"
	ReasonUnresolved = "unresolved"
	ReasonProvenance = "provenance"
)

// The ignore reasons of A 7.6.
const (
	IgnoreType           = "type"
	IgnoreSeen           = "seen"
	IgnoreSelf           = "self"
	IgnoreAdmin          = "admin"
	IgnoreNotAddressed   = "not-addressed"
	IgnoreEmpty          = "empty"
	IgnoreNotCoordinator = "not-coordinator"
)

// The refusals only a send gives (A 7.6).
const (
	ReasonSecret    = "secret"
	ReasonBody      = "body"
	ReasonDuplicate = "duplicate"
)

var (
	// DropReasons is the closed set of drop reasons, in A 7.6's order.
	DropReasons = []string{
		ReasonVersion, ReasonSchema, ReasonSize, ReasonEdit, ReasonSender, ReasonRole, ReasonTarget,
		ReasonVerb, ReasonRef, ReasonAllowlist, ReasonExcluded, ReasonStale, ReasonRate,
		ReasonForgeRate, ReasonUnresolved, ReasonProvenance,
	}
	// IgnoreReasons is the closed set of ignore reasons.
	IgnoreReasons = []string{
		IgnoreType, IgnoreSeen, IgnoreSelf, IgnoreAdmin, IgnoreNotAddressed, IgnoreEmpty, IgnoreNotCoordinator,
	}
	// SendRefusals are the drop reasons and the three only a send gives.
	SendRefusals = append(append([]string(nil), DropReasons...), ReasonSecret, ReasonBody, ReasonDuplicate)
)

// The kinds of a delivered entry (A 13.3).
const (
	KindHuman      = "HUMAN"
	KindPeer       = "PEER"
	KindPing       = "PING"
	KindNotice     = "NOTICE"
	KindTimeout    = "TIMEOUT"
	KindMembership = "MEMBERSHIP"
)

// The trust a delivered ping's reference carries (A 9.3, 13.2).
const (
	TrustTrusted  = "trusted"
	TrustExternal = "external"
)

// Outcome is what the verdict decided (A 7.6).
type Outcome int

// The outcomes. Record is an answer recorded against one of this member's pings and not
// delivered (rule 09); Index a questions-room post for the question index (rule 03); Redact an
// admin redaction of an item still held (rule 01); Fault an event judged in the reports room,
// which no member reads (rule 05).
const (
	Deliver Outcome = iota + 1
	Ignore
	Drop
	Record
	Index
	Redact
	Fault
)

// Event is the part of a timeline event the verdict reads.
type Event struct {
	ID, Type, Sender string
	OriginTs         int64
	Content          Value
}

// Related is what the member's store knows of an earlier event of the team: its sender and
// room, a ping's verb, and whether it was addressed to this member.
type Related struct {
	Sender, RoomID, Verb string
	AddressedToSelf      bool
}

// OwnPing is one of this member's own pings: its verb, its addressees and its
// origin_server_ts (B's local send time until its own event comes back, A 8.2).
type OwnPing struct {
	Verb     string
	To       []string
	OriginTs int64
}

// Lookup is the member's store as the verdict reads it.
type Lookup interface {
	Seen(id string) bool
	Event(id string) (Related, bool)
	OwnPing(id string) (OwnPing, bool)
}

// Answer is an answer to one of this member's pings (A 8.2): the ping, the verb, and whether its
// origin_server_ts is at or before the ping's deadline.
type Answer struct {
	Ping, Verb string
	OnTime     bool
}

// ForgeCheck is the forge check a ping's reference still needs (A 9.3), which the member runs
// with its own login before it delivers: the reference, its class, the ping's branch and, for
// a listed repository with branches, the branches that alone prove it.
type ForgeCheck struct {
	Ref      Ref
	Class    RefClass
	Branch   string
	Branches []string
}

// Trust is the `trust:` line of a proven reference: trusted for listed or org, else external.
func (f ForgeCheck) Trust() string {
	if f.Class == RefExternal {
		return TrustExternal
	}
	return TrustTrusted
}

// Verdict is the one judgement of an event (A 7.6). Content is set for an agent event, Human for
// a human's; Carried marks a carried item, delivered as a NOTICE.
type Verdict struct {
	Outcome Outcome
	Reason  string
	Kind    string
	Event   Event
	Class   SenderClass
	Content Content
	Human   Human
	Carried bool
	Answer  *Answer
	Forge   *ForgeCheck
	Redacts string
}

// Judge judges the events of one room for one member against the roster as it stands now
// (A 6.5). Flood is the member's receive window (A 11.1), which a delivery enters.
type Judge struct {
	Server, Self, RoomID, Purpose string
	Roster                        Roster
	NowMs                         int64
	Lookup                        Lookup
	Flood                         *Window
}

func ignore(v Verdict, reason string) Verdict {
	v.Outcome, v.Reason = Ignore, reason
	return v
}

func drop(v Verdict, reason string) Verdict {
	v.Outcome, v.Reason = Drop, reason
	return v
}

// refuse is a drop or, for the reasons A 7.6 ignores, an ignore.
func refuse(v Verdict, reason string) Verdict {
	if slices.Contains(IgnoreReasons, reason) {
		return ignore(v, reason)
	}
	return drop(v, reason)
}

func deliver(v Verdict, kind string) Verdict {
	v.Outcome, v.Kind = Deliver, kind
	return v
}

// Judge is A 7.6's rules 01-10 over one raw event, in order; the first that decides wins.
func (j Judge) Judge(raw Value) Verdict {
	typ := stringMember(raw, "type")
	if typ != MessageType {
		return j.notMessage(raw, typ)
	}
	ev, ok := decodeEvent(raw)
	v := Verdict{Event: ev}
	switch {
	case !ok:
		return drop(v, ReasonSchema)
	case j.Lookup.Seen(ev.ID):
		return ignore(v, IgnoreSeen)
	}
	if j.Purpose == PurposeQuestions {
		if indexed, fed := j.questionIndex(v); fed {
			return indexed
		}
	}
	if ev.Sender == j.Self {
		return ignore(v, IgnoreSelf)
	}
	v.Class = j.Roster.Sender(ev.Sender, j.Server)
	switch {
	case v.Class == SenderNone:
		return drop(v, ReasonSender)
	case j.Purpose == PurposeReports:
		v.Outcome = Fault
		return v
	case j.Purpose == PurposeQuestions:
		return j.questions(v)
	}
	return j.main(v)
}

// notMessage is rule 01: an admin's redaction of a well-formed event ID is Redact; the rest
// are ignored `type`.
func (j Judge) notMessage(raw Value, typ string) Verdict {
	redacts := stringMember(readFields(raw, "").optObject("content", ""), "redacts")
	if typ == RedactionType && stringMember(raw, "sender") == AdminID(j.Server) && ValidEventID(redacts) {
		return Verdict{Outcome: Redact, Redacts: redacts}
	}
	return Verdict{Outcome: Ignore, Reason: IgnoreType}
}

func stringMember(v Value, key string) string {
	if m, ok := v.Get(key); ok && m.kind == KindString {
		return m.s
	}
	return ""
}

// decodeEvent is rule 02: the event ID's grammar, no state key, not redacted, an object
// content, a non-negative origin_server_ts, and a sender.
func decodeEvent(raw Value) (Event, bool) {
	f := readFields(raw, ReasonSchema)
	ev := Event{
		ID:       f.str("event_id", true, ReasonSchema),
		Type:     MessageType,
		Sender:   f.str("sender", true, ReasonSchema),
		OriginTs: f.integer("origin_server_ts", true, -1, ReasonSchema),
		Content:  f.need("content", ReasonSchema),
	}
	unsigned := f.optObject("unsigned", ReasonSchema)
	if _, redacted := unsigned.Get("redacted_because"); redacted || f.has("state_key") {
		return ev, false
	}
	return ev, f.rule == "" && ValidEventID(ev.ID) && ev.OriginTs >= 0
}

// questionIndex is rule 03's exception: a question or thread post from any agent account, or a
// question from the admin, feeds the index once it decodes, and is judged no further.
func (j Judge) questionIndex(v Verdict) (Verdict, bool) {
	keys := QuestionKeys
	class := ClassifyUserID(v.Event.Sender, j.Server)
	if class == IDAdmin {
		keys = []string{KeyQuestion}
	}
	if class != IDAgent && class != IDAdmin || !hasAnyKey(v.Event.Content, keys) {
		return v, false
	}
	c, reason := DecodeContent(v.Event.Content, keys)
	if reason != "" {
		return drop(v, reason), true
	}
	v.Outcome, v.Content = Index, c
	return v, true
}

func hasAnyKey(content Value, keys []string) bool {
	for _, k := range keys {
		if _, ok := content.Get(k); ok {
			return true
		}
	}
	return false
}

// questions is rule 05 in the questions room: the coordinator reads humans only.
func (j Judge) questions(v Verdict) Verdict {
	switch {
	case j.Roster.Sender(j.Self, j.Server) != SenderCoordinator:
		return ignore(v, IgnoreNotCoordinator)
	case v.Class == SenderHuman:
		return j.human(v, false)
	}
	return drop(v, ReasonSender)
}

// main is rules 06-08 in the main room.
func (j Judge) main(v Verdict) Verdict {
	switch {
	case v.Class == SenderAdmin:
		return j.admin(v)
	case v.Class != SenderHuman:
		return j.agent(v)
	case !j.Roster.HumanText(j.Self):
		return drop(v, ReasonSender)
	}
	return j.human(v, true)
}

// human is the human path (A 7.5) to rule 10; in questions every message is addressed and
// none is stale. A carried item is the admin's alone (A 7.6 rule 05, 7.7).
func (j Judge) human(v Verdict, main bool) Verdict {
	if _, carried := v.Event.Content.Get(KeyCarried); carried {
		return drop(v, ReasonSender)
	}
	h, reason := DecodeHuman(v.Event.Content)
	if reason != "" {
		return refuse(v, reason)
	}
	v.Human = h
	original, ok := j.original(h, v.Event.Sender)
	switch {
	case !ok:
		return drop(v, ReasonEdit)
	case main && !j.humanAddressed(h, original):
		return ignore(v, IgnoreNotAddressed)
	case main && TextStale(v.Event.OriginTs, j.NowMs, j.Roster.Limits):
		return drop(v, ReasonStale)
	case !j.admit(v.Event.Sender):
		return drop(v, ReasonRate)
	}
	return deliver(v, KindHuman)
}

// original is the message an edit revises, which must be in this room with the same sender
// (A 7.5); a message that is no edit has none and is fine.
func (j Judge) original(h Human, sender string) (Related, bool) {
	if h.Revises == "" {
		return Related{}, true
	}
	r, ok := j.Lookup.Event(h.Revises)
	return r, ok && r.RoomID == j.RoomID && r.Sender == sender
}

// humanAddressed is A 7.5's addressing: a mention, `@room`, a reply to this member's event, a
// thread this member started, or an edit of a message addressed to it.
func (j Judge) humanAddressed(h Human, original Related) bool {
	return slices.Contains(h.Mentions, j.Self) || h.MentionRoom || h.Revises != "" && original.AddressedToSelf ||
		j.sentBySelf(h.ReplyTo) || j.sentBySelf(h.ThreadRoot)
}

func (j Judge) sentBySelf(id string) bool {
	if id == "" {
		return false
	}
	r, ok := j.Lookup.Event(id)
	return ok && r.Sender == j.Self
}

// admit enters one event of sender in the receive flood window (A 11.1, P65).
func (j Judge) admit(sender string) bool {
	return j.Flood.Admit(sender, j.Roster.Limits.RecvPerSenderMinute, ReceiveWindowMs, j.NowMs)
}

// admin is rule 06: the admin's notices other than a member's own, and its carried items. The
// admin is not flood-limited and its items are never stale (A 11.1).
func (j Judge) admin(v Verdict) Verdict {
	if !hasAnyKey(v.Event.Content, AdminMainKeys) {
		return ignore(v, IgnoreAdmin)
	}
	c, reason := DecodeContent(v.Event.Content, AdminMainKeys)
	if reason != "" {
		return drop(v, reason)
	}
	v.Content = c
	to := []string{c.Carried.To}
	if c.Key == KeyNotice {
		if MemberNotice(c.Notice.Event) {
			return ignore(v, IgnoreAdmin)
		}
		reason, to = j.Roster.checkAdminNotice(c.Notice), c.Notice.To
	} else {
		v.Carried = true
		reason = checkCarried(c.Carried, j.Server)
	}
	switch {
	case reason != "":
		return drop(v, reason)
	case !slices.Contains(to, j.Self):
		return ignore(v, IgnoreNotAddressed)
	}
	return deliver(v, KindNotice)
}

// agent is rules 08-10 for a coordinator, a worker or the probe.
func (j Judge) agent(v Verdict) Verdict {
	content := v.Event.Content
	if _, carried := content.Get(KeyCarried); carried {
		return drop(v, ReasonSender)
	}
	if _, ping := content.Get(KeyPing); v.Class == SenderProbe && !ping {
		return drop(v, ReasonRole)
	}
	c, reason := DecodeContent(content, MainKeys)
	if reason != "" {
		return drop(v, reason)
	}
	v.Content = c
	kind, reason, forge := j.Roster.checkAgent(v.Event.Sender, v.Class, c, j.probeCheck(c))
	switch {
	case reason != "":
		return drop(v, reason)
	case kind == KindPeer && !j.Roster.HumanText(j.Self):
		return drop(v, ReasonSender)
	case !slices.Contains(addressees(c), j.Self):
		return ignore(v, IgnoreNotAddressed)
	}
	v.Forge = forge
	answer, stale, isAnswer := j.answer(v)
	if isAnswer && (answer.Verb == VerbAck || stale) {
		v.Outcome, v.Answer = Record, &answer
		return v
	}
	switch {
	case j.stale(v):
		return drop(v, ReasonStale)
	case !j.admit(v.Event.Sender):
		return drop(v, ReasonRate)
	}
	if isAnswer {
		v.Answer = &answer
	}
	return deliver(v, kind)
}

// stale is rule 10's age for a ping (A 8.5) and agent text; a notice has none.
func (j Judge) stale(v Verdict) bool {
	switch v.Content.Key {
	case KeyPing:
		return PingStale(v.Event.OriginTs, v.Content.Ping.Verb, j.NowMs, j.Roster.Limits)
	case KeyText:
		return TextStale(v.Event.OriginTs, j.NowMs, j.Roster.Limits)
	}
	return false
}

// answer is the answer this ping gives to one of this member's own pings (A 8.2): an answer
// verb in the answer form, rooted at an own ping that addressed its sender; and whether it is
// stale.
func (j Judge) answer(v Verdict) (Answer, bool, bool) {
	p := v.Content.Ping
	if v.Content.Key != KeyPing || !IsAnswerVerb(p.Verb) || !v.Content.Relation.IsAnswer() {
		return Answer{}, false, false
	}
	own, ok := j.Lookup.OwnPing(v.Content.Relation.Root)
	if !ok || !slices.Contains(own.To, v.Event.Sender) {
		return Answer{}, false, false
	}
	deadline, expects := PingDeadlineMs(own.OriginTs, own.Verb, j.Roster.Limits)
	answer := Answer{Ping: v.Content.Relation.Root, Verb: p.Verb, OnTime: expects && AnswerOnTime(v.Event.OriginTs, deadline)}
	return answer, PingStale(v.Event.OriginTs, p.Verb, j.NowMs, j.Roster.Limits), true
}

// probeCheck is whether a ping answers the probe's own `check`, so the probe is addressable.
func (j Judge) probeCheck(c Content) bool { return probeCheckOf(j.Lookup, j.Roster, c) }

// probeCheckOf is probeCheck for the receiver and the sender alike.
func probeCheckOf(lookup Lookup, r Roster, c Content) bool {
	if c.Key != KeyPing || !c.Relation.IsAnswer() {
		return false
	}
	root, ok := lookup.Event(c.Relation.Root)
	return ok && root.Verb == VerbCheck && r.Members[root.Sender].Role == RoleProbe
}

// addressees is the content's `to`.
func addressees(c Content) []string {
	switch c.Key {
	case KeyPing:
		return c.Ping.To
	case KeyText:
		return c.Text.To
	case KeyNotice:
		return c.Notice.To
	case KeyCarried:
		return []string{c.Carried.To}
	}
	return nil
}

// checkAgent is the roster's rules over an agent's main-room content, for the receiver and the
// sender alike: a ping by the verb table and section 9, a text's addressees, a notice about the
// sender itself. It gives the kind, or the reason it fails, and the forge check a ping needs.
func (r Roster) checkAgent(sender string, class SenderClass, c Content, probeCheck bool) (string, string, *ForgeCheck) {
	switch c.Key {
	case KeyPing:
		reason, forge := r.checkPing(class, c.Ping, c.Relation, probeCheck)
		return KindPing, reason, forge
	case KeyText:
		return KindPeer, checkTo(c.Text.To, MaxTextTo, func(id string) bool { return r.AddressableAgent(id) || r.IsHuman(id) }), nil
	}
	n := c.Notice
	if !MemberNotice(n.Event) || n.Subject != sender {
		return KindNotice, ReasonTarget, nil
	}
	return KindNotice, r.checkNoticeTo(n), nil
}

// checkPing is A 8.1 then A 9.1-9.2: the verb, its senders, its relation, its addressees and its
// reference.
func (r Roster) checkPing(class SenderClass, p Ping, rel Relation, probeCheck bool) (string, *ForgeCheck) {
	rule, known := Verbs[p.Verb]
	switch {
	case !known:
		return ReasonVerb, nil
	case !rule.SentBy(class):
		return ReasonRole, nil
	case !relationFits(rule.Relation, rel):
		return ReasonSchema, nil
	case p.Verb == VerbCheck && len(p.To) != 1:
		return ReasonTarget, nil
	}
	if reason := checkTo(p.To, MaxPingTo, func(id string) bool { return r.AddressableFor(p.Verb, id, probeCheck) }); reason != "" {
		return reason, nil
	}
	return r.checkRef(rule, p.Ref, p.Branch)
}

func relationFits(need Need, rel Relation) bool {
	switch need {
	case Required:
		return rel.IsAnswer()
	case Forbidden:
		return rel.Kind == RelNone
	case Optional:
	}
	return rel.Kind == RelNone || rel.IsAnswer()
}

// checkTo is a sorted `to` of 1 to most distinct addressees, each one ok: else `target`.
func checkTo(to []string, most int, ok func(string) bool) string {
	if len(to) < 1 || len(to) > most {
		return ReasonTarget
	}
	for i, id := range to {
		if i > 0 && to[i-1] == id || !ok(id) {
			return ReasonTarget
		}
	}
	return ""
}

// refShape is a reference's grammar for the verb (A 8.1, 9.1): required, forbidden or optional,
// its form, and a branch only beside a path or commit reference.
func refShape(rule VerbRule, ref, branch string) (Ref, string) {
	if ref == "" {
		if rule.Ref == Required || branch != "" {
			return Ref{}, ReasonRef
		}
		return Ref{}, ""
	}
	parsed, ok := ParseRef(ref)
	switch {
	case !ok || !rule.AllowsForm(parsed.Form):
		return Ref{}, ReasonRef
	case branch != "" && (parsed.Form != RefPath && parsed.Form != RefCommit || !ValidBranch(branch)):
		return Ref{}, ReasonRef
	}
	return parsed, ""
}

// checkRef is refShape, then the reference's trust class (A 9.2) and the path prefixes (A 9.1),
// giving the forge check the reference needs.
func (r Roster) checkRef(rule VerbRule, ref, branch string) (string, *ForgeCheck) {
	parsed, reason := refShape(rule, ref, branch)
	if reason != "" || ref == "" {
		return reason, nil
	}
	class := r.Forge.Class(parsed)
	switch {
	case class == RefExcluded:
		return ReasonExcluded, nil
	case class == RefExternal && !rule.External:
		return ReasonAllowlist, nil
	case parsed.Form == RefPath && class != RefExternal && !PathAllowed(parsed.Path, r.Forge.PathPrefixes):
		return ReasonAllowlist, nil
	}
	return "", &ForgeCheck{Ref: parsed, Class: class, Branch: branch, Branches: r.Forge.ListedBranches(parsed)}
}

// checkNoticeTo is a notice's addressees (A 7.4): the humans and the coordinator, never the
// subject; `moved` goes to agents only.
func (r Roster) checkNoticeTo(n Notice) string {
	if n.Event == EventMoved {
		return checkTo(n.To, MaxMovedTo, r.AddressableAgent)
	}
	return checkTo(n.To, MaxNoticeTo, func(id string) bool {
		m, member := r.Members[id]
		return id != n.Subject && (r.IsHuman(id) || member && !m.Parked && m.Role == RoleCoordinator)
	})
}

// checkAdminNotice is an admin notice's subject, a member (or, for `removed`, a former one),
// and its addressees.
func (r Roster) checkAdminNotice(n Notice) string {
	if _, member := r.Members[n.Subject]; !member && n.Event != EventMoved && n.Event != EventRemoved {
		return ReasonTarget
	}
	return r.checkNoticeTo(n)
}

// checkCarried is a carried item's `from`, a human or an agent of the server, and a carried
// ping's verb and reference by A 8.1 and 9.1; the reference is not forge-checked again.
func checkCarried(c Carried, server string) string {
	if class := ClassifyUserID(c.From, server); class != IDHuman && class != IDAgent {
		return ReasonSchema
	}
	if c.Kind != CarriedPing {
		return ""
	}
	rule, known := Verbs[c.Verb]
	if !known {
		return ReasonVerb
	}
	return reasonOf(refShape(rule, c.Ref, ""))
}

func reasonOf(_ Ref, reason string) string { return reason }
