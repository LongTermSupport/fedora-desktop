package board

import (
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
)

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
	Loaded map[string]bool   // the team has a homeserver unit
	Active map[string]string // the unit's ACTIVE word from the one list-units pass
	Status map[string]declare.Status

	Live        map[string]int    // tmux session name -> attached clients
	SeatSession map[string]string // "<dir>|<seat>@<team>" -> registry session name
	SeatRestore map[string]bool   // same key -> whether the session is started again after a reboot
	Checkouts   []string

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
		Port: map[string]string{}, URL: map[string]string{}, Server: map[string]string{},
		Loaded: map[string]bool{}, Active: map[string]string{}, Status: map[string]declare.Status{},
		Live: map[string]int{}, SeatSession: map[string]string{}, SeatRestore: map[string]bool{},
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

// addRecord notes the checkout of a ccy session record and the seats its launcher was given.
func (s *Snapshot) addRecord(rec registry.Record) {
	if rec.Prefix != "ccy" {
		return
	}
	s.addCheckout(rec.Dir)
	for ref := range strings.SplitSeq(teamsListOf(rec.Args), ",") {
		if ref != "" {
			s.SeatSession[rec.Dir+"|"+ref] = rec.Name
			s.SeatRestore[rec.Dir+"|"+ref] = rec.Restore
		}
	}
}

// addMemberRows appends a team's seats, its other members and its people.
func (s *Snapshot) addMemberRows(c *Collector, team string, seats []seatAt) {
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
	for _, m := range roster.Members {
		if !shown[m.Handle] {
			s.Rows = append(s.Rows, Row{
				Team: team, Member: m.Handle, Kind: Kind(m.Kind()), State: activity[m.Handle],
				Applied: "-", Session: "-", Restore: "-", Where: m.Role, Handle: m.Handle, Dir: "-", Role: m.Role,
			})
		}
	}
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
