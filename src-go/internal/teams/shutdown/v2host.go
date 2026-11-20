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
// launcher's exit decides), and what the session's seat lock of the team says of its keeper
// (seat.KeeperOf): a live session on ccy's tmux that holds a seat of the team, its seat lock
// held, is closed as any host seat is, its lock asked again just before the close (recheck). A
// desired membership is never asked: it outlives its session, and tmux reuses names. A holder
// whose seat lock is free and whose name no live ccy session has has no session; any other
// holder whose lock is not held (no lock yet, as while it starts; a lock the probe refuses; a
// free lock with a live session of its name) cannot be told apart from the team's own, so it is
// out of reach, as is a seat held by a live session not on ccy's tmux or whose holding record
// cannot be read. A free seat of this host's pool has no session; a member whose seat is not in
// the pool runs elsewhere. A pool entry that is not a checkout's pool is named and the other
// checkouts are read, as the seat sweep does; holdings or seat locks that cannot be read decide
// nothing: each member left is named once as not reachable.
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
	keys, bad, err := checkouts(store)
	if err != nil {
		p.notReached(whyUnreachable+"the seat store's pool: "+err.Error(), rows...)
		return
	}
	p.unreached = append(p.unreached, bad...)
	holdings, err := teamHoldings(store, keys, id)
	if err != nil {
		p.notReached(whyUnreachable+"the seat store's holdings: "+err.Error(), rows...)
		return
	}
	live, notLive, err := s.byKeeper(store, holdings)
	if err != nil {
		p.notReached(whyUnreachable+"the seat store's seat locks: "+err.Error(), rows...)
		return
	}
	closed := p.closeHolders(snap, store, id, live)
	for _, row := range rows {
		if closed[row.Handle] {
			continue
		}
		if p.heldNotClosed(snap, row.Handle, holdings, notLive) {
			continue
		}
		pooled, err := inPool(store, keys, id, row.Handle)
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

// keptBy is a complete holding and what its session's seat lock of the team says.
type keptBy struct {
	h      seat.Holding
	keeper seat.Keeper
}

// byKeeper splits the complete holdings by what the seat lock of the session each names says
// (Shutdown.Keeper): live (held), or not live with what it says.
func (s *Shutdown) byKeeper(store paths.Store, holdings []seat.Holding) (live []seat.Holding, notLive []keptBy, err error) {
	for _, h := range holdings {
		if !h.Complete {
			continue
		}
		k, err := s.Keeper(store, h.Record.Session, h.Seat.Team)
		if err != nil {
			return nil, nil, fmt.Errorf("session %s: %w", h.Record.Session, err)
		}
		if k == seat.KeeperRunning {
			live = append(live, h)
		} else {
			notLive = append(notLive, keptBy{h, k})
		}
	}
	return live, notLive, nil
}

// v2Holder is a v2 host job's session and the seat store and team its seat lock is in, which
// recheck asks again just before the close.
type v2Holder struct {
	store   paths.Store
	session paths.SessionName
	team    paths.TeamID
}

// recheck asks each v2 host job's seat lock again just before the close: the question may have
// waited a long time, and a holder that ended meanwhile leaves its name to tmux. A job whose
// lock is no longer held is not closed: it is out of reach, changed while confirming.
func (s *Shutdown) recheck(p *plan) {
	var kept []*job
	for _, j := range p.jobs {
		if j.holder == nil {
			kept = append(kept, j)
			continue
		}
		k, err := s.Keeper(j.holder.store, j.holder.session, j.holder.team)
		if err == nil && k == seat.KeeperRunning {
			kept = append(kept, j)
			continue
		}
		now := keeperNow(k)
		if err != nil {
			now = "it cannot be read now: " + err.Error()
		}
		p.unreached = append(p.unreached, unreached{
			j.handle, "held by session " + j.name + ", whose seat lock changed while confirming: " + now +
				"; it is not typed into",
		})
	}
	p.jobs = kept
}

// keeperNow is what a seat lock says now, in the words recheck uses.
func keeperNow(k seat.Keeper) string {
	switch k {
	case seat.KeeperEnded:
		return "it is free now"
	case seat.KeeperNoLock:
		return "it is not there now"
	case seat.KeeperRefused:
		return "it cannot be probed now"
	case seat.KeeperRunning:
	}
	return "it is held"
}

// heldNotClosed names a handle held but not closed, and is whether it was: a holder whose seat
// lock is free and whose name no live ccy session has has no session; any other holder not live
// (whyV2NotLive), a live holder not on ccy's tmux, or a holding record that cannot be read, is
// out of reach.
func (p *plan) heldNotClosed(snap *board.Snapshot, handle string, holdings []seat.Holding, notLive []keptBy) bool {
	of := func(h seat.Holding) bool { return h.Seat.Name.String() == handle }
	if i := slices.IndexFunc(notLive, func(k keptBy) bool { return of(k.h) }); i >= 0 {
		session := notLive[i].h.Record.Session.String()
		_, named := snap.Live[session]
		if notLive[i].keeper == seat.KeeperEnded && !named {
			p.idle = append(p.idle, unreached{handle, whyV2Ended(session)})
			return true
		}
		p.unreached = append(p.unreached, unreached{handle, "held by session " + session + ", " + whyV2NotLive(notLive[i].keeper)})
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
func (p *plan) closeHolders(snap *board.Snapshot, store paths.Store, team paths.TeamID, holdings []seat.Holding) map[string]bool {
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
			where:  "seat " + joined + ", its ccy session on this host",
			holder: &v2Holder{store: store, session: n, team: team},
		})
	}
	return closed
}

// teamHoldings is the holdings of the team's seats in the checkouts of this host's seat store.
func teamHoldings(store paths.Store, keys []paths.CheckoutKey, team paths.TeamID) ([]seat.Holding, error) {
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

// checkouts is the checkouts with a pool in the seat store, none when it has no pool, and each
// pool entry that is not a checkout's pool directory, named as not reachable. The pool is the
// owner's alone, so such an entry is never skipped silently; it is reported and the other
// checkouts are read, as the seat sweep judges it.
func checkouts(store paths.Store) ([]paths.CheckoutKey, []unreached, error) {
	entries, err := os.ReadDir(store.Pool())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var keys []paths.CheckoutKey
	var bad []unreached
	for _, e := range entries {
		key, err := paths.ParseCheckoutKey(e.Name())
		if err == nil && !e.IsDir() {
			err = errors.New("not a directory")
		}
		if err != nil {
			bad = append(bad, unreached{
				store.Pool() + "/" + e.Name(),
				"not a checkout's pool (" + err.Error() + "), so no seat in it is read; the seat sweep reports it too",
			})
			continue
		}
		keys = append(keys, key)
	}
	return keys, bad, nil
}

// inPool is whether a checkout of this host's seat store holds the handle's seat of the team.
func inPool(store paths.Store, keys []paths.CheckoutKey, team paths.TeamID, handle string) (bool, error) {
	for _, key := range keys {
		seats, err := system.PoolSeats(store, key, team)
		if err != nil {
			return false, err
		}
		if slices.ContainsFunc(seats, func(n paths.SeatName) bool { return n.String() == handle }) {
			return true, nil
		}
	}
	return false, nil
}
