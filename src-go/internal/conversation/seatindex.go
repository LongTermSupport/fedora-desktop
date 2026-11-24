package conversation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/transcript"
)

// seatIndexDir is where a checkout keeps each seat's conversation pointer (issue 417): one file
// per seat, `<seat>@<team>.json`. Only ccy-teams writes it, whole (fsx.WriteAtomic); the records
// the running sessions write are only read, so no session's writer races it.
const seatIndexDir = ".claude/ccy/seat-conversations"

// The sources of a pointer: harvested from a session's record, or set by hand
// (`ccy-teams seats conversation <seats> --set <id>`).
const (
	sourceSession = "session"
	sourceSet     = "set"
)

// SeatPointer is a seat's conversation: its id, the session whose record gave it ("" for one set
// by hand), where it came from and when that record was written.
type SeatPointer struct {
	Seat      string
	ID        string
	Session   string
	Source    string
	WrittenMS int64
}

// pointerV1 is the pointer file, field for field.
type pointerV1 struct {
	V         int    `json:"v"`
	Seat      string `json:"seat"`
	SessionID string `json:"session_id"`
	Session   string `json:"session"`
	Source    string `json:"source"`
	WrittenMS *int64 `json:"written_ms"`
}

func pointerPath(checkout, seat string) string {
	return filepath.Join(checkout, seatIndexDir, seat+".json")
}

// readPointer is seat's pointer; found is false when it has none.
func readPointer(checkout, seat string) (SeatPointer, bool, error) {
	path := pointerPath(checkout, seat)
	data, err := readFile(path)
	if errors.Is(err, errNotRecorded) {
		return SeatPointer{}, false, nil
	}
	if err != nil {
		return SeatPointer{}, false, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p pointerV1
	if err := dec.Decode(&p); err != nil || dec.More() {
		return SeatPointer{}, false, fmt.Errorf("%s is not a seat's conversation pointer: %w", path, errors.Join(err, errNotARecord))
	}
	switch {
	case p.V != 1 || p.WrittenMS == nil || *p.WrittenMS < 0 || !slices.Contains([]string{sourceSession, sourceSet}, p.Source):
		return SeatPointer{}, false, fmt.Errorf("%s is not a version 1 seat conversation pointer", path)
	case p.Seat != seat:
		return SeatPointer{}, false, fmt.Errorf("%s names the seat %q, not %s", path, p.Seat, seat)
	case !registry.SessionID(p.SessionID):
		return SeatPointer{}, false, fmt.Errorf("%s holds %q, which is not a Claude session id", path, p.SessionID)
	}
	return SeatPointer{Seat: seat, ID: p.SessionID, Session: p.Session, Source: p.Source, WrittenMS: *p.WrittenMS}, true, nil
}

// writePointer stores p as its seat's pointer, whole or not at all.
func writePointer(checkout string, p SeatPointer) error {
	path := pointerPath(checkout, p.Seat)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("could not make %s: %w", filepath.Dir(path), err)
	}
	ms := p.WrittenMS
	data, err := json.Marshal(pointerV1{V: 1, Seat: p.Seat, SessionID: p.ID, Session: p.Session, Source: p.Source, WrittenMS: &ms})
	if err != nil {
		return fmt.Errorf("could not encode the pointer of %s: %w", p.Seat, err)
	}
	return fsx.WriteAtomic(path, "seat conversation pointer", append(data, '\n'), 0o600)
}

// transcriptDir is where this checkout's ccy conversations keep their transcripts.
func transcriptDir(checkout string, where Where) (string, error) {
	return transcript.Dir(membership.KindCcy, checkout, where.ClaudeHome)
}

// SetSeats points every seat of list in checkout at the conversation id, set by hand at nowMS:
// the repair `ccy-teams seats conversation <seats> --set <id>` makes. The id must be a Claude
// session id whose transcript is in this checkout. A session that holds a seat and records a
// conversation after nowMS moves its pointer on again.
func SetSeats(checkout, list, id string, where Where, nowMS int64) error {
	seats, _, err := parseList(list)
	if err != nil {
		return err
	}
	if !registry.SessionID(id) {
		return fmt.Errorf("%q is not a Claude session id", id)
	}
	dir, err := transcriptDir(checkout, where)
	if err != nil {
		return err
	}
	if _, err := transcript.For(dir, id); err != nil {
		return fmt.Errorf("the conversation %s has no transcript in this checkout, so it cannot be resumed here: %w", id, err)
	}
	for _, s := range seats {
		p := SeatPointer{Seat: s.Seat + "@" + s.Team, ID: id, Source: sourceSet, WrittenMS: nowMS}
		if err := writePointer(checkout, p); err != nil {
			return err
		}
	}
	return nil
}
