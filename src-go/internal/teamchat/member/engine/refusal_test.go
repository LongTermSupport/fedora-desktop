package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// refusedAs is err as the exits error refusal makes of it.
func refusedAs(t *testing.T, what string, err error) *exits.Error {
	t.Helper()
	xe, ok := errors.AsType[*exits.Error](err)
	if !ok {
		t.Fatalf("%s: %v is not an exits error", what, err)
	}
	return xe
}

// fedora-desktop#421: every refusal a send can meet says its cause and the next step, never the
// reason code alone (the code is already the object's): "the send was refused (ref); fix the
// message" is gone for every reason, and each reason has its own message.
func TestEverySendRefusalSaysItsCause(t *testing.T) {
	ops := tid(t, "ops")
	seen := map[string]string{}
	for _, reason := range proto.SendRefusals {
		xe := refusedAs(t, reason, refusal(ops, &proto.RefusalError{Reason: reason}))
		if xe.Message == "" || strings.Contains(xe.Message, "("+reason+")") || strings.Contains(xe.Message, "fix the message") {
			t.Errorf("%s: %q names only its code", reason, xe.Message)
		}
		if other, dup := seen[xe.Message]; dup {
			t.Errorf("%s and %s share %q", reason, other, xe.Message)
		}
		seen[xe.Message] = reason
		if _, known := exits.ExitOf(xe.Code); !known || xe.Code == exits.CodeInternal {
			t.Errorf("%s: the code %q", reason, xe.Code)
		}
	}
	for reason, code := range map[string]string{
		proto.ReasonRef: exits.CodeRef, proto.ReasonAllowlist: exits.CodeRef, proto.ReasonExcluded: exits.CodeRef,
		proto.ReasonSender: exits.CodeRole, proto.ReasonRole: exits.CodeRole, proto.ReasonSize: exits.CodeSize,
		proto.ReasonForgeRate: exits.CodeForgeRate, proto.ReasonUnresolved: exits.CodeForge, proto.ReasonProvenance: exits.CodeProvenance,
		proto.ReasonVersion: exits.CodeSchema, proto.ReasonStale: exits.CodeSchema, proto.ReasonEdit: exits.CodeSchema,
	} {
		if xe := refusedAs(t, reason, refusal(ops, &proto.RefusalError{Reason: reason})); xe.Code != code {
			t.Errorf("%s: the code %q, want %q", reason, xe.Code, code)
		}
	}
	if xe := refusedAs(t, "an unknown reason", refusal(ops, &proto.RefusalError{Reason: "nope"})); xe.Code != exits.CodeInternal {
		t.Errorf("an unknown reason: %+v", xe)
	}
	plain := errors.New("not a refusal")
	if err := refusal(ops, plain); !errors.Is(err, plain) {
		t.Errorf("a plain error became %v", err)
	}
}

// A reference refusal names its cause, the reference and the verb it was for, and how to give
// a reference; an answer's names the reference of the ping it answers when it is known.
func TestARefRefusalNamesItsCauseAndTheFix(t *testing.T) {
	ops := tid(t, "ops")
	excluded := "commit:example-org/private@" + strings.Repeat("b", 40)
	reply := refAsked{verb: proto.VerbDone, how: refFlag}
	for name, tc := range map[string]struct {
		reason, cause string
		asked         refAsked
		want          []string
	}{
		"missing, the ping's ref known": {proto.ReasonRef, proto.RefCauseMissing, refAsked{verb: proto.VerbDone, how: refFlag, pinged: reviewRef}, []string{"done needs --ref", "commit:OWNER/REPO@SHA", reviewRef, "gives back"}},
		"missing, the ping's ref not":   {proto.ReasonRef, proto.RefCauseMissing, reply, []string{"done needs --ref", "path:OWNER/REPO@SHA:PATH", "issue:OWNER/REPO#N"}},
		"missing for a ping":            {proto.ReasonRef, proto.RefCauseMissing, refAsked{verb: "run-qa", how: refArg}, []string{"run-qa needs a reference after the verb", "pr:OWNER/REPO#N@SHA"}},
		"forbidden":                     {proto.ReasonRef, proto.RefCauseForbidden, refAsked{verb: proto.VerbAck, how: refFlag, sent: reviewRef}, []string{"ack takes no reference", "drop --ref"}},
		"form":                          {proto.ReasonRef, proto.RefCauseForm, refAsked{verb: "run-qa", how: refArg, sent: "the-commit"}, []string{"run-qa takes a reference of the form", "commit:OWNER/REPO@SHA", "the-commit is not one"}},
		"branch":                        {proto.ReasonRef, proto.RefCauseBranch, refAsked{verb: "review", how: refArg}, []string{"--branch goes only with a path: or commit: reference"}},
		"excluded":                      {proto.ReasonExcluded, proto.RefCauseExcluded, refAsked{verb: "review", how: refArg, sent: excluded}, []string{excluded, "team ops excludes"}},
		"not listed":                    {proto.ReasonAllowlist, proto.RefCauseNotListed, refAsked{verb: "sync", how: refArg, sent: reviewRef}, []string{reviewRef, "outside team ops", "sync takes only"}},
		"path prefix":                   {proto.ReasonAllowlist, proto.RefCausePathPrefix, refAsked{verb: "review", how: refArg, sent: reviewRef}, []string{reviewRef, "path prefixes"}},
		"no cause":                      {proto.ReasonRef, "", reply, []string{"teamchat help"}},
	} {
		xe := refusedAs(t, name, refusalFor(ops, &proto.RefusalError{Reason: tc.reason, Detail: tc.cause}, tc.asked))
		if xe.Code != exits.CodeRef {
			t.Errorf("%s: the code %q", name, xe.Code)
		}
		for _, w := range tc.want {
			if !strings.Contains(xe.Message, w) {
				t.Errorf("%s: %q does not say %q", name, xe.Message, w)
			}
		}
	}
	// With nothing asked (a queued send re-checked), the message still names the cause.
	xe := refusedAs(t, "queued", refusal(ops, &proto.RefusalError{Reason: proto.ReasonExcluded, Detail: proto.RefCauseExcluded}))
	if !strings.Contains(xe.Message, "team ops excludes") {
		t.Errorf("a queued excluded reference: %q", xe.Message)
	}
}

// The issue's own run: done answering a ping with no --ref names --ref and, for a fetched ping,
// the ping's reference; a ping with no reference names the reference after the verb.
func TestAnAnswerWithNoRefSaysWhatTheRefMustBe(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sendingWith(ops, w.replyStore(ops))
	d := firstOf(w.reply(verbReply(reviewEv, proto.VerbDone), nil))
	refusedCode(t, "done with no --ref", d, exits.CodeRef)
	if !strings.Contains(d.Error.Message, "done needs --ref") {
		t.Errorf("done with no --ref says %q", d.Error.Message)
	}
	f.setVerdict(evID(unknownEv), fetchedPing(ops, unknownEv, "fetch", reviewRef))
	d = firstOf(w.reply(verbReply(unknownEv, proto.VerbDone), nil))
	refusedCode(t, "done to a fetched ping with no --ref", d, exits.CodeRef)
	if !strings.Contains(d.Error.Message, reviewRef) {
		t.Errorf("done to a fetched ping says %q, not the ping's reference", d.Error.Message)
	}
	ack := verbReply(reviewEv, proto.VerbAck)
	ack.Ref = new(reviewRef)
	if d = firstOf(w.reply(ack, nil)); d.Error == nil || !strings.Contains(d.Error.Message, "ack takes no reference") {
		t.Errorf("ack --ref = %+v", d.Error)
	}
	d = firstOf(w.ping(&ops, "review", nil, "repo.3"))
	refusedCode(t, "review with no reference", d, exits.CodeRef)
	if !strings.Contains(d.Error.Message, "review needs a reference after the verb") {
		t.Errorf("review with no reference says %q", d.Error.Message)
	}
}

// fetchedPing is the verdict of a fetched ping n with verb and ref, from repo.3.
func fetchedPing(id paths.TeamID, n int, verb, ref string) proto.Verdict {
	return proto.Verdict{
		Outcome: proto.Deliver, Kind: proto.KindPing, Event: proto.Event{ID: evID(n), Sender: agent(id, 3), OriginTs: startWall},
		Content: proto.Content{Key: proto.KeyPing, Ping: proto.Ping{Verb: verb, To: []string{agent(id, 1)}, Ref: ref}},
	}
}
