package board

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// The words of a v2 team's rows (SPEC-team-close TC15).
const (
	wordV2         = "teamchat (v2)"
	wordV2Shadowed = "also a teamchat (v2) team of this name, which ccy-teams cannot show beside it (ccy-teams system refuses the name)"
	wordV2Unlisted = "the v2 teams could not be listed"
	wordOnBus      = "on the bus at the last health check"
	wordOffBus     = "off the bus at the last health check"
)

// v2Root is the directory a v2 team's status directory is in, under the teamchat root.
func (c *Collector) v2Root() string { return c.TeamchatRoot + paths.StatusRoot + "/teams" }

// addV2Teams notes the v2 (teamchat) teams: every directory of the status root whose name is a
// team id. One whose name is a v1 team's is that team's note, not a second team. A status root
// that is not there holds none; one that cannot be read is said, and the board goes on with
// the v1 teams.
func (c *Collector) addV2Teams(s *Snapshot, seen map[string]bool) {
	entries, err := os.ReadDir(c.v2Root())
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		c.Term.PrintError(err.Error() + "; the v2 (teamchat) teams are left out.")
		s.Incomplete = wordV2Unlisted
		return
	}
	for _, e := range entries {
		if _, err := paths.ParseTeamID(e.Name()); err != nil || !e.IsDir() {
			continue
		}
		if seen[e.Name()] {
			s.V2Shadowed[e.Name()] = true
			continue
		}
		s.V2Teams = append(s.V2Teams, e.Name())
	}
}

// addV2Rows appends a v2 team's row and its members' rows, read from its status record (the
// read teamchat-admin status makes) and its LXC record: files the owner group reads, and one
// systemctl for the homeserver unit's state.
func (c *Collector) addV2Rows(ctx context.Context, s *Snapshot, team string) {
	id, err := paths.ParseTeamID(team)
	if err != nil {
		return
	}
	rec, found, readErr := status.Read(c.TeamchatRoot+paths.StatusFile(id), id)
	view := status.Shown(rec, found, readErr, c.Now())
	where := []string{wordV2}
	if readErr != nil {
		where = append(where, readErr.Error())
	}
	for _, p := range view.TeamProblems() {
		where = append(where, p.What+" ("+p.Fix+")")
	}
	lxc, _, err := lxcstate.ReadRecord(c.TeamchatRoot, id)
	if err != nil {
		where = append(where, err.Error())
	}
	if s.Incomplete != "" {
		where = append(where, s.Incomplete+" (above)")
	}
	state, err := c.Systemd.UnitState(ctx, paths.ServerUnit(id))
	if err != nil {
		c.Term.PrintError(err.Error())
		state = declare.WordUnknown
	}
	s.Rows = append(s.Rows, Row{
		Team: team, Member: wordV2, Kind: KindTeam, State: state, Applied: "-", Session: "-", Restore: "-",
		Where: strings.Join(where, ", "), Handle: "-", Dir: "-", V2: true,
	})
	if !found {
		return
	}
	for _, m := range view.Record.Members {
		s.Rows = append(s.Rows, v2MemberRow(team, m, lxc))
	}
}

// v2MemberRow is a v2 member's row: its handle, kind, role and state; an LXC member's also
// whether it is online, and its container from the LXC record.
func v2MemberRow(team string, m status.Member, lxc lxcstate.Record) Row {
	row := Row{
		Team: team, Member: m.Handle, Kind: Kind(m.Kind), State: m.State, Applied: "-", Session: "-", Restore: "-",
		Where: "-", Handle: m.Handle, Dir: "-", Role: m.Role, V2: true,
	}
	if m.Online == nil {
		return row
	}
	// online is the record's: whether the member was on the bus at health's last check, not a
	// live reading, so the words say so.
	online := *m.Online
	row.State += ", " + map[bool]string{true: wordOnBus, false: wordOffBus}[online]
	e, ok := lxc.ByHandle(m.Handle)
	switch {
	case !ok:
		row.Where = "no LXC record entry names it"
	case online:
		row.Where = "container " + e.Container.String() + ", its session " + wordOnBus
	case e.Why != "":
		row.Where = "container " + e.Container.String() + ", its session " + wordOffBus + " (" + e.Why + ")"
	default:
		row.Where = "container " + e.Container.String() + ", its session " + wordOffBus
	}
	return row
}

// IsV2 is whether team is a v2 (teamchat) team of the pass.
func (s *Snapshot) IsV2(team string) bool { return slices.Contains(s.V2Teams, team) }

// V2Refusal is what a v1 flow (restart, wind-down) says of a v2 team it is given, instead of
// "there is no team": what the team is, and what reaches it.
func V2Refusal(team string) string {
	return team + " is a v2 (teamchat) team, and this acts on agent-bus (v1) teams only; ccy-teams shutdown " +
		team + " closes its LXC seats' sessions."
}
