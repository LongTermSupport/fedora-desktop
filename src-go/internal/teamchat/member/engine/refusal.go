package engine

import (
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// A send refusal's message names its cause and the next step; the reason is the object's code
// alone (fedora-desktop#421, rule project_refusal_reason_text).
const (
	msgVersion  exits.Template = "the message's wire version is not the team's; this is a bug: tell a human"
	msgSchema   exits.Template = "the message's form is not one its verb allows: an answer (ack, nack, done, blocked) goes with teamchat reply <ping id> --verb, any other verb with teamchat ping"
	msgSize     exits.Template = "the message is over the protocol's size limit; shorten the text or name fewer addressees"
	msgEdit     exits.Template = "an edit is not a message a member sends; this is a bug: tell a human"
	msgSender   exits.Template = "this member is not an active agent of team {}, so it sends nothing there; run status"
	msgRole     exits.Template = "this member's role may not send that; ask the coordinator instead"
	msgTarget   exits.Template = "an addressee is not one this message may go to (parked, gone, the sender itself, named twice, or too many); run status for the members"
	msgVerb     exits.Template = "the verb is not one of the protocol's; teamchat help ping lists them"
	msgStale    exits.Template = "the message is older than its verb's timeout; send it again"
	msgLeak     exits.Template = "the text holds what looks like a secret (a token or a key); remove it and send again"
	msgBody     exits.Template = "the text is not in its room's form; teamchat help names each verb's form"
	msgNoReason exits.Template = "teamchat refused the send for a reason it does not explain; this is a bug: tell a human"

	msgRefMissing     exits.Template = "{} needs {}: one of {}"
	msgRefMissingEcho exits.Template = "{} needs {}: one of {}; the ping it answers carried {}, which an answer normally gives back"
	msgRefForbidden   exits.Template = "{} takes no reference; drop {}"
	msgRefForm        exits.Template = "{} takes a reference of the form {}; {} is not one"
	msgRefBranch      exits.Template = "--branch goes only with a path: or commit: reference, and must be a branch name"
	msgRefExcluded    exits.Template = "{} is in a repository team {} excludes; reference another repository"
	msgRefNotListed   exits.Template = "{} is outside team {}'s repositories, and {} takes only theirs; reference a repository the team lists"
	msgRefPathPrefix  exits.Template = "{} is outside team {}'s path prefixes; reference a path under one of them"
	msgRefAllowlist   exits.Template = "{} is outside what team {} allows references to; reference one of the team's repositories"
	msgRefUnexplained exits.Template = "{} does not fit {}; teamchat help ping and teamchat help reply list each verb's reference forms"
)

// The ways a sender gives a reference: reply's flag, and ping's argument after the verb.
const (
	refFlag = "--ref"
	refArg  = "a reference after the verb"
)

// refAsked is what a send asked of its reference, for its refusal's message: the verb, how a
// reference is given, the reference sent, and for an answer the reference of the ping it
// answers when it is known. The zero value is a send re-checked from the queue.
type refAsked struct {
	verb, how, sent, pinged string
}

// plainRefusals are the reasons whose message needs no fill: the code and template of each.
var plainRefusals = map[string]struct {
	code string
	msg  exits.Template
}{
	proto.ReasonVersion:    {exits.CodeSchema, msgVersion},
	proto.ReasonSchema:     {exits.CodeSchema, msgSchema},
	proto.ReasonSize:       {exits.CodeSize, msgSize},
	proto.ReasonEdit:       {exits.CodeSchema, msgEdit},
	proto.ReasonRole:       {exits.CodeRole, msgRole},
	proto.ReasonTarget:     {exits.CodeTarget, msgTarget},
	proto.ReasonVerb:       {exits.CodeVerb, msgVerb},
	proto.ReasonStale:      {exits.CodeSchema, msgStale},
	proto.ReasonSecret:     {exits.CodeSecret, msgLeak},
	proto.ReasonBody:       {exits.CodeBody, msgBody},
	proto.ReasonDuplicate:  {exits.CodeDuplicate, msgDuplicate},
	proto.ReasonForgeRate:  {exits.CodeForgeRate, msgForgeRate},
	proto.ReasonUnresolved: {exits.CodeForge, msgUnresolved},
	proto.ReasonProvenance: {exits.CodeProvenance, msgProvenance},
}

// refusal is a composer's or Admit's refusal as the send's exit (A 7.6's send refusals, B
// 2.2.4), each reason with its own message; any other error is returned as it is.
func refusal(t paths.TeamID, err error) error {
	return refusalFor(t, err, refAsked{})
}

// refusalFor is refusal for a send that asked asked of its reference.
func refusalFor(t paths.TeamID, err error, asked refAsked) error {
	re, ok := errors.AsType[*proto.RefusalError](err)
	if !ok {
		return err
	}
	if p, plain := plainRefusals[re.Reason]; plain {
		return exits.New(p.code, &t, p.msg)
	}
	switch re.Reason {
	case proto.ReasonSender:
		return exits.New(exits.CodeRole, &t, msgSender, exits.Team(t))
	case proto.ReasonRate:
		return exits.New(exits.CodeRate, &t, msgRate, exits.Team(t))
	case proto.ReasonRef, proto.ReasonAllowlist, proto.ReasonExcluded:
		return refRefusal(t, re, asked)
	}
	return exits.New(exits.CodeInternal, &t, msgNoReason)
}

// refRefusal is a reference refusal's message, chosen by its cause (the refusal's Detail).
func refRefusal(t paths.TeamID, re *proto.RefusalError, asked refAsked) *exits.Error {
	verb, how, sent := words(asked.verb, "the verb"), words(asked.how, "a reference"), refName(asked.sent)
	team := exits.Team(t)
	cause := re.Detail
	if re.Reason == proto.ReasonExcluded {
		cause = proto.RefCauseExcluded
	}
	switch cause {
	case proto.RefCauseMissing:
		if asked.pinged != "" {
			return exits.New(exits.CodeRef, &t, msgRefMissingEcho, verb, how, refForms(asked.verb), exits.Name(asked.pinged))
		}
		return exits.New(exits.CodeRef, &t, msgRefMissing, verb, how, refForms(asked.verb))
	case proto.RefCauseForbidden:
		return exits.New(exits.CodeRef, &t, msgRefForbidden, verb, how)
	case proto.RefCauseForm:
		return exits.New(exits.CodeRef, &t, msgRefForm, verb, refForms(asked.verb), sent)
	case proto.RefCauseBranch:
		return exits.New(exits.CodeRef, &t, msgRefBranch)
	case proto.RefCauseExcluded:
		return exits.New(exits.CodeRef, &t, msgRefExcluded, sent, team)
	case proto.RefCauseNotListed:
		return exits.New(exits.CodeRef, &t, msgRefNotListed, sent, team, verb)
	case proto.RefCausePathPrefix:
		return exits.New(exits.CodeRef, &t, msgRefPathPrefix, sent, team)
	}
	if re.Reason == proto.ReasonAllowlist {
		return exits.New(exits.CodeRef, &t, msgRefAllowlist, sent, team)
	}
	return exits.New(exits.CodeRef, &t, msgRefUnexplained, sent, verb)
}

// words is s as a fill, or fallback for none.
func words(s, fallback string) exits.Fill {
	if s == "" {
		return exits.Words(fallback)
	}
	if s == refFlag {
		return exits.Name(s)
	}
	return exits.Words(s)
}

// refName is a reference sent as a fill, or "the reference" for none.
func refName(ref string) exits.Fill {
	if ref == "" {
		return exits.Words("the reference")
	}
	return exits.Name(ref)
}

// refForms are the spellings of the forms verb's reference may take, every form for a verb the
// protocol does not name.
func refForms(verb string) exits.Fill {
	forms := proto.RefForms
	if rule, known := proto.Verbs[verb]; known {
		forms = rule.RefForms()
	}
	fills := make([]exits.Fill, 0, len(forms))
	for _, form := range forms {
		fills = append(fills, exits.Name(proto.RefSpellings[form]))
	}
	return exits.Join(" or ", fills...)
}
