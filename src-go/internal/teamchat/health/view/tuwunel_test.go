//go:build tuwunel

package view

// The pool-password watch against a real Tuwunel (VW3 on the server fakehs stands in for):
// `go test -tags tuwunel` with FAKEHS_TUWUNEL_BIN naming the binary. A member signed in on its
// seat's device alone is no problem; a second device, a deactivated account and a missing one
// each are, as the server answers them.

import (
	"context"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/tuwunelhs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const tuwunelTestLimit = 2 * time.Minute

// realServer is a Tuwunel with its admin and coord's account signed in on its seat's device; it
// gives the admin, a client with no token, and coord's password.
func realServer(ctx context.Context, t *testing.T) (*matrix.Admin, *matrix.Admin, string) {
	t.Helper()
	hs := tuwunelhs.Start(ctx, t, server)
	anon, err := matrix.NewAdmin(matrix.Config{BaseURL: hs.Base})
	if err != nil {
		t.Fatalf("NewAdmin: %v", err)
	}
	t.Cleanup(anon.Close)
	sess, err := anon.Register(ctx, hs.RegKey, matrix.Registration{Username: proto.AdminLocalpart, Password: tuwunelhs.RandomKey(t), Admin: true})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	a, err := anon.WithToken(sess.AccessToken.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	password := tuwunelhs.RandomKey(t)
	if _, err = a.CreateAccount(ctx, user(coord), password, ""); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if _, err = anon.LoginDevice(ctx, coord, password, paths.SeatDevice); err != nil {
		t.Fatalf("LoginDevice: %v", err)
	}
	return a, anon, password
}

func TestTuwunelSeatWatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), tuwunelTestLimit)
	defer cancel()
	a, anon, password := realServer(ctx, t)
	roster := apply.NewRosterFile(alphaID(t))
	roster.Members[coord] = apply.Member{State: apply.MemberActive, Kind: "podman", HumanText: true, Cred: "password"}
	roster.Coordinator = coord
	watch := func() []string {
		t.Helper()
		r := &reader{a: a, server: server, in: Input{Roster: roster}}
		if err := r.watch(ctx); err != nil {
			t.Fatalf("watch: %v", err)
		}
		return whats(r.view.Problems)
	}
	if got := watch(); len(got) != 0 {
		t.Fatalf("a seat on its own device: %v", got)
	}
	if _, err := anon.Login(ctx, coord, password, "another"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got := watch(); len(got) != 1 || got[0] != coord+" has a device that is not its seat's | ccy-teams reissue "+coord {
		t.Fatalf("a second device: %v", got)
	}
	if err := a.SetDeactivated(ctx, user(coord), true); err != nil {
		t.Fatalf("SetDeactivated: %v", err)
	}
	if got := watch(); len(got) != 1 || got[0] != coord+" is deactivated | name another coordinator in the declaration, then teamchat-admin member retire "+coord+" and add a new member" {
		t.Fatalf("deactivated: %v", got)
	}
	delete(roster.Members, coord)
	roster.Members[worker] = apply.Member{State: apply.MemberParked, Kind: "podman", HumanText: true, Cred: "password"}
	if got := watch(); len(got) != 1 || got[0] != worker+" has no account | ccy-teams reissue "+worker {
		t.Fatalf("no account: %v", got)
	}
}
