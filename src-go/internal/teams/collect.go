package teams

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// errReported is a collection that failed after saying why on stderr.
var errReported = errors.New("already reported on stderr")

// Words the table and the callers compare against.
const (
	wordUnknown    = "unknown"
	wordUnreadable = "unreadable"
	wordNeedSudo   = "need sudo"

	wordNotInstalled = "not installed"
)

// Snapshot is everything one pass over the machine found: the teams, their applied state,
// the sessions and seats, the members, and the table rows built from them. A pass reads each
// source once (systemd, tmux, the registry, the declaration).
type Snapshot struct {
	Teams  []string // declared present, then loaded, then stamped: first-seen order
	Port   map[string]string
	URL    map[string]string
	Server map[string]string
	Loaded map[string]bool // the team has a homeserver unit
	Status map[string]declare.Status

	Live        map[string]int    // tmux session name -> attached clients
	SeatSession map[string]string // "<dir>|<seat>@<team>" -> registry session name
	SeatRestore map[string]bool   // same key -> whether the session is started again after a reboot
	Checkouts   []string

	Members        map[string]agentbus.Roster
	MembersUnknown map[string]string // team -> why its members are not shown
	SignedIn       map[string]string // "<team>|<person>" -> their Element sessions in words

	// NeedSudo is " <team>" for each team whose members wait on sudo (a team can repeat).
	NeedSudo string
	// Incomplete says a seat or a session record could not be read; every team row then shows it.
	Incomplete string

	Rows []Row
}

func newSnapshot() *Snapshot {
	return &Snapshot{
		Port: map[string]string{}, URL: map[string]string{}, Server: map[string]string{},
		Loaded: map[string]bool{}, Status: map[string]declare.Status{},
		Live: map[string]int{}, SeatSession: map[string]string{}, SeatRestore: map[string]bool{},
		Members: map[string]agentbus.Roster{}, MembersUnknown: map[string]string{}, SignedIn: map[string]string{},
	}
}

// statusWord is the APPLIED column for team, in words.
func (s *Snapshot) statusWord(team string) string {
	st, ok := s.Status[team]
	if !ok {
		return wordUnknown
	}
	return applyWord(st)
}

func (s *Snapshot) addTeam(seen map[string]bool, team string) {
	if !seen[team] {
		seen[team] = true
		s.Teams = append(s.Teams, team)
	}
}

// collectTeams fills Teams, Port, URL, Server and Loaded from the declaration, the homeserver
// units and the applied stamps.
func (a *App) collectTeams(ctx context.Context, s *Snapshot) error {
	if err := a.needCheckout(); err != nil {
		return err
	}
	declared, err := a.Declare.Declared(ctx)
	if err != nil {
		a.reportErr(err)
		a.Term.PrintError("the teams declared in " + a.HostVars + " could not be read (the reason is above).")
		return errReported
	}
	seen := map[string]bool{}
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
	units, err := a.Systemd.HomeserverUnits(ctx)
	if err != nil {
		a.Term.PrintError("systemctl could not list the agent-bus homeserver units.")
		return errReported
	}
	for _, u := range units {
		s.Loaded[u.Team] = true
		s.addTeam(seen, u.Team)
	}
	// A team whose stamp says it is built, though not declared present, needs a decision.
	stamps, err := os.ReadDir(a.AppliedDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		a.Term.PrintError("the applied-state directory " + a.AppliedDir + " could not be read: " + err.Error())
		return errReported
	}
	for _, e := range stamps {
		if team, ok := strings.CutSuffix(e.Name(), ".json"); ok && team != "" {
			s.addTeam(seen, team)
		}
	}
	return nil
}

// readStatus fills Status: whether each team is applied as declared.
func (a *App) readStatus(ctx context.Context, s *Snapshot) error {
	installed := make([]string, 0, len(s.Loaded))
	for team := range s.Loaded {
		installed = append(installed, team)
	}
	states, err := a.Declare.Status(ctx, a.RootDir, a.AppliedDir, installed)
	if err != nil {
		a.reportErr(err)
		a.Term.PrintError("whether each team is applied as declared could not be read (the reason is above).")
		return errReported
	}
	s.Status = map[string]declare.Status{}
	for _, st := range states {
		s.Status[st.Team] = st
	}
	return nil
}

// refresh is the teams and their applied state, read again: what apply and remove check
// before and after running the play.
func (a *App) refresh(ctx context.Context) (*Snapshot, error) {
	s := newSnapshot()
	if err := a.collectTeams(ctx, s); err != nil {
		return nil, err
	}
	if err := a.readStatus(ctx, s); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Snapshot) addCheckout(dir string) {
	if dir == "" || slices.Contains(s.Checkouts, dir) {
		return
	}
	s.Checkouts = append(s.Checkouts, dir)
}

// teamsListOf is the --teams value in a launcher's arguments, up to a bare "--"; the last wins.
func teamsListOf(args []string) string {
	list := ""
	for i, arg := range args {
		switch {
		case arg == "--":
			return list
		case arg == flagTeams:
			list = ""
			if i+1 < len(args) {
				list = args[i+1]
			}
		case strings.HasPrefix(arg, "--teams="):
			list = strings.TrimPrefix(arg, "--teams=")
		}
	}
	return list
}

// collectSessions fills Live, SeatSession, SeatRestore and Checkouts from tmux, the session
// registry and the working directory.
func (a *App) collectSessions(ctx context.Context, s *Snapshot) error {
	sessions, err := a.Tmux.List(ctx)
	if err != nil {
		a.Term.PrintError(err.Error())
		return errReported
	}
	for _, sess := range sessions {
		s.Live[sess.Name] = sess.Attached
		if strings.HasPrefix(sess.Name, "ccy-") {
			s.addCheckout(sess.Path)
		}
	}
	store, err := a.Registry()
	if err != nil {
		a.Term.PrintError(err.Error())
		return errReported
	}
	listing, err := store.List()
	if err != nil {
		a.Term.PrintError(err.Error())
		return errReported
	}
	for _, name := range listing.Unreadable {
		a.Term.PrintError("the session record " + filepath.Join(store.Dir(), name) + " could not be read; its seats show no session.")
		s.Incomplete = "a session record is unreadable"
	}
	for _, rec := range listing.Records {
		s.addRecord(rec)
	}
	if err := a.addWorkTree(ctx, s); err != nil {
		a.Term.PrintError(err.Error())
		return errReported
	}
	return nil
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

// addWorkTree notes the checkout the command was run in, if it is in one.
func (a *App) addWorkTree(ctx context.Context, s *Snapshot) error {
	here, err := a.Git.ProjectHere(ctx, "")
	if err != nil || !here {
		return err
	}
	top, err := a.Git.TopLevel(ctx, "")
	if err != nil {
		return err
	}
	s.addCheckout(top)
	return nil
}

// homeserverState is systemd.HomeserverUp, or "down (<systemd's word>)".
func (a *App) homeserverState(ctx context.Context, team string) (string, error) {
	return a.Systemd.HomeserverState(ctx, team)
}

// isCommandErr reports whether err is a tool that ran and exited non-zero, which has already
// copied its own stderr to the terminal.
func isCommandErr(err error) bool {
	cmdErr, ok := errors.AsType[*agentbus.CommandError](err)
	return ok && cmdErr != nil
}

// collectMembers reads team's roster, and each person's Element sessions, as root without
// prompting. What could not be read is recorded as a reason, never skipped silently.
func (a *App) collectMembers(ctx context.Context, s *Snapshot, team string) {
	roster, err := a.Bus.List(ctx, team)
	switch {
	case err == nil:
		s.Members[team] = roster
	case errors.Is(err, sudo.ErrNeedsPassword):
		s.MembersUnknown[team] = wordNeedSudo
		s.NeedSudo += " " + team
		return
	default:
		if !isCommandErr(err) {
			a.reportErr(err)
		}
		s.MembersUnknown[team] = "read failed (above)"
		return
	}
	for _, h := range roster.Humans {
		key := team + "|" + h.Name
		devices, err := a.Bus.HumanDevices(ctx, team, h.Name)
		switch {
		case err == nil && len(devices) == 0:
			s.SignedIn[key] = "never signed in"
		case err == nil:
			s.SignedIn[key] = strconv.Itoa(len(devices)) + " signed in"
		case errors.Is(err, sudo.ErrNeedsPassword):
			s.SignedIn[key] = wordNeedSudo
			s.NeedSudo += " " + team
		default:
			if !isCommandErr(err) {
				a.reportErr(err)
			}
			s.SignedIn[key] = wordUnknown
		}
	}
}

type seatAt struct {
	dir  string
	seat agentbus.Seat
}

// readSeats is every seat of the checkouts that have a seat directory, read as the user.
func (a *App) readSeats(ctx context.Context, s *Snapshot) []seatAt {
	var all []seatAt
	for _, dir := range s.Checkouts {
		info, err := os.Stat(filepath.Join(dir, ".claude", "ccy", "pingbus", "seats"))
		if err != nil || !info.IsDir() {
			continue
		}
		seats, unreadable, err := a.Bus.SeatList(ctx, dir)
		if err != nil || unreadable {
			if err != nil && !isCommandErr(err) {
				a.reportErr(err)
			}
			a.Term.PrintError("agent-bus seat list could not read every seat in " + dir + " (above); those are left out.")
			s.Incomplete = "a seat is unreadable"
		}
		for _, seat := range seats {
			all = append(all, seatAt{dir: dir, seat: seat})
		}
	}
	return all
}

// roomWord is whether a person is in the team's room, in words.
func roomWord(membership string) string {
	switch membership {
	case "join":
		return "in room"
	case "invite":
		return "invited"
	}
	return "not in room"
}

// collect is one pass over the machine: every source read once, the rows built.
func (a *App) collect(ctx context.Context) (*Snapshot, error) {
	s := newSnapshot()
	if err := a.collectTeams(ctx, s); err != nil {
		return nil, err
	}
	if len(s.Teams) == 0 {
		return s, nil
	}
	if err := a.readStatus(ctx, s); err != nil {
		return nil, err
	}
	if err := a.collectSessions(ctx, s); err != nil {
		return nil, err
	}
	seats := a.readSeats(ctx, s)
	for _, team := range s.Teams {
		if err := a.addTeamRows(ctx, s, team, seats); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// addTeamRows appends the team's own row, then its seats, other members and people.
func (a *App) addTeamRows(ctx context.Context, s *Snapshot, team string, seats []seatAt) error {
	state, where, err := a.teamState(ctx, s, team)
	if err != nil {
		return err
	}
	if s.Incomplete != "" {
		where += ", " + s.Incomplete + " (above)"
	}
	if s.Status[team].State == declare.ApplyUnreadable {
		where += ", entry unreadable: " + s.Status[team].Detail
	}
	s.Rows = append(s.Rows, Row{
		Team: team, Member: "-", Kind: kindTeam, State: state, Applied: s.statusWord(team),
		Session: "-", Restore: "-", Where: where, Handle: "-", Dir: "-",
	})
	s.addMemberRows(a, team, seats)
	return nil
}

// teamState is a team's STATE and the start of its WHERE: its port, and why its members are
// not shown. It reads the members of a team that is up.
func (a *App) teamState(ctx context.Context, s *Snapshot, team string) (string, string, error) {
	port := s.Port[team]
	if port == "" {
		port = "unknown (not declared)"
	}
	where := "port " + port
	if !s.Loaded[team] {
		s.MembersUnknown[team] = wordNotInstalled
		if s.Status[team].State == declare.ApplyDecide {
			return wordNotInstalled, where + ", not declared present: ccy-teams remove " + team + " takes it down", nil
		}
		return wordNotInstalled, where + ", install it: ccy-teams apply " + team, nil
	}
	state, err := a.homeserverState(ctx, team)
	if err != nil {
		a.Term.PrintError(err.Error())
		return "", "", errReported
	}
	if state == systemd.HomeserverUp {
		a.collectMembers(ctx, s, team)
	} else {
		s.MembersUnknown[team] = "homeserver down"
	}
	if why := s.MembersUnknown[team]; why != "" {
		where += ", members: " + why
	}
	return state, where, nil
}

// addMemberRows appends a team's seats, its other members and its people.
func (s *Snapshot) addMemberRows(a *App, team string, seats []seatAt) {
	roster := s.Members[team]
	activity := map[string]string{}
	for _, m := range roster.Members {
		activity[m.Handle] = "active"
		if m.Parked {
			activity[m.Handle] = "parked"
		}
	}
	unknown := wordUnknown
	if s.MembersUnknown[team] == wordNeedSudo {
		unknown = wordNeedSudo
	}
	shown := map[string]bool{}
	for _, sa := range seats {
		if sa.seat.Team == team {
			s.Rows = append(s.Rows, a.seatRow(s, team, sa, activity, unknown))
			shown[sa.seat.Handle] = true
		}
	}
	for _, m := range roster.Members {
		if !shown[m.Handle] {
			s.Rows = append(s.Rows, Row{
				Team: team, Member: m.Handle, Kind: rowKind(m.Kind()), State: activity[m.Handle],
				Applied: "-", Session: "-", Restore: "-", Where: m.Role, Handle: m.Handle, Dir: "-",
			})
		}
	}
	for _, h := range roster.Humans {
		signed := s.SignedIn[team+"|"+h.Name]
		if signed == "" {
			signed = wordUnknown
		}
		s.Rows = append(s.Rows, Row{
			Team: team, Member: h.Name, Kind: kindHuman, State: roomWord(h.Membership),
			Applied: "-", Session: signed, Restore: "-", Where: "Element; Enter: new password", Handle: "-", Dir: "-",
		})
	}
}

// restoreWord is the RESTORE column for the seat at key: "-" when no session record names it.
func restoreWord(s *Snapshot, key string) string {
	restores, recorded := s.SeatRestore[key]
	if !recorded {
		return "-"
	}
	return term.YesNo(restores)
}

// seatRow is a ccy seat's row: its session and RESTORE when a registry record names it, else
// the seat's own state.
func (a *App) seatRow(s *Snapshot, team string, sa seatAt, activity map[string]string, unknown string) Row {
	key := sa.dir + "|" + sa.seat.Seat + "@" + team
	record, restore := s.SeatSession[key], restoreWord(s, key)
	session := record
	if record == "" {
		session, restore = "free", "-"
		if sa.seat.Held {
			session = "held"
		}
	} else if _, live := s.Live[record]; !live {
		session += "(stopped)"
	}
	state := activity[sa.seat.Handle]
	if state == "" {
		state = unknown
	}
	return Row{
		Team: team, Member: sa.seat.Seat, Kind: kindCcy, State: state, Applied: "-", Session: session, Record: record,
		Restore: restore, Where: term.AbbrevHome(sa.dir, a.Home), Handle: sa.seat.Handle, Dir: sa.dir,
	}
}
