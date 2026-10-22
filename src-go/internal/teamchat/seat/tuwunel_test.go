//go:build tuwunel

package seat

// Take and release against a real Tuwunel (ST6 on the server fakehs stands in for, SPEC M44's
// harness): `go test -tags tuwunel` with FAKEHS_TUWUNEL_BIN naming the binary. A token an
// earlier holder of the seat's device kept is dead after a take, and the take's own token is
// dead after the release.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/tuwunelhs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const tuwunelTestLimit = 2 * time.Minute

// realServer is a running Tuwunel with the team's admin registered and the seat's account
// made with testPassword.
type realServer struct {
	t     *testing.T
	base  string
	clock *fakeClock
}

func startReal(ctx context.Context, t *testing.T, clock *fakeClock) realServer {
	t.Helper()
	server := paths.ServerName(mustTeam(t))
	hs := tuwunelhs.Start(ctx, t, server)
	anon, err := matrix.NewAdmin(matrix.Config{BaseURL: hs.Base})
	if err != nil {
		t.Fatalf("NewAdmin: %v", err)
	}
	t.Cleanup(anon.Close)
	sess, err := anon.Register(ctx, hs.RegKey, matrix.Registration{
		Username: proto.AdminLocalpart, Password: tuwunelhs.RandomKey(t), Admin: true,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	admin, err := anon.WithToken(sess.AccessToken.Reveal())
	if err != nil {
		t.Fatalf("WithToken: %v", err)
	}
	if _, err := admin.CreateAccount(ctx, "@"+testHandle+":"+server, testPassword, ""); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	return realServer{t: t, base: hs.Base, clock: clock}
}

func (r realServer) member(token string) *matrix.Client {
	r.t.Helper()
	c, err := matrix.NewMember(matrix.Config{BaseURL: r.base, PlainHTTPHosts: []string{loopback}, Token: token, Clock: r.clock})
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(c.Close)
	return c
}

// alive is whether token is a live token of the seat's account; a refusal other than
// M_UNKNOWN_TOKEN fails the test.
func (r realServer) alive(ctx context.Context, token string) bool {
	r.t.Helper()
	_, err := r.member(token).Whoami(ctx)
	if e, failed := errors.AsType[*matrix.Error](err); failed && e.Errcode == "M_UNKNOWN_TOKEN" {
		return false
	}
	if err != nil {
		r.t.Fatalf("whoami: %v", err)
	}
	return true
}

// TestTuwunelTakeAndRelease is ST6 against the real server: the take ends a kept earlier token
// of TCSEAT, and the release ends the take's.
func TestTuwunelTakeAndRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), tuwunelTestLimit)
	defer cancel()
	clock := newClock()
	hs := startReal(ctx, t, clock)
	home := t.TempDir()
	key := mustKey(t, "c0")
	store, err := Open(Config{
		StateHome: filepath.Join(home, "state"), RuntimeDir: filepath.Join(home, "run"), Checkout: key,
		Clock: clock, Credential: Password{Clock: clock}, Sessions: &sessions{}, Announcer: &announcer{outcome: Outcome{Posted: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	seat := Seat{Team: mustTeam(t), Name: mustSeatName(t, testHandle)}
	pool := store.store.PoolSeat(key, seat.Team, seat.Name)
	data, err := bundleAt(t, hs.base).Encode()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, pool.Password(), testPassword)
	writeFile(t, pool.Bundle(), string(data))

	kept, err := hs.member(signedOut).LoginDevice(ctx, testHandle, testPassword, paths.SeatDevice)
	if err != nil {
		t.Fatalf("the earlier holder's login: %v", err)
	}
	stale := kept.AccessToken.Reveal()
	if !hs.alive(ctx, stale) {
		t.Fatal("the earlier holder's token is not live before the take")
	}

	session := mustSession(t, "s1")
	a, err := store.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	taken, err := store.Take(ctx, a, Take{Seat: seat, Session: session, State: StateActive})
	if err = errors.Join(err, a.Unlock()); err != nil {
		t.Fatalf("take: %v", err)
	}
	if hs.alive(ctx, stale) {
		t.Error("the earlier holder's token survived the take")
	}
	token := readFile(t, taken.Dir+"/token")
	if !hs.alive(ctx, token) {
		t.Fatal("the take's token is not live")
	}

	if err := store.Release(ctx, Release{Seat: seat, Session: session, Leaving: true}); err != nil {
		t.Fatalf("release: %v", err)
	}
	if hs.alive(ctx, token) {
		t.Error("the take's token survived the release")
	}
}
