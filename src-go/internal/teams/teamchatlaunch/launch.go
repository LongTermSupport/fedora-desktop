// Package teamchatlaunch is the ccy launcher's v2 (teamchat) launch (Plan 00209 Task 4.2,
// LAUNCH-ccy.md; seams 53, 54, 27): the seats taken from the owner's seat store into the
// session's directory, generation 1 of its desired.json, the guard that keeps the seat store
// off every other mount, the podman arguments that bind the two directories, and the release
// when the launch ends.
//
// The launcher is bash on a ratchet, so it holds none of this: `ccy-teams internal
// launch-args` prints the arguments it mounts (Args), and `ccy-teams internal
// bus-disconnected` is its exit (Release). For a launch on agent-bus both are what the bash
// did before, byte for byte.
package teamchatlaunch

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/duty"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatchoice"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
)

// MembershipMount is where the session's control directory is mounted read-only; the
// container's ccy-start-claude is told it by TEAMCHAT_MEMBERSHIP (decision L1).
const MembershipMount = paths.SessionMembershipMount

// stopTimeout is podman's --stop-timeout for a v2 session (seam 53): the keeper's goodbye and
// its seat lock's release before podman kills the container.
const stopTimeout = "20"

// The refusals of a launch: a mount that reaches the seat store (exit 78), a seat list the
// pool cannot answer (exit 64), and a Launch built without its launcher's kind of seat (a
// defect in the launcher, exit 1).
var (
	ErrMount = errors.New("a mount reaches the seat store")
	ErrSeat  = errors.New("no such seat")
	ErrKind  = errors.New("internal error: the launch has no kind of seat")
)

// MountSources is the source of every bind in podman run arguments: -v and --volume (joined or
// not) and --mount's source= or src=.
func MountSources(args []string) []string {
	var sources []string
	for i := 0; i < len(args); i++ {
		flag, value, joined := strings.Cut(args[i], "=")
		if !joined && i+1 < len(args) {
			value = args[i+1]
		}
		switch flag {
		case "-v", "--volume":
			sources = append(sources, strings.SplitN(value, ":", 2)[0])
		case "--mount":
			for field := range strings.SplitSeq(value, ",") {
				if k, v, ok := strings.Cut(field, "="); ok && (k == "source" || k == "src") {
					sources = append(sources, v)
				}
			}
		default:
			continue
		}
		if !joined {
			i++
		}
	}
	return sources
}

// realPath is p with every link it reaches resolved; the part that does not exist yet is kept
// as written, so a mount the engine would create is judged where it would land.
func realPath(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(p, rest)
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

// within is whether p is dir or below it.
func within(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, strings.TrimSuffix(dir, "/")+"/")
}

// Guard refuses any source whose real path is an ancestor of, equal to, or inside the seat
// store root (seam 53): a container that saw the store would see every pool seat's password.
func Guard(root string, sources []string) error {
	store := realPath(root)
	for _, src := range sources {
		if src == "" || !filepath.IsAbs(src) {
			continue
		}
		resolved := realPath(src)
		if within(resolved, store) || within(store, resolved) {
			return fmt.Errorf("%w: the mount of %s (%s) would show the container the seat store %s; mount a directory beside it instead", ErrMount, src, resolved, store)
		}
	}
	return nil
}

// ChooseSeat is the pool seat of team of kind (the launcher's: podman for ccy, host for cc) whose
// handle's seat part is seat (C 3.7.4's seat number, given by the launch's <seat>@<team>),
// refused when none or more than one has it. A seat part only another kind has is refused naming
// that kind and its launcher (register R55): a launch never takes another launcher's seat.
func ChooseSeat(pool []paths.SeatName, team, seatPart, kind string) (paths.SeatName, error) {
	var found []paths.SeatName
	other := ""
	names := make([]string, 0, len(pool))
	for _, n := range pool {
		names = append(names, n.String())
		h, ok := proto.ParseHandle(n.String())
		switch {
		case !ok || h.Seat != seatPart:
		case h.Type == kind:
			found = append(found, n)
		default:
			other = n.String()
		}
	}
	switch {
	case len(found) == 1:
		return found[0], nil
	case len(found) == 0 && other != "":
		return paths.SeatName{}, fmt.Errorf("%w: %s", ErrSeat, seatchoice.WrongKind(seatPart, team, other, kind))
	case len(found) == 0:
		return paths.SeatName{}, fmt.Errorf("%w: no pool seat of this checkout has seat %s (it has: %s)", ErrSeat, seatPart, strings.Join(names, ", "))
	}
	return paths.SeatName{}, fmt.Errorf("%w: more than one pool seat has seat %s (%v); remove the one you do not use", ErrSeat, seatPart, found)
}

// HandleState is the seat's handle state from the team's status record's roster, or
// StateNoRoster where this machine has no record for the team (a member machine).
func HandleState(rec status.Record, found bool, name paths.SeatName) (seat.HandleState, error) {
	if !found {
		return seat.StateNoRoster, nil
	}
	for _, m := range rec.Members {
		if m.Handle != name.String() {
			continue
		}
		switch m.State {
		case "enrolled":
			return seat.StateActive, nil
		case "parked":
			return seat.StateParked, nil
		case "lost":
			return seat.StateLost, nil
		case "retired":
			return seat.StateRetired, nil
		}
		return 0, fmt.Errorf("%w: %s is %s on the team's roster, which a launch does not take", ErrSeat, name, m.State)
	}
	return 0, fmt.Errorf("%w: %s is not on the team's roster", ErrSeat, name)
}

// SeatDuty is the seat's duty as the team's status record's row gives it, "" where this machine
// has no record for the team or the row gives none (PROPOSAL-seat-kinds.md OD2 (a)).
func SeatDuty(rec status.Record, found bool, name paths.SeatName) duty.Duty {
	if !found {
		return ""
	}
	for _, m := range rec.Members {
		if m.Handle == name.String() && m.Duty != nil {
			return *m.Duty
		}
	}
	return ""
}

// Chosen is one seat of the launch: its team, its pool seat, the handle's seat part and the
// seat's duty ("" for none known).
type Chosen struct {
	Team paths.TeamID
	Name paths.SeatName
	Seat string
	Duty duty.Duty
}

// Desired is generation 1 of the session's desired.json (C 3.7.5): the launch's seats, no
// farewell, inbound accepted.
func Desired(session paths.SessionName, chosen []Chosen, nowMS int64) ([]byte, error) {
	return firstGeneration(session, chosen, nowMS).Encode()
}

func firstGeneration(session paths.SessionName, chosen []Chosen, nowMS int64) state.Desired {
	d := state.Desired{
		Session: session, Generation: 1, WrittenMS: nowMS,
		Seats: []state.DesiredSeat{}, Farewell: []state.Farewell{}, Inbound: state.InboundAccept,
	}
	for _, c := range chosen {
		d.Seats = append(d.Seats, state.DesiredSeat{Team: c.Team, Seat: c.Seat, Duty: c.Duty})
	}
	return d
}

// RunArgs is what podman run is given for a v2 session: the session's seat directory
// read-write and its control directory read-only (relabelled private on SELinux), the two
// variables that make ccy-start-claude start a teamchat member, and the stop timeout.
func RunArgs(store paths.Store, session paths.SessionName, relabel bool) []string {
	z := ""
	if relabel {
		z = ",Z"
	}
	return []string{
		"-v", store.Session(session) + ":" + paths.SessionSeatMount + ":rw" + z,
		"-v", store.Control(session) + ":" + MembershipMount + ":ro" + z,
		"-e", "PINGBUS_MEMBERSHIP=off", "-e", "TEAMCHAT_MEMBERSHIP=" + MembershipMount,
		"--stop-timeout", stopTimeout,
	}
}

// Taker takes every seat of a launch, under one allocation lock, or none: a take that fails
// releases the ones before it.
type Taker interface {
	TakeAll(ctx context.Context, takes []seat.Take) error
}

// Launch is one v2 launch's take.
type Launch struct {
	Store    paths.Store
	Checkout paths.CheckoutKey
	// Kind is the launcher's kind of seat (seatchoice.KindPodman for ccy, KindHost for cc): the
	// only pool seats the launch takes. The compiler does not hold it: Run and OpenHotSeats
	// refuse any other value (ErrKind), so a Launch built without it fails at once.
	Kind   string
	Taker  Taker
	Status func(paths.TeamID) (status.Record, bool, error)
	NowMS  func() int64
}

// Run guards the mounts, takes the seats for session, writes desired.json and returns the run
// arguments. Nothing is taken when the guard or a seat refuses; nothing is written when a take
// fails.
func (l *Launch) Run(ctx context.Context, session paths.SessionName, list string, mounts []string, relabel bool) ([]string, error) {
	if err := l.checkKind(); err != nil {
		return nil, err
	}
	if err := Guard(l.Store.Root(), MountSources(mounts)); err != nil {
		return nil, err
	}
	chosen, takes, err := l.choose(list)
	if err != nil {
		return nil, err
	}
	for i := range takes {
		takes[i].Session = session
	}
	if err = l.Taker.TakeAll(ctx, takes); err != nil {
		if errors.Is(err, seat.ErrNoRecord) {
			return nil, fmt.Errorf("%w; the seat sweep releases a seat whose session has no record, so start the session with ccy (or cc) at a terminal outside ccy's tmux, which writes the record, or declare it with ccy-sessions declare and start it with ccy-sessions restore", err)
		}
		return nil, err
	}
	if err = WriteDesired(l.Store, firstGeneration(session, chosen, l.NowMS())); err != nil {
		return nil, err
	}
	return RunArgs(l.Store, session, relabel), nil
}

// checkKind refuses a Launch whose Kind is not a launcher's kind of seat, naming the kind given.
func (l *Launch) checkKind() error {
	if l.Kind == seatchoice.KindPodman || l.Kind == seatchoice.KindHost {
		return nil
	}
	return fmt.Errorf("%w (%q; want %q for ccy or %q for cc)", ErrKind, l.Kind, seatchoice.KindPodman, seatchoice.KindHost)
}

// choose reads the canonical list and finds each seat in the pool.
func (l *Launch) choose(list string) ([]Chosen, []seat.Take, error) {
	seats, err := membership.ParseSeats(list)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: the seats %q: %w", ErrSeat, list, err)
	}
	if len(seats) == 0 {
		return nil, nil, fmt.Errorf("%w: a teamchat launch names its seats (<seat>@<team>)", ErrSeat)
	}
	var chosen []Chosen
	var takes []seat.Take
	for _, s := range seats {
		team, err := paths.ParseTeamID(s.Team)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %w", ErrSeat, err)
		}
		pool, err := system.PoolSeats(l.Store, l.Checkout, team)
		if err != nil {
			return nil, nil, err
		}
		name, err := ChooseSeat(pool, s.Team, s.Seat, l.Kind)
		if err != nil {
			return nil, nil, fmt.Errorf("team %s: %w", s.Team, err)
		}
		rec, found, err := l.Status(team)
		if err != nil {
			return nil, nil, fmt.Errorf("read the status record of team %s: %w", s.Team, err)
		}
		st, err := HandleState(rec, found, name)
		if err != nil {
			return nil, nil, fmt.Errorf("team %s: %w", s.Team, err)
		}
		chosen = append(chosen, Chosen{Team: team, Name: name, Seat: s.Seat, Duty: SeatDuty(rec, found, name)})
		takes = append(takes, seat.Take{Seat: seat.Seat{Team: team, Name: name}, State: st})
	}
	return chosen, takes, nil
}
