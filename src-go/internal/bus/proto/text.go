package proto

import (
	"errors"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

const (
	maxTextTo    = 16
	maxTextBytes = 4096
)

var (
	textContentKeys = []string{keyMsgtype, keyBody, keyMentions, TextKey}
	textObjectKeys  = []string{keyV, keyTo, keyText}
)

// ErrNotEventID is an re that is not a §3 event ID, given to a builder.
var ErrNotEventID = errors.New("re is not an event ID")

// RenderText is SPEC P31: the canonical body of an agent's text.
func RenderText(to []string, text string) string {
	return renderTag + " text -> " + strings.Join(sortedCopy(to), " ") + "\n" + text
}

// BuildText is SPEC P30: an agent's text as full m.room.message content, to sorted; with re
// ("" for none) a reply to that event. It is not validated; ErrNotEventID for a bad re.
func BuildText(to []string, text, re string) (pyjson.Object, error) {
	sorted := sortedCopy(to)
	content := pyjson.Object{
		keyMsgtype:  msgtypeNotice,
		keyBody:     RenderText(to, text),
		keyMentions: pyjson.Object{keyUserIDs: anyStrings(sorted)},
		TextKey:     pyjson.Object{keyV: versionOne, keyTo: anyStrings(sorted), keyText: text},
	}
	if re != "" {
		if !IsEventID(re) {
			return nil, ErrNotEventID
		}
		content[keyRelatesTo] = pyjson.Object{keyInReplyTo: pyjson.Object{keyEventID: re}}
	}
	return content, nil
}

// ValidateTextContent is SPEC P32: an agent's text checked against §7, first failure wins;
// the secret shapes (P33) only when checkSecret, as on send. A failure is a RefusalError.
func ValidateTextContent(content any, ctx *Context, checkSecret bool) (AgentText, error) {
	text, reason := validateText(content, ctx, checkSecret)
	if reason != "" {
		return AgentText{}, refuse(reason)
	}
	return text, nil
}

func validateText(content any, ctx *Context, checkSecret bool) (AgentText, Reason) {
	obj, isObj := content.(pyjson.Object)
	if !isObj {
		return AgentText{}, ReasonSchema
	}
	if reason := textEnvelope(obj); reason != "" {
		return AgentText{}, reason
	}
	to, text, reason := textObject(obj[TextKey])
	if reason != "" {
		return AgentText{}, reason
	}
	targets, reason := checkTargets(to, maxTextTo, "", ctx)
	if reason != "" {
		return AgentText{}, reason
	}
	if checkSecret && SecretShape(text) != "" {
		return AgentText{}, RefuseSecret
	}
	if !mentionsAre(obj[keyMentions], targets) {
		return AgentText{}, ReasonSchema
	}
	body, isStr := obj[keyBody].(string)
	if !isStr {
		return AgentText{}, ReasonSchema
	}
	if body != RenderText(to, text) {
		return AgentText{}, ReasonBody
	}
	return AgentText{To: targets, Text: text}, ""
}

// textEnvelope is P32 up to the text object: version first, the reply, the keys.
func textEnvelope(obj pyjson.Object) Reason {
	if versionOtherThanOne(obj[TextKey]) {
		return ReasonVersion
	}
	if reason := textReply(obj); reason != "" {
		return reason
	}
	keys := textContentKeys
	if _, reply := obj[keyRelatesTo]; reply {
		keys = append([]string{keyRelatesTo}, keys...)
	}
	if !keysAre(obj, keys) || !stringIs(obj[keyMsgtype], msgtypeNotice) {
		return ReasonSchema
	}
	return ""
}

// textReply is P32's m.relates_to: absent, or exactly a reply to an event ID; an edit is edit.
func textReply(obj pyjson.Object) Reason {
	if _, present := obj[keyNewContent]; present {
		return ReasonEdit
	}
	v, present := obj[keyRelatesTo]
	if !present {
		return ""
	}
	relates, isObj := v.(pyjson.Object)
	if isObj && stringIs(relates[keyRelType], relReplace) {
		return ReasonEdit
	}
	inReply, isReply := relates[keyInReplyTo].(pyjson.Object)
	if !isObj || len(relates) != 1 || !isReply || len(inReply) != 1 {
		return ReasonSchema
	}
	id, isStr := inReply[keyEventID].(string)
	if !isStr || !IsEventID(id) {
		return ReasonSchema
	}
	return ""
}

// textObject is P32's text object: exactly v, to and text, typed, and text within the limit.
// A text with a lone surrogate has no UTF-8 size and is schema (D1).
func textObject(v any) ([]string, string, Reason) {
	obj, isObj := v.(pyjson.Object)
	if !isObj || !keysAre(obj, textObjectKeys) {
		return nil, "", ReasonSchema
	}
	n, isNum := obj[keyV].(pyjson.Number)
	to, isList := stringList(obj[keyTo])
	text, isStr := obj[keyText].(string)
	if !isNum || n.Kind != pyjson.Int || !isList || !isStr || text == "" {
		return nil, "", ReasonSchema
	}
	size, encodable := utf8Size(text)
	if !encodable {
		return nil, "", ReasonSchema
	}
	if size > maxTextBytes {
		return nil, "", ReasonSize
	}
	return to, text, ""
}
