package system

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatchoice"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// ExitUndecided is a team whose system cannot be decided yet.
const ExitUndecided = term.ExitConfig

// Cmd is `ccy-teams system <team>...`: one line per team on stdout, `<given> <id> <system>`, or
// the refusal on stderr and exit 64 (78 for a team that cannot be decided yet). The checkout is
// the working directory with its links resolved, the key the launch's seat take uses.
type Cmd struct {
	Getenv   func(string) string
	Getwd    func() (string, error)
	Stdout   io.Writer
	Stderr   io.Writer
	Declared func(context.Context) ([]string, error)
	Status   func(paths.TeamID) (status.Record, bool, error)
	// Sessions are the session records a seat's holder is judged by (PoolState).
	Sessions seat.Sessions
	// Life is nil for a team whose pool seats can be taken here, else why not (Life.Check,
	// register R59): PoolState refuses such a team and PoolTeams leaves it out.
	Life func(paths.TeamID) error
	// Reach asks a pool seat's homeserver whether it answers (Answers, issue 430), on Timers.
	Reach  seat.Reacher
	Timers matrix.Timers
}

// Here is the seat store and the checkout key of the working directory, from HOME and
// XDG_STATE_HOME.
func Here(getenv func(string) string, getwd func() (string, error)) (paths.Store, paths.CheckoutKey, string, error) {
	state, err := paths.StateHome(getenv("HOME"), getenv("XDG_STATE_HOME"))
	if err != nil {
		return paths.Store{}, paths.CheckoutKey{}, "", fmt.Errorf("the state home: %w", err)
	}
	store, err := paths.SeatStore(state)
	if err != nil {
		return paths.Store{}, paths.CheckoutKey{}, "", fmt.Errorf("the seat store: %w", err)
	}
	wd, err := getwd()
	if err != nil {
		return paths.Store{}, paths.CheckoutKey{}, "", fmt.Errorf("the working directory: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(wd)
	if err != nil {
		return paths.Store{}, paths.CheckoutKey{}, "", fmt.Errorf("the working directory: %w", err)
	}
	key, err := paths.CheckoutKeyOf(resolved)
	if err != nil {
		return paths.Store{}, paths.CheckoutKey{}, "", fmt.Errorf("the checkout key of %s: %w", resolved, err)
	}
	return store, key, state, nil
}

// Lines resolves teams for the working directory's checkout.
func (c *Cmd) Lines(ctx context.Context, teams []string) ([]Line, error) {
	store, key, _, err := Here(c.Getenv, c.Getwd)
	if err != nil {
		return nil, err
	}
	return Resolve(ctx, teams, Sources{Status: c.Status, PoolHolds: PoolHolds(store, key), Declared: c.Declared})
}

// Live is whether team is live on teamchat for the working directory's checkout (choose-teams).
func (c *Cmd) Live(ctx context.Context, team string) (bool, error) {
	lines, err := c.Lines(ctx, []string{team})
	if err != nil {
		return false, err
	}
	return len(lines) == 1 && lines[0].System == Teamchat, nil
}

// SeatCount is how many pool seats of kind of team the working directory's checkout has, for
// the chooser's list; which seat to take is PoolState's, which says who holds each (issue 423).
func (c *Cmd) SeatCount(team, kind string) (int, error) {
	handles, err := c.Handles(team, kind)
	return len(handles), err
}

// Handles are the member handles of the working directory's pool seats of kind of team, in pool
// order: what a host launch's Matrix consent names (Task 5.1). Only kind's: a seat of another
// kind is another launcher's (register R55). seatchoice.AnyKind is every seat, for a refusal
// that names a seat's kind.
func (c *Cmd) Handles(team, kind string) ([]string, error) {
	id, err := paths.ParseTeamID(team)
	if err != nil {
		return nil, fmt.Errorf("team %s: %w", team, err)
	}
	store, key, _, err := Here(c.Getenv, c.Getwd)
	if err != nil {
		return nil, err
	}
	seats, err := PoolSeats(store, key, id)
	if err != nil {
		return nil, err
	}
	var handles []string
	for _, s := range seats {
		if seatchoice.IsKind(s.String(), kind) {
			handles = append(handles, s.String())
		}
	}
	return handles, nil
}

// PoolState is the working directory's pool seats of kind of team as a launch of kind with no
// seat named offers them: each with the session a take would be refused for (issue 423). With
// no session records wired it refuses, never calling a held seat free.
func (c *Cmd) PoolState(team, kind string) ([]seatchoice.Seat, error) {
	if c.Sessions == nil {
		return nil, errors.New("the seats' holders cannot be judged: no session records are wired")
	}
	if c.Life == nil {
		return nil, errNoLife
	}
	id, err := paths.ParseTeamID(team)
	if err != nil {
		return nil, fmt.Errorf("team %s: %w", team, err)
	}
	if err = c.Life(id); err != nil {
		return nil, err
	}
	store, key, _, err := Here(c.Getenv, c.Getwd)
	if err != nil {
		return nil, err
	}
	return PoolState(store, key, id, c.Sessions, kind)
}

// errNoLife is a Cmd asked for seats to offer with no team lifecycle wired: a removed team's
// seats would read as free (register R59).
var errNoLife = errors.New("whether a team is still installed cannot be judged: no lifecycle is wired")

// PoolState is checkout's pool seats of kind of team in store (the only seats a launch of kind is
// offered, register R55), each with the session a take would be refused for ("" free). It is the take's own test of a holding (seat.Store's clearHeld), read
// without opening the store for allocation: a complete holding whose session's keeper runs now
// (seat.KeeperOf) or whose record is kept (sessions) is held; one whose session is over, or an
// incomplete one, the take releases or quarantines, so it is free. A record or lock that cannot
// be read is an error, never a free seat.
func PoolState(store paths.Store, checkout paths.CheckoutKey, team paths.TeamID, sessions seat.Sessions, kind string) ([]seatchoice.Seat, error) {
	names, err := PoolSeats(store, checkout, team)
	if err != nil {
		return nil, err
	}
	holdings, err := seat.ReadHoldings(store, checkout)
	if err != nil {
		return nil, fmt.Errorf("the holdings of %s: %w", team, err)
	}
	out := make([]seatchoice.Seat, 0, len(names))
	for _, n := range names {
		if seatchoice.KindOf(n.String()) != kind {
			continue
		}
		s := seatchoice.Seat{Name: n.String(), Handle: n.String()}
		if h, ok := proto.ParseHandle(n.String()); ok {
			s.Name = h.Seat
		}
		for _, h := range holdings {
			if !h.Complete || h.Seat.Team != team || h.Seat.Name != n {
				continue
			}
			held, err := sessionHolds(store, sessions, h.Record.Session, team)
			if err != nil {
				return nil, fmt.Errorf("is seat %s of %s held: %w", n, team, err)
			}
			if held {
				s.HeldBy = h.Record.Session.String()
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// sessionHolds is whether session holds its seat of team: its keeper runs now, or its record is
// kept (the seat store's own test).
func sessionHolds(store paths.Store, sessions seat.Sessions, session paths.SessionName, team paths.TeamID) (bool, error) {
	keeper, err := seat.KeeperOf(store, session, team)
	if err != nil || keeper == seat.KeeperRunning {
		return keeper == seat.KeeperRunning, err
	}
	return sessions.Restorable(session)
}

// PoolTeams are the teams with a seat in the working directory's pool, sorted, less those whose
// seats cannot be taken here (Life: a removed team is never offered, register R59).
func (c *Cmd) PoolTeams() ([]string, error) {
	if c.Life == nil {
		return nil, errNoLife
	}
	store, key, _, err := Here(c.Getenv, c.Getwd)
	if err != nil {
		return nil, err
	}
	teams, err := PoolTeams(store, key)
	if err != nil {
		return nil, err
	}
	live := make([]string, 0, len(teams))
	for _, team := range teams {
		id, err := paths.ParseTeamID(team)
		if err != nil {
			return nil, err
		}
		err = c.Life(id)
		switch {
		case errors.Is(err, ErrTeamGone):
			continue
		case err != nil:
			return nil, err
		}
		live = append(live, team)
	}
	return live, nil
}

// Run prints the lines, or refuses.
func (c *Cmd) Run(ctx context.Context, teams []string) term.ExitCode {
	lines, err := c.Lines(ctx, teams)
	if err != nil {
		return c.Fail(err)
	}
	for _, l := range lines {
		if _, err := fmt.Fprintf(c.Stdout, "%s %s %s\n", l.Given, l.ID, l.System); err != nil {
			return c.Fail(err)
		}
	}
	return term.ExitOK
}

// Fail reports err on stderr and is its exit code: 64 refused, 78 undecided, 1 otherwise.
func (c *Cmd) Fail(err error) term.ExitCode {
	code := term.ExitFailure
	switch {
	case errors.Is(err, ErrRefused):
		code = term.ExitUsage
	case errors.Is(err, ErrUndecided):
		code = ExitUndecided
	}
	if _, werr := fmt.Fprintf(c.Stderr, "ccy-teams system: %v.\n", err); werr != nil {
		return term.ExitFailure
	}
	return code
}
