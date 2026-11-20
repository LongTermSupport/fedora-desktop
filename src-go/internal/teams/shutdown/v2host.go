package shutdown

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
)

// v2Members is a v2 team's rows of members that may have a session: not the team row, not
// parked or retired.
func v2Members(snap *board.Snapshot, team string) []board.Row {
	var rows []board.Row
	for _, row := range snap.Rows {
		parked := strings.HasPrefix(row.State, "parked") || strings.HasPrefix(row.State, "retired")
		if row.Team == team && row.V2 && row.Kind != board.KindTeam && !parked {
			rows = append(rows, row)
		}
	}
	return rows
}

// resolveV2Host finds a v2 team's host seats (SPEC-team-close TC2, TC4, TC12) by the seat
// store's holdings, the one record of which session holds a seat (seat.HeldBy, as the
// launcher's exit decides), and the store's own live-keeper test (seat.KeeperLive): a live
// session on ccy's tmux that holds a seat of the team, its seat lock of the team held, is closed
// as any host seat is. A desired membership is never asked, and a holding whose keeper is not
// live is a crashed run's, which the store keeps for a restore: both outlive their session, and
// tmux reuses names. A seat held by a live session not on ccy's tmux, or whose holding record
// cannot be read, is out of reach; a crashed holder's seat and a free seat of this host's pool
// have no session; a member whose seat is not in the pool runs elsewhere. Holdings or seat locks
// that cannot be read decide nothing: each member left is named once as not reachable.
func (s *Shutdown) resolveV2Host(snap *board.Snapshot, team string, p *plan) {
	var rows []board.Row
	for _, row := range v2Members(snap, team) {
		if row.Kind != kindLxc {
			rows = append(rows, row)
		}
	}
	id, err := paths.ParseTeamID(team)
	if err != nil {
		p.notReached(err.Error(), rows...)
		return
	}
	store, err := s.SeatStore()
	if err != nil {
		p.notReached(whyUnreachable+"the seat store: "+err.Error(), rows...)
		return
	}
	holdings, err := teamHoldings(store, id)
	if err != nil {
		p.notReached(whyUnreachable+"the seat store's holdings: "+err.Error(), rows...)
		return
	}
	live, ended, err := s.byKeeper(store, holdings)
	if err != nil {
		p.notReached(whyUnreachable+"the seat store's seat locks: "+err.Error(), rows...)
		return
	}
	closed := p.closeHolders(snap, live)
	for _, row := range rows {
		if closed[row.Handle] {
			continue
		}
		if p.heldNotClosed(snap, row.Handle, holdings, ended) {
			continue
		}
		pooled, err := inPool(store, id, row.Handle)
		switch {
		case err != nil:
			p.unreached = append(p.unreached, unreached{row.Handle, whyUnreachable + "the seat store's pool: " + err.Error()})
		case pooled:
			p.idle = append(p.idle, unreached{row.Handle, whyNoSession + " on this host"})
		default:
			p.unreached = append(p.unreached, unreached{row.Handle, whyV2Elsewhere})
		}
	}
}

// byKeeper splits the complete holdings by whether the session each names has a live keeper
// (seat.KeeperLive): live, or ended (a crashed or rebooted run's, its seat lock free).
func (s *Shutdown) byKeeper(store paths.Store, holdings []seat.Holding) (live, ended []seat.Holding, err error) {
	for _, h := range holdings {
		if !h.Complete {
			continue
		}
		kept, err := s.Keeper(store, h.Record.Session, h.Seat.Team)
		if err != nil {
			return nil, nil, fmt.Errorf("session %s: %w", h.Record.Session, err)
		}
		if kept == seat.KeeperRunning {
			live = append(live, h)
		} else {
			ended = append(ended, h)
		}
	}
	return live, ended, nil
}

// heldNotClosed names a handle held but not closed, and is whether it was: a crashed holder's
// seat has no session; a live holder not on ccy's tmux, or a holding record that cannot be read,
// is out of reach.
func (p *plan) heldNotClosed(snap *board.Snapshot, handle string, holdings, ended []seat.Holding) bool {
	of := func(h seat.Holding) bool { return h.Seat.Name.String() == handle }
	if i := slices.IndexFunc(ended, of); i >= 0 {
		session := ended[i].Record.Session.String()
		_, reused := snap.Live[session]
		p.idle = append(p.idle, unreached{handle, whyV2Ended(session, reused)})
		return true
	}
	i := slices.IndexFunc(holdings, of)
	if i < 0 {
		return false
	}
	why := whyV2HeldUnread
	if holdings[i].Complete {
		why = "held by session " + holdings[i].Record.Session.String() + ", " + whyV2HeldOffTmux
	}
	p.unreached = append(p.unreached, unreached{handle, why})
	return true
}

// closeHolders makes a job of each live session on ccy's tmux that holds a seat of the holdings
// (seat.HeldBy), and gives the handles of the seats those jobs close.
func (p *plan) closeHolders(snap *board.Snapshot, holdings []seat.Holding) map[string]bool {
	closed := map[string]bool{}
	for _, name := range slices.Sorted(maps.Keys(snap.Live)) {
		n, err := paths.ParseSessionName(name)
		if err != nil {
			continue
		}
		var handles []string
		for _, h := range seat.HeldBy(holdings, n) {
			closed[h.Seat.Name.String()] = true
			handles = append(handles, h.Seat.Name.String())
		}
		if len(handles) == 0 {
			continue
		}
		joined := strings.Join(handles, ", ")
		p.jobs = append(p.jobs, &job{
			name: name, seat: joined, handle: joined, attached: snap.Live[name],
			where: "seat " + joined + ", its ccy session on this host",
		})
	}
	return closed
}

// teamHoldings is the holdings of the team's seats in every checkout of this host's seat store.
func teamHoldings(store paths.Store, team paths.TeamID) ([]seat.Holding, error) {
	keys, err := checkouts(store)
	if err != nil {
		return nil, err
	}
	var out []seat.Holding
	for _, key := range keys {
		holdings, err := seat.ReadHoldings(store, key)
		if err != nil {
			return nil, err
		}
		for _, h := range holdings {
			if h.Seat.Team == team {
				out = append(out, h)
			}
		}
	}
	return out, nil
}

// checkouts is the checkouts with a pool in the seat store; none when it has no pool. The pool
// is the owner's alone, so an entry that is not a checkout's pool directory is an error naming
// it, as the seat sweep and the store's Holdings judge it, never skipped.
func checkouts(store paths.Store) ([]paths.CheckoutKey, error) {
	entries, err := os.ReadDir(store.Pool())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []paths.CheckoutKey
	for _, e := range entries {
		key, err := paths.ParseCheckoutKey(e.Name())
		if err == nil && !e.IsDir() {
			err = errors.New("not a directory")
		}
		if err != nil {
			return nil, fmt.Errorf("%s/%s: not a checkout's pool: %w", store.Pool(), e.Name(), err)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// inPool is whether a checkout of this host's seat store holds the handle's seat of the team.
func inPool(store paths.Store, team paths.TeamID, handle string) (bool, error) {
	keys, err := checkouts(store)
	if err != nil {
		return false, err
	}
	for _, key := range keys {
		seats, seatsErr := system.PoolSeats(store, key, team)
		if seatsErr != nil {
			return false, seatsErr
		}
		if slices.ContainsFunc(seats, func(n paths.SeatName) bool { return n.String() == handle }) {
			return true, nil
		}
	}
	return false, nil
}
