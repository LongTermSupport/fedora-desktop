package wire

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
)

const hookToken = "s3cr3t-inbox-token"

func hookArgs() HookArgs {
	return HookArgs{
		Payload: PayloadOK, SessionID: new("0b6f6a54-3c55-4a3e-9d7c-6b1b2b1f9a00"),
		TranscriptPath: new("/home/u/.claude/projects/p/0b6f6a54.jsonl"), Source: new("startup"),
		Socket: new("/run/user/1000/cc-socks/4242.sock"), Token: new(hookToken),
	}
}

func encodeArgs(t *testing.T, a HookArgs) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// HookArgs travel whole: every key, null for an absent field, and the token in the clear on
// the wire, which only the link reads.
func TestHookArgsRoundTrip(t *testing.T) {
	a := hookArgs()
	raw := encodeArgs(t, a)
	for _, key := range []string{`"payload":"ok"`, `"unusable":null`, `"stop_hook_false":false`, `"reason":null`, `"token":"` + hookToken + `"`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("%s lacks %s", raw, key)
		}
	}
	back, err := DecodeHookArgs(OpHookSessionStart, raw)
	if err != nil || *back.Token != hookToken || *back.Source != "startup" {
		t.Fatalf("back %v, err %v", back, err)
	}
	stop := HookArgs{Payload: PayloadOK, StopHookFalse: true}
	if _, err := DecodeHookArgs(OpHookStop, encodeArgs(t, stop)); err != nil {
		t.Fatal(err)
	}
	end := HookArgs{Payload: PayloadOK, Reason: new("exit")}
	if _, err := DecodeHookArgs(OpHookSessionEnd, encodeArgs(t, end)); err != nil {
		t.Fatal(err)
	}
	for _, why := range []string{UnusableSize, UnusableUTF8, UnusableJSON} {
		u := HookArgs{Payload: PayloadUnusable, Unusable: new(why), Socket: new("/s"), Token: new("t")}
		if _, err := DecodeHookArgs(OpHookPrompt, encodeArgs(t, u)); err != nil {
			t.Fatalf("%s: %v", why, err)
		}
	}
}

// DecodeHookArgs refuses, as exit 10 schema, what a hook never sends.
func TestDecodeHookArgsRefuses(t *testing.T) {
	long := func(n int) *string { return new(strings.Repeat("a", n)) }
	for name, tc := range map[string]struct {
		op string
		a  HookArgs
	}{
		"not a hook op":             {OpRecv, HookArgs{Payload: PayloadOK}},
		"payload":                   {OpHookPrompt, HookArgs{Payload: "maybe"}},
		"unusable without a reason": {OpHookPrompt, HookArgs{Payload: PayloadUnusable}},
		"a reason outside the set":  {OpHookPrompt, HookArgs{Payload: PayloadUnusable, Unusable: new("big")}},
		"a reason on ok":            {OpHookPrompt, HookArgs{Payload: PayloadOK, Unusable: new(UnusableJSON)}},
		"fields with unusable":      {OpHookPrompt, HookArgs{Payload: PayloadUnusable, Unusable: new(UnusableJSON), SessionID: new("x")}},
		"source off session-start":  {OpHookPrompt, HookArgs{Payload: PayloadOK, Source: new("startup")}},
		"stop flag off stop":        {OpHookPrompt, HookArgs{Payload: PayloadOK, StopHookFalse: true}},
		"reason off session-end":    {OpHookStop, HookArgs{Payload: PayloadOK, Reason: new("exit")}},
		"long session id":           {OpHookPrompt, HookArgs{Payload: PayloadOK, SessionID: long(MaxHookSessionID + 1)}},
		"long transcript":           {OpHookPrompt, HookArgs{Payload: PayloadOK, TranscriptPath: long(MaxHookTranscript + 1)}},
		"long source":               {OpHookSessionStart, HookArgs{Payload: PayloadOK, Source: long(MaxHookWord + 1)}},
		"long reason":               {OpHookSessionEnd, HookArgs{Payload: PayloadOK, Reason: long(MaxHookWord + 1)}},
		"long socket":               {OpHookPrompt, HookArgs{Payload: PayloadOK, Socket: long(MaxHookSocket + 1)}},
		"long token":                {OpHookPrompt, HookArgs{Payload: PayloadOK, Token: long(MaxHookToken + 1)}},
	} {
		_, err := DecodeHookArgs(tc.op, encodeArgs(t, tc.a))
		if e, ok := errors.AsType[*exits.Error](err); !ok || e.Code != exits.CodeSchema {
			t.Fatalf("%s: %v", name, err)
		}
		if err != nil && strings.Contains(err.Error(), "aaaa") {
			t.Fatalf("%s: the refusal quotes its input: %v", name, err)
		}
	}
	for _, raw := range []string{`{"payload":"ok"}`, `[]`, `{"payload":"ok","unusable":null,"session_id":null,"transcript_path":null,"source":null,"stop_hook_false":false,"reason":null,"socket":null,"token":null,"x":1}`} {
		if _, err := DecodeHookArgs(OpHookPrompt, json.RawMessage(raw)); err == nil {
			t.Fatalf("%s: accepted", raw)
		}
	}
}

// Every formatter of hook args and of a request carrying them redacts the token.
func TestTheTokenIsRedacted(t *testing.T) {
	a := hookArgs()
	req := Request{V: Version, Op: OpHookSessionStart, Kit: "0123456789abcdef", DeadlineMS: 1, Args: encodeArgs(t, a), Stdin: new(hookToken)}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%d", "%x", "%q"} {
		for name, v := range map[string]any{"args": a, "request": req, "args pointer": &a, "request pointer": &req} {
			text := fmt.Sprintf(verb, v)
			if strings.Contains(text, hookToken) || strings.Contains(text, fmt.Sprintf("%x", hookToken)) {
				t.Fatalf("%s %s: %s", name, verb, text)
			}
			if !strings.Contains(text, redacted) {
				t.Fatalf("%s %s shows no redaction: %s", name, verb, text)
			}
		}
	}
	if s := req.String(); !strings.Contains(s, OpHookSessionStart) || !strings.Contains(s, "startup") {
		t.Fatalf("the request's form lost what is not secret: %s", s)
	}
	none := Request{V: Version, Op: OpRecv, Kit: "0123456789abcdef", DeadlineMS: 1, Args: json.RawMessage(`{"team":null}`)}
	if s := none.String(); !strings.Contains(s, `{"team":null}`) || strings.Contains(s, "stdin") {
		t.Fatalf("a request with no token: %s", s)
	}
	bad := Request{Op: OpRecv, Args: json.RawMessage(`{"token":`)}
	if s := bad.String(); strings.Contains(s, "token") {
		t.Fatalf("unparsed args are shown: %s", s)
	}
	if s := (HookArgs{Payload: PayloadOK}).String(); strings.Contains(s, redacted) {
		t.Fatalf("no token, yet redacted: %s", s)
	}
}
