package engine

import "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"

// point is one of the engine's failpoints. Its call sites pass the number, never the name: only
// the busfailpoints build (hitPoint) and the tests resolve the name, so a release binary holds
// no point's name (failpoint's gate) although the engine's hit is a field the tests replace.
type point int

const (
	pointAfterHandOut point = iota
	pointAfterAuth
	pointAfterAwaiting
	pointAfterUserLine
	pointBetweenStoreWrite
	pointAfterMissing
)

// name is the failpoint the point is; "" for none.
func (p point) name() failpoint.Name {
	switch p {
	case pointAfterHandOut:
		return failpoint.LinkAfterHandOut
	case pointAfterAuth:
		return failpoint.LinkAfterAuth
	case pointAfterAwaiting:
		return failpoint.LinkAfterAwaiting
	case pointAfterUserLine:
		return failpoint.LinkAfterUserLine
	case pointBetweenStoreWrite:
		return failpoint.LinkBetweenStoreWrites
	case pointAfterMissing:
		return failpoint.LinkAfterMissing
	}
	return ""
}
