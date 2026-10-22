// Package guest is the operations the host runs as an LXC guest's user (B 2.13; SPEC-n3
// GU1-GU5) and `seat set-aside` (SA1). Each prints only CHANGED<TAB><what> lines, what from
// its op's fixed set, and refuses as an error object.
package guest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

// What each placing op prints after CHANGED and a tab, one line per change (TB2.4).
const (
	// WhatSeat is a seat directory placed whole (place-bundle), or emptied but seat.lock (remove).
	WhatSeat = "seat"
	// WhatToken is the seat's token replaced.
	WhatToken = "token"
	// WhatBundle is the seat's seat.json replaced.
	WhatBundle = "seat.json"
	// WhatStore is the seat's store.json set aside.
	WhatStore = "store.json"
	// WhatDesired is desired.json written.
	WhatDesired = "desired.json"
)

// The fixed set of what each op may print, for the host's parser (TB2.4).
var (
	PlaceBundleChanges = []string{WhatSeat, WhatToken, WhatBundle, WhatStore}
	MembershipChanges  = []string{WhatDesired}
	RemoveChanges      = []string{WhatSeat}
	SetAsideChanges    = []string{WhatStore}
)

// maxIssued bounds the issued bundle place-bundle reads on stdin: a seat.json and a credential.
const maxIssued = seat.MaxBundle + seat.MaxSecret

// fileMode is every file these ops place: token, seat.json, desired.json.
const fileMode = 0o600

// Clock is an op's time: now for a set-aside store's name, and the waits of the seat lock's
// tries.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// changes is where an op says what it changed, one CHANGED line per call.
type changes interface {
	Changed(what string) error
}

// Home is the guest user an op acts for and its home, opened as the root every path is
// reached from.
type Home struct {
	Root *homefs.Root
	User paths.GuestUser
}

func (h Home) layout() (paths.Layout, error) {
	return paths.HomeLayout(h.Root.Path(), paths.GuestRuntimeDir(h.User))
}

// config is ~/.config, made 0700 when absent, opened as a root of its own: a directory the
// user keeps free of group and other write.
func (h Home) config() (*homefs.Root, error) {
	dir := h.Root.Path() + "/.config"
	if err := h.Root.Mkdir(dir); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	return h.Root.Sub(dir, homefs.Owned)
}

const (
	msgBundle      exits.Template = "the seat bundle on stdin is not one token bundle"
	msgSeatFault   exits.Template = "a part of the seat directory of team {} is a link, not this user's, or the wrong type or mode"
	msgSeatSubdir  exits.Template = "the seat directory of team {} holds a directory; remove it by hand after checking it"
	msgHeld        exits.Template = "the seat of team {} is held by a running session; stop it and try again"
	msgDesired     exits.Template = "the desired membership on stdin does not decode, or is not this user's session"
	msgDesiredPath exits.Template = "the desired membership's directory is a link, not this user's, or the wrong type or mode"
)

// PlaceBundle is `guest place-bundle` (GU3): the issued token bundle on stdin placed as its
// team's seat in the guest's home.
func PlaceBundle(ctx context.Context, h Home, clock Clock, stdin io.Reader, out changes) error {
	return placeBundle(ctx, h, clock, stdin, out, func() {})
}

// placeBundle is PlaceBundle with between run after the token's rename, before seat.json's.
func placeBundle(ctx context.Context, h Home, clock Clock, stdin io.Reader, out changes, between func()) error {
	data, err := readCapped(stdin, maxIssued)
	if err != nil {
		return err
	}
	is, err := seat.DecodeIssued(data)
	if err != nil || is.Kind != seat.KindToken {
		return exits.New(exits.CodeBundle, nil, msgBundle)
	}
	team := is.Seat.Team
	bundleData, err := is.Seat.Encode()
	if err != nil {
		return exits.New(exits.CodeBundle, &team, msgBundle)
	}
	p := placement{team: team, token: []byte(is.Secret.Reveal()), bundle: bundleData, out: out, between: between}
	err = withState(h, func(root *homefs.Root, l paths.Layout) error {
		p.seat = l.Seat(team)
		found, existsErr := root.Exists(p.seat.Dir())
		if existsErr != nil {
			return existsErr
		}
		if !found {
			return p.fresh(root, l)
		}
		dir, subErr := root.Sub(p.seat.Dir(), homefs.Private)
		if subErr != nil {
			return subErr
		}
		return errors.Join(p.into(ctx, clock, dir, is.Seat.UserID), dir.Close())
	})
	return seatFault(team, err)
}

// placement is one bundle's files and where they go.
type placement struct {
	team    paths.TeamID
	seat    paths.Seat
	token   []byte
	bundle  []byte
	out     changes
	between func()
}

// fresh places a seat directory that does not exist: its team's leftover claims removed, a
// new claim filled with token then seat.json, and renamed in without replacing anything.
func (p placement) fresh(root *homefs.Root, l paths.Layout) error {
	entries, err := root.Entries(l.State())
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name, p.team.String()+".new-") {
			continue
		}
		full := l.State() + "/" + e.Name
		if e.Type.IsDir() {
			err = root.RemoveTree(full)
		} else {
			err = root.Unlink(full)
		}
		if err != nil {
			return err
		}
	}
	id, err := newClaim()
	if err != nil {
		return err
	}
	claim := l.SeatClaim(p.team, id)
	if err := root.Mkdir(claim.Dir()); err != nil {
		return err
	}
	if err := root.Create(claim.Token(), p.token, fileMode); err != nil {
		return err
	}
	if err := root.Create(claim.Bundle(), p.bundle, fileMode); err != nil {
		return err
	}
	if err := root.Rename(claim.Dir(), p.seat.Dir()); err != nil {
		return err
	}
	return p.out.Changed(WhatSeat)
}

// into places the files into the existing seat directory dir: in place for the same identity;
// for another, or a seat.json that does not decode, under the seat lock with the store set
// aside first, the lock released last.
func (p placement) into(ctx context.Context, clock Clock, dir *homefs.Root, userID string) error {
	if p.sameIdentity(dir, userID) {
		return p.replace(dir)
	}
	hold, err := holdSeat(ctx, clock, dir, p.seat, p.team)
	if err != nil {
		return err
	}
	err = setAsideStore(dir, clock, p.seat, p.out)
	if err == nil {
		err = p.replace(dir)
	}
	return errors.Join(err, hold.Release())
}

// sameIdentity is whether the seat's seat.json decodes to this team and user (and so seat).
func (p placement) sameIdentity(dir *homefs.Root, userID string) bool {
	f, err := dir.Read(p.seat.Bundle(), seat.MaxBundle)
	if err != nil {
		return false
	}
	b, err := seat.DecodeMemberBundle(f.Data)
	return err == nil && b.Bundle.Team == p.team && b.Bundle.UserID == userID
}

// replace replaces token, then seat.json, each only where its bytes differ.
func (p placement) replace(dir *homefs.Root) error {
	changed, err := replaceIfDiffers(dir, p.seat.Token(), p.token, seat.MaxSecret, p.out, WhatToken)
	if err != nil {
		return err
	}
	if changed {
		p.between()
	}
	_, err = replaceIfDiffers(dir, p.seat.Bundle(), p.bundle, seat.MaxBundle, p.out, WhatBundle)
	return err
}

// replaceIfDiffers replaces full with data, mode 0600, unless it already is exactly that; a
// file there that is not the user's regular file, or is over limit, is refused, never replaced.
func replaceIfDiffers(root *homefs.Root, full string, data []byte, limit int, out changes, what string) (bool, error) {
	f, err := root.Read(full, limit)
	switch {
	case err == nil && bytes.Equal(f.Data, data) && f.Perm == fileMode:
		return false, nil
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return false, err
	}
	if err := root.Replace(full, data, fileMode); err != nil {
		return false, err
	}
	return true, out.Changed(what)
}

// newClaim is a fresh claim id: 16 lowercase hex from crypto/rand.
func newClaim() (paths.ClaimID, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return paths.ClaimID{}, fmt.Errorf("guest: a claim id: %w", err)
	}
	return paths.ParseClaimID(hex.EncodeToString(b[:]))
}

// Membership is `guest membership` (GU4): the desired membership on stdin, for the session
// lxc-<user>, written as the guest's desired.json in its canonical bytes.
func Membership(h Home, stdin io.Reader, out changes) error {
	data, err := readCapped(stdin, state.MaxDesired)
	if over, ok := errors.AsType[*exits.Error](err); ok && over.Code == exits.CodeBundle {
		return exits.New(exits.CodeMembership, nil, msgDesired)
	}
	if err != nil {
		return err
	}
	d, err := state.DecodeDesired(data)
	if err != nil || d.Session.String() != "lxc-"+h.User.String() {
		return exits.New(exits.CodeMembership, nil, msgDesired)
	}
	canonical, err := d.Encode()
	if err != nil {
		return err
	}
	l, err := h.layout()
	if err != nil {
		return err
	}
	config, err := h.config()
	if err == nil {
		err = config.MkdirAll(l.Desired())
		if err == nil {
			_, err = replaceIfDiffers(config, l.DesiredFile(), canonical, state.MaxDesired, out, WhatDesired)
		}
		err = errors.Join(err, config.Close())
	}
	if errors.Is(err, homefs.ErrConfig) {
		return exits.New(exits.CodeMembership, nil, msgDesiredPath)
	}
	return err
}

// Remove is `guest remove` (GU5): every file of the team's seat directory but seat.lock
// removed under the seat lock; a directory in it is refused before anything is removed.
func Remove(ctx context.Context, h Home, clock Clock, team paths.TeamID, out changes) error {
	err := withState(h, func(root *homefs.Root, l paths.Layout) error {
		s := l.Seat(team)
		return inSeat(root, s, func(dir *homefs.Root) error {
			hold, err := holdSeat(ctx, clock, dir, s, team)
			if err != nil {
				return err
			}
			return errors.Join(removeFiles(dir, s, team, out), hold.Release())
		})
	})
	return seatFault(team, err)
}

func removeFiles(dir *homefs.Root, s paths.Seat, team paths.TeamID, out changes) error {
	entries, err := dir.Entries(s.Dir())
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if s.Dir()+"/"+e.Name == s.Lock() {
			continue
		}
		if e.Type.IsDir() {
			return exits.New(exits.CodeBundle, &team, msgSeatSubdir, exits.Team(team))
		}
		names = append(names, e.Name)
	}
	for _, name := range names {
		if err := dir.Unlink(s.Dir() + "/" + name); err != nil {
			return err
		}
	}
	if len(names) == 0 {
		return nil
	}
	return out.Changed(WhatSeat)
}

// SetAside is `seat set-aside` (SA1): team's store.json under the state root stateDir renamed
// to its set-aside name under the seat lock. No seat directory or no store is a success that
// prints nothing.
func SetAside(ctx context.Context, clock Clock, stateDir string, team paths.TeamID, out changes) error {
	s, err := paths.StateSeat(stateDir, team)
	if err != nil {
		return exits.New(exits.CodeBundle, &team, msgSeatFault, exits.Team(team))
	}
	root, err := homefs.Open(stateDir, homefs.Owned)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err == nil {
		err = errors.Join(inSeat(root, s, func(dir *homefs.Root) error {
			hold, holdErr := holdSeat(ctx, clock, dir, s, team)
			if holdErr != nil {
				return holdErr
			}
			return errors.Join(setAsideStore(dir, clock, s, out), hold.Release())
		}), root.Close())
	}
	return seatFault(team, err)
}

// setAsideStore renames the seat's store.json aside; the caller holds the seat lock.
func setAsideStore(dir *homefs.Root, clock Clock, s paths.Seat, out changes) error {
	found, err := dir.Exists(s.Store())
	if err != nil || !found {
		return err
	}
	if err := dir.Rename(s.Store(), s.StoreAside(clock.Now().UnixMilli())); err != nil {
		return err
	}
	return out.Changed(WhatStore)
}

// withState runs f with the guest's state root opened below ~/.config, made 0700 as needed.
func withState(h Home, f func(root *homefs.Root, l paths.Layout) error) error {
	l, err := h.layout()
	if err != nil {
		return err
	}
	config, err := h.config()
	if err != nil {
		return err
	}
	err = config.MkdirAll(l.State())
	if err == nil {
		err = f(config, l)
	}
	return errors.Join(err, config.Close())
}

// inSeat runs f with the seat directory opened as a root, the user's and 0700; no seat
// directory is nothing to do.
func inSeat(root *homefs.Root, s paths.Seat, f func(dir *homefs.Root) error) error {
	dir, err := root.Sub(s.Dir(), homefs.Private)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.Join(f(dir), dir.Close())
}

// holdSeat takes the seat lock for a removal; held after every try is 75.
func holdSeat(ctx context.Context, clock Clock, dir *homefs.Root, s paths.Seat, team paths.TeamID) (*lock.SeatHold, error) {
	hold, err := lock.HoldSeat(ctx, clock, dir, s)
	if errors.Is(err, lock.ErrBusy) {
		return nil, exits.New(exits.CodeSeatHeld, &team, msgHeld, exits.Team(team))
	}
	return hold, err
}

// seatFault is err with a seat path that is not as the member must have it made 78 bundle.
func seatFault(team paths.TeamID, err error) error {
	var refused *exits.Error
	if errors.Is(err, homefs.ErrConfig) && !errors.As(err, &refused) {
		return exits.New(exits.CodeBundle, &team, msgSeatFault, exits.Team(team))
	}
	return err
}

// readCapped reads all of r, refusing more than limit bytes as 78 bundle.
func readCapped(r io.Reader, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return nil, fmt.Errorf("guest: read stdin: %w", err)
	}
	if len(data) > limit {
		return nil, exits.New(exits.CodeBundle, nil, msgBundle)
	}
	return data, nil
}
