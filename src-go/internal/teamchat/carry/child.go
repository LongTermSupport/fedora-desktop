package carry

import (
	"context"
	"fmt"
	"io"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
)

// Child is the teamchat-ctl child's carry ops as root asks them (SC-C1 to SC-C3); a failure the
// child answered carries its class, a server lost mid-read 69 (RS15).
type Child interface {
	// Tail is carry.tail: each room's newest event and the roster event, on the live server.
	Tail(ctx context.Context, team paths.TeamID, teamFile []byte) (Tail, error)
	// Place is carry.place: the carried files written into ctl/<team>/, the names answered.
	Place(ctx context.Context, team paths.TeamID, a PlaceArgs) ([]string, error)
	// Check is carry.check: each tail event found or not, on the loopback port given.
	Check(ctx context.Context, team paths.TeamID, a CheckArgs) (Presence, error)
}

// Tail is carry.tail's answer: each room's newest event, in rooms.json's order, and the main
// room's roster event's epoch (SC2).
type Tail struct {
	Rooms []TailEntry
	Epoch restore.Epoch
}

// Agrees refuses a tail whose roster event is not roster.json's rev and restore epoch (SC2).
func (t Tail) Agrees(r RosterFacts) error {
	if t.Epoch.Rev != r.Rev || t.Epoch.RestoreEpoch != r.RestoreEpoch {
		return refusedf("the roster event and roster.json disagree (rev %d and %d, restore epoch %d and %d): run team apply first",
			t.Epoch.Rev, r.Rev, t.Epoch.RestoreEpoch, r.RestoreEpoch)
	}
	return nil
}

// PlaceArgs are carry.place's args (SC-C2): the team file, the roster and rooms files and the
// admin token as carried, each guest seat's token, each lost export by name, size and sha256,
// and Stream, their bytes in that order (StagedLost), which follows the request (sc-D11).
type PlaceArgs struct {
	TeamFile, Roster, Rooms []byte
	Token                   string
	Guest                   []GuestToken
	Lost                    []LostExport
	Stream                  io.Reader
}

// GuestToken is a guest seat's token copy, `guest/<handle>.token`.
type GuestToken struct{ Handle, Token string }

// LostExport is `restore-<n>-lost.jsonl` (RS14) as carry.place names it: its plain name, its
// size and its sha256 in lower-case hex. Its bytes are never in the request (sc-D11).
type LostExport struct {
	Name   string
	Size   int64
	SHA256 string
}

// Check is the export's own grammar: a lost export's plain name, a size from 0 to LostCap, and
// a sha256 of 64 lower-case hex digits.
func (l LostExport) Check() error {
	if !lostName.MatchString(l.Name) {
		return fmt.Errorf("%q is not a lost export's name, restore-<n>-lost.jsonl", l.Name)
	}
	if l.Size < 0 || l.Size > LostCap {
		return fmt.Errorf("%s: a size of %d is not 0 to %d bytes", l.Name, l.Size, LostCap)
	}
	if !isDigest(l.SHA256) {
		return fmt.Errorf("%s: the sha256 is not 64 lower-case hex digits", l.Name)
	}
	return nil
}

// CheckArgs are carry.check's args (SC-C3): the team file, the loopback port of the instance
// under check and the tail it must hold.
type CheckArgs struct {
	TeamFile []byte
	Port     paths.Port
	Tail     []TailEntry
}

// Presence is carry.check's answer: per tail entry, in its order, found or not, and the roster
// event's epoch.
type Presence struct {
	Found []bool
	Epoch restore.Epoch
}

// EncodeTail is a tail on the wire, as the records hold it.
func EncodeTail(tail []TailEntry) proto.Value { return tailValue(tail) }

// DecodeTail reads a tail strictly: at least one room, each once, each of the grammar.
func DecodeTail(v proto.Value) ([]TailEntry, error) {
	rd := &reader{file: "the tail"}
	tail := decodeTail(rd, v, "tail")
	if rd.err != nil {
		return nil, rd.err
	}
	return tail, nil
}
