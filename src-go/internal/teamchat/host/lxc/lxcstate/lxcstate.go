// Package lxcstate holds the host's two LXC records (Plan 00209 SPEC-n3.md LX7, LX9,
// DESIGN-v2-C-host.md 3.9.3): a team's LXC record, `/var/lib/teamchat-status/lxc/<team>.json`,
// which names each seat (container, checkout, seat), the user it runs as, its handle and its
// status, and the host's copy of a seat's desired membership,
// `/var/lib/teamchat-admin/_lxc/<container>/<key>.json`, from which the guest's desired.json is
// written (n3b-D2). Both are the host's own files, read and written only by
// the host's trust reads and atomic writes (`state`), never below a guest root. Every decode is
// strict: a file must be what Encode would write.
package lxcstate

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/sys/unix"

	memberstate "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// An entry's status (C 3.9.3, n3b-D1), and the caps of the two files.
const (
	StatusJoined = "joined"
	// StatusOffline is C 3.9.3's `stopped`, renamed and widened (n3b-D1): a container stopped,
	// frozen or missing, one that stopped during the phase, or a session unit that is down. Why
	// names the cause.
	StatusOffline  = "offline"
	StatusDeferred = "deferred"
	StatusLeft     = "left"
	MaxRecord      = 1 << 20
	MaxCopy        = 64 << 10
)

// The modes of the files and their directories: the record is the owner group's to read, the
// copy root's alone.
const (
	recordMode    fs.FileMode = 0o640
	recordDirMode fs.FileMode = 0o755
	copyMode      fs.FileMode = 0o600
	copyDirMode   fs.FileMode = 0o700
)

// ErrInvalid is wrapped by every refusal of a record or a copy.
var ErrInvalid = errors.New("invalid LXC record")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Record is a team's LXC record (C 3.9.3).
type Record struct {
	V       int          `json:"v"`
	Team    paths.TeamID `json:"team"`
	Members []Entry      `json:"members"`
}

// Entry is one seat of the team, keyed (container, checkout, seat); User is who runs it.
type Entry struct {
	Container paths.ContainerName `json:"container"`
	Checkout  string              `json:"checkout"`
	Seat      int                 `json:"seat"`
	User      paths.GuestUser     `json:"user"`
	Handle    string              `json:"handle"` // "" while deferred or offline with no handle
	Status    string              `json:"status"`
	SinceMS   int64               `json:"since_ms"`
	Why       string              `json:"why"` // status.OneLine, or "" (always for joined)
}

func (e Entry) at(c paths.ContainerName, checkout string, seat int) bool {
	return e.Container == c && e.Checkout == checkout && e.Seat == seat
}

func compareEntries(a, b Entry) int {
	return cmp.Or(cmp.Compare(a.Container.String(), b.Container.String()), cmp.Compare(a.Checkout, b.Checkout),
		cmp.Compare(a.Seat, b.Seat))
}

// SeatID is a seat's identity, the inputs of its key (C 3.9.1).
type SeatID struct {
	Team     paths.TeamID `json:"team"`
	Checkout string       `json:"checkout"`
	Seat     int          `json:"seat"`
}

// Key is the seat's key; a seat whose check fails panics, as paths.LXCSeatKey does.
func (s SeatID) Key() paths.SeatKey { return paths.LXCSeatKey(s.Team, s.Checkout, s.Seat) }

// check is declare's rule for the key's inputs (DR4): paths.ValidLXCSeat and
// paths.ValidGuestPath.
func (s SeatID) check() error {
	if !paths.ValidLXCSeat(s.Seat) {
		return invalid("a seat is not 1-%d", paths.MaxLXCSeat)
	}
	if !paths.ValidGuestPath(s.Checkout) {
		return invalid("a checkout is not an absolute, clean path below / with no control or format character")
	}
	return nil
}

// SeatOf is e's seat: the record's team, e's checkout and seat.
func (r Record) SeatOf(e Entry) SeatID {
	return SeatID{Team: r.Team, Checkout: e.Checkout, Seat: e.Seat}
}

// Encode is the record's bytes, its entries sorted; a record that would not decode is refused.
func (r Record) Encode() ([]byte, error) {
	data, err := json.Marshal(r.normal())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if _, err := DecodeRecord(data, r.Team); err != nil {
		return nil, err
	}
	return data, nil
}

// DecodeRecord reads team's record strictly: every key present and none unknown, v 1, the
// team's own, each (container, checkout, seat) once and in order, each handle an lxc agent
// handle held by at most one entry that is not left, and each why one printable line.
func DecodeRecord(data []byte, team paths.TeamID) (Record, error) {
	var r Record
	if err := decodeStrict(data, MaxRecord, &r); err != nil {
		return Record{}, err
	}
	if err := r.check(team); err != nil {
		return Record{}, err
	}
	return r, sameWritten(data, r)
}

func (e Entry) check(team paths.TeamID) error {
	if err := (SeatID{Team: team, Checkout: e.Checkout, Seat: e.Seat}).check(); err != nil {
		return err
	}
	switch {
	case !slices.Contains([]string{StatusJoined, StatusOffline, StatusDeferred, StatusLeft}, e.Status):
		return invalid("a status is not joined, offline, deferred or left")
	case e.SinceMS <= 0:
		return invalid("a since_ms is not a time")
	case e.Why != "" && !status.OneLine(e.Why):
		return invalid("a why is not one printable line")
	case e.Status == StatusJoined && e.Why != "":
		return invalid("a joined entry has a why")
	case e.Handle == "" && (e.Status == StatusJoined || e.Status == StatusLeft):
		return invalid("a joined or left entry has no handle")
	case e.Handle != "" && !lxcHandle(e.Handle):
		return invalid("a handle is not an LXC agent's")
	}
	return nil
}

func lxcHandle(s string) bool {
	h, ok := proto.ParseHandle(s)
	return ok && !h.IsProbe() && h.String() == s && h.Prefix().Type == "lxc"
}

// Find is the entry of the seat (c, checkout, seat).
func (r Record) Find(c paths.ContainerName, checkout string, seat int) (Entry, bool) {
	for _, e := range r.Members {
		if e.at(c, checkout, seat) {
			return e, true
		}
	}
	return Entry{}, false
}

// ByHandle is the entry that is not left holding handle, the L3 probe's map from a handle to
// its guest (LP1); a handle only left entries hold is free, and no entry holds "".
func (r Record) ByHandle(handle string) (Entry, bool) {
	for _, e := range r.Members {
		if handle != "" && e.Handle == handle && e.Status != StatusLeft {
			return e, true
		}
	}
	return Entry{}, false
}

// Set returns r with e in place of its seat's entry, or added. since_ms is kept when the status
// and the handle are unchanged, and is nowMS otherwise: e.SinceMS is not read.
func (r Record) Set(e Entry, nowMS int64) Record {
	e.SinceMS = nowMS
	if old, ok := r.Find(e.Container, e.Checkout, e.Seat); ok && old.Status == e.Status && old.Handle == e.Handle {
		e.SinceMS = old.SinceMS
	}
	r = r.Drop(e.Container, e.Checkout, e.Seat)
	r.Members = append(r.Members, e)
	return r.normal()
}

// Drop returns r without the seat (c, checkout, seat)'s entry.
func (r Record) Drop(c paths.ContainerName, checkout string, seat int) Record {
	r.Members = slices.DeleteFunc(slices.Clone(r.Members), func(e Entry) bool { return e.at(c, checkout, seat) })
	return r.normal()
}

// normal is the record as Encode writes it: its entries sorted, in a slice of its own.
func (r Record) normal() Record {
	members := slices.Clone(r.Members)
	if members == nil {
		members = []Entry{}
	}
	slices.SortFunc(members, compareEntries)
	r.Members = members
	return r
}

func (r Record) check(team paths.TeamID) error {
	switch {
	case r.V != 1:
		return invalid("v is not 1")
	case r.Team != team:
		return invalid("the record is not this team's")
	case r.Members == nil:
		return invalid("members is not a list")
	}
	live := map[string]bool{}
	for i, e := range r.Members {
		if err := e.check(team); err != nil {
			return err
		}
		if i > 0 && compareEntries(r.Members[i-1], e) >= 0 {
			return invalid("the entries are not each (container, checkout, seat) once, in order")
		}
		if e.Handle == "" || e.Status == StatusLeft {
			continue
		}
		if live[e.Handle] {
			return invalid("two entries that are not left hold one handle")
		}
		live[e.Handle] = true
	}
	return nil
}

// ReadRecord is team's record under the host root ("" on a host); found is false, and the
// record team's empty one, when there is none.
func ReadRecord(root string, team paths.TeamID) (Record, bool, error) {
	path := root + paths.LXCRecord(team)
	data, found, err := state.ReadFile(path, MaxRecord)
	if err != nil {
		return Record{}, false, err
	}
	if !found {
		return Record{V: 1, Team: team, Members: []Entry{}}, false, nil
	}
	r, err := DecodeRecord(data, team)
	if err != nil {
		return Record{}, false, fmt.Errorf("%s: %w", path, err)
	}
	return r, true, nil
}

// WriteRecord writes r 0640 root:gid atomically, only when its bytes differ from the file's
// (changed false: a converged apply writes nothing). A file there that does not decode is
// refused, not replaced: the record is the host's memory of each guest's handle.
func WriteRecord(root string, r Record, gid int) (bool, error) {
	data, err := r.Encode()
	if err != nil {
		return false, err
	}
	path := root + paths.LXCRecord(r.Team)
	old, found, err := state.ReadFile(path, MaxRecord)
	if err != nil {
		return false, err
	}
	if found {
		if _, err := DecodeRecord(old, r.Team); err != nil {
			return false, fmt.Errorf("%s: %w", path, err)
		}
		if bytes.Equal(old, data) {
			return false, nil
		}
	}
	if err := mkdirExact(filepath.Dir(path), recordDirMode); err != nil {
		return false, err
	}
	return true, state.WriteFile(path, data, recordMode, gid)
}

// Copy is the host's copy of a seat's desired membership (C 3.9.3, n3-D13): written before the
// guest's desired.json, so a converged apply compares against it without reading the guest. Its
// SeatID is the key's inputs, checked against the key it is read by, so a key collision never
// hands one seat another's generation; its seats and farewells are its team's alone.
type Copy struct {
	V          int                       `json:"v"`
	Team       paths.TeamID              `json:"team"`
	Checkout   string                    `json:"checkout"`
	Seat       int                       `json:"seat"`
	Generation int64                     `json:"generation"`
	Seats      []memberstate.DesiredSeat `json:"seats"`
	Farewell   []memberstate.Farewell    `json:"farewell"`
}

// decodeCopy reads id's copy strictly: as written, of id, and a membership the guest's own
// decoder takes.
func decodeCopy(data []byte, id SeatID) (Copy, error) {
	var cp Copy
	if err := decodeStrict(data, MaxCopy, &cp); err != nil {
		return Copy{}, err
	}
	if err := cp.check(); err != nil {
		return Copy{}, err
	}
	if cp.ID() != id {
		return Copy{}, invalid("the copy is another seat's")
	}
	return cp, sameWritten(data, cp.normal())
}

// NewCopy is id's copy of no file: generation 0, no seats and no farewells.
func NewCopy(id SeatID) Copy {
	return Copy{Team: id.Team, Checkout: id.Checkout, Seat: id.Seat}.normal()
}

// ID is the seat the copy is of.
func (cp Copy) ID() SeatID { return SeatID{Team: cp.Team, Checkout: cp.Checkout, Seat: cp.Seat} }

// ReadCopy is the host's copy of the seat id's desired membership in container c under the host
// root; found is false, and the copy id's empty one of generation 0, when there is none.
func ReadCopy(root string, c paths.ContainerName, id SeatID) (Copy, bool, error) {
	if err := id.check(); err != nil {
		return Copy{}, false, err
	}
	path := root + paths.LXCDesiredCopy(c, id.Key())
	data, found, err := state.ReadFile(path, MaxCopy)
	if err != nil {
		return Copy{}, false, err
	}
	if !found {
		return NewCopy(id), false, nil
	}
	cp, err := decodeCopy(data, id)
	if err != nil {
		return Copy{}, false, fmt.Errorf("%s: %w", path, err)
	}
	return cp, true, nil
}

// WriteCopy writes cp, the copy of its seat in container c, root 0600 atomically, its
// directories root 0700.
func WriteCopy(root string, c paths.ContainerName, cp Copy) error {
	data, err := cp.encode()
	if err != nil {
		return err
	}
	path := root + paths.LXCDesiredCopy(c, cp.ID().Key())
	for _, dir := range []string{filepath.Dir(filepath.Dir(path)), filepath.Dir(path)} {
		if err := mkdirExact(dir, copyDirMode); err != nil {
			return err
		}
	}
	return state.WriteFile(path, data, copyMode, -1)
}

// DeleteCopy removes the seat k's copy in container c, after its guest files are removed (the
// absent phase); a copy already gone is no error, so a retried phase converges.
func DeleteCopy(root string, c paths.ContainerName, k paths.SeatKey) error {
	if err := os.Remove(root + paths.LXCDesiredCopy(c, k)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Next is the seat's copy for seats and farewell with generation one up, or the same copy when
// nothing changed (changed false: a converged apply writes nothing). Order does not count as a
// change.
func (cp Copy) Next(seats []memberstate.DesiredSeat, farewell []memberstate.Farewell) (Copy, bool) {
	cur := cp.normal()
	next := Copy{
		Team: cp.Team, Checkout: cp.Checkout, Seat: cp.Seat, Generation: cp.Generation, Seats: seats, Farewell: farewell,
	}.normal()
	if slices.Equal(cur.Seats, next.Seats) && slices.Equal(cur.Farewell, next.Farewell) {
		return cur, false
	}
	next.Generation++
	return next, true
}

// Desired is the guest's desired.json for the copy (C 3.7.5): the session lxc-<key>, which
// `guest membership` requires (GU4), and inbound accept.
func (cp Copy) Desired(writtenMS int64) memberstate.Desired {
	n := cp.normal()
	return memberstate.Desired{
		V: memberstate.Version, Session: paths.LXCSession(cp.ID().Key()), Generation: n.Generation, WrittenMS: writtenMS,
		Seats: n.Seats, Farewell: n.Farewell, Inbound: memberstate.InboundAccept,
	}
}

func (cp Copy) check() error {
	switch {
	case cp.V != 1:
		return invalid("the copy's v is not 1")
	case cp.Generation < 0:
		return invalid("the copy's generation is negative")
	case cp.Seats == nil || cp.Farewell == nil:
		return invalid("the copy's seats or farewell is not a list")
	}
	if err := cp.ID().check(); err != nil {
		return err
	}
	for _, s := range cp.Seats {
		if s.Team != cp.Team {
			return invalid("the copy holds another team's seat")
		}
	}
	for _, f := range cp.Farewell {
		if f.Team != cp.Team {
			return invalid("the copy holds another team's farewell")
		}
	}
	if _, err := cp.Desired(1).Encode(); err != nil {
		return fmt.Errorf("%w: the copy is not a membership the guest takes: %w", ErrInvalid, err)
	}
	return nil
}

// normal is the copy as it is written: v 1, its seats sorted by team and its farewells by team
// and seat, in slices of its own.
func (cp Copy) normal() Copy {
	cp.V = 1
	cp.Seats = slices.Clone(cp.Seats)
	if cp.Seats == nil {
		cp.Seats = []memberstate.DesiredSeat{}
	}
	slices.SortFunc(cp.Seats, func(a, b memberstate.DesiredSeat) int {
		return cmp.Or(cmp.Compare(a.Team.String(), b.Team.String()), cmp.Compare(a.Seat, b.Seat))
	})
	cp.Farewell = slices.Clone(cp.Farewell)
	if cp.Farewell == nil {
		cp.Farewell = []memberstate.Farewell{}
	}
	slices.SortFunc(cp.Farewell, func(a, b memberstate.Farewell) int {
		return cmp.Or(cmp.Compare(a.Team.String(), b.Team.String()), cmp.Compare(a.Seat, b.Seat), cmp.Compare(a.Reason, b.Reason))
	})
	return cp
}

func (cp Copy) encode() ([]byte, error) {
	if err := cp.normal().check(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(cp.normal())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if _, err := decodeCopy(data, cp.ID()); err != nil {
		return nil, err
	}
	return data, nil
}

// decodeStrict decodes one JSON document by A 2.1's rules (no repeated key, nothing after it)
// into v, refusing an unknown key.
func decodeStrict(data []byte, limit int, v any) error {
	if len(data) > limit {
		return invalid("larger than %d bytes", limit)
	}
	if _, err := proto.Parse(data); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return invalid("content after the value")
	}
	return nil
}

// sameWritten requires the file to be what its value encodes to, so a missing key, a key in
// another case (which encoding/json matches) or an unsorted list is refused.
func sameWritten(data []byte, v any) error {
	again, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	read, err := proto.Parse(data)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	written, err := proto.Parse(again)
	if err != nil || !written.Equal(read) {
		return invalid("a key is missing, or the file is not in its written form")
	}
	return nil
}

// mkdirExact makes dir, or takes the directory there, and sets its mode through a descriptor
// opened without following a link, whatever the umask. Its parent must exist: the play and
// `team apply` make the state roots.
func mkdirExact(dir string, mode fs.FileMode) error {
	if err := os.Mkdir(dir, copyDirMode); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	d, err := os.OpenFile(filepath.Clean(dir), os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("%s is not a directory: %w", dir, err)
	}
	return errors.Join(d.Chmod(mode), d.Close())
}
