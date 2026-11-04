package proto

import (
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// The content keys and message types of §4 and §7.
const (
	PingKey       = "agent_bus.ping"
	TextKey       = "agent_bus.text"
	renderTag     = "[agent-bus]"
	msgtypeNotice = "m.notice"
	msgtypeHuman  = "m.text"
	keyMsgtype    = "msgtype"
	keyBody       = "body"
	keyMentions   = "m.mentions"
	keyUserIDs    = "user_ids"
	keyRelatesTo  = "m.relates_to"
	keyNewContent = "m.new_content"
	keyInReplyTo  = "m.in_reply_to"
	keyEventID    = "event_id"
	keyRelType    = "rel_type"
	keyV          = "v"
	keyVerb       = "verb"
	keyTo         = "to"
	keyRef        = "ref"
	keyRe         = "re"
	keyBranch     = "branch"
	keyText       = "text"
	relReplace    = "m.replace"
	maxPingTo     = 32
	maxPingBytes  = 4096
)

// The trusts a ping's reference is given (§11, §15).
const (
	TrustTrusted  = "trusted"
	TrustExternal = "external"
)

var (
	pingContentKeys  = []string{keyMsgtype, keyBody, keyMentions, PingKey}
	pingObjectKeys   = []string{keyV, keyVerb, keyTo, keyRef, keyRe, keyBranch}
	pingRequiredKeys = []string{keyV, keyVerb, keyTo}
	versionOne       = pyjson.Number{Lit: "1", Kind: pyjson.Int}
	branchRefForms   = []string{FormPath, FormCommit}
)

// PingFields are a ping's optional keys; "" is absent.
type PingFields struct {
	Ref, Re, Branch string
}

// RenderPing is SPEC P25: the canonical body of a ping.
func RenderPing(verb string, to []string, f PingFields) string {
	ref := f.Ref
	if ref == "" {
		ref = "-"
	}
	text := renderTag + " " + verb + " " + ref
	if f.Branch != "" {
		text += " on " + f.Branch
	}
	text += " -> " + strings.Join(sortedCopy(to), " ")
	if f.Re != "" {
		text += " re " + f.Re
	}
	return text
}

// BuildPing is SPEC P24: a ping's full m.room.message content, to sorted and the absent keys
// left out. It is not validated.
func BuildPing(verb string, to []string, f PingFields) pyjson.Object {
	obj := pyjson.Object{keyV: versionOne, keyVerb: verb, keyTo: anyStrings(sortedCopy(to))}
	for key, value := range map[string]string{keyRef: f.Ref, keyRe: f.Re, keyBranch: f.Branch} {
		if value != "" {
			obj[key] = value
		}
	}
	return pyjson.Object{
		keyMsgtype:  msgtypeNotice,
		keyBody:     RenderPing(verb, to, f),
		keyMentions: pyjson.Object{keyUserIDs: anyStrings(sortedCopy(to))},
		PingKey:     obj,
	}
}

// ValidatePingContent is SPEC P26-P29: a ping's content checked against §4-§6 and §11, first
// failure wins, the sender's role aside (CheckRole). The ping carries verb, to sorted, ref,
// re, trust and branch; a failure is a RefusalError.
func ValidatePingContent(content any, ctx *Context) (Ping, error) {
	ping, reason := validatePing(content, ctx)
	if reason != "" {
		return Ping{}, refuse(reason)
	}
	return ping, nil
}

// pingRaw is a ping object whose keys and types have passed P26.
type pingRaw struct {
	verb   string
	to     []string
	fields PingFields
	has    map[string]bool
}

func validatePing(content any, ctx *Context) (Ping, Reason) {
	obj, isObj := content.(pyjson.Object)
	if !isObj {
		return Ping{}, ReasonSchema
	}
	if reason := pingEnvelope(obj); reason != "" {
		return Ping{}, reason
	}
	raw, reason := pingObject(obj[PingKey])
	if reason != "" {
		return Ping{}, reason
	}
	ping, reason := pingSemantics(raw, ctx)
	if reason != "" {
		return Ping{}, reason
	}
	if !mentionsAre(obj[keyMentions], ping.To) {
		return Ping{}, ReasonSchema
	}
	body, isStr := obj[keyBody].(string)
	if !isStr {
		return Ping{}, ReasonSchema
	}
	if body != RenderPing(raw.verb, raw.to, raw.fields) {
		return Ping{}, ReasonBody
	}
	return ping, ""
}

// pingEnvelope is P26 up to the ping object: version first, then edit, size and the keys.
func pingEnvelope(obj pyjson.Object) Reason {
	if versionOtherThanOne(obj[PingKey]) {
		return ReasonVersion
	}
	if hasAnyKey(obj, keyRelatesTo, keyNewContent) {
		return ReasonEdit
	}
	size, err := ContentSize(obj)
	if err != nil {
		return ReasonSchema
	}
	if size > maxPingBytes {
		return ReasonSize
	}
	if !keysAre(obj, pingContentKeys) || !stringIs(obj[keyMsgtype], msgtypeNotice) {
		return ReasonSchema
	}
	return ""
}

// pingObject is P26's keys and types of the ping object.
func pingObject(v any) (pingRaw, Reason) {
	obj, isObj := v.(pyjson.Object)
	if !isObj {
		return pingRaw{}, ReasonSchema
	}
	raw := pingRaw{has: map[string]bool{}}
	for key, value := range obj {
		if !slices.Contains(pingObjectKeys, key) || !raw.take(key, value) {
			return pingRaw{}, ReasonSchema
		}
	}
	for _, key := range pingRequiredKeys {
		if !raw.has[key] {
			return pingRaw{}, ReasonSchema
		}
	}
	return raw, ""
}

// take records one key of the ping object, false when its value has the wrong type.
func (r *pingRaw) take(key string, value any) bool {
	r.has[key] = true
	switch key {
	case keyV:
		n, isNum := value.(pyjson.Number)
		return isNum && n.Kind == pyjson.Int
	case keyTo:
		to, ok := stringList(value)
		r.to = to
		return ok
	}
	s, isStr := value.(string)
	switch key {
	case keyVerb:
		r.verb = s
	case keyRef:
		r.fields.Ref = s
	case keyRe:
		r.fields.Re = s
	case keyBranch:
		r.fields.Branch = s
	}
	return isStr
}

// pingSemantics is P26 from the verb on: verb, targets, reference and trust, branch, re.
func pingSemantics(raw pingRaw, ctx *Context) (Ping, Reason) {
	rule, known := Verbs[raw.verb]
	if !known {
		return Ping{}, ReasonVerb
	}
	to, reason := checkTargets(raw.to, maxPingTo, raw.verb, ctx)
	if reason != "" {
		return Ping{}, reason
	}
	ref, trust, reason := checkRef(raw.fields.Ref, raw.has[keyRef], rule, ctx)
	if reason != "" {
		return Ping{}, reason
	}
	if reason := checkBranch(raw.fields.Branch, raw.has[keyBranch], ref); reason != "" {
		return Ping{}, reason
	}
	if reason := checkRe(raw.fields.Re, raw.has[keyRe], rule); reason != "" {
		return Ping{}, reason
	}
	return Ping{Verb: raw.verb, To: to, Ref: ref, Re: raw.fields.Re, Trust: trust, Branch: raw.fields.Branch}, ""
}

// checkTargets is P26's and P32's target rule: 1 to limit distinct users, each addressable
// for the verb ("" for agent text); the users come back sorted.
func checkTargets(to []string, limit int, verb string, ctx *Context) ([]string, Reason) {
	sorted := sortedCopy(to)
	if len(to) < 1 || len(to) > limit || len(slices.Compact(slices.Clone(sorted))) != len(to) {
		return nil, ReasonTarget
	}
	for _, user := range to {
		if !Addressable(user, verb, ctx) {
			return nil, ReasonTarget
		}
	}
	return sorted, ""
}

// checkRef is SPEC P27: the reference's presence and form for the verb, then its trust.
func checkRef(text string, present bool, rule VerbRule, ctx *Context) (*Ref, string, Reason) {
	if !present {
		if rule.Ref == Required {
			return nil, "", ReasonRef
		}
		return nil, "", ""
	}
	ref, parsed := ParseRef(text)
	if rule.Ref == Forbidden || !parsed || !slices.Contains(rule.Forms, ref.Form) {
		return nil, "", ReasonRef
	}
	if IsExcluded(ref, ctx) {
		return nil, "", ReasonExcluded
	}
	if RefSource(ref, ctx) == SourceExternal {
		if !rule.External {
			return nil, "", ReasonAllowlist
		}
		return &ref, TrustExternal, ""
	}
	if ref.Form == FormPath && len(ctx.PathPrefixes) > 0 && !PathAllowed(ref.Path, ctx.PathPrefixes) {
		return nil, "", ReasonAllowlist
	}
	return &ref, TrustTrusted, ""
}

// checkBranch is SPEC P28.
func checkBranch(branch string, present bool, ref *Ref) Reason {
	if !present {
		return ""
	}
	if ref == nil || !slices.Contains(branchRefForms, ref.Form) || !IsBranch(branch) {
		return ReasonRef
	}
	return ""
}

// checkRe is SPEC P29.
func checkRe(re string, present bool, rule VerbRule) Reason {
	if !present {
		if rule.Re == Required {
			return ReasonRe
		}
		return ""
	}
	if rule.Re == Forbidden || !IsEventID(re) {
		return ReasonRe
	}
	return ""
}

// versionOtherThanOne is §1's first check: an object whose v is an int other than 1.
func versionOtherThanOne(v any) bool {
	obj, isObj := v.(pyjson.Object)
	if !isObj {
		return false
	}
	n, isNum := obj[keyV].(pyjson.Number)
	return isNum && n.Kind == pyjson.Int && !n.IsInt(1)
}

func hasAnyKey(obj pyjson.Object, keys ...string) bool {
	for _, key := range keys {
		if _, present := obj[key]; present {
			return true
		}
	}
	return false
}

// keysAre is Python's set(obj) == set(keys).
func keysAre(obj pyjson.Object, keys []string) bool {
	if len(obj) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, present := obj[key]; !present {
			return false
		}
	}
	return true
}

func stringIs(v any, want string) bool {
	s, isStr := v.(string)
	return isStr && s == want
}

// stringList is a list whose items are all strings.
func stringList(v any) ([]string, bool) {
	items, isList := v.([]any)
	if !isList {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, isStr := item.(string)
		if !isStr {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// listHas is Python's `want in v` for a list v.
func listHas(v any, want string) bool {
	items, isList := v.([]any)
	return isList && slices.ContainsFunc(items, func(item any) bool { return stringIs(item, want) })
}

// mentionsAre is Python's mentions == {"user_ids": to}.
func mentionsAre(v any, to []string) bool {
	obj, isObj := v.(pyjson.Object)
	if !isObj || len(obj) != 1 {
		return false
	}
	ids, isList := stringList(obj[keyUserIDs])
	return isList && slices.Equal(ids, to)
}

func anyStrings(s []string) []any {
	out := make([]any, len(s))
	for i, item := range s {
		out[i] = item
	}
	return out
}
