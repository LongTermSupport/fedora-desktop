package conversation

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccyenv"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/transcript"
)

// Seated is the conversation a ccy launch of a seat list in a checkout resumes (issues 409,
// 417): the one every named seat with a pointer agrees on, the session whose record gave it
// ("" when it was set by hand), and the named seats with no pointer of their own, which the
// launched session records.
type Seated struct {
	Session    string
	ID         string
	Unrecorded []string
}

// ErrNoSeatConversation is ForSeats' refusal when the seats have no conversation to resume: none
// is recorded, the sessions that held them recorded none, or the agreed one has no transcript.
var ErrNoSeatConversation = errors.New("no recorded conversation")

// ErrSeatsDisagree is ForSeats' refusal when the named seats point at different conversations.
var ErrSeatsDisagree = errors.New("the seats' records disagree")

// Seats is a lookup of a seat list in a checkout: the pointer of each named seat that has one
// (in the list's order), the named seats with none, and the sessions that held a named seat but
// gave no usable record (each with its reason).
type Seats struct {
	Canonical  string
	Pointers   []SeatPointer
	Unrecorded []string
	checkout   string
	unusable   []session
	transcript string
}

// session is one session's records in the checkout: the seats it holds (seat@team), and what
// its conversation record gives, or why it gives nothing.
type session struct {
	name  string
	seats []string
	conv  entry
	err   error
}

// parseList is a --teams list's seats and canonical form, spaces dropped.
func parseList(list string) ([]membership.Seat, string, error) {
	seats, err := membership.ParseSeats(strings.Join(strings.Fields(list), ""))
	if err != nil {
		return nil, "", fmt.Errorf("the seats %q: %w", list, err)
	}
	if len(seats) == 0 {
		return nil, "", errors.New("no seat is named: give <seat>@<team>[,<seat>@<team>...]")
	}
	return seats, membership.Canonical(seats), nil
}

// ForSeats is the conversation a launch of list in checkout resumes: LookupSeats, then Resolve.
func ForSeats(checkout, list string, where Where) (Seated, error) {
	s, err := LookupSeats(checkout, list, where)
	if err != nil {
		return Seated{}, err
	}
	return s.Resolve()
}

// LookupSeats is each named seat's pointer, after bringing the checkout's seat index up to date
// (the harvest): every session of the system list's teams are on (agent-bus: the keepers' reports
// in the checkout; teamchat: the desired memberships in the owner's seat store) whose record
// reads as a restore reads it, transcript in this checkout included, sets the pointer of every
// seat it holds when its record is newer than the pointer. A seat is held by one session at a
// time, so the newest record is its latest use; and the pointer outlives the session's folder,
// which a later launch of other seats reuses. A pointer that cannot be read is refused, naming
// its file. It never falls back to the checkout's newest transcript.
func LookupSeats(checkout, list string, where Where) (Seats, error) {
	seats, canonical, err := parseList(list)
	if err != nil {
		return Seats{}, err
	}
	base := registry.Record{Dir: checkout, Prefix: membership.KindCcy, Args: []string{ccyenv.FlagTeams, canonical}}
	sys, err := systemOf(base, checkout, where.Getenv)
	if err != nil {
		return Seats{}, err
	}
	var all []session
	if sys == system.Teamchat {
		all, err = v2Sessions(where.Getenv)
	} else {
		all, err = v1Sessions(checkout)
	}
	if err != nil {
		return Seats{}, err
	}
	pointers, err := harvest(checkout, newestRecords(all, base, where))
	if err != nil {
		return Seats{}, err
	}
	dir, err := transcriptDir(checkout, where)
	if err != nil {
		return Seats{}, err
	}
	out := Seats{Canonical: canonical, checkout: checkout, transcript: dir}
	for _, s := range seats {
		name := s.Seat + "@" + s.Team
		p, ok := pointers[name]
		if !ok {
			if p, ok, err = readPointer(checkout, name); err != nil {
				return Seats{}, err
			}
		}
		if ok {
			out.Pointers = append(out.Pointers, p)
		} else {
			out.Unrecorded = append(out.Unrecorded, name)
		}
	}
	for _, s := range all {
		if s.err != nil && slices.ContainsFunc(s.seats, func(seat string) bool { return strings.Contains(","+canonical+",", ","+seat+",") }) {
			out.unusable = append(out.unusable, s)
		}
	}
	return out, nil
}

// newestRecords reads every session's record into all (its conversation, or why it gives none)
// and gives, for each seat a session with a usable record holds, the newest such record.
func newestRecords(all []session, base registry.Record, where Where) map[string]SeatPointer {
	newest := map[string]SeatPointer{}
	for i := range all {
		rec := base
		rec.Name = all[i].name
		all[i].conv, all[i].err = readRecorded(rec, where)
		if all[i].err != nil {
			continue
		}
		for _, seat := range all[i].seats {
			p := SeatPointer{Seat: seat, ID: all[i].conv.id, Session: all[i].name, Source: sourceSession, WrittenMS: all[i].conv.writtenMS}
			if cur, ok := newest[seat]; !ok || p.WrittenMS > cur.WrittenMS {
				newest[seat] = p
			}
		}
	}
	return newest
}

// harvest writes each seat's pointer from newest where the session record is newer than the
// pointer, and gives every seat's pointer after it.
func harvest(checkout string, newest map[string]SeatPointer) (map[string]SeatPointer, error) {
	names := make([]string, 0, len(newest))
	for name := range newest {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make(map[string]SeatPointer, len(names))
	for _, name := range names {
		cur, found, err := readPointer(checkout, name)
		if err != nil {
			return nil, err
		}
		if found && cur.WrittenMS >= newest[name].WrittenMS {
			out[name] = cur
			continue
		}
		if err := writePointer(checkout, newest[name]); err != nil {
			return nil, err
		}
		out[name] = newest[name]
	}
	return out, nil
}

// Resolve is the conversation the named seats agree on. Refused: no named seat has a pointer
// (no session held one, or the sessions that did recorded none: ErrNoSeatConversation); the
// pointers disagree (ErrSeatsDisagree, each seat named with its record); the agreed conversation
// has no transcript on this host (ErrNoSeatConversation, naming the path it was expected at).
func (s Seats) Resolve() (Seated, error) {
	if len(s.Pointers) == 0 {
		if len(s.unusable) == 0 {
			return Seated{}, fmt.Errorf("%w: no conversation is recorded for %s in %s: no session held these seats, and none was set by hand",
				ErrNoSeatConversation, s.Canonical, s.checkout)
		}
		lines := make([]string, 0, len(s.unusable))
		for _, u := range s.unusable {
			lines = append(lines, fmt.Sprintf("  %s (%s): %v", u.name, seatsOf(u), u.err))
		}
		return Seated{}, fmt.Errorf("%w: no session in %s that held %s has a conversation to resume:\n%s",
			ErrNoSeatConversation, s.checkout, s.Canonical, strings.Join(lines, "\n"))
	}
	agreed := s.Pointers[0]
	for _, p := range s.Pointers[1:] {
		if p.ID != agreed.ID {
			return Seated{}, fmt.Errorf("%w: the seats %s disagree on their conversation, so which to resume cannot be told:\n%s",
				ErrSeatsDisagree, s.Canonical, Describe(s.Pointers))
		}
		if p.WrittenMS > agreed.WrittenMS {
			agreed = p
		}
	}
	if _, err := transcript.For(s.transcript, agreed.ID); err != nil {
		return Seated{}, fmt.Errorf("%w: the conversation %s recorded for %s has no transcript on this host: %s is not there",
			ErrNoSeatConversation, agreed.ID, s.Canonical, filepath.Join(s.transcript, agreed.ID+".jsonl"))
	}
	return Seated{Session: agreed.Session, ID: agreed.ID, Unrecorded: s.Unrecorded}, nil
}

// Describe is a line per pointer: its seat, its conversation, where it came from and when.
func Describe(pointers []SeatPointer) string {
	lines := make([]string, 0, len(pointers))
	for _, p := range pointers {
		from := "recorded by session " + p.Session
		if p.Source == sourceSet {
			from = "set by ccy-teams seats conversation --set"
		}
		lines = append(lines, fmt.Sprintf("  %s: conversation %s, %s at %s", p.Seat, p.ID, from,
			time.UnixMilli(p.WrittenMS).UTC().Format(time.RFC3339)))
	}
	return strings.Join(lines, "\n")
}

func seatsOf(s session) string {
	if len(s.seats) == 0 {
		return "no seats"
	}
	return "seats " + strings.Join(s.seats, ",")
}

// v1Sessions is every session folder beside the checkout's seats, with the seats its keeper's
// report shows held (none when it wrote no report). A report that is not a keeper's is refused:
// the session it describes cannot be told.
func v1Sessions(checkout string) ([]session, error) {
	dir := membership.SessionDir(checkout, "")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not list the sessions of %s: %w", dir, err)
	}
	var out []session
	for _, e := range entries {
		if !e.IsDir() || !membership.ValidSession(e.Name()) {
			continue
		}
		s := session{name: e.Name()}
		report, err := membership.ReadReport(checkout, e.Name())
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return nil, err
		default:
			for _, held := range report.Seats {
				if report.Holds(held.Team, held.Seat) {
					s.seats = append(s.seats, held.Seat+"@"+held.Team)
				}
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// v2Sessions is every session folder of the owner's seat store's control/, with the seats its
// desired membership names. The seat store is the owner's for every checkout, so a session of
// another checkout is among them; its conversation has no transcript in this one, so its record
// never sets a pointer here.
func v2Sessions(getenv func(string) string) ([]session, error) {
	store, err := seatStore(getenv)
	if err != nil {
		return nil, err
	}
	control := store.Root() + "/control"
	entries, err := os.ReadDir(control)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not list %s: %w", control, err)
	}
	var out []session
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name, err := paths.ParseSessionName(e.Name())
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", control, e.Name(), err)
		}
		d, found, err := readDesired(store.DesiredFile(name), name)
		if err != nil {
			return nil, err
		}
		s := session{name: e.Name()}
		if found {
			for _, held := range d.Seats {
				s.seats = append(s.seats, held.Seat+"@"+held.Team.String())
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// readDesired is a teamchat session's desired.json, a regular file read capped; found is false
// when there is none.
func readDesired(path string, name paths.SessionName) (state.Desired, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return state.Desired{}, false, nil
	}
	if err != nil {
		return state.Desired{}, false, fmt.Errorf("could not read %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return state.Desired{}, false, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return state.Desired{}, false, fmt.Errorf("could not read %s: %w", path, err)
	}
	data, err := io.ReadAll(io.LimitReader(f, state.MaxDesired+1))
	if err = errors.Join(err, f.Close()); err != nil {
		return state.Desired{}, false, fmt.Errorf("could not read %s: %w", path, err)
	}
	d, err := state.DecodeDesired(data)
	if err != nil {
		return state.Desired{}, false, fmt.Errorf("%s: %w", path, err)
	}
	if d.Session != name {
		return state.Desired{}, false, fmt.Errorf("%s names the session %s", path, d.Session)
	}
	return d, true, nil
}
