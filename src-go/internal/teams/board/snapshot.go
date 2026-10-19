package board

import (
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/lxcrecord"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
)

// stoppedAtApply is added to a joined LXC member's WHERE when its container was not running at
// the last apply, which skipped it (SPEC-lxc-stopped-members.md L10).
const stoppedAtApply = "container stopped at the last apply"

// heldBackWords is the STATE and WHERE of a listed member the LXC record holds back: a skipped
// one keeps its state with the stopped container said; a deferred one (leaving) is deferred.
func heldBackWords(row Row, item lxcrecord.Item) (string, string) {
	if item.Status == lxcrecord.StatusDeferred {
		return lxcrecord.StatusDeferred, item.Words()
	}
	if row.Where == "" {
		return row.State, stoppedAtApply
	}
	return row.State, row.Where + ", " + stoppedAtApply
}

// Words the table and the callers compare against.
const (
	WordNeedSudo = "need sudo"

	wordNotInstalled  = "not installed"
	WordNeverSignedIn = "never signed in"
)

// Snapshot is everything one pass over the machine found: the teams, their applied state,
// the sessions and seats, the members, and the table rows built from them. A pass reads each
// source once (systemd, tmux, the registry, the declaration).
type Snapshot struct {
	Teams  []string // declared present, then loaded, then stamped: first-seen order
	Port   map[string]string
	URL    map[string]string
	Server map[string]string
	// Description is what each declared team is for; empty for a team that has none.
	Description map[string]string
	// DescriptionState says a team's description cannot be shown (vaulted, invalid).
	DescriptionState map[string]declare.DescriptionState
	Loaded           map[string]bool   // the team has a homeserver unit
	Active           map[string]string // the unit's ACTIVE word from the one list-units pass
	Status           map[string]declare.Status

	Live        map[string]int    // tmux session name -> attached clients
	SeatSession map[string]string // "<dir>|<seat>@<team>" -> registry session name
	SeatRestore map[string]bool   // same key -> whether the session is started again after a reboot
	// Seats is the one lookup of which session names a seat (records and desired memberships);
	// SeatSession and SeatRestore are its answers keyed for the table.
	Seats     *discover.Index
	Checkouts []string

	Members        map[string]agentbus.Roster
	MembersUnknown map[string]string // team -> why its members are not shown
	SignedIn       map[string]string // "<team>|<person>" -> their Element sessions in words
	Sessions       map[string]int    // same key -> how many, for the people whose count could be read

	// NeedSudo is " <team>" for each team whose members wait on sudo (a team can repeat).
	NeedSudo string
	// Incomplete says a seat or a session record could not be read; every team row then shows it.
	Incomplete string

	Rows []Row
}

// NewSnapshot is a Snapshot with nothing read yet.
func NewSnapshot() *Snapshot {
	return &Snapshot{
		Port: map[string]string{}, URL: map[string]string{}, Server: map[string]string{}, Description: map[string]string{},
		DescriptionState: map[string]declare.DescriptionState{},
		Loaded:           map[string]bool{}, Active: map[string]string{}, Status: map[string]declare.Status{},
		Live: map[string]int{}, SeatSession: map[string]string{}, SeatRestore: map[string]bool{}, Seats: discover.New(),
		Members: map[string]agentbus.Roster{}, MembersUnknown: map[string]string{}, SignedIn: map[string]string{},
		Sessions: map[string]int{},
	}
}

// SessionsOf is how many Element sessions person has in team; known is false when the count
// could not be read, which is not the same as none.
func (s *Snapshot) SessionsOf(team, person string) (int, bool) {
	count, known := s.Sessions[team+"|"+person]
	return count, known
}

// RoleOf is the role of the member with handle in team, or "" when it is not a member or the
// members could not be read.
func (s *Snapshot) RoleOf(team, handle string) string {
	m, ok := s.Members[team].Member(handle)
	if !ok {
		return ""
	}
	return m.Role
}

// NoDescription is what a team row's MEMBER cell says for a team that has no description.
const NoDescription = "(no description)"

// StatusWord is the APPLIED column for team, in words.
func (s *Snapshot) StatusWord(team string) string {
	st, ok := s.Status[team]
	if !ok {
		return declare.WordUnknown
	}
	return st.Word()
}

// RowAt is the row a picked tag numbers.
func (s *Snapshot) RowAt(tag string) (Row, bool) {
	i, err := strconv.Atoi(tag)
	if err != nil || i < 0 || i >= len(s.Rows) {
		return Row{}, false
	}
	return s.Rows[i], true
}

// StateOf is the STATE of a team's own row.
func (s *Snapshot) StateOf(team string) string {
	for _, row := range s.Rows {
		if row.Team == team && row.Kind == KindTeam {
			return row.State
		}
	}
	return ""
}

// HoldsLiveSeat is whether a session still running holds a seat of team in the checkout at top.
func (s *Snapshot) HoldsLiveSeat(top, team string) bool {
	for key, session := range s.SeatSession {
		if !strings.HasPrefix(key, top+"|") || !strings.HasSuffix(key, "@"+team) {
			continue
		}
		if _, live := s.Live[session]; live {
			return true
		}
	}
	return false
}

// aboutTeam is the team row's MEMBER cell: what the team is for, whole.
func (s *Snapshot) aboutTeam(team string) string {
	if state := s.DescriptionState[team]; state != "" {
		return "(description " + string(state) + ")"
	}
	if about := s.Description[team]; about != "" {
		return about
	}
	return NoDescription
}

// addDeclared notes the teams declared present, in the order the declaration lists them.
func (s *Snapshot) addDeclared(seen map[string]bool, declared []declare.Team) {
	for _, t := range declared {
		if !t.Present {
			continue
		}
		s.addTeam(seen, t.Name)
		s.Port[t.Name] = "-"
		if t.Port != 0 {
			s.Port[t.Name] = strconv.Itoa(t.Port)
		}
		s.Server[t.Name] = t.ServerName
		s.Description[t.Name] = t.Description
		s.DescriptionState[t.Name] = t.DescriptionState
		if t.URL != "" {
			s.URL[t.Name] = t.URL
		}
	}
}

func (s *Snapshot) addTeam(seen map[string]bool, team string) {
	if !seen[team] {
		seen[team] = true
		s.Teams = append(s.Teams, team)
	}
}

func (s *Snapshot) setSessions(key string, count int) {
	s.Sessions[key] = count
	s.SignedIn[key] = signedInWord(count)
}

func (s *Snapshot) addCheckout(dir string) {
	if dir == "" || slices.Contains(s.Checkouts, dir) {
		return
	}
	s.Checkouts = append(s.Checkouts, dir)
}

// addRecord notes the checkout of a ccy or cc session record and the seats its launcher was
// given (discover.Index.AddRecord).
func (s *Snapshot) addRecord(rec registry.Record) {
	if !discover.IsLauncher(rec.Prefix) {
		return
	}
	s.addCheckout(rec.Dir)
	s.Seats.AddRecord(rec)
	s.fillSeats()
}

// addDesired notes a live session's desired membership: a seat a hot add gave it that its record
// does not name yet.
func (s *Snapshot) addDesired(d membership.Desired) {
	if _, live := s.Live[d.Session]; !live {
		return
	}
	s.addCheckout(d.Checkout)
	s.Seats.AddDesired(d)
	s.fillSeats()
}

// fillSeats keys the index's answers for the table.
func (s *Snapshot) fillSeats() {
	for _, k := range s.Seats.Keys() {
		key := k.Dir + "|" + k.Ref()
		s.SeatSession[key] = s.Seats.SessionOf(k)
		if restores, recorded := s.Seats.Restore(k); recorded {
			s.SeatRestore[key] = restores
		}
	}
}

// addMemberRows appends a team's seats, its other members, the LXC members its record holds
// back, and its people.
func (s *Snapshot) addMemberRows(c *Collector, team string, seats []seatAt, lxc lxcrecord.Record) {
	roster := s.Members[team]
	activity := map[string]string{}
	for _, m := range roster.Members {
		activity[m.Handle] = "active"
		if m.Parked {
			activity[m.Handle] = "parked"
		}
	}
	unknown := declare.WordUnknown
	if s.MembersUnknown[team] == WordNeedSudo {
		unknown = WordNeedSudo
	}
	shown := map[string]bool{}
	for _, sa := range seats {
		if sa.seat.Team == team {
			s.Rows = append(s.Rows, c.seatRow(s, team, sa, activity, unknown))
			shown[sa.seat.Handle] = true
		}
	}
	s.addListedRows(team, roster, activity, shown, lxc)
	for _, h := range roster.Humans {
		signed := s.SignedIn[team+"|"+h.Name]
		if signed == "" {
			signed = declare.WordUnknown
		}
		s.Rows = append(s.Rows, Row{
			Team: team, Member: h.Name, Kind: KindHuman, State: roomWord(h.Membership),
			Applied: "-", Session: signed, Restore: "-", Where: "Element; Enter for choices", Handle: "-", Dir: "-",
		})
	}
}

// addListedRows appends the team's listed members that no seat row showed, with what the LXC
// record says of each it held back, then a row for each deferred LXC member the list does not
// name (one never joined, so with no handle yet).
func (s *Snapshot) addListedRows(team string, roster agentbus.Roster, activity map[string]string, shown map[string]bool,
	lxc lxcrecord.Record,
) {
	held := map[string]lxcrecord.Item{}
	for _, item := range lxc.HeldBack() {
		if item.Handle != "" {
			held[item.Handle] = item
		}
	}
	for _, m := range roster.Members {
		if shown[m.Handle] {
			continue
		}
		shown[m.Handle] = true
		row := Row{
			Team: team, Member: m.Handle, Kind: Kind(m.Kind()), State: activity[m.Handle],
			Applied: "-", Session: "-", Restore: "-", Where: m.Role, Handle: m.Handle, Dir: "-", Role: m.Role,
		}
		if item, ok := held[m.Handle]; ok {
			row.State, row.Where = heldBackWords(row, item)
		}
		s.Rows = append(s.Rows, row)
	}
	for _, item := range lxc.HeldBack() {
		if item.Status == lxcrecord.StatusDeferred && !shown[item.Handle] {
			s.Rows = append(s.Rows, Row{
				Team: team, Member: item.Label(), Kind: "lxc", State: lxcrecord.StatusDeferred,
				Applied: "-", Session: "-", Restore: "-", Where: item.Words(), Handle: "-", Dir: "-",
			})
		}
	}
}
