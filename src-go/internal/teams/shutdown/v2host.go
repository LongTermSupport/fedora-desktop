package shutdown

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatsweep"
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

// resolveV2Host finds a v2 team's host seats (SPEC-team-close TC2): a live session on ccy's tmux
// whose desired membership, in the owner's seat store under the session's name (the file ccy
// mounts as TEAMCHAT_MEMBERSHIP), names a seat of the team is closed as any host seat is. A
// member no live session holds has no session when its seat is in this host's pool, and is out
// of reach when it is not (another machine). A membership that cannot be read leaves its session
// named as not reachable, never closed and never taken for "no session".
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
	held := map[string]bool{}
	for _, name := range slices.Sorted(maps.Keys(snap.Live)) {
		seats, err := desiredSeats(store, name, id)
		if err != nil {
			p.unreached = append(p.unreached, unreached{"session " + name, whyUnreachable + err.Error()})
			continue
		}
		if len(seats) == 0 {
			continue
		}
		var handles []string
		for _, seat := range seats {
			held[seat] = true
			handles = append(handles, handlesOf(rows, seat)...)
		}
		p.jobs = append(p.jobs, &job{
			name: name, seat: strings.Join(seats, ", "), handle: strings.Join(handles, ", "), attached: snap.Live[name],
			where: "seat " + strings.Join(handles, ", ") + ", its ccy session on this host",
		})
	}
	for _, row := range rows {
		h, ok := proto.ParseHandle(row.Handle)
		if ok && held[h.Seat] {
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

// desiredSeats is the seats of team that the live session's desired membership names; none for
// a session with no membership in the seat store (a v1 session, or no teamchat team).
func desiredSeats(store paths.Store, name string, team paths.TeamID) ([]string, error) {
	n, ok := sessionName(name)
	if !ok {
		return nil, nil
	}
	d, found, err := seatsweep.Desired{Store: store}.Read(n)
	if err != nil || !found {
		return nil, err
	}
	var seats []string
	for _, seat := range d.Seats {
		if seat.Team == team {
			seats = append(seats, seat.Seat)
		}
	}
	return seats, nil
}

// sessionName is the live session's name in the seat store, and false for a tmux session name
// the store's grammar refuses (no membership can be filed under it).
func sessionName(name string) (paths.SessionName, bool) {
	n, err := paths.ParseSessionName(name)
	return n, err == nil
}

// handlesOf is the handles of the rows whose seat part is seat, or the seat itself when no row
// names it.
func handlesOf(rows []board.Row, seat string) []string {
	var out []string
	for _, row := range rows {
		if h, ok := proto.ParseHandle(row.Handle); ok && h.Seat == seat {
			out = append(out, row.Handle)
		}
	}
	if len(out) == 0 {
		return []string{seat}
	}
	return out
}

// inPool is whether a checkout of this host's seat store holds the handle's seat of the team.
func inPool(store paths.Store, team paths.TeamID, handle string) (bool, error) {
	entries, err := os.ReadDir(store.Pool())
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		key, keyErr := paths.ParseCheckoutKey(e.Name())
		if keyErr != nil || !e.IsDir() {
			continue
		}
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
