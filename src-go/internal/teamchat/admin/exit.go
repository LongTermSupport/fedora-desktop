package admin

import (
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/rooms"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// ExitCodeOf is C 3.3's exit code for a failure (RE9, AP11, CV3): a team lock held is 75; a
// child's failure is its class's code; drift the rooms engine, apply or the host steps refused,
// a verb's own refusal, and an approve the gate refused, is 70,
// a homeserver that did not answer usably or kept rate-limiting is 69, anything else 1. The
// matrix client's own ExitCode is the member side's table and is not used (n2a-D13).
func ExitCodeOf(err error) term.ExitCode {
	if code, ok := verbExit(err); ok {
		return code
	}
	if e, ok := errors.AsType[*rooms.Error](err); ok && e.Drift {
		return ExitRefused
	}
	if e, ok := errors.AsType[*apply.Error](err); ok && e.Drift {
		return ExitRefused
	}
	if e, ok := errors.AsType[*declare.RefusedError](err); ok && e != nil {
		return ExitRefused
	}
	if errors.Is(err, matrix.ErrUnreachable) || errors.Is(err, matrix.ErrRateLimited) {
		return ExitUnreachable
	}
	return term.ExitFailure
}

// verbExit is the code of a failure the verbs' own layer names: the lock, the child, a refusal
// of a verb's or of the host steps.
func verbExit(err error) (term.ExitCode, bool) {
	if e, ok := errors.AsType[*host.BusyError](err); ok && e != nil {
		return ExitBusy, true
	}
	if e, ok := errors.AsType[*ChildError](err); ok {
		return classExit[e.Class], true
	}
	if e, ok := errors.AsType[*refusal](err); ok && e != nil {
		return ExitRefused, true
	}
	if e, ok := errors.AsType[*host.Error](err); ok && e.Drift {
		return ExitRefused, true
	}
	return 0, false
}
