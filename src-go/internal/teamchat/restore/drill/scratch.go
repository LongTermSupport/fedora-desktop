// Package drill is `teamchat-admin restore-drill` (issue #324): a scratch team made, backed up,
// changed and restored on a machine that holds no other team, each clause of host checks H-C5
// and H-C15 checked, and the proof record written only when every clause it attempted passed.
// This file is its scratch team: the id, the marker that makes a team the drill's, the refusal
// to start beside any other team or a leftover, and the clean-up of leftovers.
package drill

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// ErrRefused is a drill that will not start on this machine (exit 70).
var ErrRefused = errors.New("restore drill: refused")

func refusedf(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrRefused}, args...)...)
}

// idBytes is the random bytes of a scratch id: 12 hex characters after the prefix.
const idBytes = 6

// scratchID is a scratch team's id: the prefix declare refuses, then 12 lowercase hex.
var scratchID = regexp.MustCompile(`^` + regexp.QuoteMeta(declare.DrillPrefix) + `[0-9a-f]{12}$`)

// NewTeam is a fresh scratch team id from r (crypto/rand on a host).
func NewTeam(r io.Reader) (paths.TeamID, error) {
	b := make([]byte, idBytes)
	if _, err := io.ReadFull(r, b); err != nil {
		return paths.TeamID{}, fmt.Errorf("a scratch team id: %w", err)
	}
	return paths.ParseTeamID(declare.DrillPrefix + hex.EncodeToString(b))
}

// IsScratch is whether id has a scratch team's form.
func IsScratch(id paths.TeamID) bool { return scratchID.MatchString(id.String()) }

// Marker is AdminState(t)/drill.json: the build that started the drill, and when.
type Marker struct {
	Digest    paths.Digest
	StartedMS int64
}

// maxMarker caps drill.json.
const maxMarker = 1 << 10

// maxMS is the largest time the marker holds, JSON's safe integer.
const maxMS = 1<<53 - 1

// EncodeMarker is the marker's JSON.
func EncodeMarker(m Marker) []byte {
	return proto.Obj(proto.M("v", proto.Int(1)), proto.M("digest", proto.Str(m.Digest.String())),
		proto.M("started_ms", proto.Int(m.StartedMS))).Compact()
}

// DecodeMarker reads the marker strictly.
func DecodeMarker(raw []byte) (Marker, error) {
	if len(raw) > maxMarker {
		return Marker{}, fmt.Errorf("drill.json: over %d bytes", maxMarker)
	}
	v, err := proto.Parse(raw)
	if err != nil {
		return Marker{}, fmt.Errorf("drill.json: %w", err)
	}
	keys := map[string]proto.Value{}
	for _, m := range v.Members() {
		keys[m.Key] = m.Val
	}
	ver, dig, started := keys["v"], keys["digest"], keys["started_ms"]
	if v.Kind() != proto.KindObject || len(keys) != 3 || ver.Kind() != proto.KindInt || ver.Int() != 1 ||
		dig.Kind() != proto.KindString || started.Kind() != proto.KindInt || started.Int() < 0 || started.Int() > maxMS {
		return Marker{}, errors.New("drill.json: is not {v:1, digest, started_ms}")
	}
	d, err := paths.ParseDigest(dig.Str())
	if err != nil {
		return Marker{}, fmt.Errorf("drill.json: %w", err)
	}
	return Marker{Digest: d, StartedMS: started.Int()}, nil
}

// WriteMarker makes the scratch team's state directory, which must not exist, and writes its
// marker there before anything else of the team, owner only.
func WriteMarker(root string, id paths.TeamID, m Marker) error {
	if !IsScratch(id) {
		return fmt.Errorf("%s is not a scratch team id", id)
	}
	if _, err := DecodeMarker(EncodeMarker(m)); err != nil {
		return fmt.Errorf("a marker no drill could read back: %w", err)
	}
	if err := os.Mkdir(root+paths.AdminState(id), 0o700); err != nil {
		return fmt.Errorf("the scratch team's state directory: %w", err)
	}
	return state.WriteFile(root+paths.DrillMarker(id), EncodeMarker(m), 0o600, -1)
}

// ReadMarker is the team's marker, which uid must own (the trust rule); found is false for none.
func ReadMarker(root string, uid uint32, id paths.TeamID) (Marker, bool, error) {
	raw, found, err := state.ReadOwnedFile(root+paths.DrillMarker(id), maxMarker, uid)
	if err != nil || !found {
		return Marker{}, found, err
	}
	m, err := DecodeMarker(raw)
	return m, err == nil, err
}

// held is the teams the admin state root holds a directory of: the marked scratch teams, and
// every other (a declared team, or a scratch id whose marker is absent).
type held struct{ marked, others []paths.TeamID }

func teams(root string, uid uint32) (held, error) {
	var h held
	entries, err := os.ReadDir(root + paths.AdminStateRoot)
	if err != nil {
		return h, fmt.Errorf("the admin state root: %w", err)
	}
	for _, e := range entries {
		id, err := paths.ParseTeamID(e.Name())
		if err != nil || !e.IsDir() {
			continue // not a team's: the LXC records, the proof record
		}
		found := false
		if IsScratch(id) {
			if _, found, err = ReadMarker(root, uid, id); err != nil {
				return h, fmt.Errorf("the marker of %s: %w", id, err)
			}
		}
		if found {
			h.marked = append(h.marked, id)
		} else {
			h.others = append(h.others, id)
		}
	}
	return h, nil
}

// CheckStart refuses a drill on a machine holding any team but the drill's own (Q1: the drill
// never runs beside a real team), and one holding a leftover drill, which --clean removes.
func CheckStart(root string, uid uint32) error {
	h, err := teams(root, uid)
	if err != nil {
		return err
	}
	if len(h.others) > 0 {
		return refusedf("this machine holds team %s: the restore drill never runs beside a team", names(h.others))
	}
	if len(h.marked) > 0 {
		return refusedf("an earlier drill left team %s: run teamchat-admin restore-drill --clean first", names(h.marked))
	}
	return nil
}

func names(ids []paths.TeamID) string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	slices.Sort(out)
	return strings.Join(out, ", ")
}

// Cleaner is what --clean does to one marked scratch team, each under that team's lock: whether
// a restore is recorded, its discard (RG2), and the team's removal with purge.
type Cleaner struct {
	Recorded func(id paths.TeamID) (bool, error)
	Discard  func(ctx context.Context, id paths.TeamID) error
	Remove   func(ctx context.Context, id paths.TeamID) error
}

// Clean removes every marked scratch team, and nothing else: a recorded restore is discarded
// first, then the team is removed with purge. It stops at the first failure.
func Clean(ctx context.Context, root string, uid uint32, c Cleaner) ([]paths.TeamID, error) {
	h, err := teams(root, uid)
	if err != nil {
		return nil, err
	}
	var cleaned []paths.TeamID
	for _, id := range h.marked {
		recorded, err := c.Recorded(id)
		if err == nil && recorded {
			err = c.Discard(ctx, id)
		}
		if err == nil {
			err = c.Remove(ctx, id)
		}
		if err != nil {
			return cleaned, fmt.Errorf("cleaning %s: %w", id, err)
		}
		cleaned = append(cleaned, id)
	}
	return cleaned, nil
}
