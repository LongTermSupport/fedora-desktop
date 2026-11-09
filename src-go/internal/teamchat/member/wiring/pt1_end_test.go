//go:build busfailpoints

package wiring

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

// PT1's removal and its two endings, run against the real keeper and link (SPEC-n3 PT1; P4 and
// P5). Each runs at rest: the schedule's operations before it are drained first (rest), because a
// removal drops what is pending, handed and awaiting by design and an end leaves the items still
// owed to the next session, so P1 could not stay strict across either. What a removal or an end
// races is the failpoint rows' (TestFailpointInRemove and the rest of e2e_failpoints_test.go).

// The notices a removal and the two endings post, as pt1Seen.want names them.
const (
	pt1WantReleased     = "left released"
	pt1WantStopped      = "left other"
	pt1WantDisconnected = "disconnected"
)

// remove is a desired generation that releases the seat (LN8). It returns once the keeper has
// deleted the team's store and logged that the seat's leave notice is owed, which is the removal
// as the engine and the keeper see it done; a store there again at once is a team still writing
// after its removal (pt1Seen.storeBack). The leave notice is waited for by leaveDone.
func (r *pt1Runner) remove() {
	r.t.Helper()
	r.rest()
	r.limbo = r.awaitingIDs()
	r.w.release()
	eventually(r.t, func() bool {
		return strings.Contains(r.s.log("keeper.log"), "seat "+teamName+": its leave notice is owed")
	}, r.s.describe)
	_, err := os.Stat(r.w.storePath())
	r.storeBack = !errors.Is(err, fs.ErrNotExist)
	r.removed = true
	r.atRemoval = len(r.copies())
	r.want = pt1WantReleased
}

// leaveDone waits for a removal's leave: the keeper's line that the seat is released, which it
// writes when the link says `left`, after both of the leave's posts, and the notice in main. It
// then asks recv once, the one verb that could hand a removed team's item to the agent.
func (r *pt1Runner) leaveDone() {
	r.t.Helper()
	eventually(r.t, func() bool { return strings.Contains(r.s.log("keeper.log"), "seat "+teamName+" released") }, r.s.describe)
	r.awaitWant()
	r.take(r.s.recvAny())
}

// awaitWant waits for the run's one notice to be in main.
func (r *pt1Runner) awaitWant() {
	r.t.Helper()
	eventually(r.t, func() bool { return r.w.notices(r.want) > 0 }, func() string {
		return "want " + r.want + "; notices " + strings.Join(r.w.lifecycle(), ", ") + "\n" + r.s.describe()
	})
}

// end is the session's end by the keeper's SIGTERM (pt1End) or by CMD's SIGKILL (pt1Die), after
// which no process of the run is left: it waits for the keeper to exit and for every link it
// started to be gone, so a notice posted twice is posted by then. A removed seat's end posts
// nothing for it, so the notice the run owes stays the leave's.
func (r *pt1Runner) end(kind pt1Kind) {
	r.t.Helper()
	if r.removed {
		r.leaveDone()
	} else {
		r.rest()
		r.want = map[pt1Kind]string{pt1End: pt1WantStopped, pt1Die: pt1WantDisconnected}[kind]
	}
	r.links = r.s.links()
	r.ended = true
	if kind == pt1End {
		kill(r.t, r.s.keeper.Pid(), syscall.SIGTERM)
	} else {
		kill(r.t, r.s.agent, syscall.SIGKILL)
	}
	r.s.wait()
	for _, pid := range r.links {
		eventually(r.t, func() bool { return gone(r.t, pid) }, r.s.describe)
	}
	r.awaitWant()
}
