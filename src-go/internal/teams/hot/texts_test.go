package hot

import (
	"fmt"
	"testing"
)

// TestNamedTextsKeepTheirBytes pins each named text (texts.go), filled as its call site fills
// it, to the line the hot commands printed before it was named: naming a text for the plan
// checkers that read it (Plan 00201 Task 2.24) changes no byte a person sees.
func TestNamedTextsKeepTheirBytes(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{fmt.Sprintf(stepLine, stepRole, "x set to coordinator"), "role: x set to coordinator"},
		{fmt.Sprintf(stepLine, stepRecord, recordNoTeamsCC), "record: no --teams (cc keeps --bus)"},
		{fmt.Sprintf(stepLine, stepRecord, recordNoTeams), "record: no --teams"},
		{fmt.Sprintf(recordTeams, "1@a"), "--teams 1@a"},
		{fmt.Sprintf(stepLine, stepMembership, fmt.Sprintf(generationWritten, 2)), "membership: generation 2 written"},
		{fmt.Sprintf(withNote, "generation 3 written", farewellDone), "generation 3 written (farewell done)"},
		{
			fmt.Sprintf(stepLine, stepMembership, membershipRemoved),
			"membership: removed (the session is not running; its next launch writes it)",
		},
		{
			fmt.Sprintf(stepLine, stepSession, fmt.Sprintf(toldIs, roleWords("a", "coordinator"))),
			"session: told it is a's coordinator",
		},
		{roleWords("a", "worker"), "a worker in a"},
		{fmt.Sprintf(stepLine, fmt.Sprintf(stepSessionNamed, "s"), hotControlled), "session s: running, can be hot-controlled"},
		{
			fmt.Sprintf(stepLine, fmt.Sprintf(stepSessionNamed, "s"), fmt.Sprintf(sessionBusy, "ccy-teams session add s a")),
			"session s: another change to it is in progress: ccy-teams session add s a",
		},
		{
			fmt.Sprintf(notOnBusText, startedCC, againCC),
			"it was started without --teams or --bus, so it is not on the agent team bus (the membership under its name " +
				"is an earlier session's); start it again with cc --bus or cc --teams to change it while it runs",
		},
		{
			fmt.Sprintf(notOnBusText, startedCcy, againCcy),
			"it was started with --no-bus, so it is not on the agent team bus (the membership under its name " +
				"is an earlier session's); relaunch it from ccy's tmux without --no-bus to change it while it runs",
		},
		{
			fmt.Sprintf(stepLine, fmt.Sprintf(stepTeamNamed, "b"), fmt.Sprintf(teamDown, "b", "b")),
			"team b: homeserver for b is down: systemctl status agent-bus-hs@b",
		},
		{
			fmt.Sprintf(stepLine, stepKeeper, fmt.Sprintf(keeperRunningAgain, "1 seat", 0.25)),
			"keeper: running again, 1 seat held (0.2 s)",
		},
		{fmt.Sprintf(stateUnguarded, "s"), "unguarded: ccy-teams session repair s"},
		{stateOK, "ok"},
		{fmt.Sprintf(endedDuringAdd, "2"), "session ended during the add; seat 2 left free"},
		{fmt.Sprintf(stepLine, stepRoute, fmt.Sprintf(routeReaches, "b")), "route: this host reaches b's homeserver"},
		{fmt.Sprintf(movedText, "s", "b", "a"), "Moved: s is in b, and no longer in a."},
		{
			fmt.Sprintf(stepLine, stepSession, fmt.Sprintf(ccNoInboundText, "s", "a", "2", "2")),
			"session: s is in a as seat 2, but it started on no team (cc --bus), so team messages cannot reach it until cc " +
				"is started again, and cc has nothing that can restart it: end it (/exit) and start it again from " +
				"ccy-sessions, which starts it in seat 2",
		},
		{fmt.Sprintf(doneJoinedNothingRestarted, "a", "h"), "Done. The session is told it joined a as h. Nothing restarted."},
		{fmt.Sprintf(alreadyIn, "s", "a", "2"), "Nothing to do: s is already in a as seat 2."},
		{
			fmt.Sprintf(stepLine, stepKeeper, fmt.Sprintf(ccRepairText, "s")),
			"keeper: s is a cc session: its seat keeper ends with its claude, so it cannot be started again from " +
				"outside; end the session (/exit) and start it again from ccy-sessions, which takes its seats",
		},
		{fmt.Sprintf(namedByOther, "s"), "named by session s, which has not taken it yet"},
		{fmt.Sprintf(namedByRecord, "s"), "named by session s's record, which a restore starts again"},
		{
			fmt.Sprintf(relaunchesOnce, "s", "180 s"),
			"  s started on no team, so it relaunches once, at its next idle moment, to accept team messages (up to 180 s).",
		},
		{
			fmt.Sprintf(stepLine, stepSession, fmt.Sprintf(relaunchedOnce, "2", 3.0)),
			"session: relaunched once to accept team messages (it started on no team): resumed, seat 2 held (3 s)",
		},
		{
			fmt.Sprintf(stepLine, fmt.Sprintf(stepSeatNamed, "2"), fmt.Sprintf(seatFree, "/p")),
			"seat 2: free, not named by any session in /p",
		},
		{fmt.Sprintf(stepLine, stepRebaseline, rebaselined), "re-baseline: sync position cleared, listening status expired"},
		{fmt.Sprintf(stepLine, stepGoodbye, "h left the team: removed by the owner"), "goodbye: h left the team: removed by the owner"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
