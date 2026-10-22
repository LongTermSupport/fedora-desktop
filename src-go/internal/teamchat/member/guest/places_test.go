package guest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/bundle"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/reexec"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

const (
	// loginOne and loginTwo are a seat's two credentials.
	loginOne = "syt_guest-1"
	loginTwo = "syt_guest-2"
	room     = "!" + "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
	// nowMS is the fake clock's time.
	nowMS = int64(1_760_000_000_000)
	// otherUID owns what a test makes another user's.
	otherUID = 4242
)

var helpers = reexec.Helpers{
	// flock holds LOCK_EX on its own description of the file Args[0] until released.
	"flock": func(h *reexec.Helper) int {
		f, err := os.OpenFile(h.Args[0], os.O_RDWR, 0)
		if err != nil {
			return 2
		}
		if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			return 3
		}
		if err := h.Ready(); err != nil {
			return 4
		}
		if err := f.Close(); err != nil {
			return 5
		}
		return 0
	},
}

func TestMain(m *testing.M) { os.Exit(reexec.Main(m, helpers)) }

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type fakeClock struct{ sleeps []time.Duration }

func (c *fakeClock) Now() time.Time { return time.UnixMilli(nowMS) }

func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	return nil
}

// recorder is the op's changes, one entry per CHANGED line.
type recorder struct{ whats []string }

func (r *recorder) Changed(what string) error {
	r.whats = append(r.whats, what)
	return nil
}

// world is a guest user's home, mode 0700, opened as the op's root.
type world struct {
	t      *testing.T
	home   Home
	layout paths.Layout
	team   paths.TeamID
	seat   paths.Seat
	clock  *fakeClock
}

func newWorld(t *testing.T) *world {
	t.Helper()
	dir := t.TempDir() + "/home"
	check(t, os.Mkdir(dir, 0o700))
	root, err := homefs.Open(dir, homefs.Owned)
	check(t, err)
	t.Cleanup(func() { check(t, root.Close()) })
	user, err := paths.ParseGuestUser("dev")
	check(t, err)
	layout, err := paths.HomeLayout(dir, paths.GuestRuntimeDir(user))
	check(t, err)
	tm, err := paths.ParseTeamID("ops")
	check(t, err)
	return &world{t: t, home: Home{Root: root, User: user}, layout: layout, team: tm, seat: layout.Seat(tm), clock: &fakeClock{}}
}

func userID(tm paths.TeamID, n int) string {
	return "@repo." + strconv.Itoa(n) + "+site.podman:" + paths.ServerName(tm)
}

func (w *world) bundle(n int) seat.Bundle {
	w.t.Helper()
	issuer, err := paths.ParseDigest("0123456789abcdef")
	check(w.t, err)
	server := paths.ServerName(w.team)
	return seat.Bundle{
		V: 1, Team: w.team, ServerName: server, UserID: userID(w.team, n), AdminID: "@admin:" + server,
		MainRoom: room, BaseURL: "http://127.0.0.1:8448", PlainHTTPHosts: []string{"127.0.0.1"},
		DeviceID: paths.SeatDevice, IssuedMS: 1_700_000_000_000, Issuer: issuer,
	}
}

func (w *world) issued(b seat.Bundle, kind seat.Kind, token string) []byte {
	w.t.Helper()
	secret, err := seat.ParseSecret(token)
	check(w.t, err)
	data, err := seat.Issued{Seat: b, Kind: kind, Secret: secret}.Encode()
	check(w.t, err)
	return data
}

func (w *world) place(data []byte) ([]string, error) {
	var r recorder
	err := PlaceBundle(w.t.Context(), w.home, w.clock, bytes.NewReader(data), &r)
	return r.whats, err
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("a broken stdin") }

// A stdin that cannot be read is an error, never an empty bundle or membership.
func TestAnUnreadableStdinIsAnError(t *testing.T) {
	w := newWorld(t)
	var r recorder
	if err := PlaceBundle(t.Context(), w.home, w.clock, failingReader{}, &r); err == nil || errors.As(err, new(*exits.Error)) {
		t.Fatalf("place-bundle: %v", err)
	}
	if err := Membership(w.home, failingReader{}, &r); err == nil || errors.As(err, new(*exits.Error)) {
		t.Fatalf("membership: %v", err)
	}
}

// GU1: a ~/.config that is a link or not a directory is refused, never followed.
func TestALinkedConfigIsRefused(t *testing.T) {
	w := newWorld(t)
	check(t, os.Mkdir(w.home.Root.Path()+"/elsewhere", 0o700))
	check(t, os.Symlink("elsewhere", w.home.Root.Path()+"/.config"))
	_, err := w.place(w.issued(w.bundle(1), seat.KindToken, loginOne))
	wantExit(t, err, 78, exits.CodeBundle)
	_, err = w.membership(w.desired("lxc-dev", 1))
	wantExit(t, err, 78, exits.CodeMembership)
}

// A failed replace or rename is an error that leaves the old file in place.
func TestAFailedWriteIsAnError(t *testing.T) {
	w := newWorld(t)
	w.placed(1, loginOne)
	check(t, os.Mkdir(w.seat.Dir()+"/.token.new", 0o700))
	if _, err := w.place(w.issued(w.bundle(1), seat.KindToken, loginTwo)); err == nil {
		t.Fatal("a replace over a blocked temporary succeeded")
	}
	store := w.store()
	check(t, os.WriteFile(w.seat.StoreAside(nowMS), nil, 0o600))
	if _, err := w.setAside(); err == nil || !bytes.Equal(w.read(w.seat.Store()), store) {
		t.Fatalf("a set-aside onto a taken name: %v", err)
	}
}

func (w *world) placeBetween(data []byte, between func()) ([]string, error) {
	var r recorder
	err := placeBundle(w.t.Context(), w.home, w.clock, bytes.NewReader(data), &r, between)
	return r.whats, err
}

// placed places seat n's bundle with token and fails the test on any error.
func (w *world) placed(n int, token string) []string {
	w.t.Helper()
	whats, err := w.place(w.issued(w.bundle(n), seat.KindToken, token))
	check(w.t, err)
	return whats
}

// store writes a store.json for seat 1.
func (w *world) store() []byte {
	w.t.Helper()
	run, err := paths.ParseRunID("0123456789abcdef")
	check(w.t, err)
	data, err := json.Marshal(state.NewStore(w.team, userID(w.team, 1), state.StoreRun{ID: run, StartedMS: 1}, state.Gate{}))
	check(w.t, err)
	check(w.t, os.WriteFile(w.seat.Store(), data, 0o600))
	return data
}

func (w *world) read(path string) []byte {
	w.t.Helper()
	data, err := os.ReadFile(path)
	check(w.t, err)
	return data
}

func (w *world) mode(path string) os.FileMode {
	w.t.Helper()
	st, err := os.Lstat(path)
	check(w.t, err)
	return st.Mode()
}

// lockInode makes seat.lock if absent and is its inode.
func (w *world) lockInode() uint64 {
	w.t.Helper()
	f, err := os.OpenFile(w.seat.Lock(), os.O_RDWR|os.O_CREATE, 0o600)
	check(w.t, err)
	check(w.t, f.Close())
	var st unix.Stat_t
	check(w.t, unix.Lstat(w.seat.Lock(), &st))
	return st.Ino
}

func (w *world) exists(path string) bool {
	w.t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		w.t.Fatal(err)
	}
	return err == nil
}

// pair reads the seat's pair as the link does, from the state root.
func (w *world) pair() (bundle.Pair, error) {
	w.t.Helper()
	root, err := homefs.Open(w.layout.State(), homefs.Private)
	check(w.t, err)
	defer func() { check(w.t, root.Close()) }()
	return bundle.ReadPair(root, w.seat)
}

// hold holds seat.lock from a second real process until the test ends.
func (w *world) hold() *reexec.Process {
	w.t.Helper()
	w.lockInode()
	return reexec.Start(w.t, "flock", w.seat.Lock())
}

// wantExit fails the test unless err is an error object with exit and code.
func wantExit(t *testing.T, err error, exit int, code string) {
	t.Helper()
	e, ok := errors.AsType[*exits.Error](err)
	if !ok || int(e.Exit) != exit || e.Code != code {
		t.Fatalf("error %v, want exit %d code %s", err, exit, code)
	}
	for _, token := range []string{loginOne, loginTwo} {
		if strings.Contains(e.Message, token) {
			t.Fatalf("the token is in the error: %q", e.Message)
		}
	}
}

func wantWhats(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("changes %q, want %q", got, want)
	}
}

// GU3: no seat directory is placed whole by a claimed directory renamed in; leftovers go first.
func TestPlaceBundleIntoNoSeat(t *testing.T) {
	w := newWorld(t)
	check(t, os.MkdirAll(w.layout.State()+"/ops.new-0011223344556677", 0o700))
	check(t, os.WriteFile(w.layout.State()+"/ops.new-0011223344556677/token", []byte("old"), 0o600))
	check(t, os.WriteFile(w.layout.State()+"/ops.new-8899aabbccddeeff", nil, 0o600))
	check(t, os.Chmod(w.home.Root.Path()+"/.config/teamchat", 0o700))
	check(t, os.Chmod(w.layout.State(), 0o700))
	other := w.layout.State() + "/opsx.new-0011223344556677"
	check(t, os.Mkdir(other, 0o700))
	wantWhats(t, w.placed(1, loginOne), WhatSeat)
	entries, err := os.ReadDir(w.layout.State())
	check(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	wantWhats(t, names, "ops", "opsx.new-0011223344556677")
	p, err := w.pair()
	check(t, err)
	if p.Token.Reveal() != loginOne || p.Bundle.Bundle.UserID != userID(w.team, 1) {
		t.Fatalf("pair %+v", p)
	}
	for path, want := range map[string]os.FileMode{
		w.seat.Dir(): os.ModeDir | 0o700, w.seat.Token(): 0o600, w.seat.Bundle(): 0o600,
	} {
		if got := w.mode(path); got != want {
			t.Errorf("%s mode %v, want %v", path, got, want)
		}
	}
}

// GU1: the directories a placement makes are 0700, from a home with no ~/.config.
func TestPlaceBundleMakesItsDirectoriesPrivate(t *testing.T) {
	w := newWorld(t)
	w.placed(1, loginOne)
	for _, dir := range []string{w.home.Root.Path() + "/.config", w.home.Root.Path() + "/.config/teamchat", w.layout.State()} {
		if got := w.mode(dir); got != os.ModeDir|0o700 {
			t.Errorf("%s mode %v", dir, got)
		}
	}
}

// GU3: the same identity keeps store.json and seat.lock; only what differs is replaced.
func TestPlaceBundleSameIdentityKeepsStoreAndLock(t *testing.T) {
	w := newWorld(t)
	w.placed(1, loginOne)
	store := w.store()
	ino := w.lockInode()
	wantWhats(t, w.placed(1, loginOne))
	wantWhats(t, w.placed(1, loginTwo), WhatToken)
	reissued := w.bundle(1)
	reissued.IssuedMS++
	whats, err := w.place(w.issued(reissued, seat.KindToken, loginTwo))
	check(t, err)
	wantWhats(t, whats, WhatBundle)
	if !bytes.Equal(w.read(w.seat.Store()), store) || w.lockInode() != ino {
		t.Fatal("the store or seat.lock changed")
	}
	if len(w.clock.sleeps) != 0 {
		t.Fatalf("the same identity waited on the seat lock: %v", w.clock.sleeps)
	}
	p, err := w.pair()
	check(t, err)
	if p.Token.Reveal() != loginTwo || p.Bundle.Bundle.IssuedMS != reissued.IssuedMS {
		t.Fatalf("pair %+v", p)
	}
}

// GU3: a session's held seat.json of the same user is the same identity.
func TestPlaceBundleOverAHeldSeatJSONOfTheSameUser(t *testing.T) {
	w := newWorld(t)
	w.placed(1, loginOne)
	claim, err := paths.ParseClaimID("00112233aabbccdd")
	check(t, err)
	held, err := seat.HeldBundle{Bundle: w.bundle(1), Claim: claim}.Encode()
	check(t, err)
	check(t, os.WriteFile(w.seat.Bundle(), held, 0o600))
	store := w.store()
	wantWhats(t, w.placed(1, loginOne), WhatBundle)
	if !bytes.Equal(w.read(w.seat.Store()), store) {
		t.Fatal("the store changed")
	}
}

// GU3: a different user_id or seat, or an undecodable seat.json, sets the store aside under
// the seat lock, then places the files.
func TestPlaceBundleDifferentIdentitySetsTheStoreAside(t *testing.T) {
	for name, prepare := range map[string]func(w *world){
		"another user": func(w *world) { w.placed(1, loginOne) },
		"undecodable":  func(w *world) { w.placed(1, loginOne); check(w.t, os.WriteFile(w.seat.Bundle(), []byte("{"), 0o600)) },
		"no seat.json": func(w *world) { w.placed(1, loginOne); check(w.t, os.Remove(w.seat.Bundle())) },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			prepare(w)
			store := w.store()
			ino := w.lockInode()
			wantWhats(t, w.placed(2, loginTwo), WhatStore, WhatToken, WhatBundle)
			if w.exists(w.seat.Store()) || !bytes.Equal(w.read(w.seat.StoreAside(nowMS)), store) {
				t.Fatal("the store is not set aside")
			}
			if w.lockInode() != ino {
				t.Fatal("seat.lock is a new inode")
			}
			p, err := w.pair()
			check(t, err)
			if p.Token.Reveal() != loginTwo || p.Bundle.Bundle.UserID != userID(w.team, 2) {
				t.Fatalf("pair %+v", p)
			}
		})
	}
}

// GU3: a different identity with no store sets nothing aside.
func TestPlaceBundleDifferentIdentityWithNoStore(t *testing.T) {
	w := newWorld(t)
	w.placed(1, loginOne)
	wantWhats(t, w.placed(2, loginOne), WhatBundle)
}

// GU3, LK3: a different identity under a held seat lock is 75 and changes nothing.
func TestPlaceBundleDifferentIdentityUnderAHeldLock(t *testing.T) {
	w := newWorld(t)
	w.placed(1, loginOne)
	store := w.store()
	ino := w.lockInode()
	w.hold()
	token, seatJSON := w.read(w.seat.Token()), w.read(w.seat.Bundle())
	whats, err := w.place(w.issued(w.bundle(2), seat.KindToken, loginTwo))
	wantExit(t, err, 75, exits.CodeSeatHeld)
	wantWhats(t, whats)
	if !bytes.Equal(w.read(w.seat.Store()), store) || !bytes.Equal(w.read(w.seat.Token()), token) ||
		!bytes.Equal(w.read(w.seat.Bundle()), seatJSON) || w.lockInode() != ino {
		t.Fatal("a held seat changed")
	}
	if len(w.clock.sleeps) != 2 {
		t.Fatalf("sleeps %v, want two waits between three tries", w.clock.sleeps)
	}
}

// GU3, MS3: a link reading the pair between the token's rename and seat.json's never meets a
// 78, in either case.
func TestALinkReadingBetweenTheRenames(t *testing.T) {
	for name, n := range map[string]int{"same identity": 1, "another identity": 2} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.placed(1, loginOne)
			w.store()
			called := false
			whats, err := w.placeBetween(w.issued(w.bundle(n), seat.KindToken, loginTwo), func() {
				called = true
				if _, err := w.pair(); err != nil {
					t.Errorf("a read between the renames: %v", err)
				}
			})
			check(t, err)
			if !called || len(whats) == 0 {
				t.Fatalf("between called %v, changes %q", called, whats)
			}
		})
	}
}

// GU3: a password bundle, a bundle that does not decode and one over its cap are 78 bundle.
func TestPlaceBundleRefusesWhatIsNotATokenBundle(t *testing.T) {
	w := newWorld(t)
	for name, data := range map[string][]byte{
		"password": w.issued(w.bundle(1), seat.KindPassword, loginOne),
		"garbage":  []byte("{"),
		"oversize": bytes.Repeat([]byte(" "), maxIssued+1),
	} {
		whats, err := w.place(data)
		wantExit(t, err, 78, exits.CodeBundle)
		wantWhats(t, whats)
		if w.exists(w.seat.Dir()) {
			t.Fatalf("%s: a seat was placed", name)
		}
	}
}

// GU1: a seat directory or file another uid owns is refused, never written through.
func TestPlaceBundleRefusesAnotherUsersSeat(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root to make a file another uid's")
	}
	w := newWorld(t)
	w.placed(1, loginOne)
	check(t, os.Lchown(w.seat.Dir(), otherUID, otherUID))
	_, err := w.place(w.issued(w.bundle(1), seat.KindToken, loginTwo))
	wantExit(t, err, 78, exits.CodeBundle)
	check(t, os.Lchown(w.seat.Dir(), 0, 0))
	check(t, os.Lchown(w.seat.Token(), otherUID, otherUID))
	_, err = w.place(w.issued(w.bundle(2), seat.KindToken, loginTwo))
	wantExit(t, err, 78, exits.CodeBundle)
	check(t, os.Lchown(w.layout.State(), otherUID, otherUID))
	_, err = w.place(w.issued(w.bundle(1), seat.KindToken, loginTwo))
	wantExit(t, err, 78, exits.CodeBundle)
}

func (w *world) membership(data []byte) ([]string, error) {
	var r recorder
	err := Membership(w.home, bytes.NewReader(data), &r)
	return r.whats, err
}

func (w *world) desired(session string, generation int64) []byte {
	w.t.Helper()
	name, err := paths.ParseSessionName(session)
	check(w.t, err)
	d := state.Desired{
		Session: name, Generation: generation, WrittenMS: nowMS,
		Seats: []state.DesiredSeat{{Team: w.team, Seat: "1"}}, Farewell: []state.Farewell{}, Inbound: "accept",
	}
	data, err := d.Encode()
	check(w.t, err)
	return data
}

// GU4: desired.json is written canonically, 0600 in 0700 directories; the same bytes print
// nothing.
func TestMembershipWritesDesired(t *testing.T) {
	w := newWorld(t)
	data := w.desired("lxc-dev", 1)
	var spaced bytes.Buffer
	check(t, json.Indent(&spaced, data, "", "  "))
	whats, err := w.membership(spaced.Bytes())
	check(t, err)
	wantWhats(t, whats, WhatDesired)
	if !bytes.Equal(w.read(w.layout.DesiredFile()), data) || w.mode(w.layout.DesiredFile()) != 0o600 ||
		w.mode(w.layout.Desired()) != os.ModeDir|0o700 {
		t.Fatal("desired.json is not the canonical bytes, 0600 in a 0700 directory")
	}
	whats, err = w.membership(data)
	check(t, err)
	wantWhats(t, whats)
	whats, err = w.membership(w.desired("lxc-dev", 2))
	check(t, err)
	wantWhats(t, whats, WhatDesired)
}

// GU4: a desired membership for another session, or one that does not decode, is 78
// membership and writes nothing.
func TestMembershipRefuses(t *testing.T) {
	w := newWorld(t)
	for _, data := range [][]byte{w.desired("lxc-other", 1), w.desired("ccy-dev", 1), []byte("{}"), bytes.Repeat([]byte(" "), state.MaxDesired+1)} {
		whats, err := w.membership(data)
		wantExit(t, err, 78, exits.CodeMembership)
		wantWhats(t, whats)
	}
	if w.exists(w.layout.DesiredFile()) {
		t.Fatal("a refused membership was written")
	}
}

// GU1: a membership directory another uid owns is refused.
func TestMembershipRefusesAnotherUsersDirectory(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root to make a file another uid's")
	}
	w := newWorld(t)
	_, err := w.membership(w.desired("lxc-dev", 1))
	check(t, err)
	check(t, os.Lchown(w.layout.Desired(), otherUID, otherUID))
	_, err = w.membership(w.desired("lxc-dev", 2))
	wantExit(t, err, 78, exits.CodeMembership)
}

func (w *world) remove() ([]string, error) {
	var r recorder
	err := Remove(w.t.Context(), w.home, w.clock, w.team, &r)
	return r.whats, err
}

// GU5: every file of the seat but seat.lock goes, under the seat lock; nothing to remove
// prints nothing.
func TestRemove(t *testing.T) {
	w := newWorld(t)
	whats, err := w.remove()
	check(t, err)
	wantWhats(t, whats)
	w.placed(1, loginOne)
	w.store()
	check(t, os.Symlink("/nowhere", w.seat.Dir()+"/link"))
	ino := w.lockInode()
	whats, err = w.remove()
	check(t, err)
	wantWhats(t, whats, WhatSeat)
	entries, err := os.ReadDir(w.seat.Dir())
	check(t, err)
	if len(entries) != 1 || entries[0].Name() != "seat.lock" || w.lockInode() != ino {
		t.Fatalf("left %v", entries)
	}
	whats, err = w.remove()
	check(t, err)
	wantWhats(t, whats)
}

// GU5: a directory in the seat directory is refused before anything is removed.
func TestRemoveRefusesADirectory(t *testing.T) {
	w := newWorld(t)
	w.placed(1, loginOne)
	check(t, os.Mkdir(w.seat.Dir()+"/sub", 0o700))
	whats, err := w.remove()
	wantExit(t, err, 78, exits.CodeBundle)
	wantWhats(t, whats)
	if !w.exists(w.seat.Token()) {
		t.Fatal("a refused removal removed the token")
	}
}

// GU5, LK3: removal against a held seat is 75, removes nothing and keeps seat.lock's inode.
func TestRemoveUnderAHeldLock(t *testing.T) {
	w := newWorld(t)
	w.placed(1, loginOne)
	ino := w.lockInode()
	w.hold()
	whats, err := w.remove()
	wantExit(t, err, 75, exits.CodeSeatHeld)
	wantWhats(t, whats)
	if !w.exists(w.seat.Token()) || w.lockInode() != ino {
		t.Fatal("a held seat changed")
	}
}

func (w *world) setAside() ([]string, error) {
	var r recorder
	err := SetAside(w.t.Context(), w.clock, w.layout.State(), w.team, &r)
	return r.whats, err
}

// SA1: store.json is renamed aside under the seat lock; no store is a success with no line.
func TestSetAside(t *testing.T) {
	w := newWorld(t)
	whats, err := w.setAside()
	check(t, err)
	wantWhats(t, whats)
	w.placed(1, loginOne)
	whats, err = w.setAside()
	check(t, err)
	wantWhats(t, whats)
	store := w.store()
	ino := w.lockInode()
	whats, err = w.setAside()
	check(t, err)
	wantWhats(t, whats, WhatStore)
	if w.exists(w.seat.Store()) || !bytes.Equal(w.read(w.seat.StoreAside(nowMS)), store) || w.lockInode() != ino {
		t.Fatal("the store was not set aside, or seat.lock changed")
	}
}

// SA1, LK3: against a held seat set-aside is 75, and seat.lock stays the same inode.
func TestSetAsideUnderAHeldLock(t *testing.T) {
	w := newWorld(t)
	w.placed(1, loginOne)
	store := w.store()
	ino := w.lockInode()
	holder := w.hold()
	whats, err := w.setAside()
	wantExit(t, err, 75, exits.CodeSeatHeld)
	wantWhats(t, whats)
	if !bytes.Equal(w.read(w.seat.Store()), store) || w.lockInode() != ino {
		t.Fatal("a held seat changed")
	}
	holder.Release()
	whats, err = w.setAside()
	check(t, err)
	wantWhats(t, whats, WhatStore)
	if w.lockInode() != ino {
		t.Fatal("seat.lock is a new inode")
	}
}

// SA1: a state root or seat directory that is not as the member must have it is 78 bundle.
func TestSetAsideRefusesALinkedSeat(t *testing.T) {
	w := newWorld(t)
	w.placed(1, loginOne)
	check(t, os.Rename(w.seat.Dir(), w.layout.State()+"/real"))
	check(t, os.Symlink("real", w.seat.Dir()))
	_, err := w.setAside()
	wantExit(t, err, 78, exits.CodeBundle)
	linked := w.home.Root.Path() + "/linked-state"
	check(t, os.Symlink(w.layout.State(), linked))
	var r recorder
	wantExit(t, SetAside(t.Context(), w.clock, linked, w.team, &r), 78, exits.CodeBundle)
	wantExit(t, SetAside(t.Context(), w.clock, "relative", w.team, &r), 78, exits.CodeBundle)
}

// The fixed sets hold every what an op prints (TB2.4).
func TestEveryOpsWhatIsInItsSet(t *testing.T) {
	for set, whats := range map[string][]string{
		"place-bundle": PlaceBundleChanges, "membership": MembershipChanges, "remove": RemoveChanges, "set-aside": SetAsideChanges,
	} {
		if len(whats) == 0 || slices.Contains(whats, "") {
			t.Errorf("%s: %q", set, whats)
		}
	}
	for _, c := range []struct {
		set  []string
		what string
	}{{PlaceBundleChanges, WhatSeat}, {PlaceBundleChanges, WhatToken}, {PlaceBundleChanges, WhatBundle}, {PlaceBundleChanges, WhatStore}, {MembershipChanges, WhatDesired}, {RemoveChanges, WhatSeat}, {SetAsideChanges, WhatStore}} {
		if !slices.Contains(c.set, c.what) {
			t.Errorf("%s is not in its set %q", c.what, c.set)
		}
	}
}
