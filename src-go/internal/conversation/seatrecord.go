package conversation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccyenv"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/transcript"
)

// The seat record (issue 417, the owner's ruling: the record is indexed per seat). A session's
// own record (Locate) lives only as long as the session's state, and a teamchat session's end
// removes that; the seat record is the checkout's, one file per seat, so a seat's conversation is
// known after its session has gone. It is written by the host only: the launch that resumes or
// starts the seats' conversation (`ccy-teams internal seat-resume`), the session's end, which
// carries the conversation it ended in (SyncSeats), and the owner's repair
// (`ccy-teams seats conversation <seats> --set <id>`).

// seatRecordDir is where the checkout keeps its seat records, under ccy's own ignored directory.
const seatRecordDir = ".claude/ccy/seat-conversations"

// The sources of a seat record: who wrote it.
const (
	// SourceLaunch is a plain seated launch that resumed the seats' conversation, or one named
	// with --resume <id> that agreed with it.
	SourceLaunch = "launch"
	// SourceFresh is a launch with --fresh: a new conversation with an id ccy chose.
	SourceFresh = "fresh"
	// SourceRebind is --resume <id> --rebind: the seats moved onto another conversation.
	SourceRebind = "rebind"
	// SourceRelaunch is a restore, a team restart or a supervisor's relaunch resuming the
	// session's own conversation.
	SourceRelaunch = "relaunch"
	// SourceSet is the owner's repair, ccy-teams seats conversation --set.
	SourceSet = "set"
	// SourceExit is the conversation the session ended in.
	SourceExit = "exit"
)

var seatSources = []string{SourceLaunch, SourceFresh, SourceRebind, SourceRelaunch, SourceSet, SourceExit}

// seatRecord is one seat's record, field for field.
type seatRecord struct {
	V         int    `json:"v"`
	Seat      string `json:"seat"`
	Team      string `json:"team"`
	SessionID string `json:"session_id"`
	Source    string `json:"source"`
	WrittenMS int64  `json:"written_ms"`
}

// SeatRecordPath is where checkout keeps the record of s.
func SeatRecordPath(checkout string, s membership.Seat) string {
	return filepath.Join(checkout, seatRecordDir, s.Team, s.Seat+".json")
}

// parseList is a --teams list of seats (spaces dropped, as agent-bus drops them), at least one.
func parseList(list string) ([]membership.Seat, error) {
	seats, err := membership.ParseSeats(strings.Join(strings.Fields(list), ""))
	if err != nil {
		return nil, fmt.Errorf("the seats %q: %w", list, err)
	}
	if len(seats) == 0 {
		return nil, errors.New("no seat is named: give <seat>@<team>[,<seat>@<team>...]")
	}
	return seats, nil
}

// RecordSeats points every seat of list at the conversation id, written by source at nowMS.
func RecordSeats(checkout, list, id, source string, nowMS int64) error {
	seats, err := parseList(list)
	if err != nil {
		return err
	}
	if !registry.SessionID(id) {
		return fmt.Errorf("%q is not a Claude session id", id)
	}
	if !slices.Contains(seatSources, source) {
		return fmt.Errorf("%q is not a seat record's source", source)
	}
	for _, s := range seats {
		data, err := json.Marshal(seatRecord{V: 1, Seat: s.Seat, Team: s.Team, SessionID: id, Source: source, WrittenMS: nowMS})
		if err != nil {
			return fmt.Errorf("the record of seat %s@%s: %w", s.Seat, s.Team, err)
		}
		path := SeatRecordPath(checkout, s)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("the seat records of %s: %w", checkout, err)
		}
		if err := fsx.WriteAtomic(path, "seat record", append(data, '\n'), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// readSeatRecord is the record of s in checkout; found is false when there is none.
func readSeatRecord(checkout string, s membership.Seat) (seatRecord, bool, error) {
	path := SeatRecordPath(checkout, s)
	data, err := readFile(path)
	if errors.Is(err, errNotRecorded) {
		return seatRecord{}, false, nil
	}
	if err != nil {
		return seatRecord{}, false, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var r seatRecord
	if err := dec.Decode(&r); err != nil || dec.More() {
		return seatRecord{}, false, fmt.Errorf("%s is not a seat record: %w", path, errors.Join(err, errNotARecord))
	}
	switch {
	case r.V != 1 || r.WrittenMS < 0 || !slices.Contains(seatSources, r.Source):
		return seatRecord{}, false, fmt.Errorf("%s is not a version 1 seat record", path)
	case r.Seat != s.Seat || r.Team != s.Team:
		return seatRecord{}, false, fmt.Errorf("%s names seat %s@%s", path, r.Seat, r.Team)
	case !registry.SessionID(r.SessionID):
		return seatRecord{}, false, fmt.Errorf("%s holds %q, which is not a Claude session id", path, r.SessionID)
	}
	return r, true, nil
}

// Case is which of the four ways a seat list's conversation cannot be named.
type Case string

// The four cases of issue 417.
const (
	CaseNoSession      Case = "no-session"
	CaseNoConversation Case = "no-conversation"
	CaseAmbiguous      Case = "ambiguous"
	CaseNoTranscript   Case = "no-transcript"
)

// SeatError is ForSeats' refusal in one of the four cases; Err says it.
type SeatError struct {
	Case Case
	Err  error
}

func (e *SeatError) Error() string { return e.Err.Error() }

func (e *SeatError) Unwrap() error { return e.Err }

// WaysOn is the two ways on from a refusal for list: repair the record, or start fresh on purpose.
func WaysOn(list string) string {
	return "  Repair: point the seats at their conversation: ccy-teams seats conversation " + list + " --set <id>\n" +
		"  Fresh:  start a new conversation on purpose:    ccy --teams " + list + " --fresh"
}

// TranscriptOf is the ccy transcript of the conversation id in checkout, refused when it is not
// there, naming the path Claude Code reads it from.
func TranscriptOf(checkout, id string) (string, error) {
	dir, err := transcript.Dir(membership.KindCcy, checkout, "")
	if err != nil {
		return "", err
	}
	path, err := transcript.For(dir, id)
	if err != nil {
		return "", fmt.Errorf("the conversation %s has no transcript on this host: %s is not there", id, filepath.Join(dir, id+".jsonl"))
	}
	return path, nil
}

// fromSeatRecords is ForSeats from the seats' own records; ok is false when no named seat has
// one.
func fromSeatRecords(checkout, canonical string, seats []membership.Seat) (Seated, bool, error) {
	var (
		got        Seated
		recorded   []string
		unrecorded []string
	)
	ids := map[string]bool{}
	for _, s := range seats {
		r, found, err := readSeatRecord(checkout, s)
		if err != nil {
			return Seated{}, true, err
		}
		item := s.Seat + "@" + s.Team
		if !found {
			unrecorded = append(unrecorded, item)
			continue
		}
		ids[r.SessionID] = true
		got.ID = r.SessionID
		recorded = append(recorded, fmt.Sprintf("  %s: conversation %s (%s)", item, r.SessionID, r.Source))
	}
	switch {
	case len(recorded) == 0:
		return Seated{}, false, nil
	case len(ids) > 1:
		return Seated{}, true, &SeatError{Case: CaseAmbiguous, Err: fmt.Errorf(
			"the records of the seats %s in %s disagree on their conversation, so which is theirs cannot be told:\n%s",
			canonical, checkout, strings.Join(recorded, "\n"))}
	}
	if _, err := TranscriptOf(checkout, got.ID); err != nil {
		return Seated{}, true, &SeatError{Case: CaseNoTranscript, Err: fmt.Errorf("the seats %s recorded a conversation that cannot be resumed: %w", canonical, err)}
	}
	got.Unrecorded = unrecorded
	return got, true, nil
}

// Synced is what SyncSession carried: the conversation and the seats it now names.
type Synced struct {
	ID, List string
}

// SyncSession is SyncSeats for the seats session holds by its own state, read before the session's
// end removes it: its teamchat desired membership, else its v1 keeper's report. A session that
// holds no seat is ErrNothingToSync.
func SyncSession(checkout, session string, where Where, nowMS int64) (Synced, error) {
	list, err := sessionSeats(checkout, session, where.Getenv)
	if err != nil {
		return Synced{}, err
	}
	if list == "" {
		return Synced{}, fmt.Errorf("%w (session %s holds no seat)", ErrNothingToSync, session)
	}
	id, err := SyncSeats(checkout, session, list, where, nowMS)
	return Synced{ID: id, List: list}, err
}

// sessionSeats is the seat list session's own state names, "" for none.
func sessionSeats(checkout, session string, getenv func(string) string) (string, error) {
	var items []string
	if name, err := paths.ParseSessionName(session); err == nil {
		store, storeErr := seatStore(getenv)
		if storeErr != nil {
			return "", storeErr
		}
		d, found, readErr := readDesired(store.DesiredFile(name), name)
		if readErr != nil {
			return "", readErr
		}
		for _, s := range d.Seats {
			items = append(items, s.Seat+"@"+s.Team.String())
		}
		if found && len(items) > 0 {
			return strings.Join(items, ","), nil
		}
	}
	report, err := membership.ReadReport(checkout, session)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for _, held := range report.Seats {
		items = append(items, held.Seat+"@"+held.Team)
	}
	return strings.Join(items, ","), nil
}

// ErrNothingToSync is SyncSeats' answer when the session recorded no conversation it can resume.
var ErrNothingToSync = errors.New("the session recorded no conversation to carry into its seats")

// SyncSeats carries the conversation session ended in (its own record, read as a restore reads it)
// into the records of the seats of list it held, and is that id. A session with no conversation
// (none recorded, or one that never took a turn) is ErrNothingToSync, and the seats' records stay
// as they were.
func SyncSeats(checkout, session, list string, where Where, nowMS int64) (string, error) {
	seats, err := parseList(list)
	if err != nil {
		return "", err
	}
	canonical := membership.Canonical(seats)
	rec := registry.Record{Name: session, Dir: checkout, Prefix: membership.KindCcy, Args: []string{ccyenv.FlagTeams, canonical}}
	r, err := readRecorded(rec, where)
	if errors.Is(err, errNotRecorded) || errors.Is(err, errNoTranscript) {
		return "", fmt.Errorf("%w (%w)", ErrNothingToSync, err)
	}
	if err != nil {
		return "", fmt.Errorf("the conversation of session %s: %w", session, err)
	}
	if err := RecordSeats(checkout, canonical, r.id, SourceExit, nowMS); err != nil {
		return "", err
	}
	return r.id, nil
}
