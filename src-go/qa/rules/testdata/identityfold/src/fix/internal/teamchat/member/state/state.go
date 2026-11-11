// Package state stands for the member's own files, which hold a reference in the canonical form
// the sender folded it to: the allowance row the test gives excuses validRef, and nothing else.
package state

import "fix/internal/teamchat/proto"

func validRef(s string) bool {
	_, ok := proto.ParseRef(s)
	return ok
}

func notExcused(s string) bool {
	_, ok := proto.ParseRef(s) // want `proto.ParseRef given a name that is not folded first`
	return ok
}
