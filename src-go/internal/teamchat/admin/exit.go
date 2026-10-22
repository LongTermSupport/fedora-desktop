package admin

import (
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/rooms"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// ExitCodeOf is C 3.3's exit code for a failure (RE9, AP11, CV3): a team lock held is 75; a
// child's failure is its class's code; drift the rooms engine, apply or the host steps refused,
// a verb's own refusal, and an approve the gate refused, is 70,
// a homeserver that did not answer usably or kept rate-limiting is 69, anything else 1. The
// matrix client's own ExitCode is the member side's table and is not used (n2a-D13). An LXC
// member offline that the run targeted is 69, but only when every joined error is one: else the
// first error that is not offline decides (n3b-D3).
func ExitCodeOf(err error) term.ExitCode {
	if offline(err) {
		if err = decisive(err); offline(err) {
			return ExitUnreachable
		}
	}
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
	if errors.Is(err, teamlock.ErrBusy) {
		return ExitBusy, true
	}
	if e, ok := errors.AsType[*ChildError](err); ok {
		return classExit[e.Class], true
	}
	if e, ok := errors.AsType[*refusal](err); ok && e != nil {
		return ExitRefused, true
	}
	if e, ok := errors.AsType[*configError](err); ok && e != nil {
		return ExitConfig, true
	}
	if e, ok := errors.AsType[*host.Error](err); ok && e.Drift {
		return ExitRefused, true
	}
	return 0, false
}

func offline(err error) bool {
	e, ok := errors.AsType[*lxc.OfflineError](err)
	return ok && e != nil
}

// decisive is n3b-D3's error of a join: the first, depth first, that is not offline, or err when
// every one is.
func decisive(err error) error {
	j, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return err
	}
	for _, e := range j.Unwrap() {
		if d := decisive(e); !offline(d) {
			return d
		}
	}
	return err
}
