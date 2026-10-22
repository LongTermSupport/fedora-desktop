// Package bundle is the member's one reader of a seat's bundle, its seat.json and token
// (SPEC-n3 MS3, B11). Only the link reads it, on each use, as a pair: seat.json, then token,
// then seat.json again, and the pair counts only when both seat.json reads are the same inode
// and the same bytes. A pair read torn is read again at once, and torn twice it is ErrTorn,
// which the link leaves for its next event (LN9): a placement replacing the bundle is never a
// permanent refusal.
//
// Everything is read through homefs below the seat directory's own fd: no link is followed, a
// FIFO is never waited on, and every file must be the caller's. The token must be mode 0600 and
// one credential (seat.ParseSecret). A pair is refused as exit 78 code bundle when the seat
// directory holds a password, when seat.json is in neither of the two shapes seat writes
// (seat.DecodeMemberBundle) or names a team other than the directory it is in, and when its
// user id is not the one the seat's store was written for. Distinct refuses two held seats'
// pairs that share a user id or a token. No error, message or log line holds the token.
package bundle

import (
	"bytes"
	"crypto/subtle"
	"errors"
	"io/fs"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

// ErrTorn is a pair whose seat.json changed between its two reads, twice running: a placement
// is under way, so the pair is read again at the next event, never refused.
var ErrTorn = errors.New("bundle: seat.json changed while its pair was read; read it again later")

// tokenMode is the one mode a token may have (B11).
const tokenMode = 0o600

// Pair is a seat's bundle read whole: the team its directory is named for, its seat.json and
// its token.
type Pair struct {
	Team   paths.TeamID
	Bundle seat.MemberBundle
	Token  seat.Secret
}

const (
	notATeam    exits.Template = "the seat directory {} is not named for a team"
	notAsMust   exits.Template = "{} is missing, a link, not this user's, the wrong type or over its cap"
	password    exits.Template = "the seat directory {} holds a password; a member seat takes a token only"
	modeNot600  exits.Template = "the token in {} is not mode 0600"
	notOneLine  exits.Template = "the token in {} is not one printable line"
	shape       exits.Template = "seat.json in {} is not a guest's or a session's bundle"
	otherTeam   exits.Template = "seat.json in {} is for team {}, not the directory's"
	otherUser   exits.Template = "seat.json in {} is for {}, not the user its store was written for"
	badStore    exits.Template = "store.json in {} does not decode"
	sharedUser  exits.Template = "the bundles of teams {} and {} share a user id"
	sharedToken exits.Template = "the bundles of teams {} and {} share a token"
)

// ReadPair reads the pair in the seat directory s, below root. No seat directory or no
// seat.json is fs.ErrNotExist (no bundle placed yet); a pair torn twice is ErrTorn; a refused
// pair is a 78 *exits.Error.
func ReadPair(root *homefs.Root, s paths.Seat) (Pair, error) {
	return readPair(root, s, func() {})
}

// readPair is ReadPair with between run after the token's read, before seat.json's second.
func readPair(root *homefs.Root, s paths.Seat, between func()) (Pair, error) {
	team, err := paths.ParseTeamID(filepath.Base(s.Dir()))
	if err != nil {
		return Pair{}, exits.New(exits.CodeBundle, nil, notATeam, exits.Name(s.Dir()))
	}
	dir, err := root.Sub(s.Dir(), homefs.Private)
	if errors.Is(err, fs.ErrNotExist) {
		return Pair{}, err
	}
	if err != nil {
		return Pair{}, fault(team, s.Dir(), err)
	}
	p, err := readIn(dir, s, team, between)
	if closeErr := dir.Close(); closeErr != nil && err == nil {
		return Pair{}, closeErr
	}
	return p, err
}

func readIn(dir *homefs.Root, s paths.Seat, team paths.TeamID, between func()) (Pair, error) {
	found, err := dir.Exists(s.Password())
	if err != nil {
		return Pair{}, err
	}
	if found {
		return Pair{}, exits.New(exits.CodeBundle, &team, password, exits.Name(s.Dir()))
	}
	data, token, err := readOnce(dir, s, team, between)
	if errors.Is(err, ErrTorn) {
		data, token, err = readOnce(dir, s, team, between)
	}
	if err != nil {
		return Pair{}, err
	}
	p, err := decode(s, team, data, token)
	if err != nil {
		return Pair{}, err
	}
	return p, checkStore(dir, s, p)
}

// readOnce reads seat.json, token and seat.json again; the pair is ErrTorn unless both
// seat.json reads are the same inode and bytes.
func readOnce(dir *homefs.Root, s paths.Seat, team paths.TeamID, between func()) ([]byte, homefs.File, error) {
	first, err := dir.Read(s.Bundle(), seat.MaxBundle)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, homefs.File{}, err
	}
	if err != nil {
		return nil, homefs.File{}, fault(team, s.Bundle(), err)
	}
	token, err := dir.Read(s.Token(), seat.MaxSecret)
	if err != nil {
		return nil, homefs.File{}, fault(team, s.Token(), err)
	}
	between()
	second, err := dir.Read(s.Bundle(), seat.MaxBundle)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, homefs.File{}, ErrTorn
	}
	if err != nil {
		return nil, homefs.File{}, fault(team, s.Bundle(), err)
	}
	if first.Dev != second.Dev || first.Ino != second.Ino || !bytes.Equal(first.Data, second.Data) {
		return nil, homefs.File{}, ErrTorn
	}
	return first.Data, token, nil
}

func decode(s paths.Seat, team paths.TeamID, data []byte, token homefs.File) (Pair, error) {
	if token.Perm != tokenMode {
		return Pair{}, exits.New(exits.CodeBundle, &team, modeNot600, exits.Name(s.Dir()))
	}
	secret, err := seat.ParseSecret(string(token.Data))
	if err != nil {
		return Pair{}, exits.New(exits.CodeBundle, &team, notOneLine, exits.Name(s.Dir()))
	}
	b, err := seat.DecodeMemberBundle(data)
	if err != nil {
		return Pair{}, exits.New(exits.CodeBundle, &team, shape, exits.Name(s.Dir()))
	}
	if b.Bundle.Team != team {
		return Pair{}, exits.New(exits.CodeBundle, &team, otherTeam, exits.Name(s.Dir()), exits.Team(b.Bundle.Team))
	}
	return Pair{Team: team, Bundle: b, Token: secret}, nil
}

// checkStore refuses a pair whose user id is not the one the seat's store was written for; a
// seat with no store yet has nothing to differ from.
func checkStore(dir *homefs.Root, s paths.Seat, p Pair) error {
	st, err := state.Read(dir, s.Store(), state.MaxStore, state.DecodeStore)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case errors.Is(err, state.ErrInvalid) || errors.Is(err, homefs.ErrConfig):
		return exits.New(exits.CodeStore, &p.Team, badStore, exits.Name(s.Dir()))
	case err != nil:
		return err
	case st.UserID != p.Bundle.Bundle.UserID:
		return exits.New(exits.CodeBundle, &p.Team, otherUser, exits.Name(s.Dir()), exits.Name(p.Bundle.Bundle.UserID))
	}
	return nil
}

// fault is a failed read of full: a file that is not as the member must have it (TB4), or
// missing where the pair needs it, is 78 bundle; any other failure is returned as it is.
func fault(team paths.TeamID, full string, err error) error {
	if errors.Is(err, homefs.ErrConfig) || errors.Is(err, fs.ErrNotExist) {
		return exits.New(exits.CodeBundle, &team, notAsMust, exits.Name(full))
	}
	return err
}

// Distinct refuses two held seats' pairs that share a user id or a token (B11), as 78 bundle
// naming the two teams.
func Distinct(pairs []Pair) error {
	for i, a := range pairs {
		for _, b := range pairs[i+1:] {
			if a.Bundle.Bundle.UserID == b.Bundle.Bundle.UserID {
				return exits.New(exits.CodeBundle, &b.Team, sharedUser, exits.Team(a.Team), exits.Team(b.Team))
			}
			if subtle.ConstantTimeCompare([]byte(a.Token.Reveal()), []byte(b.Token.Reveal())) == 1 {
				return exits.New(exits.CodeBundle, &b.Team, sharedToken, exits.Team(a.Team), exits.Team(b.Team))
			}
		}
	}
	return nil
}
