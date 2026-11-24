package conversation

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
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
	held       []session
	transcript string
}

// session is one session's records in the checkout: the seats it holds (seat@team), what its
// conversation record gives, or why it gives nothing, and when it last spoke: its record's
// written_ms when that was read, else the time its report (desired membership) was written.
type session struct {
	name     string
	seats    []string
	conv     entry
	err      error
	reportMS int64
}

// ms is when the session last recorded anything.
func (s session) ms() int64 {
	if s.conv.writtenMS > 0 {
		return s.conv.writtenMS
	}
	return s.reportMS
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

// LookupSeats is each named seat's pointer as the checkout's seat index would have it brought up
// to date (the harvest), computed without writing: every session of the system list's teams are
// on (agent-bus: the keepers' reports in the checkout; teamchat: the desired memberships in the
// owner's seat store) whose record reads as a restore reads it, transcript in this checkout
// included, moves the pointer of every seat it holds when its record is newer than the pointer.
// A seat is held by one session at a time, so the newest record is its latest use. It reads only
// what concerns the named seats: a named seat's pointer that cannot be read is refused, naming
// its file; a report, desired membership or pointer that cannot be read and does not name one is
// ignored (an unreadable report cannot be shown to hold a named seat). It never falls back to the
// checkout's newest transcript.
func LookupSeats(checkout, list string, where Where) (Seats, error) {
	return lookup(checkout, list, where, false)
}

// SaveSeats is LookupSeats that also writes the pointers it moves, under the index lock: what a
// launch runs, because the pointer must outlive the session's folder, which the launch of other
// seats it precedes may reuse.
func SaveSeats(checkout, list string, where Where) (Seats, error) {
	return lookup(checkout, list, where, true)
}

func lookup(checkout, list string, where Where, save bool) (Seats, error) {
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
	readRecords(all, base, where, sys == system.Teamchat)
	named := make(map[string]bool, len(seats))
	for _, s := range seats {
		named[s.Seat+"@"+s.Team] = true
	}
	pointers, err := harvestMaybeSaved(checkout, newestUsable(all), named, save)
	if err != nil {
		return Seats{}, err
	}
	dir, err := transcriptDir(checkout, where)
	if err != nil {
		return Seats{}, err
	}
	out := Seats{Canonical: canonical, checkout: checkout, transcript: dir}
	if err := out.fill(seats, pointers, newestUnusable(all)); err != nil {
		return Seats{}, err
	}
	for _, s := range all {
		if s.err == nil && len(s.seats) > 0 {
			out.held = append(out.held, s)
		}
	}
	return out, nil
}

// harvestMaybeSaved is harvest, under the index lock when it writes.
func harvestMaybeSaved(checkout string, newest map[string]SeatPointer, named map[string]bool, save bool) (map[string]SeatPointer, error) {
	if !save {
		return harvest(checkout, newest, named, false)
	}
	var pointers map[string]SeatPointer
	err := withIndexLock(checkout, func() error {
		var herr error
		pointers, herr = harvest(checkout, newest, named, true)
		return herr
	})
	return pointers, err
}

// fill puts each named seat's pointer (or the seat among the unrecorded) into s, and the newest
// session to hold it with no usable record when that is newer than its pointer.
func (s *Seats) fill(seats []membership.Seat, pointers map[string]SeatPointer, unusable map[string]session) error {
	for _, seat := range seats {
		name := seat.Seat + "@" + seat.Team
		p, ok := pointers[name]
		if !ok {
			var err error
			if p, ok, err = readPointer(s.checkout, name); err != nil {
				return err
			}
		}
		if ok {
			s.Pointers = append(s.Pointers, p)
		} else {
			s.Unrecorded = append(s.Unrecorded, name)
		}
		u, found := unusable[name]
		if found && (!ok || u.ms() > p.WrittenMS) && !slices.ContainsFunc(s.unusable, func(x session) bool { return x.name == u.name }) {
			s.unusable = append(s.unusable, u)
		}
	}
	return nil
}

// readRecords reads every session's record (its conversation, or why it gives none). A teamchat
// session whose conversation has no transcript here is another checkout's (the seat store is the
// owner's for every checkout): it holds none of this checkout's seats.
func readRecords(all []session, base registry.Record, where Where, teamchat bool) {
	for i := range all {
		rec := base
		rec.Name = all[i].name
		all[i].conv, all[i].err = readRecorded(rec, where)
		if teamchat && errors.Is(all[i].err, errNoTranscript) {
			all[i].seats = nil
		}
	}
}

// newestUsable is, for each seat a session with a usable record holds, the newest such record.
func newestUsable(all []session) map[string]SeatPointer {
	newest := map[string]SeatPointer{}
	for _, s := range all {
		if s.err != nil {
			continue
		}
		for _, seat := range s.seats {
			p := SeatPointer{Seat: seat, ID: s.conv.id, Session: s.name, Source: sourceSession, WrittenMS: s.conv.writtenMS}
			if cur, ok := newest[seat]; !ok || p.WrittenMS > cur.WrittenMS {
				newest[seat] = p
			}
		}
	}
	return newest
}

// newestUnusable is, for each seat a session with no usable record holds, the newest such session.
func newestUnusable(all []session) map[string]session {
	newest := map[string]session{}
	for _, s := range all {
		if s.err == nil {
			continue
		}
		for _, seat := range s.seats {
			if cur, ok := newest[seat]; !ok || s.ms() > cur.ms() {
				newest[seat] = s
			}
		}
	}
	return newest
}

// harvest moves each seat's pointer to newest where the session record is newer than the
// pointer, writing it when save, and gives every seat's pointer after it. A pointer that cannot
// be read is refused for a named seat and left alone for any other.
func harvest(checkout string, newest map[string]SeatPointer, named map[string]bool, save bool) (map[string]SeatPointer, error) {
	names := make([]string, 0, len(newest))
	for name := range newest {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make(map[string]SeatPointer, len(names))
	for _, name := range names {
		cur, found, err := readPointer(checkout, name)
		switch {
		case err != nil && named[name]:
			return nil, err
		case err != nil:
			continue
		case found && cur.WrittenMS >= newest[name].WrittenMS:
			out[name] = cur
			continue
		}
		if save {
			if err := writePointer(checkout, newest[name]); err != nil {
				return nil, err
			}
		}
		out[name] = newest[name]
	}
	return out, nil
}

// Resolve is the conversation the named seats agree on. Refused: the newest session to hold a
// named seat gave no usable record, newer than the seat's pointer (ErrNoSeatConversation, naming
// it: no older conversation is resumed in its place); no named seat has a pointer
// (ErrNoSeatConversation); the pointers disagree (ErrSeatsDisagree, each seat named with its
// record); the agreed conversation, recorded by a session, is recorded by more than one session
// that holds seats now (ErrSeatsDisagree: two sessions would resume one transcript); the agreed
// conversation has no transcript on this host (ErrNoSeatConversation, naming the path).
func (s Seats) Resolve() (Seated, error) {
	if len(s.unusable) > 0 {
		lines := make([]string, 0, len(s.unusable))
		for _, u := range s.unusable {
			lines = append(lines, fmt.Sprintf("  %s (%s): %v", u.name, seatsOf(u), u.err))
		}
		return Seated{}, fmt.Errorf("%w: the newest session in %s to hold a seat of %s has no conversation to resume, so no older one is resumed in its place:\n%s",
			ErrNoSeatConversation, s.checkout, s.Canonical, strings.Join(lines, "\n"))
	}
	if len(s.Pointers) == 0 {
		return Seated{}, fmt.Errorf("%w: no conversation is recorded for %s in %s: no session held these seats, and none was set by hand",
			ErrNoSeatConversation, s.Canonical, s.checkout)
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
	if same := s.holding(agreed.ID); !agreed.SetByHand() && len(same) > 1 {
		lines := make([]string, 0, len(same))
		for _, h := range same {
			lines = append(lines, fmt.Sprintf("  %s (%s): recorded %s", h.name, seatsOf(h), time.UnixMilli(h.conv.writtenMS).UTC().Format(time.RFC3339)))
		}
		return Seated{}, fmt.Errorf("%w: the conversation %s of %s is recorded by more than one session that holds seats now, so two sessions would resume one transcript; end one of them:\n%s",
			ErrSeatsDisagree, agreed.ID, s.Canonical, strings.Join(lines, "\n"))
	}
	if _, err := transcript.For(s.transcript, agreed.ID); err != nil {
		return Seated{}, fmt.Errorf("%w: the conversation %s recorded for %s has no transcript on this host: %s is not there",
			ErrNoSeatConversation, agreed.ID, s.Canonical, filepath.Join(s.transcript, agreed.ID+".jsonl"))
	}
	return Seated{Session: agreed.Session, ID: agreed.ID, Unrecorded: s.Unrecorded}, nil
}

// Recorded is whether a named seat has a pointer, or a session that held one gave a record
// that cannot be used: false is a first launch, with nothing to resume or refuse.
func (s Seats) Recorded() bool { return len(s.Pointers) > 0 || len(s.unusable) > 0 }

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

// holding is the sessions with a usable record of id that hold seats now. A session claims a
// seat by its report; a seat is held by one session at a time, so a session one of whose seats a
// newer session claims is gone and is not among them.
func (s Seats) holding(id string) []session {
	var out []session
	for _, h := range s.held {
		if h.conv.id != id {
			continue
		}
		gone := slices.ContainsFunc(s.held, func(o session) bool {
			return o.name != h.name && o.ms() > h.ms() && slices.ContainsFunc(o.seats, func(seat string) bool { return slices.Contains(h.seats, seat) })
		})
		if !gone {
			out = append(out, h)
		}
	}
	return out
}

func seatsOf(s session) string {
	if len(s.seats) == 0 {
		return "no seats"
	}
	return "seats " + strings.Join(s.seats, ",")
}

// v1Sessions is every session folder beside the checkout's seats, with the seats its keeper's
// report shows held and when the report was written (none when it wrote no report, or one that
// cannot be read: such a report cannot be shown to hold a named seat, so it is ignored).
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
		if report, err := membership.ReadReport(checkout, e.Name()); err == nil {
			for _, held := range report.Seats {
				if report.Holds(held.Team, held.Seat) {
					s.seats = append(s.seats, held.Seat+"@"+held.Team)
				}
			}
			s.reportMS = modifiedMS(membership.ReportPath(checkout, e.Name()))
		}
		out = append(out, s)
	}
	return out, nil
}

// v2Sessions is every session folder of the owner's seat store's control/, with the seats its
// desired membership names and when that was written. The seat store is the owner's for every
// checkout, so a session of another checkout is among them; its conversation has no transcript
// in this one, so its record never sets a pointer here. A folder that is not a session's, or a
// desired membership that cannot be read, cannot be shown to hold a named seat: ignored.
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
			continue
		}
		s := session{name: e.Name()}
		if d, found, err := readDesired(store.DesiredFile(name), name); err == nil && found {
			for _, held := range d.Seats {
				s.seats = append(s.seats, held.Seat+"@"+held.Team.String())
			}
			s.reportMS = modifiedMS(store.DesiredFile(name))
		}
		out = append(out, s)
	}
	return out, nil
}

// modifiedMS is when path was last written, in Unix milliseconds; a file that cannot be asked
// counts as the newest possible, so its session is never taken for an older one.
func modifiedMS(path string) int64 {
	info, err := os.Lstat(path)
	if err != nil {
		return math.MaxInt64
	}
	return info.ModTime().UnixMilli()
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
