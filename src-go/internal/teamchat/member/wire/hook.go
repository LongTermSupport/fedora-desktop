package wire

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// A hook's payload: used, or unusable for one of the closed set of reasons (B 2.8).
const (
	PayloadOK       = "ok"
	PayloadUnusable = "unusable"

	UnusableSize = "size"
	UnusableUTF8 = "utf8"
	UnusableJSON = "json"
)

// The byte bounds of a hook's fields; the hook sends a longer value as absent, so a request
// always fits MaxRequest. The socket's is paths.InboxSocket's, the transcript's the codec's.
const (
	MaxHookSessionID  = 255
	MaxHookTranscript = 1024
	MaxHookWord       = 64
	MaxHookSocket     = 107
	MaxHookToken      = maxToken
)

// HookArgs are the four hook ops' (B 2.8): the payload fields the link uses, each null when
// the payload did not hold it as a string (or, for stop, the literal false), and the inbox
// socket variables. The token is a secret (B11): every formatter redacts it; only the wire
// line carries it.
type HookArgs struct {
	Payload        string  `json:"payload"`
	Unusable       *string `json:"unusable"`
	SessionID      *string `json:"session_id"`
	TranscriptPath *string `json:"transcript_path"`
	// Source is session-start's only.
	Source *string `json:"source"`
	// StopHookFalse is stop's only: the payload held stop_hook_active as the JSON literal
	// false (B26).
	StopHookFalse bool `json:"stop_hook_false"`
	// Reason is session-end's only.
	Reason *string `json:"reason"`
	Socket *string `json:"socket"`
	Token  *string `json:"token"`
}

// DecodeHookArgs decodes op's args strictly and checks their rules; a refusal is exit 10
// code schema and quotes nothing it was given.
func DecodeHookArgs(op string, raw json.RawMessage) (HookArgs, error) {
	var a HookArgs
	if err := DecodeArgs(raw, &a); err != nil {
		return HookArgs{}, err
	}
	if rule := checkHookArgs(op, a); rule != "" {
		return HookArgs{}, exits.New(exits.CodeSchema, nil, msgArgs, exits.Words(rule))
	}
	return a, nil
}

// checkHookArgs is the first rule a breaks for op, "" for none.
func checkHookArgs(op string, a HookArgs) string {
	if op != OpHookSessionStart && op != OpHookPrompt && op != OpHookStop && op != OpHookSessionEnd {
		return "not a hook op"
	}
	if rule := checkPayload(a); rule != "" {
		return rule
	}
	if a.Source != nil && op != OpHookSessionStart || a.StopHookFalse && op != OpHookStop || a.Reason != nil && op != OpHookSessionEnd {
		return "a field of another hook"
	}
	for _, f := range []struct {
		s    *string
		most int
	}{
		{a.SessionID, MaxHookSessionID},
		{a.TranscriptPath, MaxHookTranscript},
		{a.Source, MaxHookWord},
		{a.Reason, MaxHookWord},
		{a.Socket, MaxHookSocket},
		{a.Token, MaxHookToken},
	} {
		if f.s != nil && len(*f.s) > f.most {
			return "a field is too long"
		}
	}
	return ""
}

// checkPayload is the payload's rules: ok, or unusable with its reason and no payload field.
func checkPayload(a HookArgs) string {
	switch {
	case a.Payload != PayloadOK && a.Payload != PayloadUnusable:
		return "payload is not ok or unusable"
	case (a.Payload == PayloadUnusable) != (a.Unusable != nil):
		return "unusable is not given exactly for an unusable payload"
	case a.Unusable == nil:
		return ""
	case *a.Unusable != UnusableSize && *a.Unusable != UnusableUTF8 && *a.Unusable != UnusableJSON:
		return "unusable is not size utf8 or json"
	case a.SessionID != nil || a.TranscriptPath != nil || a.Source != nil || a.StopHookFalse || a.Reason != nil:
		return "an unusable payload has fields"
	}
	return ""
}

// String is the args as JSON with the token redacted.
func (a HookArgs) String() string {
	if a.Token != nil {
		a.Token = new(redacted)
	}
	data, err := json.Marshal(a)
	if err != nil {
		return "hook args (that do not encode)"
	}
	return "hook args " + string(data)
}

// GoString keeps %#v from printing the token.
func (a HookArgs) GoString() string { return "wire." + a.String() }

// Format gives every verb the redacted form, so %d or %x cannot print the token either.
func (a HookArgs) Format(f fmt.State, verb rune) { format(f, verb, a.String(), a.GoString()) }

// String is the request for a log line: its args with any token member redacted, and only
// the size of its stdin.
func (r Request) String() string {
	s := "request " + r.Op + " kit " + r.Kit + " deadline_ms " + strconv.FormatInt(r.DeadlineMS, 10)
	if v, err := proto.Parse(r.Args); err == nil {
		s += " args " + string(redactToken(v).Compact())
	} else {
		s += " args (not strict json)"
	}
	if r.Stdin != nil {
		s += " stdin " + strconv.Itoa(len(*r.Stdin)) + " bytes"
	}
	return s
}

// GoString keeps %#v from printing a token.
func (r Request) GoString() string { return "wire." + r.String() }

// Format gives every verb the redacted form.
func (r Request) Format(f fmt.State, verb rune) { format(f, verb, r.String(), r.GoString()) }

// redactToken is v with an object's token member, when it holds a value, shown as redacted.
func redactToken(v proto.Value) proto.Value {
	if v.Kind() != proto.KindObject {
		return v
	}
	members := v.Members()
	out := make([]proto.Member, len(members))
	for i, m := range members {
		if m.Key == "token" && m.Val.Kind() != proto.KindNull {
			m = proto.M(m.Key, proto.Str(redacted))
		}
		out[i] = m
	}
	return proto.Obj(out...)
}

func format(f fmt.State, verb rune, text, goText string) {
	if verb == 'v' && f.Flag('#') {
		text = goText
	}
	if _, err := io.WriteString(f, text); err != nil {
		return
	}
}
