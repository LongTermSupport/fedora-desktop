package hot

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// rosterLimit is the most agents a team can have: its record's roles map (protocol §8, H27).
const rosterLimit = 64

// choice is the seat an add takes.
type choice struct {
	seat, handle string
	exists       bool
}

// addTo is H22: the steps in order, each said. given is the seat asked for, or "".
func (x *Hot) addTo(ctx context.Context, t *target, r request, team, given string) term.ExitCode {
	for _, s := range t.desired.Seats {
		if s.Team == team {
			return x.alreadyIn(ctx, t, s, given)
		}
	}
	roster, teamCode, up := x.teamUp(ctx, team)
	if !up {
		return teamCode
	}
	alloc, allocCode, held := x.allocate(t)
	if !held {
		return allocCode
	}
	defer x.letGo(alloc)
	c, seatCode, picked := x.pickSeat(ctx, t, team, given)
	if !picked {
		return seatCode
	}
	if !c.exists && len(roster.Members) >= rosterLimit {
		x.mark(markFailed, fmt.Sprintf("seat %s: %s has %d agents, the most a team can have: park or remove a member first (ccy-teams, Ctrl-X on a member)",
			c.seat, team, len(roster.Members)))
		return term.ExitFailure
	}
	question := fmt.Sprintf("Add %s to %s as seat %s", t.session, team, c.seat)
	if c.handle != "" {
		question += " (" + c.handle + ")"
	}
	if r.role != "" {
		question += ", " + r.role
	}
	if askCode, proceed := x.confirm(ctx, question+"?", r.yes); !proceed {
		return askCode
	}
	if madeCode, made := x.makeSeat(ctx, t, team, &c); !made {
		return madeCode
	}
	if reachCode, reached := x.reach(ctx, t, team); !reached {
		return reachCode
	}
	undo := noUndo
	if r.role != "" {
		var roleCode term.ExitCode
		var set bool
		if undo, roleCode, set = x.setRoleTo(ctx, team, c.handle, r.role, r.demoteCurrent); !set {
			return roleCode
		}
	}
	return x.apply(ctx, t, team, c, alloc, undo)
}

// teamUp is H22 step 2 (H26, H27): the team's homeserver is up, and its roster read.
func (x *Hot) teamUp(ctx context.Context, team string) (agentbus.Roster, term.ExitCode, bool) {
	state, err := x.Host.Systemd.HomeserverState(ctx, team)
	if err != nil {
		x.mark(markFailed, "team "+team+": systemctl could not say whether its homeserver is up: "+err.Error())
		return agentbus.Roster{}, term.ExitFailure, false
	}
	if state != systemd.HomeserverUp {
		x.mark(markFailed, "team "+team+": homeserver for "+team+" is down: systemctl status agent-bus-hs@"+team)
		return agentbus.Roster{}, term.ExitFailure, false
	}
	roster, err := x.Host.Bus.List(ctx, team)
	if err != nil {
		x.mark(markFailed, "team "+team+": its members could not be read: "+lastLine(err))
		return agentbus.Roster{}, term.ExitFailure, false
	}
	x.mark(markDone, fmt.Sprintf("team %s: homeserver up, %d of %d agents", team, len(roster.Members), rosterLimit))
	return roster, term.ExitOK, true
}

// allocate takes the checkout's seat allocation lock (H20), held until the desired file names
// the seat, so agent-bus seat take and another add see it.
func (x *Hot) allocate(t *target) (*lock, term.ExitCode, bool) {
	path, err := allocPath(t.runtime, t.checkout())
	if err != nil {
		x.mark(markFailed, "seat: "+err.Error())
		return nil, term.ExitFailure, false
	}
	l, err := takeLock(path, "ccy-teams session add "+t.session+" (pid "+strconv.Itoa(os.Getpid())+")")
	if busy, ok := errors.AsType[*busyError](err); ok {
		x.mark(markFailed, "seat: another seat allocation in "+t.checkout()+" is in progress: "+busy.holder)
		return nil, exitBusy, false
	}
	if err != nil {
		x.mark(markFailed, "seat: "+err.Error())
		return nil, term.ExitFailure, false
	}
	return l, term.ExitOK, true
}

// pickSeat is H22 step 3: the seat given, or the lowest free one, refused when its lock is held,
// another live session's desired membership names it, or a stopped session's record that a
// restore starts again names it (H21).
func (x *Hot) pickSeat(ctx context.Context, t *target, team, given string) (choice, term.ExitCode, bool) {
	seats, unreadable, err := x.Host.Bus.SeatList(ctx, t.checkout())
	if err != nil {
		x.mark(markFailed, "seat: agent-bus seat list in "+t.checkout()+": "+err.Error())
		return choice{}, term.ExitFailure, false
	}
	if unreadable {
		x.mark(markSkip, "seat: a seat of "+t.checkout()+" cannot be read (agent-bus seat list says which); it is not offered")
	}
	claims, err := x.claimsElsewhere(ctx, t)
	if err != nil {
		x.mark(markFailed, "seat: "+err.Error())
		return choice{}, term.ExitFailure, false
	}
	taken := func(seat string) string {
		for _, s := range seats {
			if s.Team == team && s.Seat == seat && s.Held {
				return "held by another session"
			}
		}
		k := discover.Key{Dir: t.checkout(), Team: team, Seat: seat}
		if others := claims.live.DesiredBy(k); len(others) > 0 {
			return "named by session " + others[0] + ", which has not taken it yet"
		}
		if owner := claims.restored.SessionOf(k); owner != "" {
			return "named by session " + owner + "'s record, which a restore starts again"
		}
		return ""
	}
	c, why, err := x.chooseSeat(ctx, t, team, given, seats, taken)
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
// first), or else the next number past them; why says why it cannot be had.
func (x *Hot) chooseSeat(ctx context.Context, t *target, team, given string, seats []agentbus.Seat,
	taken func(string) string,
) (choice, string, error) {
	if given != "" {
		c := choice{seat: given}
		for _, s := range seats {
			if s.Team == team && s.Seat == given {
				c.exists, c.handle = true, s.Handle
			}
		}
		return c, taken(given), nil
	}
	offered, err := x.Host.Bus.SeatChooseIn(ctx, t.checkout(), team, memberType)
	if err != nil {
		return choice{}, "", fmt.Errorf("agent-bus seat choose %s: %w", team, err)
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
	next := 1
	for _, row := range rows {
		if n, convErr := strconv.Atoi(row.Seat); convErr == nil && n >= next {
			next = n + 1
		}
	}
	if seat := strconv.Itoa(next); taken(seat) == "" {
		return choice{seat: seat}, "", nil
	}
	return choice{seat: offered.Default}, "every seat of " + team + " in this checkout is held or named by another session", nil
}

func rank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// claims is who else names a seat of this checkout: live sessions' desired memberships, and
// the records of stopped sessions that a restore starts again (Fable's M3 note N4).
type claims struct {
	live, restored *discover.Index
}

// claimsElsewhere reads every other session's claims (discover). A membership that cannot be
// read is said, and its seats are not counted.
func (x *Hot) claimsElsewhere(ctx context.Context, t *target) (claims, error) {
	c := claims{live: discover.New(), restored: discover.New()}
	desired, unreadable, err := discover.ReadDesired(t.runtime)
	if err != nil {
		return c, err
	}
	for _, why := range unreadable {
		x.mark(markSkip, "seat: "+why+"; its seats are not counted")
	}
	sessions, err := x.Host.Tmux.List(ctx)
	if err != nil {
		return c, fmt.Errorf("tmux could not list the sessions: %w", err)
	}
	live := func(name string) bool {
		return slices.ContainsFunc(sessions, func(s tmuxccy.Session) bool { return s.Name == name })
	}
	for _, d := range desired {
		if d.Session != t.session && live(d.Session) {
			c.live.AddDesired(d)
		}
	}
	listing, err := t.store.List()
	if err != nil {
		return c, fmt.Errorf("the session records: %w", err)
	}
	for _, rec := range listing.Records {
		if rec.Name != t.session && rec.Restore && rec.Dir == t.checkout() && !live(rec.Name) {
			c.restored.AddRecord(rec)
		}
	}
	return c, nil
}

// makeSeat is H22 step 4: create the seat when it is new.
func (x *Hot) makeSeat(ctx context.Context, t *target, team string, c *choice) (term.ExitCode, bool) {
	if c.exists {
		x.mark(markSkip, "seat "+c.seat+": exists (no sudo needed)")
		return term.ExitOK, true
	}
	noPrompt := !x.Host.IsTerminal(x.Host.Stdin)
	if err := x.Host.Bus.SeatTakeAllocated(ctx, t.checkout(), c.seat+"@"+team, memberType, noPrompt); err != nil {
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

// apply is H22 steps 7 to 9: write, let the allocation go, wait, restore on failure (the roles
// put back too), and relaunch a session that started on no team.
func (x *Hot) apply(ctx context.Context, t *target, team string, c choice, alloc *lock, undo func(context.Context)) term.ExitCode {
	before, farewell := slices.Clone(t.desired.Seats), slices.Clone(t.desired.Farewell)
	seats := append(slices.Clone(before), membership.Seat{Team: team, Seat: c.seat})
	if err := x.commit(t, seats, farewell, ""); err != nil {
		x.restore(t, before, farewell)
		undo(ctx)
		return term.ExitFailure
	}
	x.letGo(alloc)
	w := x.waitHeld(ctx, t, team, c.seat)
	if w.why != "" {
		x.mark(markFailed, "session: "+w.why)
		if w.ended {
			x.restoreEnded(t, before)
		} else {
			x.restore(t, before, farewell)
		}
		undo(ctx)
		return term.ExitFailure
	}
	inbound := len(before) > 0
	if w.rep.Inbound != nil {
		inbound = *w.rep.Inbound
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

// restoreEnded is restore for a session that ended during the change: the record goes back,
// and no desired file is left for a session that is not running (H28).
func (x *Hot) restoreEnded(t *target, seats []membership.Seat) {
	if x.writeRecord(t, seats, "restored") != nil || x.dropDesired(t) != nil {
		x.say("The record or the membership could not be restored (above): the session's next launch replays its record.")
	}
}

// waited is how a wait for a seat ended: the report, the failure in words ("" when held), and
// whether the session ended meanwhile.
type waited struct {
	rep   membership.Report
	why   string
	ended bool
}

// waitHeld waits for the seat lock to read held and the report to show this generation with
// the seat held.
func (x *Hot) waitHeld(ctx context.Context, t *target, team, seat string) waited {
	start := x.Now()
	for {
		if x.ended(ctx, t) {
			return waited{why: "session ended during the add; seat " + seat + " left free", ended: true}
		}
		held, heldErr := x.isHeld(ctx, t, team, seat)
		rep, repErr := membership.ReadReport(t.checkout(), t.session)
		if heldErr == nil && repErr == nil && held && rep.Generation == t.desired.Generation && rep.Holds(team, seat) {
			x.mark(markDone, fmt.Sprintf("session: seat %s held, applied (%.1f s)", seat, x.elapsed(start)))
			return waited{rep: rep}
		}
		if x.Now().Sub(start) >= x.Waits.Apply {
			return waited{rep: rep, why: fmt.Sprintf("seat %s not held after %s: %s", seat, seconds(x.Waits.Apply),
				whyNot(rep, repErr, team, seat, t.desired.Generation))}
		}
		x.Sleep(x.Waits.Poll)
	}
}

// isHeld is whether the checkout's seat lock reads held.
func (x *Hot) isHeld(ctx context.Context, t *target, team, seat string) (bool, error) {
	held, handle, err := x.seatHeld(ctx, t, team, seat)
	return held && handle != "", err
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

// relaunch is H22's first join of a session launched on no team: its inbox socket refuses every
// message (H34), so it is asked to relaunch once, through the supervisor's restart, which
// replays the record.
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
	held, err := x.isHeld(ctx, t, s.Team, s.Seat)
	rep, repErr := membership.ReadReport(t.checkout(), t.session)
	if err != nil || repErr != nil || !held || !rep.Holds(s.Team, s.Seat) {
		if w := x.waitHeld(ctx, t, s.Team, s.Seat); w.why != "" {
			x.mark(markFailed, "session: "+w.why)
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
