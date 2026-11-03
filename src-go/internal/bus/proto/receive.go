package proto

import (
	"errors"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// The rooms an item is received from (SPEC P35): nobody receives the reports room.
const (
	ReceiveMain      = "main"
	ReceiveQuestions = "questions"
	eventMessage     = "m.room.message"
	maxHumanBytes    = 16384
)

// UnheardReasons are the ignore reasons ignored.log records (SPEC P41, P42).
var UnheardReasons = []Reason{IgnoreNotAddressed}

// ErrRoom is a receive asked of a room no item is received from: a caller error.
var ErrRoom = errors.New("no item is received from that room")

// Receiver is who receives an event and how: its own user ID, the event IDs already
// processed, the bundle's human_text flag and the room (ReceiveMain or ReceiveQuestions).
type Receiver struct {
	Self      string
	Seen      map[string]bool
	HumanText bool
	Room      string
}

// event is an event whose header has passed P35's steps 1-3.
type event struct {
	id      string
	sender  any
	ts      int64
	content pyjson.Object
}

func dropped(reason Reason) Outcome { return Outcome{Verdict: Drop, Reason: reason} }

func ignored(reason Reason) Outcome { return Outcome{Verdict: Ignore, Reason: reason} }

// ValidateEvent is SPEC P35-P40: the offline receive steps of §9 for one timeline event of
// the receiver's room, first match wins. ErrRoom for a room nothing is received from.
func ValidateEvent(raw any, ctx *Context, r Receiver) (Outcome, error) {
	if r.Room != ReceiveMain && r.Room != ReceiveQuestions {
		return Outcome{}, ErrRoom
	}
	ev, outcome, ok := readEvent(raw, r)
	if !ok {
		return outcome, nil
	}
	if r.Room == ReceiveQuestions {
		return questionsOutcome(ev, ctx, r), nil
	}
	switch SenderClass(ev.sender, ctx) {
	case "":
		return dropped(ReasonSender), nil
	case SenderHuman:
		return humanOutcome(ev, r), nil
	}
	if _, text := ev.content[TextKey]; text {
		if _, ping := ev.content[PingKey]; ping {
			return dropped(ReasonSchema), nil
		}
		return agentTextOutcome(ev, ctx, r), nil
	}
	return pingOutcome(ev, ctx, r), nil
}

// readEvent is P35 up to the sender class. A timestamp past int64 is schema (D3).
func readEvent(raw any, r Receiver) (event, Outcome, bool) {
	obj, isObj := raw.(pyjson.Object)
	if !isObj {
		return event{}, dropped(ReasonSchema), false
	}
	if !stringIs(obj["type"], eventMessage) {
		return event{}, ignored(IgnoreType), false
	}
	id, isStr := obj[keyEventID].(string)
	if !isStr || !IsEventID(id) {
		return event{}, dropped(ReasonSchema), false
	}
	if r.Seen[id] {
		return event{}, ignored(IgnoreSeen), false
	}
	if stringIs(obj["sender"], r.Self) {
		return event{}, ignored(IgnoreSelf), false
	}
	unsigned, isUnsigned := obj["unsigned"].(pyjson.Object)
	_, redacted := unsigned["redacted_because"]
	content, isContent := obj["content"].(pyjson.Object)
	ts, tsOK := nonNegativeInt(obj["origin_server_ts"])
	if _, state := obj["state_key"]; state || (isUnsigned && redacted) || !isContent || !tsOK {
		return event{}, dropped(ReasonSchema), false
	}
	return event{id: id, sender: obj["sender"], ts: ts, content: content}, Outcome{}, true
}

func nonNegativeInt(v any) (int64, bool) {
	n, isNum := v.(pyjson.Number)
	if !isNum || n.Kind != pyjson.Int {
		return 0, false
	}
	i, fits := n.Int64()
	return i, fits && i >= 0
}

// relatesOf is m.relates_to: present tells it is there, isObj that it is an object. A null
// is present and not an object (D7).
func relatesOf(content pyjson.Object) (pyjson.Object, bool, bool) {
	v, present := content[keyRelatesTo]
	relates, isObj := v.(pyjson.Object)
	return relates, present, isObj
}

func isEdit(content, relates pyjson.Object) bool {
	_, newContent := content[keyNewContent]
	return newContent || stringIs(relates[keyRelType], relReplace)
}

func isReply(relates pyjson.Object) bool {
	_, reply := relates[keyInReplyTo]
	return reply
}

// humanBodySize is P36's and P40's size step; a lone surrogate has no UTF-8 size (D1).
func humanBodySize(body string) Reason {
	size, encodable := utf8Size(body)
	switch {
	case !encodable:
		return ReasonSchema
	case size > maxHumanBytes:
		return ReasonSize
	}
	return ""
}

// humanOutcome is SPEC P36, steps 04h-09h.
func humanOutcome(ev event, r Receiver) Outcome {
	if !r.HumanText {
		return dropped(ReasonSender)
	}
	if !stringIs(ev.content[keyMsgtype], msgtypeHuman) {
		return dropped(ReasonSchema)
	}
	relates, present, isObj := relatesOf(ev.content)
	if present && !isObj {
		return dropped(ReasonSchema)
	}
	if isEdit(ev.content, relates) {
		return dropped(ReasonEdit)
	}
	body, isStr := ev.content[keyBody].(string)
	if !isStr {
		return dropped(ReasonSchema)
	}
	if reason := humanBodySize(body); reason != "" {
		return dropped(reason)
	}
	if !humanAddressed(ev.content, r.Self) {
		return ignored(IgnoreNotAddressed)
	}
	if isReply(relates) {
		if body = StripReplyFallback(body); body == "" {
			return ignored(IgnoreEmpty)
		}
	}
	return Outcome{Verdict: Accept, Human: humanMessage(ev, body, "")}
}

func humanMessage(ev event, text, revises string) *HumanMessage {
	sender, isStr := ev.sender.(string)
	if !isStr {
		return nil
	}
	return &HumanMessage{EventID: ev.id, Sender: sender, TS: ev.ts, Text: text, Revises: revises}
}

// humanAddressed is §7: mentions room exactly true, or user_ids a list naming self.
func humanAddressed(content pyjson.Object, self string) bool {
	mentions, isObj := content[keyMentions].(pyjson.Object)
	if !isObj {
		return false
	}
	if room, isBool := mentions["room"].(bool); isBool && room {
		return true
	}
	return listHas(mentions[keyUserIDs], self)
}

// StripReplyFallback is SPEC P37: the leading lines starting with ">" and the one empty
// line after them removed; with no such line the text is unchanged.
func StripReplyFallback(text string) string {
	lines := strings.Split(text, "\n")
	i := 0
	for i < len(lines) && strings.HasPrefix(lines[i], ">") {
		i++
	}
	if i == 0 {
		return text
	}
	if i < len(lines) && lines[i] == "" {
		i++
	}
	return strings.Join(lines[i:], "\n")
}

// agentTextOutcome is SPEC P38 (04p).
func agentTextOutcome(ev event, ctx *Context, r Receiver) Outcome {
	obj, isObj := ev.content[TextKey].(pyjson.Object)
	mentions, hasMentions := ev.content[keyMentions].(pyjson.Object)
	names := (isObj && listHas(obj[keyTo], r.Self)) || (hasMentions && listHas(mentions[keyUserIDs], r.Self))
	if !names {
		return ignored(IgnoreAgentText)
	}
	if !r.HumanText {
		return dropped(ReasonSender)
	}
	text, reason := validateText(ev.content, ctx, false)
	if reason != "" {
		return dropped(reason)
	}
	sender, isStr := ev.sender.(string)
	if !isStr {
		return dropped(ReasonSender)
	}
	return Outcome{Verdict: Accept, Peer: &PeerText{
		EventID: ev.id, Sender: sender, TS: ev.ts, Text: text.Text, CopyOf: CopyOf(text.To, r.Self, ctx),
	}}
}

// pingOutcome is SPEC P39 (05p-07p).
func pingOutcome(ev event, ctx *Context, r Receiver) Outcome {
	sender, isStr := ev.sender.(string)
	if !isStr {
		return dropped(ReasonSender)
	}
	ping, reason := validatePing(ev.content, ctx)
	if reason != "" {
		return dropped(reason)
	}
	if err := CheckRole(ping.Verb, sender, ctx); err != nil {
		return dropped(reasonOf(err))
	}
	if !slices.Contains(ping.To, r.Self) {
		return ignored(IgnoreNotAddressed)
	}
	ping.Sender, ping.EventID, ping.TS = sender, ev.id, ev.ts
	ping.CopyOf = CopyOf(ping.To, r.Self, ctx)
	return Outcome{Verdict: Accept, Ping: &ping}
}

// reasonOf is a RefusalError's reason; any other error is schema.
func reasonOf(err error) Reason {
	if refusal, ok := errors.AsType[*RefusalError](err); ok {
		return refusal.Reason
	}
	return ReasonSchema
}

// questionsOutcome is SPEC P40. A sender that is not a string is no coordinator, and is
// dropped sender as in the main room (D2).
func questionsOutcome(ev event, ctx *Context, r Receiver) Outcome {
	if ctx.Roles[r.Self] != RoleCoordinator {
		return ignored(IgnoreNotCoordinator)
	}
	if sender, isStr := ev.sender.(string); isStr && ctx.Roles[sender] == RoleCoordinator {
		return ignored(IgnoreCoordinatorPost)
	}
	if SenderClass(ev.sender, ctx) != SenderHuman || !r.HumanText {
		return dropped(ReasonSender)
	}
	if !stringIs(ev.content[keyMsgtype], msgtypeHuman) {
		return dropped(ReasonSchema)
	}
	relates, present, isObj := relatesOf(ev.content)
	if present && !isObj {
		return dropped(ReasonSchema)
	}
	body, revises, reason := questionsBody(ev.content, relates)
	if reason != "" {
		return dropped(reason)
	}
	if revises == "" && isReply(relates) {
		body = StripReplyFallback(body)
	}
	if body == "" {
		return ignored(IgnoreEmpty)
	}
	return Outcome{Verdict: Accept, Human: humanMessage(ev, body, revises)}
}

// questionsBody is P40's body: an edit's is its new content's, and the edit must be exactly
// an m.replace of an event ID with an m.text new content.
func questionsBody(content, relates pyjson.Object) (string, string, Reason) {
	v := content[keyBody]
	revises := ""
	if isEdit(content, relates) {
		id, isID := relates[keyEventID].(string)
		newContent, isObj := content[keyNewContent].(pyjson.Object)
		if !stringIs(relates[keyRelType], relReplace) || !isID || !IsEventID(id) || !isObj ||
			!stringIs(newContent[keyMsgtype], msgtypeHuman) {
			return "", "", ReasonSchema
		}
		v, revises = newContent[keyBody], id
	}
	body, isStr := v.(string)
	if !isStr {
		return "", "", ReasonSchema
	}
	return body, revises, humanBodySize(body)
}

// Unheard is SPEC P41: an outcome ignored not-addressed of a listed human's event whose
// content is an object, and whose mentions name no role-holding agent.
func Unheard(raw any, ctx *Context, o Outcome) bool {
	obj, isObj := raw.(pyjson.Object)
	if o.Verdict != Ignore || o.Reason != IgnoreNotAddressed || !isObj {
		return false
	}
	content, isContent := obj["content"].(pyjson.Object)
	if SenderClass(obj["sender"], ctx) != SenderHuman || !isContent {
		return false
	}
	mentions, hasMentions := content[keyMentions].(pyjson.Object)
	users, isList := mentions[keyUserIDs].([]any)
	if !hasMentions || !isList {
		return true
	}
	for _, user := range users {
		s, isStr := user.(string)
		if _, role := ctx.Roles[s]; isStr && role && IsAgentUserID(s, ctx.ServerName) {
			return false
		}
	}
	return true
}
