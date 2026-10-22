package admin

import (
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/rooms"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// ExitCodeOf is C 3.3's exit code for a failure (RE9, AP11): drift the rooms engine or apply
// refused, and an approve the gate refused, is 70,
// a homeserver that did not answer usably or kept rate-limiting is 69, anything else 1. The
// matrix client's own ExitCode is the member side's table and is not used (n2a-D13).
func ExitCodeOf(err error) term.ExitCode {
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
