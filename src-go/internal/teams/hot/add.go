package hot

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// choice is the seat an add takes.
type choice struct {
	seat, handle string
	exists       bool
}

// add is H22: the steps in order, each said.
func (x *Hot) add(ctx context.Context, r request) term.ExitCode {
	t, code, ok := x.resolve(ctx, r.session)
	if !ok {
		return code
	}
	for _, s := range t.desired.Seats {
		if s.Team == r.team {
			return x.alreadyIn(ctx, t, s, r.seat)
		}
	}
	if teamCode, up := x.teamUp(ctx, r.team); !up {
		return teamCode
	}
	c, seatCode, picked := x.pickSeat(ctx, t, r)
	if !picked {
		return seatCode
	}
	question := fmt.Sprintf("Add %s to %s as seat %s", r.session, r.team, c.seat)
	if c.handle != "" {
		question += " (" + c.handle + ")"
	}
	if askCode, proceed := x.confirm(ctx, question+"?", r.yes); !proceed {
		return askCode
	}
	if madeCode, made := x.makeSeat(ctx, t, r.team, &c); !made {
		return madeCode
	}
	if reachCode, reached := x.reach(ctx, t, r.team); !reached {
		return reachCode
	}
	return x.apply(ctx, t, r.team, c)
}

// teamUp is H22 step 2 (H26): the team's homeserver is up.
func (x *Hot) teamUp(ctx context.Context, team string) (term.ExitCode, bool) {
	state, err := x.Host.Systemd.HomeserverState(ctx, team)
	if err != nil {
		x.mark(markFailed, "team "+team+": systemctl could not say whether its homeserver is up: "+err.Error())
		return term.ExitFailure, false
	}
	if state != systemd.HomeserverUp {
		x.mark(markFailed, "team "+team+": homeserver for "+team+" is down: systemctl status agent-bus-hs@"+team)
		return term.ExitFailure, false
	}
	x.mark(markDone, "team "+team+": homeserver up")
	return term.ExitOK, true
}

// pickSeat is H22 step 3: the seat given, or the lowest free one, refused when its lock is held
// or another live session's desired membership names it (H21).
func (x *Hot) pickSeat(ctx context.Context, t *target, r request) (choice, term.ExitCode, bool) {
	seats, _, err := x.Host.Bus.SeatList(ctx, t.checkout())
	if err != nil {
		x.mark(markFailed, "seat: agent-bus seat list in "+t.checkout()+": "+err.Error())
		return choice{}, term.ExitFailure, false
	}
	wanted, err := x.wantedElsewhere(ctx, t)
	if err != nil {
		x.mark(markFailed, "seat: "+err.Error())
		return choice{}, term.ExitFailure, false
	}
	taken := func(seat string) string {
		for _, s := range seats {
			if s.Team == r.team && s.Seat == seat && s.Held {
				return "held by another session"
			}
		}
		if others := wanted.DesiredBy(discover.Key{Dir: t.checkout(), Team: r.team, Seat: seat}); len(others) > 0 {
			return "named by session " + others[0] + ", which has not taken it yet"
		}
		return ""
	}
	c, why, err := x.chooseSeat(ctx, t, r, seats, taken)
	if err != nil {
		x.mark(markFailed, "seat: "+err.Error())
		return choice{}, term.ExitFailure, false
	}
	if why != "" {
		x.mark(markFailed, "seat "+c.seat+": "+why)
		return choice{}, exitBusy, false
	}
	x.mark(markDone, "seat "+c.seat+": free, not named by any session in "+term.AbbrevHome(t.checkout(), x.Host.Home))
	return c, term.ExitOK, true
}

// chooseSeat is the seat given, or the first free or new seat agent-bus offers (its default
// first); why says why it cannot be had.
func (x *Hot) chooseSeat(ctx context.Context, t *target, r request, seats []agentbus.Seat,
	taken func(string) string,
) (choice, string, error) {
	if r.seat != "" {
		c := choice{seat: r.seat}
		for _, s := range seats {
			if s.Team == r.team && s.Seat == r.seat {
				c.exists, c.handle = true, s.Handle
			}
		}
		return c, taken(r.seat), nil
	}
	offered, err := x.Host.Bus.SeatChooseIn(ctx, t.checkout(), r.team, memberType)
	if err != nil {
		return choice{}, "", fmt.Errorf("agent-bus seat choose %s: %w", r.team, err)
	}
	rows := slices.Clone(offered.Rows)
	slices.SortStableFunc(rows, func(a, b agentbus.Choice) int {
		return rank(a.Seat != offered.Default) - rank(b.Seat != offered.Default)
	})
	for _, row := range rows {
		if row.State != "held" && taken(row.Seat) == "" {
			return choice{seat: row.Seat, handle: row.Handle, exists: row.State != "new"}, "", nil
		}
	}
	return choice{seat: offered.Default}, "every seat of " + r.team + " in this checkout is held or named by another session", nil
}

func rank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// wantedElsewhere is every other live session's desired membership (discover).
func (x *Hot) wantedElsewhere(ctx context.Context, t *target) (*discover.Index, error) {
	desired, _, err := discover.ReadDesired(x.Host.Getenv("XDG_RUNTIME_DIR"))
	if err != nil {
		return nil, err
	}
	live, err := x.Host.Tmux.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("tmux could not list the sessions: %w", err)
	}
	ix := discover.New()
	for _, d := range desired {
		if d.Session != t.session && slices.ContainsFunc(live, func(s tmuxccy.Session) bool { return s.Name == d.Session }) {
			ix.AddDesired(d)
		}
	}
	return ix, nil
}

// makeSeat is H22 step 4: create the seat when it is new.
func (x *Hot) makeSeat(ctx context.Context, t *target, team string, c *choice) (term.ExitCode, bool) {
	if c.exists {
		x.mark(markSkip, "seat "+c.seat+": exists (no sudo needed)")
		return term.ExitOK, true
	}
	noPrompt := !x.Host.IsTerminal(x.Host.Stdin)
	if err := x.Host.Bus.SeatTake(ctx, t.checkout(), c.seat+"@"+team, memberType, noPrompt); err != nil {
		x.mark(markFailed, "seat "+c.seat+": agent-bus seat take: "+lastLine(err))
		if errors.Is(err, agentbus.ErrBusy) {
			return exitBusy, false
		}
		return term.ExitFailure, false
	}
	held, handle, err := x.seatHeld(ctx, t, team, c.seat)
	if err != nil || handle == "" || held {
		x.mark(markFailed, "seat "+c.seat+": agent-bus seat take ran, but agent-bus seat list does not show the seat free")
		return term.ExitFailure, false
	}
	c.handle = handle
	x.mark(markDone, "seat "+c.seat+": created ("+handle+")")
	return term.ExitOK, true
}

// reach is H22 step 5 (H30): the route from inside the container.
func (x *Hot) reach(ctx context.Context, t *target, team string) (term.ExitCode, bool) {
	if err := sessprobe.ExecIn(ctx, x.Host.Runner, t.container, "pingbus", "reach", "--team", team); err != nil {
		x.mark(markFailed, "route: "+lastLine(err))
		return term.ExitFailure, false
	}
	x.mark(markDone, "route: the container reaches "+team+"'s homeserver")
	return term.ExitOK, true
}

// apply is H22 steps 7 to 9: write, wait, restore on failure, and relaunch a session that
// started on no team.
func (x *Hot) apply(ctx context.Context, t *target, team string, c choice) term.ExitCode {
	before, farewell := slices.Clone(t.desired.Seats), slices.Clone(t.desired.Farewell)
	seats := append(slices.Clone(before), membership.Seat{Team: team, Seat: c.seat})
	if err := x.commit(t, seats, farewell, ""); err != nil {
		x.restore(t, before, farewell)
		return term.ExitFailure
	}
	rep, why := x.waitHeld(ctx, t, team, c.seat)
	if why != "" {
		x.mark(markFailed, "session: "+why)
		x.restore(t, before, farewell)
		return term.ExitFailure
	}
	inbound := len(before) > 0
	if rep.Inbound != nil {
		inbound = *rep.Inbound
	}
	if inbound {
		x.say("Done. The session is told it joined " + team + " as " + c.handle + ". Nothing restarted.")
		return term.ExitOK
	}
	if code := x.relaunch(ctx, t, team, c.seat); code != term.ExitOK {
		return code
	}
	x.say("Done. The session is told it joined " + team + " as " + c.handle + ".")
	return term.ExitOK
}

// restore puts the record and the desired file back as they were (a failure at step 8).
func (x *Hot) restore(t *target, seats []membership.Seat, farewell []membership.Farewell) {
	if err := x.commit(t, seats, farewell, "restored"); err != nil {
		x.say("The record or the membership could not be restored (above): run the same command again, or remove the team from the session.")
	}
}

// waitHeld waits for the seat lock to read held and the report to show this generation with
// the seat held; why is the failure, in words.
func (x *Hot) waitHeld(ctx context.Context, t *target, team, seat string) (membership.Report, string) {
	start := x.Now()
	for {
		id, err := x.container(ctx, t.session)
		if err == nil && id != t.container {
			return membership.Report{}, "session ended during the add; seat " + seat + " left free"
		}
		held, _, heldErr := x.seatHeld(ctx, t, team, seat)
		rep, repErr := membership.ReadReport(t.checkout(), t.session)
		if heldErr == nil && repErr == nil && held && rep.Generation == t.desired.Generation && rep.Holds(team, seat) {
			x.mark(markDone, fmt.Sprintf("session: seat %s held, applied (%.1f s)", seat, x.elapsed(start)))
			return rep, ""
		}
		if x.Now().Sub(start) >= x.Waits.Apply {
			return rep, fmt.Sprintf("seat %s not held after %s: %s", seat, seconds(x.Waits.Apply), whyNot(rep, repErr, team, seat, t.desired.Generation))
		}
		x.Sleep(x.Waits.Poll)
	}
}

func whyNot(rep membership.Report, repErr error, team, seat string, generation int) string {
	if repErr != nil {
		return repErr.Error()
	}
	if why := rep.Why(team, seat); why != "" {
		return why
	}
	return "the seat keeper did not apply generation " + strconv.Itoa(generation)
}

// relaunch is H22's first join of a session launched on no team: it has no inbox socket, so it
// is asked to relaunch once, through the supervisor's restart, which replays the record.
func (x *Hot) relaunch(ctx context.Context, t *target, team, seat string) term.ExitCode {
	x.say("  " + t.session + " started on no team, so it relaunches once, at its next idle moment, to accept team messages (up to " +
		seconds(x.Waits.Relaunch) + ").")
	if err := membership.WriteRelaunch(t.dir, t.desired.Generation, x.Now()); err != nil {
		x.mark(markFailed, "session: the relaunch could not be asked for: "+err.Error())
		return term.ExitFailure
	}
	start := x.Now()
	for x.Now().Sub(start) < x.Waits.Relaunch {
		id, err := x.container(ctx, t.session)
		if err == nil && id != "" && id != t.container {
			rep, repErr := membership.ReadReport(t.checkout(), t.session)
			if repErr == nil && rep.Holds(team, seat) && rep.Inbound != nil && *rep.Inbound {
				x.mark(markDone, fmt.Sprintf("session: relaunched once to accept team messages (it started on no team): resumed, seat %s held (%.0f s)",
					seat, x.elapsed(start)))
				return term.ExitOK
			}
		}
		x.Sleep(x.Waits.Poll)
	}
	x.mark(markFailed, "session: not relaunched within "+seconds(x.Waits.Relaunch)+". It holds seat "+seat+
		" but cannot be woken by team messages yet. The request stays, so it relaunches at its next idle moment; "+
		"that needs the ccy supervisor. Without one, end it (/exit) and start it again from ccy-sessions, which takes seat "+seat+".")
	return term.ExitFailure
}

// alreadyIn is H19 for add: the team is already the session's.
func (x *Hot) alreadyIn(ctx context.Context, t *target, s membership.Seat, given string) term.ExitCode {
	if given != "" && given != s.Seat {
		return x.usageError(fmt.Sprintf("%s is already in %s as seat %s: one seat per team per session; remove it first.", t.session, s.Team, s.Seat))
	}
	pending, err := membership.RelaunchPending(t.dir)
	if err != nil {
		x.mark(markFailed, "session: "+err.Error())
		return term.ExitFailure
	}
	if pending {
		x.mark(markFailed, "session: in "+s.Team+" as seat "+s.Seat+", but not relaunched yet: team messages cannot wake it "+
			"until it relaunches at its next idle moment (the request stays); remove "+s.Team+" to withdraw it")
		return term.ExitFailure
	}
	held, _, err := x.seatHeld(ctx, t, s.Team, s.Seat)
	rep, repErr := membership.ReadReport(t.checkout(), t.session)
	if err != nil || repErr != nil || !held || !rep.Holds(s.Team, s.Seat) {
		if _, why := x.waitHeld(ctx, t, s.Team, s.Seat); why != "" {
			x.mark(markFailed, "session: "+why)
			return term.ExitFailure
		}
		x.say("Done. " + t.session + " is in " + s.Team + " as seat " + s.Seat + ".")
		return term.ExitOK
	}
	x.mark(markSkip, "team "+s.Team+": already joined")
	x.mark(markSkip, "seat "+s.Seat+": already this session's")
	x.mark(markSkip, "record: --teams "+membership.Canonical(t.desired.Seats))
	x.mark(markSkip, fmt.Sprintf("membership: generation %d already names seat %s", t.desired.Generation, s.Seat))
	x.mark(markSkip, "session: seat "+s.Seat+" held")
	x.say("Nothing to do: " + t.session + " is already in " + s.Team + " as seat " + s.Seat + ".")
	return term.ExitOK
}

func seconds(d time.Duration) string { return strconv.FormatFloat(d.Seconds(), 'f', -1, 64) + " s" }
