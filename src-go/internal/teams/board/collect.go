package board

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/hostcheckout"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/config"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/lxcrecord"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// errReported is a collection that failed after saying why on stderr.
var errReported = errors.New("already reported on stderr")

// Collector reads the machine into Snapshots. Every source is a field, so a test stands in for
// each.
type Collector struct {
	config.Config

	Systemd *systemd.Systemd
	Declare *declare.Client
	Tmux    *tmuxccy.Tmux
	// Checkouts says whether the folder the command runs in is the host's checkout (its seats
	// are read) or an LXC container's (they are not: that checkout is the container's).
	Checkouts *hostcheckout.Finder
	Bus       *agentbus.Client
	Registry  func() (*registry.Store, error)

	Term   *term.Term
	Out    *term.Printer
	Stdout io.Writer
	Stderr io.Writer
	// NeedCheckout refuses, saying why, when the checkout ccy-teams was built from has gone.
	NeedCheckout func() error
	// Columns is how wide a terminal stream is, 0 when unknown: the table is laid out to fit it.
	Columns func(any) int
	// Now is the clock a health record's age is judged by.
	Now func() time.Time
	// Getenv is the environment: XDG_RUNTIME_DIR, where the sessions' desired memberships are.
	Getenv func(string) string
	// Parallel is how many agent-bus calls a pass runs at once; 0 is the default bound.
	Parallel int

	// devices is who is signed in to Element, by "<team>|<person>", read once and kept until
	// ForgetDevices: one sudo process per person is too much to pay on every redraw.
	devices map[string]int
	// workTree is the checkout the command was run in, asked once: why is set when whose
	// checkout it is could not be told, and it is then left out.
	workTree struct {
		asked bool
		here  hostcheckout.Checkout
		why   string
	}
}

// New is the Collector of a host.
func New(h *host.Host) *Collector {
	return &Collector{
		Config: h.Config, Systemd: h.Systemd, Declare: h.Declare, Tmux: h.Tmux, Checkouts: h.Checkouts, Bus: h.Bus,
		Registry: h.Registry, Term: h.Term, Out: h.Out, Stdout: h.Stdout, Stderr: h.Stderr,
		NeedCheckout: h.NeedCheckout, Now: time.Now, Getenv: h.Getenv,
		Columns: func(v any) int { return h.Columns(v) },
	}
}

// Refresh is the teams and their applied state, read again: what apply and remove check
// before and after running the play.
func (c *Collector) Refresh(ctx context.Context) (*Snapshot, error) {
	s := NewSnapshot()
	if err := c.collectTeams(ctx, s); err != nil {
		return nil, err
	}
	return s, nil
}

// Statuses is Refresh's applied state of every team declared or built, for the flows that
// only need to know what is behind.
func (c *Collector) Statuses(ctx context.Context) (map[string]declare.Status, error) {
	s, err := c.Refresh(ctx)
	if err != nil {
		return nil, err
	}
	return s.Status, nil
}

// ForgetDevices drops what was read of who is signed in to Element, for team's people or, with
// no team, everyone's: reading each person costs a sudo process, so the list keeps a read until
// something that changes it is done (a password set, a team applied, removed or created).
func (c *Collector) ForgetDevices(team string) {
	if team == "" {
		clear(c.devices)
		return
	}
	for key := range c.devices {
		if strings.HasPrefix(key, team+"|") {
			delete(c.devices, key)
		}
	}
}

type seatAt struct {
	dir  string
	seat agentbus.Seat
}

// Collect is one pass over the machine: every source read once, the rows built. The agent-bus
// calls run together (read), and what they found is used in the order they were always made.
func (c *Collector) Collect(ctx context.Context) (*Snapshot, error) {
	s := NewSnapshot()
	if err := c.collectTeams(ctx, s); err != nil {
		return nil, err
	}
	if len(s.Teams) == 0 {
		return s, nil
	}
	if err := c.collectSessions(ctx, s); err != nil {
		return nil, err
	}
	r := c.read(ctx, s)
	seats := c.seatsRead(s, r)
	for _, team := range s.Teams {
		c.addTeamRows(s, team, seats, r)
	}
	return s, nil
}

func (c *Collector) keepDevices(key string, count int) {
	if c.devices == nil {
		c.devices = map[string]int{}
	}
	c.devices[key] = count
}

// reportErr prints why something failed, as the tool that failed said it.
func (c *Collector) reportErr(err error) {
	c.Out.Print(c.Stderr, err.Error()+"\n")
}

// collectTeams fills Teams, Port, URL, Server, Loaded, Active and Status from the homeserver
// units, the declaration and the applied stamps. The units come first because the status check
// is told which teams have one; declare.py then answers the declaration and the status in one
// run.
func (c *Collector) collectTeams(ctx context.Context, s *Snapshot) error {
	if err := c.NeedCheckout(); err != nil {
		return err
	}
	units, err := c.Systemd.HomeserverUnits(ctx)
	if err != nil {
		c.Term.PrintError("systemctl could not list the agent-bus homeserver units.")
		return errReported
	}
	installed := make([]string, 0, len(units))
	for _, u := range units {
		s.Loaded[u.Team] = true
		s.Active[u.Team] = u.Active
		installed = append(installed, u.Team)
	}
	declared, states, err := c.Declare.Overview(ctx, c.RootDir, c.AppliedDir, installed)
	if err != nil {
		c.reportErr(err)
		c.Term.PrintError("the teams declared in " + c.HostVars + ", and whether each is applied as declared, could not be read (the reason is above).")
		return errReported
	}
	seen := map[string]bool{}
	s.addDeclared(seen, declared)
	for _, u := range units {
		s.addTeam(seen, u.Team)
	}
	for _, st := range states {
		s.Status[st.Team] = st
	}
	return c.addStamped(s, seen)
}

// addStamped notes a team whose stamp says it is built, though not declared present: it needs
// a decision.
func (c *Collector) addStamped(s *Snapshot, seen map[string]bool) error {
	stamps, err := os.ReadDir(c.AppliedDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		c.Term.PrintError("the applied-state directory " + c.AppliedDir + " could not be read: " + err.Error())
		return errReported
	}
	for _, e := range stamps {
		if team, ok := strings.CutSuffix(e.Name(), ".json"); ok && team != "" {
			s.addTeam(seen, team)
		}
	}
	return nil
}

// collectSessions fills Live, SeatSession, SeatRestore and Checkouts from tmux, the session
// registry and the working directory.
func (c *Collector) collectSessions(ctx context.Context, s *Snapshot) error {
	sessions, err := c.Tmux.List(ctx)
	if err != nil {
		c.Term.PrintError(err.Error())
		return errReported
	}
	for _, sess := range sessions {
		s.Live[sess.Name] = sess.Attached
		if prefix, _, ok := strings.Cut(sess.Name, "-"); ok && discover.IsLauncher(prefix) {
			s.addCheckout(sess.Path)
		}
	}
	store, err := c.Registry()
	if err != nil {
		c.Term.PrintError(err.Error())
		return errReported
	}
	listing, err := store.List()
	if err != nil {
		c.Term.PrintError(err.Error())
		return errReported
	}
	for _, name := range listing.Unreadable {
		c.Term.PrintError("the session record " + filepath.Join(store.Dir(), name) + " could not be read; its seats show no session.")
		s.Incomplete = "a session record is unreadable"
	}
	for _, rec := range listing.Records {
		s.addRecord(rec)
	}
	c.addDesired(s, listing.Records)
	c.addWorkTree(ctx, s)
	return nil
}

// addDesired notes the seats live sessions' desired memberships name (a hot add's, before or
// beside its record), each only while its session's own launch wrote it (records: B1 of the M6
// review, a reused name). With no runtime directory there are none to read.
func (c *Collector) addDesired(s *Snapshot, records []registry.Record) {
	runtime := ""
	if c.Getenv != nil {
		runtime = c.Getenv("XDG_RUNTIME_DIR")
	}
	if runtime == "" {
		return
	}
	desired, unreadable, err := discover.ReadDesired(runtime)
	if err != nil {
		unreadable = append(unreadable, err.Error())
	}
	for _, why := range unreadable {
		c.Term.PrintError(why + "; its seats show no session.")
		s.Incomplete = "a session membership is unreadable"
	}
	for _, d := range discover.Current(desired, records) {
		s.addDesired(d)
	}
}

// signedInWord is a person's SESSION column: how many Element sessions they have.
func signedInWord(devices int) string {
	if devices == 0 {
		return WordNeverSignedIn
	}
	return strconv.Itoa(devices) + " signed in"
}

// addWorkTree notes the checkout the command was run in, if it is the host's. The folder does
// not change while the command runs, so it is asked about once, and a failure said once.
func (c *Collector) addWorkTree(ctx context.Context, s *Snapshot) {
	if !c.workTree.asked {
		here, err := c.Checkouts.Find(ctx, "")
		if err != nil {
			c.workTree.why = "this folder's checkout is unknown"
			c.Term.PrintError(err.Error() + "; its seats are left out.")
		}
		c.workTree.here, c.workTree.asked = here, true
	}
	if c.workTree.why != "" {
		s.Incomplete = c.workTree.why
	}
	if c.workTree.here.Host() {
		s.addCheckout(c.workTree.here.Top)
	}
}

// addMembers notes team's roster, and each person's Element sessions, as read as root without
// prompting. What could not be read is recorded as a reason, never skipped silently.
func (c *Collector) addMembers(s *Snapshot, team string, read *rosterRead) {
	read.said.play(c.Out)
	roster, err := read.roster, read.err
	switch {
	case err == nil:
		s.Members[team] = roster
	case errors.Is(err, sudo.ErrNeedsPassword):
		s.MembersUnknown[team] = WordNeedSudo
		s.NeedSudo += " " + team
		return
	default:
		if !agentbus.IsCommandError(err) {
			c.reportErr(err)
		}
		s.MembersUnknown[team] = "read failed (above)"
		return
	}
	for _, h := range roster.Humans {
		key := team + "|" + h.Name
		if count, kept := c.devices[key]; kept {
			s.setSessions(key, count)
			continue
		}
		person := read.devices[h.Name]
		person.said.play(c.Out)
		devices, err := person.devices, person.err
		switch {
		case err == nil:
			s.setSessions(key, len(devices))
			c.keepDevices(key, len(devices))
		case errors.Is(err, sudo.ErrNeedsPassword):
			s.SignedIn[key] = WordNeedSudo
			s.NeedSudo += " " + team
		default:
			if !agentbus.IsCommandError(err) {
				c.reportErr(err)
			}
			s.SignedIn[key] = declare.WordUnknown
		}
	}
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

// seatsRead is every seat of the checkouts that have a seat directory, as read as the user, in
// the snapshot's order of checkouts.
func (c *Collector) seatsRead(s *Snapshot, r reads) []seatAt {
	var all []seatAt
	for _, dir := range s.Checkouts {
		read, asked := r.seats[dir]
		if !asked {
			continue
		}
		read.said.play(c.Out)
		seats, unreadable, err := read.seats, read.unreadable, read.err
		if err != nil || unreadable {
			if err != nil && !agentbus.IsCommandError(err) {
				c.reportErr(err)
			}
			c.Term.PrintError("agent-bus seat list could not read every seat in " + dir + " (above); those are left out.")
			s.Incomplete = "a seat is unreadable"
		}
		for _, seat := range seats {
			all = append(all, seatAt{dir: dir, seat: seat})
		}
	}
	return all
}

// addTeamRows appends the team's own row, then its seats, other members and people.
func (c *Collector) addTeamRows(s *Snapshot, team string, seats []seatAt, r reads) {
	state, where := c.teamState(s, team, r)
	if s.Loaded[team] {
		if words := healthWords(c.AppliedDir, team, c.Now()); words != "" {
			where += ", " + words
		}
	}
	if s.Incomplete != "" {
		where += ", " + s.Incomplete + " (above)"
	}
	if s.Status[team].State == declare.ApplyUnreadable {
		where += ", entry unreadable: " + s.Status[team].Detail
	}
	lxc, _, err := lxcrecord.Read(c.AppliedDir, team)
	if err != nil {
		where += ", " + err.Error()
	}
	s.Rows = append(s.Rows, Row{
		Team: team, Member: s.aboutTeam(team), Kind: KindTeam, State: state, Applied: s.StatusWord(team),
		Session: "-", Restore: "-", Where: where, Handle: "-", Dir: "-",
	})
	s.addMemberRows(c, team, seats, lxc)
}

// teamState is a team's STATE and the start of its WHERE: its port, and why its members are
// not shown. It notes the members of a team that is up.
func (c *Collector) teamState(s *Snapshot, team string, r reads) (string, string) {
	port := s.Port[team]
	if port == "" {
		port = "unknown (not declared)"
	}
	where := "port " + port
	if !s.Loaded[team] {
		s.MembersUnknown[team] = wordNotInstalled
		if s.Status[team].State == declare.ApplyDecide {
			return wordNotInstalled, where + ", not declared present: ccy-teams remove " + team + " takes it down"
		}
		return wordNotInstalled, where + ", install it: ccy-teams apply " + team
	}
	state := systemd.StateWord(s.Active[team])
	if s.up(team) {
		c.addMembers(s, team, r.rosters[team])
	} else {
		s.MembersUnknown[team] = "homeserver down"
	}
	if why := s.MembersUnknown[team]; why != "" {
		where += ", members: " + why
	}
	return state, where
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
func (c *Collector) seatRow(s *Snapshot, team string, sa seatAt, activity map[string]string, unknown string) Row {
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
		Team: team, Member: sa.seat.Seat, Kind: KindCcy, State: state, Applied: "-", Session: session, Record: record,
		Restore: restore, Where: term.AbbrevHome(sa.dir, c.Home), Handle: sa.seat.Handle, Dir: sa.dir,
		Role: s.RoleOf(team, sa.seat.Handle), Held: sa.seat.Held,
	}
}
