package proto

import (
	"regexp"
	"slices"
	"strings"
)

// The typed keys of A 7.1 and 7.7: exactly one is the meaning of an agent event.
const (
	KeyPing     = "teamchat.ping"
	KeyText     = "teamchat.text"
	KeyNotice   = "teamchat.notice"
	KeyReport   = "teamchat.report"
	KeyQuestion = "teamchat.question"
	KeyThread   = "teamchat.thread"
	KeyCarried  = "teamchat.carried"
)

var typedKeys = []string{KeyPing, KeyText, KeyNotice, KeyReport, KeyQuestion, KeyThread, KeyCarried}

// The typed keys a room takes from its senders (A 7.1): an agent's in main, the admin's in main
// (A 7.6 rule 06), and the side rooms'.
var (
	MainKeys      = []string{KeyPing, KeyText, KeyNotice}
	AdminMainKeys = []string{KeyNotice, KeyCarried}
	ReportKeys    = []string{KeyReport}
	QuestionKeys  = []string{KeyQuestion, KeyThread}
)

// The fixed limits of A 11.2, and the question form's bounds (A 10.2).
const (
	MaxContent      = 16384
	MaxPingObject   = 4096
	MaxPingTo       = 32
	MaxText         = 4096
	MaxTextTo       = 16
	MaxNoticeTo     = 17
	MaxMovedTo      = 64
	MaxHumanBody    = 16384
	MaxReportBody   = 1500
	MaxQuestionBody = 8000
	MaxThreadBody   = 4000
	MaxHeadline     = 120
	MaxCarriedText  = 4096
	MaxAskedBy      = 16
	MaxQuestionRe   = 16
	MinRemindS      = 600
	MaxRemindS      = 604800
	maxQuestionN    = 999999999
)

// The message types of A 7.1's display table.
const (
	MsgText   = "m.text"
	MsgNotice = "m.notice"
)

const (
	keyRelatesTo  = "m.relates_to"
	keyInReplyTo  = "m.in_reply_to"
	keyNewContent = "m.new_content"
	keyMentions   = "m.mentions"
	relThread     = "m.thread"
	relReplace    = "m.replace"
)

// The notice events of A 7.4, in its table's order.
const (
	EventLeft         = "left"
	EventDisconnected = "disconnected"
	EventBack         = "back"
	EventAdded        = "added"
	EventRemoved      = "removed"
	EventParked       = "parked"
	EventRole         = "role"
	EventMoved        = "moved"
)

var (
	// NoticeEvents is A 7.4's closed set: an unknown event is dropped `schema`.
	NoticeEvents = []string{EventLeft, EventDisconnected, EventBack, EventAdded, EventRemoved, EventParked, EventRole, EventMoved}
	// LeftReasons are a `left` notice's reasons.
	LeftReasons = []string{"exit", "logout", "released", "other"}
	// CarriedKinds are a carried item's kinds (A 7.7).
	CarriedKinds = []string{"ping", "text", "human", "timeout", "forwarded"}
)

// CarriedPing is the carried kind whose item has a verb and a reference.
const CarriedPing = "ping"

// MemberNotice is whether a member posts this event about itself; the admin posts the others.
func MemberNotice(event string) bool {
	return event == EventLeft || event == EventDisconnected || event == EventBack
}

// RelKind is the shape of an `m.relates_to` (A 7.1): none, a plain reply, or the thread form.
type RelKind int

// The relation shapes.
const (
	RelNone RelKind = iota
	RelReply
	RelThread
)

// Relation is a decoded `m.relates_to`. For the thread form Root is the thread root, InReplyTo
// the fallback reply and FallingBack its `is_falling_back`; a plain reply has InReplyTo only.
type Relation struct {
	Kind        RelKind
	Root        string
	InReplyTo   string
	FallingBack bool
}

// ReplyTo is the plain reply form.
func ReplyTo(id string) Relation { return Relation{Kind: RelReply, InReplyTo: id} }

// AnswerTo is the answer form of A 7.2: a reply in the ping's thread.
func AnswerTo(ping string) Relation { return InThread(ping, ping, true) }

// InThread is the thread form of A 10.3.
func InThread(root, inReplyTo string, fallingBack bool) Relation {
	return Relation{Kind: RelThread, Root: root, InReplyTo: inReplyTo, FallingBack: fallingBack}
}

// IsAnswer is whether this is the answer form: the thread form falling back to its own root.
func (r Relation) IsAnswer() bool {
	return r.Kind == RelThread && r.FallingBack && r.InReplyTo == r.Root
}

// Encode is the `m.relates_to` object of a reply or the thread form.
func (r Relation) Encode() Value {
	reply := M(keyInReplyTo, Obj(M("event_id", Str(r.InReplyTo))))
	if r.Kind == RelReply {
		return Obj(reply)
	}
	return Obj(M("rel_type", Str(relThread)), M("event_id", Str(r.Root)), M("is_falling_back", Bool(r.FallingBack)), reply)
}

// decodeRelation reads the two shapes exactly: no other key, every event ID in its grammar.
func decodeRelation(v Value) (Relation, bool) {
	f := readFields(v, ReasonSchema)
	switch {
	case exactKeys(v, keyInReplyTo):
		id, ok := replyID(f)
		return ReplyTo(id), ok
	case exactKeys(v, "rel_type", "event_id", "is_falling_back", keyInReplyTo):
		rel := f.str("rel_type", true, ReasonSchema)
		root := f.str("event_id", true, ReasonSchema)
		falling := f.boolean("is_falling_back", false, ReasonSchema)
		id, ok := replyID(f)
		return InThread(root, id, falling), ok && f.rule == "" && rel == relThread && ValidEventID(root)
	}
	return Relation{}, false
}

func replyID(f *fields) (string, bool) {
	reply := f.need(keyInReplyTo, ReasonSchema)
	g := readFields(reply, ReasonSchema)
	id := g.str("event_id", true, ReasonSchema)
	return id, exactKeys(reply, "event_id") && g.rule == "" && ValidEventID(id)
}

// exactKeys is whether v is an object with exactly these keys.
func exactKeys(v Value, keys ...string) bool {
	if v.kind != KindObject || len(v.members) != len(keys) {
		return false
	}
	for _, m := range v.members {
		if !slices.Contains(keys, m.Key) {
			return false
		}
	}
	return true
}

func hasKeyDeep(v Value, key string) bool {
	for _, m := range v.members {
		if m.Key == key || hasKeyDeep(m.Val, key) {
			return true
		}
	}
	for _, item := range v.items {
		if hasKeyDeep(item, key) {
			return true
		}
	}
	return false
}

// relType is the content's `m.relates_to.rel_type`, empty when there is none.
func relType(content Value) string {
	if rel, ok := content.Get(keyRelatesTo); ok {
		if t, has := rel.Get("rel_type"); has && t.kind == KindString {
			return t.s
		}
	}
	return ""
}

// Content is a decoded agent event: its typed key, its `body`, its relation, and the typed
// object of that key. Each `to` is sorted.
type Content struct {
	Key      string
	Body     string
	Relation Relation
	Ping     Ping
	Text     Text
	Notice   Notice
	Question Question
	Thread   Thread
	Carried  Carried
}

// Ping is `teamchat.ping` (A 7.2).
type Ping struct {
	Verb   string
	To     []string
	Ref    string
	Branch string
}

// Text is `teamchat.text` (A 7.3).
type Text struct {
	To   []string
	Text string
}

// Notice is `teamchat.notice` (A 7.4): Reason for `left`, Role for `added` and `role`, no
// Subject for `moved`.
type Notice struct {
	Event, Subject, Reason, Role string
	To                           []string
}

// Question is `teamchat.question` (A 10.2); RemindAfterS 0 is none.
type Question struct {
	N            int64
	AskedBy, Re  []string
	RemindAfterS int64
}

// Thread is `teamchat.thread` (A 10.3); N 0 is the reply to an unattached human post.
type Thread struct {
	N     int64
	Close bool
}

// Carried is `teamchat.carried` (A 7.7); Verb and Ref only for kind `ping`.
type Carried struct {
	Kind, Verb, Ref, From string
	SentMs                int64
	V1Event, To, Text     string
	PossibleDuplicate     bool
}

var objectDecoders = map[string]func(c *Content, obj Value) string{
	KeyPing:     decodePingObject,
	KeyText:     decodeTextObject,
	KeyNotice:   decodeNoticeObject,
	KeyReport:   func(*Content, Value) string { return "" },
	KeyQuestion: decodeQuestionObject,
	KeyThread:   decodeThreadObject,
	KeyCarried:  decodeCarriedObject,
}

// DecodeContent decodes an agent event's content under A 7.1, given the typed keys its room and
// sender may use: the typed key, then the version (P26: before size or shape), the sizes, edits,
// `format`, `body`, the relation and the typed object. The reason is "" when it decodes; the
// roster's rules (addressing, verbs, references) are the verdict's.
func DecodeContent(content Value, keys []string) (Content, string) {
	key := typedKey(content, keys)
	if key == "" {
		return Content{}, ReasonSchema
	}
	f := readFields(content, ReasonSchema)
	obj := f.need(key, ReasonSchema)
	if f.rule != "" {
		return Content{}, ReasonSchema
	}
	if reason := version(obj); reason != "" {
		return Content{}, reason
	}
	if content.Size() > MaxContent || key == KeyPing && obj.Size() > MaxPingObject {
		return Content{}, ReasonSize
	}
	if hasKeyDeep(content, keyNewContent) || relType(content) == relReplace {
		return Content{}, ReasonEdit
	}
	c := Content{Key: key, Body: f.str("body", true, ReasonSchema)}
	if f.has("format") || f.rule != "" || !decodeContentRelation(f, &c) {
		return Content{}, ReasonSchema
	}
	if reason := objectDecoders[key](&c, obj); reason != "" {
		return Content{}, reason
	}
	if !relationAllowed(c) {
		return Content{}, ReasonSchema
	}
	return c, ""
}

func decodeContentRelation(f *fields, c *Content) bool {
	if !f.has(keyRelatesTo) {
		return true
	}
	rel, ok := decodeRelation(f.need(keyRelatesTo, ReasonSchema))
	c.Relation = rel
	return ok
}

// typedKey is the content's one typed key when it is one of keys; "" otherwise.
func typedKey(content Value, keys []string) string {
	found := ""
	for _, k := range typedKeys {
		if _, ok := content.Get(k); ok {
			if found != "" {
				return ""
			}
			found = k
		}
	}
	if !slices.Contains(keys, found) {
		return ""
	}
	return found
}

// version is A 3.1 over a typed object: `v` an integer, then major 1 and no `features`.
func version(obj Value) string {
	v, ok := obj.Get("v")
	if !ok || v.kind != KindInt {
		return ReasonSchema
	}
	if _, features := obj.Get("features"); v.i != WireMajor || features {
		return ReasonVersion
	}
	return ""
}

// relationAllowed is each key's relation: text none or a reply; a thread post the thread form,
// or a reply when it has no `n`; a ping by its verb (the verdict's); the rest none.
func relationAllowed(c Content) bool {
	switch c.Key {
	case KeyPing:
		return true
	case KeyText:
		return c.Relation.Kind != RelThread
	case KeyThread:
		if c.Thread.N > 0 {
			return c.Relation.Kind == RelThread
		}
		return c.Relation.Kind == RelReply && !c.Thread.Close
	}
	return c.Relation.Kind == RelNone
}

func sortedStrs(list []string) []string {
	out := slices.Clone(list)
	slices.Sort(out)
	return out
}

func decodePingObject(c *Content, obj Value) string {
	f := readFields(obj, ReasonSchema)
	c.Ping = Ping{
		Verb:   f.str("verb", true, ReasonSchema),
		To:     sortedStrs(f.strs("to", true, ReasonSchema)),
		Ref:    f.str("ref", false, ReasonSchema),
		Branch: f.str("branch", false, ReasonSchema),
	}
	return f.rule
}

func decodeTextObject(c *Content, obj Value) string {
	f := readFields(obj, ReasonSchema)
	c.Text = Text{To: sortedStrs(f.strs("to", true, ReasonSchema)), Text: f.str("text", true, ReasonSchema)}
	switch {
	case f.rule != "" || c.Text.Text == "":
		return ReasonSchema
	case len(c.Text.Text) > MaxText:
		return ReasonSize
	}
	return ""
}

func decodeNoticeObject(c *Content, obj Value) string {
	f := readFields(obj, ReasonSchema)
	n := Notice{Event: f.str("event", true, ReasonSchema), To: sortedStrs(f.strs("to", true, ReasonSchema))}
	switch n.Event {
	case EventMoved:
		if f.has("subject") {
			return ReasonSchema
		}
	default:
		n.Subject = f.str("subject", true, ReasonSchema)
	}
	switch n.Event {
	case EventLeft:
		n.Reason = f.str("reason", true, ReasonSchema)
		if !slices.Contains(LeftReasons, n.Reason) {
			return ReasonSchema
		}
	case EventAdded, EventRole:
		n.Role = f.str("role", true, ReasonSchema)
		if n.Role != RoleCoordinator && n.Role != RoleWorker {
			return ReasonSchema
		}
	}
	if f.rule != "" || !slices.Contains(NoticeEvents, n.Event) {
		return ReasonSchema
	}
	c.Notice = n
	return ""
}

func decodeQuestionObject(c *Content, obj Value) string {
	f := readFields(obj, ReasonSchema)
	q := Question{
		N:            f.integer("n", true, 0, ReasonSchema),
		AskedBy:      f.strs("asked_by", false, ReasonSchema),
		Re:           f.strs("re", false, ReasonSchema),
		RemindAfterS: f.integer("remind_after_s", false, 0, ReasonSchema),
	}
	switch {
	case f.rule != "" || q.N < 1 || q.N > maxQuestionN || !strings.HasPrefix(c.Body, QuestionName(int(q.N))+" "):
		return ReasonSchema
	case !distinctList(q.AskedBy, 0, MaxAskedBy, func(string) bool { return true }) || !distinctList(q.Re, 0, MaxQuestionRe, ValidEventID):
		return ReasonSchema
	case f.has("remind_after_s") && (q.RemindAfterS < MinRemindS || q.RemindAfterS > MaxRemindS):
		return ReasonSchema
	case len(c.Body) > MaxQuestionBody:
		return ReasonSize
	}
	c.Question = q
	return ""
}

func decodeThreadObject(c *Content, obj Value) string {
	f := readFields(obj, ReasonSchema)
	c.Thread = Thread{N: f.integer("n", false, 0, ReasonSchema), Close: f.boolean("close", false, ReasonSchema)}
	switch {
	case f.rule != "" || f.has("n") && (c.Thread.N < 1 || c.Thread.N > maxQuestionN):
		return ReasonSchema
	case len(c.Body) > MaxThreadBody:
		return ReasonSize
	}
	return ""
}

func decodeCarriedObject(c *Content, obj Value) string {
	f := readFields(obj, ReasonSchema)
	k := Carried{
		Kind:              f.str("kind", true, ReasonSchema),
		From:              f.str("from", true, ReasonSchema),
		SentMs:            f.integer("sent_ms", true, 0, ReasonSchema),
		V1Event:           f.str("v1_event", true, ReasonSchema),
		Text:              f.str("text", false, ReasonSchema),
		PossibleDuplicate: f.boolean("possible_duplicate", false, ReasonSchema),
	}
	to := f.strs("to", true, ReasonSchema)
	if k.Kind == CarriedPing {
		k.Verb = f.str("verb", true, ReasonSchema)
		k.Ref = f.str("ref", false, ReasonSchema)
	} else if f.has("verb") || f.has("ref") {
		return ReasonSchema
	}
	switch {
	case f.rule != "" || !slices.Contains(CarriedKinds, k.Kind) || k.SentMs < 0 || !ValidEventID(k.V1Event) || len(to) != 1:
		return ReasonSchema
	case len(k.Text) > MaxCarriedText:
		return ReasonSize
	}
	k.To = to[0]
	c.Carried = k
	return ""
}

// Human is what a member reads of a human's message (A 7.5): the body with any reply fallback
// removed, the mentions, the reply (never a thread's fallback reply), the thread root, and for
// an edit the original it revises, with the new body and the new content's mentions.
type Human struct {
	Body        string
	Mentions    []string
	MentionRoom bool
	ReplyTo     string
	ThreadRoot  string
	Revises     string
}

// replyFallbackRe is the reply fallback's first line, `> <@user:server> ...` (P37).
var replyFallbackRe = regexp.MustCompile(`^> <@[^\s>:]+:[^\s>]+> `)

// DecodeHuman reads a human's message: `m.text` only (else ignore `type`), a string body of at
// most MaxHumanBody bytes (else `schema`, `size`), and nothing left after the reply fallback
// (ignore `empty`). An edit without a well-formed original ID or new content is `edit`. A
// human's mentions and relation are read leniently: a malformed part is no mention, no relation.
func DecodeHuman(content Value) (Human, string) {
	if msgtype(content) != MsgText {
		return Human{}, IgnoreType
	}
	f := readFields(content, ReasonSchema)
	body := f.str("body", true, ReasonSchema)
	if f.rule != "" {
		return Human{}, ReasonSchema
	}
	if relType(content) == relReplace {
		return decodeEdit(content)
	}
	h := Human{}
	h.Mentions, h.MentionRoom = mentions(content)
	rel := readFields(f.optObject(keyRelatesTo, ""), "")
	inReply := readFields(rel.optObject(keyInReplyTo, ""), "").str("event_id", false, "")
	if rel.str("rel_type", false, "") == relThread {
		h.ThreadRoot = validID(rel.str("event_id", false, ""))
		if rel.boolean("is_falling_back", false, "") {
			inReply = ""
		}
	}
	h.ReplyTo = validID(inReply)
	if h.ReplyTo != "" && replyFallbackRe.MatchString(body) {
		body = stripReplyFallback(body)
	}
	return humanBody(h, body)
}

func humanBody(h Human, body string) (Human, string) {
	switch {
	case len(body) > MaxHumanBody:
		return Human{}, ReasonSize
	case strings.TrimSpace(body) == "":
		return Human{}, IgnoreEmpty
	}
	h.Body = body
	return h, ""
}

func decodeEdit(content Value) (Human, string) {
	f := readFields(content, ReasonEdit)
	newContent := f.need(keyNewContent, ReasonEdit)
	rel := readFields(f.need(keyRelatesTo, ReasonEdit), ReasonEdit)
	h := Human{Revises: rel.str("event_id", true, ReasonEdit)}
	body := readFields(newContent, ReasonEdit).str("body", true, ReasonEdit)
	if f.rule != "" || rel.rule != "" || !ValidEventID(h.Revises) || msgtype(newContent) != MsgText {
		return Human{}, ReasonEdit
	}
	h.Mentions, h.MentionRoom = mentions(newContent)
	return humanBody(h, body)
}

func validID(id string) string {
	if ValidEventID(id) {
		return id
	}
	return ""
}

func msgtype(content Value) string {
	if t, ok := content.Get("msgtype"); ok && t.kind == KindString {
		return t.s
	}
	return ""
}

// mentions are `m.mentions.user_ids` (its strings) and whether `m.mentions.room` is true.
func mentions(content Value) ([]string, bool) {
	m := readFields(readFields(content, "").optObject(keyMentions, ""), "")
	var ids []string
	if list, ok := m.obj.Get("user_ids"); ok {
		for _, item := range list.items {
			if item.kind == KindString {
				ids = append(ids, item.s)
			}
		}
	}
	return ids, m.boolean("room", false, "")
}

// stripReplyFallback removes the leading `>` lines and the one blank line after them (P37).
func stripReplyFallback(body string) string {
	lines := strings.Split(body, "\n")
	i := 0
	for i < len(lines) && strings.HasPrefix(lines[i], ">") {
		i++
	}
	if i < len(lines) && lines[i] == "" {
		i++
	}
	return strings.Join(lines[i:], "\n")
}
