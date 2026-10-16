package hot

import (
	"context"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// move is H24: add to the new team (H22), then remove from the old one (H23), under one change
// lock. A failed add leaves the old team untouched; a failed remove leaves the session in both,
// and the same move again finishes it (each half is idempotent, H19).
func (x *Hot) move(ctx context.Context, t *target, r request) term.ExitCode {
	if code, ok := x.coordinatorSaid(ctx, t, r); !ok {
		return code
	}
	add := r
	add.role, add.demoteCurrent = "", false
	if code := x.addTo(ctx, t, add, r.to, r.toSeat); code != term.ExitOK {
		x.say("Not moved: " + t.session + " is still in " + r.team + ", and nothing else was changed.")
		return code
	}
	if code := x.removeFrom(ctx, t, r, r.team); code != term.ExitOK {
		x.say(t.session + " is in " + r.to + ", and the move out of " + r.team + " is not finished: run the same move again to finish it.")
		return code
	}
	x.say("Moved: " + t.session + " is in " + r.to + ", and no longer in " + r.team + ".")
	return term.ExitOK
}

// coordinatorSaid refuses, before the add changes anything, a move of the old team's
// coordinator with no --coordinator and no terminal to ask on (H23 step 2).
func (x *Hot) coordinatorSaid(ctx context.Context, t *target, r request) (term.ExitCode, bool) {
	seat, in := seatOf(t.desired.Seats, r.team)
	if !in || r.coordinator != "" || x.interactive() {
		return term.ExitOK, true
	}
	state, err := x.Host.Systemd.HomeserverState(ctx, r.team)
	if err != nil || state != systemd.HomeserverUp {
		return term.ExitOK, true
	}
	_, handle, err := x.seatHeld(ctx, t, r.team, seat)
	if err != nil || handle == "" {
		return term.ExitOK, true
	}
	roster, err := x.Host.Bus.List(ctx, r.team)
	if err != nil {
		return term.ExitOK, true
	}
	if m, found := roster.Member(handle); found && m.Role == agentbus.RoleCoordinator {
		return x.usageError("seat " + seat + " is " + r.team + "'s coordinator: say what becomes of the role with " +
			"--coordinator=demote (no one has it) or --coordinator=hand-to=<seat>; nothing was changed."), false
	}
	return term.ExitOK, true
}
