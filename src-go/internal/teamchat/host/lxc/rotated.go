package lxc

import "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"

// WhyCARotated is the why of an LXC seat whose placed bundle holds a team CA that a rotation
// replaced (Plan 00246 SPEC-cross-network X33): the next apply reissues the seat, so its new
// bundle carries the new CA, and places it.
const WhyCARotated = "the team CA was rotated; reissued at the next apply"

// CARotated is r with every seat that holds a handle and has not left deferred for that reissue,
// and their handles in the record's order. The host's record is the one place a guest's handle is
// known, so it is how a rotation reaches the LXC seats it cannot rewrite itself.
func CARotated(r lxcstate.Record, nowMS int64) (lxcstate.Record, []string) {
	var handles []string
	for _, e := range r.Members {
		if e.Handle == "" || e.Status == lxcstate.StatusLeft {
			continue
		}
		e.Status, e.Why = lxcstate.StatusDeferred, WhyCARotated
		r = r.Set(e, nowMS)
		handles = append(handles, e.Handle)
	}
	return r, handles
}
