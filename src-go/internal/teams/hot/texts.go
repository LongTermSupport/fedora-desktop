package hot

import "fmt"

// The texts of the step lines and messages the hot commands print that plan checkers judge
// (Plan 00203's M3, M4 and M6), named so a checker reads each from this file (Plan 00201's
// m7_check product-value and product-pattern) instead of holding a copy. Each is a format a
// call site fills with fmt.Sprintf; a step line is stepLine of a step name and its words.
const (
	stepLine         = "%s: %s"
	stepRole         = "role"
	stepRecord       = "record"
	stepMembership   = "membership"
	stepSession      = "session"
	stepKeeper       = "keeper"
	stepRoute        = "route"
	stepGoodbye      = "goodbye"
	stepRebaseline   = "re-baseline"
	stepSessionNamed = "session %s"
	stepTeamNamed    = "team %s"
	stepSeatNamed    = "seat %s"

	// withNote is a step's words with the note that says why (a rollback's "restored").
	withNote          = "%s (%s)"
	recordTeams       = "--teams %s"
	recordNoTeams     = "no --teams"
	recordNoTeamsCC   = "no --teams (cc keeps --bus)"
	generationWritten = "generation %d written"
	membershipRemoved = "removed (the session is not running; its next launch writes it)"
	farewellDone      = "farewell done"

	hotControlled = "running, can be hot-controlled"
	sessionBusy   = "another change to it is in progress: %s"
	notOnBusText  = "it was started %s, so it is not on the agent team bus (the membership under its name is an earlier session's); %s to change it while it runs"
	startedCcy    = "with --no-bus"
	againCcy      = "relaunch it from ccy's tmux without --no-bus"
	startedCC     = "without --teams or --bus"
	againCC       = "start it again with cc --bus or cc --teams"

	toldIs        = "told it is %s"
	coordinatorOf = "%s's coordinator"
	workerIn      = "a worker in %s"

	teamDown       = "homeserver for %s is down: systemctl status agent-bus-hs@%s"
	seatFree       = "free, not named by any session in %s"
	endedDuringAdd = "session ended during the add; seat %s left free"
	namedByOther   = "named by session %s, which has not taken it yet"
	namedByRecord  = "named by session %s's record, which a restore starts again"
	relaunchesOnce = "  %s started on no team, so it relaunches once, at its next idle moment, to accept team messages (up to %s)."
	relaunchedOnce = "relaunched once to accept team messages (it started on no team): resumed, seat %s held (%.0f s)"
	alreadyIn      = "Nothing to do: %s is already in %s as seat %s."

	doneJoinedNothingRestarted = "Done. The session is told it joined %s as %s. Nothing restarted."

	routeReaches    = "this host reaches %s's homeserver"
	ccNoInboundText = "%s is in %s as seat %s, but it started on no team (cc --bus), so team messages cannot reach it until cc is started again, and cc has nothing that can restart it: end it (/exit) and start it again from ccy-sessions, which starts it in seat %s"
	ccRepairText    = "%s is a cc session: its seat keeper ends with its claude, so it cannot be started again from outside; end the session (/exit) and start it again from ccy-sessions, which takes its seats"

	movedText = "Moved: %s is in %s, and no longer in %s."

	keeperRunningAgain = "running again, %s held (%.1f s)"
	stateUnguarded     = "unguarded: ccy-teams session repair %s"
	stateOK            = "ok"

	rebaselined = "sync position cleared, listening status expired"
)

// step prints a step line: the mark, the step's name and its words.
func (x *Hot) step(m, name, words string) { x.mark(m, fmt.Sprintf(stepLine, name, words)) }
