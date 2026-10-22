package bundle

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

const (
	testToken = "syt_member-token-1"
	room      = "!" + "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func team(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	check(t, err)
	return id
}

func userID(tm paths.TeamID, n int) string {
	return "@repo." + strconv.Itoa(n) + "+site.podman:" + paths.ServerName(tm)
}

// poolBundle is seat n's pool seat.json for tm, its member URL on loopback.
func poolBundle(t *testing.T, tm paths.TeamID, n int) seat.Bundle {
	t.Helper()
	issuer, err := paths.ParseDigest("0123456789abcdef")
	check(t, err)
	server := paths.ServerName(tm)
	return seat.Bundle{
		V: 1, Team: tm, ServerName: server, UserID: userID(tm, n), AdminID: "@admin:" + server,
		MainRoom: room, BaseURL: "http://127.0.0.1:8448", PlainHTTPHosts: []string{"127.0.0.1"},
		DeviceID: paths.SeatDevice, IssuedMS: 1_700_000_000_000, Issuer: issuer,
	}
}

func encodeBundle(t *testing.T, b seat.Bundle) []byte {
	t.Helper()
	data, err := b.Encode()
	check(t, err)
	return data
}

// world is a member's state root, 0700, holding the seat directory of team ops.
type world struct {
	t      *testing.T
	root   *homefs.Root
	layout paths.Layout
	seat   paths.Seat
}

func newWorld(t *testing.T) *world {
	t.Helper()
	l, err := paths.HomeLayout(t.TempDir(), t.TempDir())
	check(t, err)
	s := l.Seat(team(t, "ops"))
	check(t, os.MkdirAll(s.Dir(), 0o700))
	check(t, os.Chmod(l.State(), 0o700))
	check(t, os.Chmod(s.Dir(), 0o700))
	root, err := homefs.Open(l.State(), homefs.Private)
	check(t, err)
	t.Cleanup(func() { check(t, root.Close()) })
	return &world{t: t, root: root, layout: l, seat: s}
}

func (w *world) write(path string, data []byte) {
	w.t.Helper()
	check(w.t, os.WriteFile(path, data, 0o600))
	check(w.t, os.Chmod(path, 0o600))
}

// place writes a guest's pair for seat 1 of team ops.
func (w *world) place() {
	w.t.Helper()
	w.write(w.seat.Token(), []byte(testToken))
	w.write(w.seat.Bundle(), encodeBundle(w.t, poolBundle(w.t, team(w.t, "ops"), 1)))
}

// store writes a fresh store.json for userID's seat of team ops.
func (w *world) store(id string) {
	w.t.Helper()
	run, err := paths.ParseRunID("0123456789abcdef")
	check(w.t, err)
	data, err := json.Marshal(state.NewStore(team(w.t, "ops"), id, state.StoreRun{ID: run, StartedMS: 1}, state.Gate{}))
	check(w.t, err)
	_, err = state.DecodeStore(data)
	check(w.t, err)
	w.write(w.seat.Store(), data)
}

func (w *world) read() (Pair, error) { return ReadPair(w.root, w.seat) }

// configCode is err's code when it is a 78 error object, else "".
func configCode(t *testing.T, err error) string {
	t.Helper()
	var e *exits.Error
	if !errors.As(err, &e) {
		return ""
	}
	if e.Exit != exits.ExitConfig {
		t.Errorf("exit %d, want 78: %v", e.Exit, err)
	}
	if strings.Contains(e.Message, testToken) {
		t.Errorf("the token is in the error: %q", e.Message)
	}
	return e.Code
}

// MS3: a guest's pair and a session's pair are read whole, with or without a store.
func TestReadPair(t *testing.T) {
	w := newWorld(t)
	w.place()
	p, err := w.read()
	check(t, err)
	if p.Team.String() != "ops" || p.Bundle.Held != nil || p.Bundle.Bundle.UserID != userID(p.Team, 1) || p.Token.Reveal() != testToken {
		t.Errorf("a guest's pair: %+v", p)
	}
	w.store(userID(p.Team, 1))
	claim, err := paths.ParseClaimID("00112233aabbccdd")
	check(t, err)
	held, err := seat.HeldBundle{Bundle: poolBundle(t, p.Team, 1), Claim: claim}.Encode()
	check(t, err)
	w.write(w.seat.Bundle(), held)
	p, err = w.read()
	check(t, err)
	if p.Bundle.Held == nil || p.Bundle.Held.Claim != claim {
		t.Errorf("a session's pair: %+v", p)
	}
}

// MS3: no seat.json is no bundle yet, not a refusal.
func TestReadPairNoBundle(t *testing.T) {
	w := newWorld(t)
	if _, err := w.read(); !errors.Is(err, fs.ErrNotExist) || configCode(t, err) != "" {
		t.Errorf("no seat.json: %v", err)
	}
	if _, err := ReadPair(w.root, w.layout.Seat(team(t, "dev"))); !errors.Is(err, fs.ErrNotExist) || configCode(t, err) != "" {
		t.Errorf("no seat directory: %v", err)
	}
}

// MS3: a pair whose seat.json changed between its two reads is retried once at once; torn
// twice it is ErrTorn, never a 78.
func TestReadPairTorn(t *testing.T) {
	w := newWorld(t)
	w.place()
	other := encodeBundle(t, poolBundle(t, team(t, "ops"), 2))
	swaps := 0
	swap := func() {
		swaps++
		next := w.seat.Bundle() + ".next"
		w.write(next, other)
		check(t, os.Rename(next, w.seat.Bundle()))
	}
	p, err := readPair(w.root, w.seat, func() {
		if swaps == 0 {
			swap()
		}
	})
	check(t, err)
	if swaps != 1 || p.Bundle.Bundle.UserID != userID(p.Team, 2) {
		t.Errorf("after one swap: %d swaps, %+v", swaps, p)
	}
	_, err = readPair(w.root, w.seat, swap)
	if !errors.Is(err, ErrTorn) || configCode(t, err) != "" || swaps != 3 {
		t.Errorf("swapped on every read: %v after %d swaps", err, swaps)
	}
	// The same inode with other bytes, written in place, is torn too.
	rewrites := 0
	_, err = readPair(w.root, w.seat, func() {
		rewrites++
		w.write(w.seat.Bundle(), encodeBundle(t, poolBundle(t, team(t, "ops"), 3+rewrites%2)))
	})
	if !errors.Is(err, ErrTorn) {
		t.Errorf("rewritten in place: %v", err)
	}
	// A seat.json gone between the reads is torn, and read again it is no bundle.
	_, err = readPair(w.root, w.seat, func() {
		if rmErr := os.Remove(w.seat.Bundle()); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			t.Fatal(rmErr)
		}
	})
	if !errors.Is(err, fs.ErrNotExist) || configCode(t, err) != "" {
		t.Errorf("removed between the reads: %v", err)
	}
}

// MS3, B11: each broken pair is 78 bundle, and the error never holds the token.
func TestReadPairRefusals(t *testing.T) {
	ops := team(t, "ops")
	for name, breakIt := range map[string]func(w *world){
		"a password bundle": func(w *world) { w.write(w.seat.Password(), []byte(testToken)) },
		"another team's seat.json": func(w *world) {
			w.write(w.seat.Bundle(), encodeBundle(t, poolBundle(t, team(t, "dev"), 1)))
		},
		"a user_id the store was not written for": func(w *world) { w.store(userID(ops, 2)) },
		"a token readable by others":              func(w *world) { check(t, os.Chmod(w.seat.Token(), 0o644)) },
		"a token 0400":                            func(w *world) { check(t, os.Chmod(w.seat.Token(), 0o400)) },
		"a token of another uid":                  func(w *world) { check(t, os.Lchown(w.seat.Token(), 4242, -1)) },
		"a token of two lines":                    func(w *world) { w.write(w.seat.Token(), []byte(testToken+"\n"+testToken)) },
		"an empty token":                          func(w *world) { w.write(w.seat.Token(), nil) },
		"a token over its cap":                    func(w *world) { w.write(w.seat.Token(), []byte(strings.Repeat("a", seat.MaxSecret+1))) },
		"no token":                                func(w *world) { check(t, os.Remove(w.seat.Token())) },
		"a token that is a link": func(w *world) {
			check(t, os.Rename(w.seat.Token(), w.seat.Dir()+"/real"))
			check(t, os.Symlink("real", w.seat.Token()))
		},
		"an undecodable seat.json":        func(w *world) { w.write(w.seat.Bundle(), []byte(`{"v":1}`)) },
		"a seat.json of another uid":      func(w *world) { check(t, os.Lchown(w.seat.Bundle(), 4242, -1)) },
		"a seat.json over its cap":        func(w *world) { w.write(w.seat.Bundle(), []byte(strings.Repeat(" ", seat.MaxBundle+1))) },
		"a seat directory open to others": func(w *world) { check(t, os.Chmod(w.seat.Dir(), 0o755)) },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.place()
			w.store(userID(ops, 1))
			breakIt(w)
			_, err := w.read()
			if code := configCode(t, err); code != exits.CodeBundle {
				t.Errorf("code %q: %v", code, err)
			}
		})
	}
}

// MS3: a store that does not decode is 78 store, not a bundle fault.
func TestReadPairBadStore(t *testing.T) {
	w := newWorld(t)
	w.place()
	w.write(w.seat.Store(), []byte(`{"v":1}`))
	if _, err := w.read(); configCode(t, err) != exits.CodeStore {
		t.Errorf("a broken store: %v", err)
	}
}

// MS3: a seat directory whose name is no team id, such as a placement's claim, is 78 bundle.
func TestReadPairDirectoryNotATeam(t *testing.T) {
	w := newWorld(t)
	claim, err := paths.ParseClaimID("00112233aabbccdd")
	check(t, err)
	w.seat = w.layout.SeatClaim(team(t, "ops"), claim)
	check(t, os.Mkdir(w.seat.Dir(), 0o700))
	w.place()
	if _, err := w.read(); configCode(t, err) != exits.CodeBundle {
		t.Errorf("a claim directory: %v", err)
	}
}

// B11: two held seats' pairs sharing a user id or a token are 78 bundle; distinct ones pass.
func TestDistinct(t *testing.T) {
	ops, dev := team(t, "ops"), team(t, "dev")
	pair := func(tm paths.TeamID, n int, token string) Pair {
		secret, err := seat.ParseSecret(token)
		check(t, err)
		return Pair{Team: tm, Bundle: seat.MemberBundle{Bundle: poolBundle(t, tm, n)}, Token: secret}
	}
	if err := Distinct([]Pair{pair(ops, 1, "a"), pair(dev, 1, "b")}); err != nil {
		t.Errorf("distinct pairs: %v", err)
	}
	sameUser := pair(dev, 1, "c")
	sameUser.Bundle.Bundle.UserID = userID(ops, 1)
	for name, pairs := range map[string][]Pair{
		"a shared user id": {pair(ops, 1, "a"), sameUser},
		"a shared token":   {pair(ops, 1, "a"), pair(dev, 2, "b"), pair(dev, 3, "a")},
	} {
		if code := configCode(t, Distinct(pairs)); code != exits.CodeBundle {
			t.Errorf("%s: code %q", name, code)
		}
	}
}
