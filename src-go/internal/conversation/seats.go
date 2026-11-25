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
)

// Seated is the conversation a ccy launch of a seat list in a checkout resumes (issue 409, issue
// 417): the one the seats' records name, or, when no named seat has a record, the one recorded by
// the session that held those seats (Session). Unrecorded is the named seats with no record of
// their own, which the launch records.
type Seated struct {
	Session    string
	ID         string
	Unrecorded []string
}

// ErrNoSeatConversation is ForSeats' refusal when no session that held the seats has a
// conversation to resume (none held them, or none recorded one), as opposed to a choice it will
// not make (two sessions, or one id recorded twice).
var ErrNoSeatConversation = errors.New("no recorded conversation")

// session is one session's records in the checkout: the seats its record names (seat@team), and
// what its conversation record gives, or why it gives nothing.
type session struct {
	name     string
	seats    []string
	holdsAll bool
	conv     entry
	err      error
}

// ForSeats is the conversation the seats of list in checkout resume. Their own records answer
// first (issue 417): every named seat with a record must name one id, whose transcript is here; a
// named seat with none is allowed when another has one, and is listed in Unrecorded. With no seat
// record at all, the session records answer as before: the ccy session in checkout that held every
// seat of list, whose keeper's report (agent-bus) or desired membership (teamchat) names them all,
// its record read through Locate as a restore reads it, transcript included. It never falls back
// to the checkout's newest transcript. Each refusal is a *SeatError of one of the four cases: no
// session held the seats, or none recorded a conversation (both ErrNoSeatConversation); the
// records disagree (seats with different ids, more than one session with a record, or an id
// another session of the checkout records too, each named with when it was written, the newest
// marked but never chosen); or the recorded conversation has no transcript.
func ForSeats(checkout, list string, where Where) (Seated, error) {
	seats, err := parseList(list)
	if err != nil {
		return Seated{}, err
	}
	canonical := membership.Canonical(seats)
	if got, ok, err := fromSeatRecords(checkout, canonical, seats); ok || err != nil {
		return got, err
	}
	base := registry.Record{Dir: checkout, Prefix: membership.KindCcy, Args: []string{ccyenv.FlagTeams, canonical}}
	sys, err := systemOf(base, checkout, where.Getenv)
	if err != nil {
		return Seated{}, err
	}
	var all []session
	if sys == system.Teamchat {
		all, err = v2Sessions(seats, where.Getenv)
	} else {
		all, err = v1Sessions(checkout, seats)
	}
	if err != nil {
		return Seated{}, err
	}
	for i := range all {
		rec := base
		rec.Name = all[i].name
		all[i].conv, all[i].err = readRecorded(rec, where)
	}
	got, err := choose(checkout, canonical, all)
	if err == nil {
		got.Unrecorded = strings.Split(canonical, ",")
	}
	return got, err
}

// choose is ForSeats' verdict over every session of the checkout.
func choose(checkout, canonical string, all []session) (Seated, error) {
	var holding, found []session
	for _, s := range all {
		if !s.holdsAll {
			continue
		}
		holding = append(holding, s)
		if s.err == nil {
			found = append(found, s)
		}
	}
	switch {
	case len(holding) == 0:
		return Seated{}, &SeatError{Case: CaseNoSession, Err: fmt.Errorf("%w: no session record in %s holds %s", ErrNoSeatConversation, checkout, canonical)}
	case len(found) == 0:
		lines := make([]string, 0, len(holding))
		untaken := CaseNoTranscript
		for _, s := range holding {
			lines = append(lines, fmt.Sprintf("  %s (%s): %v", s.name, seatsOf(s), s.err))
			if !errors.Is(s.err, errNoTranscript) {
				untaken = CaseNoConversation
			}
		}
		return Seated{}, &SeatError{Case: untaken, Err: fmt.Errorf("%w: no session in %s that held %s has a conversation to resume:\n%s",
			ErrNoSeatConversation, checkout, canonical, strings.Join(lines, "\n"))}
	case len(found) > 1:
		return Seated{}, &SeatError{Case: CaseAmbiguous, Err: fmt.Errorf("more than one session in %s held %s, each with its own conversation, so which is the seat's cannot be told:\n%s",
			checkout, canonical, describe(found))}
	}
	same := slices.DeleteFunc(slices.Clone(all), func(s session) bool { return s.err != nil || s.conv.id != found[0].conv.id })
	if len(same) > 1 {
		return Seated{}, &SeatError{Case: CaseAmbiguous, Err: fmt.Errorf("the conversation %s is recorded by more than one session in %s, so at least one record is stale and which session it is cannot be told; it is not chosen for you:\n%s",
			found[0].conv.id, checkout, describe(same))}
	}
	return Seated{Session: found[0].name, ID: found[0].conv.id}, nil
}

// describe is a line per session: its seats, its conversation and when the record was written,
// the newest marked.
func describe(sessions []session) string {
	var newest int64
	for _, s := range sessions {
		newest = max(newest, s.conv.writtenMS)
	}
	lines := make([]string, 0, len(sessions))
	for _, s := range sessions {
		line := fmt.Sprintf("  %s (%s): conversation %s, recorded %s", s.name, seatsOf(s), s.conv.id,
			time.UnixMilli(s.conv.writtenMS).UTC().Format(time.RFC3339))
		if s.conv.writtenMS == newest {
			line += " (the newest)"
		}
		lines = append(lines, line)
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
func v1Sessions(checkout string, want []membership.Seat) ([]session, error) {
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
				s.seats = append(s.seats, held.Seat+"@"+held.Team)
			}
			s.holdsAll = !slices.ContainsFunc(want, func(w membership.Seat) bool { return !report.Holds(w.Team, w.Seat) })
		}
		out = append(out, s)
	}
	return out, nil
}

// v2Sessions is every session folder of the owner's seat store's control/, with the seats its
// desired membership names. The seat store is the owner's for every checkout, so a session of
// another checkout is among them; its conversation has no transcript in this one, so it is
// never an answer.
func v2Sessions(want []membership.Seat, getenv func(string) string) ([]session, error) {
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
			s.holdsAll = !slices.ContainsFunc(want, func(w membership.Seat) bool {
				team, err := paths.ParseTeamID(w.Team)
				return err != nil || !d.Holds(team, w.Seat)
			})
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
