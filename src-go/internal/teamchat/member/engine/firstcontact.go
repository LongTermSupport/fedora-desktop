package engine

import (
	"context"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// firstContactWait bounds how long a send waits for its team's first contact in the run. A
// homeserver that does not answer is down after teamsync's 15 s, which ends the wait sooner
// only past this bound, so the send still has its deadline's last 20 s to be posted.
const firstContactWait = 10 * time.Second

// msgWaitsForContact is the link.log line of a send held for its team's first contact.
const msgWaitsForContact = "a send waits for the team's first contact"

// contacted is whether the team has had its first contact in the run: its sends recovered and
// its store loaded, which follow its roster's verification in that order (teamsync's session:
// a seat's first session has no store until that Load), or it is down, stopped, removed or
// refused, or has no team goroutine to make the contact. On the state goroutine.
func (h *held) contacted() bool {
	return h.recovered && h.store != nil || h.health.Down || h.health.Stopped != "" || h.sendStop != "" || h.removed || h.refused || h.team == nil
}

// noteContact releases the sends waiting for the team's first contact once it has had it. On
// the state goroutine, after every change contacted reads.
func (h *held) noteContact() {
	if h.contact != nil && h.contacted() {
		close(h.contact)
		h.contact = nil
	}
}

// awaitContact holds a `say --wait-contact` until every team it may go to (flag's, else each
// held team) has had its first contact, at most firstContactWait, so a say made at a session's
// very start (a script's one-shot `run --plain -- teamchat say --wait-contact`) is not answered
// 20 for a roster not yet read, or queued for a recovery not yet made and then left in the
// outbox when the one-shot ends, while the team is neither unverified nor down for any reason
// but the time. It then returns whatever the teams' state; the say answers as it would have. A
// say without the flag keeps n3d-D19: never held for a sender that cannot post yet.
func (e *Engine) awaitContact(ctx context.Context, flag *paths.TeamID) {
	var waits []chan struct{}
	if err := e.do(ctx, func() {
		for _, id := range e.order() {
			if h := e.teams[id]; (flag == nil || *flag == id) && h.contact != nil && !h.contacted() {
				e.logf("team %s: %s", id, msgWaitsForContact)
				waits = append(waits, h.contact)
			}
		}
	}); err != nil || len(waits) == 0 {
		return
	}
	bound, stop := e.cfg.Clock.Hold(firstContactWait)
	defer stop()
	for _, c := range waits {
		select {
		case <-c:
		case <-bound:
			return
		case <-ctx.Done():
			return
		case <-e.done:
			return
		}
	}
}
