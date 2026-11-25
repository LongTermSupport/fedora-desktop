package hot

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// TeamchatSeats is a running v2 (teamchat) session's seats, changed through its desired.json
// (C 3.7.5; teamchatlaunch.HotSeats): the seat a join takes, the join and the leave, a failed
// join put back, the keeper's report that it holds a seat, and a seat's release.
type TeamchatSeats interface {
	Desired() state.Desired
	Choose(team, given string) (string, string, error)
	Join(ctx context.Context, team, seat string) error
	Leave(team string) (state.DesiredSeat, error)
	Put(seats []state.DesiredSeat) error
	Applied(team, seat string) (bool, string, error)
	Release(ctx context.Context, team string) (bool, error)
}

// The refusals of what is not built for a v2 session yet (register R41): each names teamchat
// and the way on, so none reads as v1's "launched without the kit".
const (
	v2VerbNotBuilt   = "'session %s' is not built for a teamchat (v2) session yet; add and remove are (a move is an add, then a remove)"
	v2RoleNotBuilt   = "--role and --demote-current are not built for a teamchat (v2) session's add yet: add it, then set the role in ccy-teams (Enter on the member)"
	v2RemoveNotBuilt = "--park and --coordinator are not built for a teamchat (v2) session's remove yet: the member stays in the team with its role; park or hand over the role in ccy-teams"
)

// v2CCNotBuilt refuses a cc session on teamchat: it has no v1 membership to find it by, and hot
// control of one is not built (register R41).
const v2CCNotBuilt = "a cc session on a teamchat (v2) team is not changed while it runs yet: end it and start it again with cc --teams naming the seats it should have"

// ccOnTeamchat is whether a session with no container and no cc membership is a running cc
// session launched on teamchat: a cc record, its tmux pane live and a v2 desired membership in
// its checkout. It only reads; locate refuses such a session by name. A session with no record
// is not one, and its record is reported where resolve reads it.
func (x *Hot) ccOnTeamchat(ctx context.Context, session string) (bool, error) {
	if x.Teamchat == nil {
		return false, nil
	}
	store, err := x.Host.Registry()
	if err != nil {
		return false, err
	}
	if rec, recErr := store.Get(session); recErr == nil && rec.Prefix == membership.KindCC {
		pid, err := x.panePID(ctx, session)
		if err != nil || pid == 0 {
			return false, err
		}
		_, found, err := x.Teamchat(rec.Dir, session)
		if err != nil {
			return false, fmt.Errorf("whether it runs on teamchat (v2) could not be read: %w", err)
		}
		return found, nil
	}
	return false, nil
}

// changeV2 runs a verb on a v2 session: add and remove act on its desired.json; the others are
// refused by name.
func (x *Hot) changeV2(ctx context.Context, t *target, r request) term.ExitCode {
	switch {
	case r.verb == verbAdd && (r.role != "" || r.demoteCurrent):
		return x.refuseV2(t, v2RoleNotBuilt)
	case r.verb == verbAdd:
		return x.addV2(ctx, t, r)
	case r.verb == verbRemove && (r.park || r.coordinator != ""):
		return x.refuseV2(t, v2RemoveNotBuilt)
	case r.verb == verbRemove:
		return x.removeV2(ctx, t, r)
	}
	return x.refuseV2(t, fmt.Sprintf(v2VerbNotBuilt, r.verb))
}

func (x *Hot) refuseV2(t *target, why string) term.ExitCode {
	x.step(markFailed, fmt.Sprintf(stepSessionNamed, t.session), why)
	return exitConfig
}

// addV2 is H22 for a v2 session: the seat chosen from the pool, the record, the take and the next
// generation of desired.json, then the wait for the keeper to hold it; a seat not held is put
// back (the generation without it, the seat released, the record restored).
func (x *Hot) addV2(ctx context.Context, t *target, r request) term.ExitCode {
	before := t.v2.Desired().Seats
	if i := slices.IndexFunc(before, func(s state.DesiredSeat) bool { return s.Team.String() == r.team }); i >= 0 {
		if r.seat != "" && r.seat != before[i].Seat {
			return x.usageError(fmt.Sprintf("%s is already in %s as seat %s: one seat per team per session; remove it first.", t.session, r.team, before[i].Seat))
		}
		x.say(fmt.Sprintf(alreadyIn, t.session, r.team, before[i].Seat))
		return term.ExitOK
	}
	seatPart, handle, err := t.v2.Choose(r.team, r.seat)
	if err != nil {
		x.mark(markFailed, "seat: "+err.Error())
		return seatCode(err)
	}
	x.step(markDone, fmt.Sprintf(stepSeatNamed, seatPart), "free ("+handle+")")
	question := fmt.Sprintf("Add %s to %s as seat %s (%s)?", t.session, r.team, seatPart, handle)
	if code, proceed := x.confirm(ctx, question, r.yes); !proceed {
		return code
	}
	if err := x.writeRecord(t, append(v1Seats(before), membership.Seat{Team: r.team, Seat: seatPart}), ""); err != nil {
		return term.ExitFailure
	}
	if err := t.v2.Join(ctx, r.team, seatPart); err != nil {
		x.mark(markFailed, "seat "+seatPart+": "+err.Error())
		x.restoreRecordV2(t, before)
		return seatCode(err)
	}
	x.step(markDone, stepMembership, fmt.Sprintf(generationWritten, t.v2.Desired().Generation))
	if why, ended := x.waitV2(ctx, t, r.team, seatPart); why != "" {
		x.mark(markFailed, "session: "+why)
		x.putBackV2(ctx, t, before, r.team, ended)
		return term.ExitFailure
	}
	x.say(fmt.Sprintf(doneJoinedV2, r.team, handle))
	return term.ExitOK
}

// waitV2 waits, within Waits.Apply (the wait every add has), for the keeper to report the seat
// held at the generation written and the team's homeserver to have answered the session
// (HotSeats.Applied, R60), and is why not ("" joined) and whether the session ended meanwhile.
// The wait is not lengthened for a slow first answer: a join that has had none by then is not
// reported joined, it is put back with what was missing, and the same command can be run again.
func (x *Hot) waitV2(ctx context.Context, t *target, team, seatPart string) (string, bool) {
	start := x.Now()
	for {
		ended, err := x.ended(ctx, t)
		if err != nil {
			return fmt.Sprintf("seat %s: whether %s still runs could not be read: %v", seatPart, t.session, err), false
		}
		if ended {
			return fmt.Sprintf(endedDuringAdd, seatPart), true
		}
		held, why, err := t.v2.Applied(team, seatPart)
		if err != nil {
			why = err.Error()
		}
		if held {
			x.mark(markDone, fmt.Sprintf("session: seat %s held, its team's homeserver answered (%.1f s)", seatPart, x.elapsed(start)))
			return "", false
		}
		if x.Now().Sub(start) >= x.Waits.Apply {
			return fmt.Sprintf("seat %s not joined after %s: %s", seatPart, seconds(x.Waits.Apply), why), false
		}
		x.Sleep(x.Waits.Poll)
	}
}

// putBackV2 undoes an add whose seat was not held: the record, the generation without the seat,
// then the seat released (as a leave, which waits for the keeper to let it go). Each is tried
// whether or not the one before it could be.
func (x *Hot) putBackV2(ctx context.Context, t *target, before []state.DesiredSeat, team string, ended bool) {
	errs := []error{x.writeRecord(t, v1Seats(before), "restored")}
	if err := t.v2.Put(before); err != nil {
		x.step(markFailed, stepMembership, err.Error())
		errs = append(errs, err)
	} else {
		x.step(markDone, stepMembership, fmt.Sprintf(withNote, fmt.Sprintf(generationWritten, t.v2.Desired().Generation), "restored"))
	}
	released, err := t.v2.Release(ctx, team)
	switch {
	case err != nil:
		x.mark(markFailed, "seat: "+err.Error())
		errs = append(errs, err)
	case !released:
		x.mark(markSkip, "seat: "+notHeld(team, t.session))
	}
	if errors.Join(errs...) != nil {
		next := "run the same command again, or remove the team from the session"
		if ended {
			next = "the session's next launch replays its record, and the seat sweep releases the seat"
		}
		x.say("The record, the membership or the seat could not be put back (above): " + next + ".")
	}
}

func (x *Hot) restoreRecordV2(t *target, before []state.DesiredSeat) {
	if x.writeRecord(t, v1Seats(before), "restored") != nil {
		x.say("The record could not be restored (above): run the same command again, or remove the team from the session.")
	}
}

// removeV2 is H23 for a v2 session: the record, the next generation without the seat and with a
// released farewell (the member posts its own goodbye and stays in the team, A 7.4), then the
// seat released once the keeper lets it go.
func (x *Hot) removeV2(ctx context.Context, t *target, r request) term.ExitCode {
	before := t.v2.Desired().Seats
	i := slices.IndexFunc(before, func(s state.DesiredSeat) bool { return s.Team.String() == r.team })
	if i < 0 {
		x.mark(markSkip, "team "+r.team+": "+t.session+" is not in it")
		x.say("Nothing to do: " + t.session + " is not in " + r.team + ".")
		return term.ExitOK
	}
	seatPart := before[i].Seat
	question := fmt.Sprintf("Remove %s from %s (seat %s)? Its member says goodbye and stays in the team.", t.session, r.team, seatPart)
	if code, proceed := x.confirm(ctx, question, r.yes); !proceed {
		return code
	}
	if err := x.writeRecord(t, v1Seats(slices.Delete(slices.Clone(before), i, i+1)), ""); err != nil {
		return term.ExitFailure
	}
	if _, err := t.v2.Leave(r.team); err != nil {
		x.step(markFailed, stepMembership, err.Error())
		x.restoreRecordV2(t, before)
		return term.ExitFailure
	}
	x.step(markDone, stepMembership, fmt.Sprintf(withNote, fmt.Sprintf(generationWritten, t.v2.Desired().Generation), "farewell "+r.team+"/"+seatPart))
	released, err := t.v2.Release(ctx, r.team)
	if err != nil {
		x.mark(markFailed, "session: seat "+seatPart+" not released: "+err.Error()+
			"; the seat sweep releases it once the session's keeper lets it go")
		return seatCode(err)
	}
	if !released {
		x.mark(markSkip, "session: seat "+seatPart+": "+notHeld(r.team, t.session))
		x.say("Removed. " + t.session + " is no longer in " + r.team + ".")
		return term.ExitOK
	}
	x.step(markDone, stepSession, "seat "+seatPart+" released")
	x.say("Removed. " + t.session + " is no longer in " + r.team + "; seat " + seatPart + " is free.")
	return term.ExitOK
}

// notHeld says a release found no seat of team held for the session in the seat store.
func notHeld(team, session string) string {
	return "the seat store holds no seat of " + team + " for " + session + ", so none was released"
}

// seatCode is the exit of a seat store refusal: 75 for a seat held or the store busy, 1 otherwise.
func seatCode(err error) term.ExitCode {
	if errors.Is(err, seat.ErrHeld) || errors.Is(err, seat.ErrBusy) {
		return exitBusy
	}
	return term.ExitFailure
}

// v1Seats is seats as the session record's --teams names them.
func v1Seats(seats []state.DesiredSeat) []membership.Seat {
	out := make([]membership.Seat, 0, len(seats))
	for _, s := range seats {
		out = append(out, membership.Seat{Team: s.Team.String(), Seat: s.Seat})
	}
	return out
}
