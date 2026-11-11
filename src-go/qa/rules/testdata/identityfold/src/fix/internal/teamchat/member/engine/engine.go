// Package engine stands for the member's sender: a ping's reference the CLI gave, written to the
// ping as given (the second instance of the class) and folded first (A 9.1: a sender lowercases).
package engine

import "fix/internal/teamchat/proto"

// Args are the CLI's arguments.
type Args struct {
	Verb string
	Ref  *string
}

func ping(a Args) proto.Ping {
	p := proto.Ping{Verb: a.Verb}
	if a.Ref != nil {
		p.Ref = *a.Ref // want `a name written to proto.Ping.Ref that is not folded first`
	}
	return p
}

func answer(a Args, ref string) proto.Ping {
	q := proto.Ping{Verb: a.Verb, Ref: ref} // want `a name written to proto.Ping.Ref that is not folded first`
	q.Branch = ref
	return q
}

func folded(a Args) proto.Ping {
	p := proto.Ping{Verb: a.Verb}
	if a.Ref != nil {
		p.Ref = proto.LowercaseRefRepo(*a.Ref)
	}
	return p
}

func parsed(ref string) bool {
	_, ok := proto.ParseRef(ref) // want `proto.ParseRef given a name that is not folded first`
	_, folded := proto.ParseRef(proto.LowercaseRefRepo(ref))
	return ok && folded
}
