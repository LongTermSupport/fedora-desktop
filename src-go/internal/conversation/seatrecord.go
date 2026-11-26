package conversation

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccyenv"
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

// seatRecordsLocked, when set (tests only), is called with the lock file's path once a writer
// holds the checkout's seat-record lock, before it reads or writes any record.
var seatRecordsLocked func(lockPath string)

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
	return filepath.Join(checkout, seatRecordRel(s))
}

// seatRecordRel is the record of s from the checkout's top.
func seatRecordRel(s membership.Seat) string {
	return filepath.Join(seatRecordDir, s.Team, s.Seat+".json")
}

// parseList is a --teams list of seats (spaces dropped, as agent-bus drops them), at least one.
func parseList(list string) ([]membership.Seat, error) {
	seats, err := membership.ParseSeats(strings.Join(strings.Fields(list), ""))
	if err != nil {
		return nil, fmt.Errorf("the seats %q: %w", list, err)
	}
	if len(seats) == 0 {
		return nil, errors.New("no seat is named: give the teams (ccy --teams <team>)")
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
	// Everything is reached through the checkout as an os.Root: a container can write the
	// checkout, and a link it planted would otherwise steer the host's writes out of it.
	root, err := os.OpenRoot(checkout)
	if err != nil {
		return fmt.Errorf("the seat records of %s: %w", checkout, err)
	}
	return errors.Join(recordSeatsIn(root, checkout, seats, id, source, nowMS), root.Close())
}

// recordSeatsIn writes each seat's record under the checkout's seat-record lock, every path
// resolved inside root.
func recordSeatsIn(root *os.Root, checkout string, seats []membership.Seat, id, source string, nowMS int64) error {
	if err := root.MkdirAll(seatRecordDir, 0o700); err != nil {
		return fmt.Errorf("the seat records of %s: %w", checkout, err)
	}
	lock, err := openSeatRecordLock(root, checkout)
	if err != nil {
		return err
	}
	return withSeatRecordLock(lock, func() error {
		for _, s := range seats {
			data, err := json.Marshal(seatRecord{V: 1, Seat: s.Seat, Team: s.Team, SessionID: id, Source: source, WrittenMS: nowMS})
			if err != nil {
				return fmt.Errorf("the record of seat %s@%s: %w", s.Seat, s.Team, err)
			}
			rel := seatRecordRel(s)
			if err := root.MkdirAll(filepath.Dir(rel), 0o700); err != nil {
				return fmt.Errorf("the seat records of %s: %w", checkout, err)
			}
			if err := writeInRoot(root, rel, append(data, '\n')); err != nil {
				return fmt.Errorf("could not write the record of seat %s@%s in %s: %w", s.Seat, s.Team, checkout, err)
			}
		}
		return nil
	})
}

// writeInRoot writes rel whole inside root: an exclusively created temporary beside it under a
// random name, renamed into place, so no reader sees half of it and a planted name is never
// opened.
func writeInRoot(root *os.Root, rel string, data []byte) error {
	tmp := rel + ".tmp." + rand.Text()
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err = errors.Join(err, f.Close()); err == nil {
		err = root.Rename(tmp, rel)
	}
	if err != nil {
		if rmErr := root.Remove(tmp); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			err = errors.Join(err, rmErr)
		}
	}
	return err
}

// seatRecordLock is the checkout's seat-record lock file, in its seat-record directory.
const seatRecordLock = ".lock"

// seatRecordLockWait is how long a writer waits for the seat-record lock before it fails naming
// it: a launch and a remove hold it only while they write a few small files, so a lock held this
// long is held by something stuck (or by a container), and waiting on would hang the command.
const seatRecordLockWait = 30 * time.Second

// seatRecordLockPoll is how often a waiting writer tries the lock again.
const seatRecordLockPoll = 100 * time.Millisecond

// lockClock is the clock the wait for the seat-record lock reads; a test gives a fake one.
var lockClock interface {
	Now() time.Time
	Sleep(d time.Duration)
} = realLockClock{}

// realLockClock is the wall clock.
type realLockClock struct{}

func (realLockClock) Now() time.Time { return time.Now() }

func (realLockClock) Sleep(d time.Duration) { time.Sleep(d) }

// withSeatRecordLock runs fn holding flock(2) on lock, the checkout's seat-record lock (the old
// #417 branch's lock, 4001e4789): every writer of seat records takes it, so a launch's seats are
// written together and a team's removal never lands between them. lock is closed, which releases
// it.
func withSeatRecordLock(lock *os.File, fn func() error) error {
	if err := takeSeatRecordLock(lock); err != nil {
		return errors.Join(err, lock.Close())
	}
	if seatRecordsLocked != nil {
		seatRecordsLocked(lock.Name())
	}
	return errors.Join(fn(), lock.Close())
}

// openSeatRecordLock opens the checkout's seat-record lock inside root, refusing a lock that is
// not a plain file. An os.Root follows a link at the last name that stays inside it (O_NOFOLLOW
// is not honoured there), so the file opened is compared with what the name itself is.
func openSeatRecordLock(root *os.Root, checkout string) (*os.File, error) {
	rel := filepath.Join(seatRecordDir, seatRecordLock)
	// An os.Root follows a link at the last name, so the lock is created exclusively (which
	// refuses any link there) and one already there is opened without O_CREATE (a dangling link
	// is then an error, never a file made where it points).
	f, err := root.OpenFile(rel, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		f, err = root.OpenFile(rel, os.O_RDWR, 0)
	}
	if err != nil {
		return nil, fmt.Errorf("could not open the seat-record lock of %s: %w", checkout, err)
	}
	named, lerr := root.Lstat(rel)
	opened, serr := f.Stat()
	if err := errors.Join(lerr, serr); err != nil {
		return nil, errors.Join(fmt.Errorf("could not open the seat-record lock of %s: %w", checkout, err), f.Close())
	}
	if !named.Mode().IsRegular() || !os.SameFile(named, opened) {
		return nil, errors.Join(fmt.Errorf("could not open the seat-record lock of %s: %s is not a plain file (a link is never followed)",
			checkout, filepath.Join(checkout, rel)), f.Close())
	}
	return f, nil
}

// takeSeatRecordLock takes flock(2) on lock without blocking, trying again every
// seatRecordLockPoll; a lock still held after seatRecordLockWait is an error naming it.
func takeSeatRecordLock(lock *os.File) error {
	deadline := lockClock.Now().Add(seatRecordLockWait)
	for {
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		switch {
		case err == nil:
			return nil
		case !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR):
			return fmt.Errorf("could not lock %s: %w", lock.Name(), err)
		case !lockClock.Now().Before(deadline):
			return fmt.Errorf("the seat-record lock %s is still held after %s, so nothing was written: another launch or ccy-teams remove "+
				"in this checkout is stuck holding it; end it, then try again", lock.Name(), seatRecordLockWait)
		}
		lockClock.Sleep(seatRecordLockPoll)
	}
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
// Both name the list's teams, never its seats (Plan 00265 R90): the launch finds the seat.
func WaysOn(list string) string {
	launch := membership.Launch("ccy", list)
	return "  Repair: resume the conversation and point the seats at it: " + launch + " --resume <id> --rebind\n" +
		"  Fresh:  start a new conversation on purpose:              " + launch + " --fresh"
}

// SeatsOfTeams is list with each item that is a team alone replaced by the one seat of it whose
// conversation checkout records (Plan 00265 R90: a person names the team, never the seat). A team
// with no record here, or with several, is refused in words.
func SeatsOfTeams(checkout, list string) (string, error) {
	var items []string
	for item := range strings.SplitSeq(list, ",") {
		item = strings.Join(strings.Fields(item), "")
		if item == "" || strings.Contains(item, "@") {
			items = append(items, item)
			continue
		}
		seat, err := recordedSeatOf(checkout, item)
		if err != nil {
			return "", err
		}
		items = append(items, seat+"@"+item)
	}
	return strings.Join(items, ","), nil
}

// recordedSeatOf is the one seat of team with a seat record in checkout.
func recordedSeatOf(checkout, team string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(checkout, seatRecordDir, team))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("the seat records of team %s: %w", team, err)
	}
	var seats []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".json"); ok && e.Type().IsRegular() {
			seats = append(seats, name)
		}
	}
	switch len(seats) {
	case 0:
		return "", fmt.Errorf("no seat of team %s has a conversation recorded in this checkout; start it with ccy --teams %s", team, team)
	case 1:
		return seats[0], nil
	}
	return "", fmt.Errorf("team %s has %d seats with a conversation recorded in this checkout; resume the one you mean with ccy --teams %s --resume <id> --rebind, which asks which seat", team, len(seats), team)
}

// NeverUsed is whether the seats of list were never used in checkout: no named seat has a record
// and no session held them all (CaseNoSession), so a plain launch naming them is refused and a
// first launch says --fresh. Any other refusal of ForSeats means they were used, and is the
// launch's to say; a record or state that cannot be read is an error.
func NeverUsed(checkout, list string, where Where) (bool, error) {
	_, err := ForSeats(checkout, list, where)
	var seatErr *SeatError
	switch {
	case errors.As(err, &seatErr):
		return seatErr.Case == CaseNoSession, nil
	case err != nil:
		return false, err
	}
	return false, nil
}

// SeatLastRan is when s last ran in checkout, in Unix milliseconds: its seat record's time, while
// that conversation's transcript is here. 0 is a seat with no conversation to resume: no record,
// or one whose transcript is gone. A record that cannot be read is an error.
func SeatLastRan(checkout string, s membership.Seat) (int64, error) {
	r, found, err := readSeatRecord(checkout, s)
	if err != nil || !found {
		return 0, err
	}
	dir, err := transcript.Dir(membership.KindCcy, checkout, "")
	if err != nil {
		return 0, err
	}
	fi, err := os.Stat(filepath.Join(dir, r.SessionID+".jsonl"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("the transcript of seat %s@%s's conversation: %w", s.Seat, s.Team, err)
	case !fi.Mode().IsRegular():
		return 0, nil
	}
	return r.WrittenMS, nil
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

// Synced is what SyncSession carried: the conversation and the seats it now names, or why nothing
// was carried (Nothing, with ID empty), which is an answer and not a failure.
type Synced struct {
	ID, List string
	Nothing  string
}

// SyncSession is SyncSeats for the seats session holds by its own state, read before the session's
// end removes it: its teamchat desired membership, else its v1 keeper's report. A session that
// holds no seat, or recorded no conversation it can resume, carries nothing and says why in
// Synced.Nothing; an error is only a state or record that could not be read or written.
func SyncSession(checkout, session string, where Where, nowMS int64) (Synced, error) {
	list, err := sessionSeats(checkout, session, where.Getenv)
	if err != nil {
		return Synced{}, err
	}
	if list == "" {
		return Synced{Nothing: fmt.Sprintf("session %s holds no seat", session)}, nil
	}
	id, err := SyncSeats(checkout, session, list, where, nowMS)
	if errors.Is(err, errNothingToSync) {
		return Synced{List: list, Nothing: err.Error()}, nil
	}
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

// errNothingToSync is SyncSeats' answer when the session recorded no conversation it can resume.
// It stays in this package: a caller reads SyncSession's Synced.Nothing.
var errNothingToSync = errors.New("the session recorded no conversation to carry into its seats")

// SyncSeats carries the conversation session ended in (its own record, read as a restore reads it)
// into the records of the seats of list it held, and is that id. A session with no conversation
// (none recorded, or one that never took a turn) is errNothingToSync, and the seats' records stay
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
		return "", fmt.Errorf("%w (%w)", errNothingToSync, err)
	}
	if err != nil {
		return "", fmt.Errorf("the conversation of session %s: %w", session, err)
	}
	if err := RecordSeats(checkout, canonical, r.id, SourceExit, nowMS); err != nil {
		return "", err
	}
	return r.id, nil
}
