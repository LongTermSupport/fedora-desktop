package hot

import (
	"context"
	"fmt"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// remove is H23: the seat out of the desired file with a farewell, released, re-baselined,
// its goodbye posted, then the farewell dropped. A farewell still owed (H19) is finished by the
// next run.
func (x *Hot) remove(ctx context.Context, r request) term.ExitCode {
	t, code, ok := x.resolve(ctx, r.session)
	if !ok {
		return code
	}
	seat, in := seatOf(t.desired.Seats, r.team)
	owed := slices.ContainsFunc(t.desired.Farewell, func(f membership.Farewell) bool { return f.Team == r.team })
	if !in && !owed {
		x.mark(markSkip, "team "+r.team+": "+r.session+" is not in it")
		x.say("Nothing to do: " + r.session + " is not in " + r.team + ".")
		return term.ExitOK
	}
	state, err := x.Host.Systemd.HomeserverState(ctx, r.team)
	if err != nil {
		x.mark(markFailed, "team "+r.team+": systemctl could not say whether its homeserver is up: "+err.Error())
		return term.ExitFailure
	}
	up := state == systemd.HomeserverUp
	// The session is on no team once this seat is out: now, or already on an owed-farewell rerun.
	last := (in && len(t.desired.Seats) == 1) || (!in && len(t.desired.Seats) == 0)
	if in {
		if code, ok := x.takeOut(ctx, t, r, seat, up); !ok {
			return code
		}
	} else {
		seat = owedSeat(t.desired.Farewell, r.team)
		x.mark(markSkip, "record: "+r.team+" already left out")
		x.mark(markSkip, "session: seat "+seat+" already released")
	}
	if last {
		if code, ok := x.withdrawRelaunch(t); !ok {
			return code
		}
	}
	return x.farewell(ctx, t, r.team, seat, up)
}

// withdrawRelaunch drops a first-join relaunch still pending once the session is on no team:
// it would relaunch for nothing.
func (x *Hot) withdrawRelaunch(t *target) (term.ExitCode, bool) {
	pending, err := membership.RelaunchPending(t.dir)
	if err == nil && pending {
		err = membership.WithdrawRelaunch(t.dir)
	}
	if err != nil {
		x.mark(markFailed, "relaunch: "+err.Error())
		return term.ExitFailure, false
	}
	if pending {
		x.mark(markDone, "relaunch: request withdrawn (the session is on no team now)")
	}
	return term.ExitOK, true
}

// takeOut is H23 steps 2 to 4: the role check, the record and the desired file, and the wait
// for the seat to be free.
func (x *Hot) takeOut(ctx context.Context, t *target, r request, seat string, up bool) (term.ExitCode, bool) {
	_, handle, err := x.seatHeld(ctx, t, r.team, seat)
	if err != nil {
		x.mark(markFailed, "seat "+seat+": "+err.Error())
		return term.ExitFailure, false
	}
	if code, ok := x.notCoordinator(ctx, r.team, seat, handle, up); !ok {
		return code, false
	}
	question := fmt.Sprintf("Remove %s from %s (seat %s, %s)? Its token stays valid: remove stops this session's pingbus, it does not revoke.",
		r.session, r.team, seat, handle)
	if code, proceed := x.confirm(ctx, question, r.yes); !proceed {
		return code, false
	}
	seats := slices.DeleteFunc(slices.Clone(t.desired.Seats), func(s membership.Seat) bool { return s.Team == r.team })
	farewell := append(slices.Clone(t.desired.Farewell), membership.Farewell{Team: r.team, Seat: seat, Reason: membership.ReasonRemoved})
	if err := x.writeRecord(t, seats, ""); err != nil {
		return term.ExitFailure, false
	}
	if err := x.writeDesired(t, seats, farewell, "farewell "+r.team+"/"+seat); err != nil {
		return term.ExitFailure, false
	}
	start := x.Now()
	for {
		held, _, err := x.seatHeld(ctx, t, r.team, seat)
		if err == nil && !held {
			x.mark(markDone, fmt.Sprintf("session: seat %s released (%.1f s)", seat, x.elapsed(start)))
			return term.ExitOK, true
		}
		if x.Now().Sub(start) >= x.Waits.Apply {
			x.mark(markFailed, "session: seat "+seat+" is still held after "+seconds(x.Waits.Apply)+
				"; the farewell is owed: run the same command again once the seat keeper has let it go (ccy-teams shows it)")
			return term.ExitFailure, false
		}
		x.Sleep(x.Waits.Poll)
	}
}

// notCoordinator refuses to remove the team's coordinator: role options come with M4.
func (x *Hot) notCoordinator(ctx context.Context, team, seat, handle string, up bool) (term.ExitCode, bool) {
	if !up {
		x.mark(markSkip, "role: not checked (the homeserver is down)")
		return term.ExitOK, true
	}
	roster, err := x.Host.Bus.List(ctx, team)
	if err != nil {
		x.mark(markFailed, "role: the members of "+team+" could not be read: "+lastLine(err))
		return term.ExitFailure, false
	}
	if m, ok := roster.Member(handle); ok && m.Role == agentbus.RoleCoordinator {
		return x.usageError("seat " + seat + " is " + team + "'s coordinator, and remove has no role options yet: make it a worker first " +
			"(ccy-teams, the seat's menu: Stop being coordinator), then remove it."), false
	}
	x.mark(markDone, "role: seat "+seat+" is a worker")
	return term.ExitOK, true
}

// farewell is H23 steps 5 to 7: re-baseline, goodbye, and drop the farewell entry once both
// are done; otherwise it stays owed.
func (x *Hot) farewell(ctx context.Context, t *target, team, seat string, up bool) term.ExitCode {
	ref := seat + "@" + team
	retired := x.pingbus(ctx, "re-baseline", "sync position cleared, listening status expired", "seat", "retire", "--root", t.seatsRoot(), ref)
	_, handle, err := x.seatHeld(ctx, t, team, seat)
	if err != nil || handle == "" {
		handle = "seat " + seat
	}
	said := x.pingbus(ctx, "goodbye", handle+" left the team: removed by the owner", "seat", "farewell", "--root", t.seatsRoot(), ref)
	if !retired || !said {
		if !up {
			x.say("Removed, but goodbye and status not sent: homeserver down. Run the same command again once it is up.")
		} else {
			x.say("Removed, but the goodbye or the re-baseline is still owed: run the same command again to finish it.")
		}
		return term.ExitFailure
	}
	farewell := slices.DeleteFunc(slices.Clone(t.desired.Farewell), func(f membership.Farewell) bool { return f.Team == team })
	if err := x.writeDesired(t, t.desired.Seats, farewell, "farewell done"); err != nil {
		return term.ExitFailure
	}
	x.say("Removed. " + t.session + " is no longer in " + team + "; seat " + seat +
		" is free. Its token stays valid: remove stops this session's pingbus, it does not revoke.")
	return term.ExitOK
}

// pingbus runs the host's pingbus and says the step.
func (x *Hot) pingbus(ctx context.Context, step, done string, args ...string) bool {
	if _, err := x.Host.Runner.Run(ctx, execx.Cmd{Name: x.Host.Pingbus, Args: args}); err != nil {
		x.mark(markFailed, step+": "+lastLine(err))
		return false
	}
	x.mark(markDone, step+": "+done)
	return true
}

func seatOf(seats []membership.Seat, team string) (string, bool) {
	for _, s := range seats {
		if s.Team == team {
			return s.Seat, true
		}
	}
	return "", false
}

func owedSeat(farewell []membership.Farewell, team string) string {
	for _, f := range farewell {
		if f.Team == team {
			return f.Seat
		}
	}
	return ""
}
