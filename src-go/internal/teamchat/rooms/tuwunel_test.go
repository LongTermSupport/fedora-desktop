//go:build tuwunel

package rooms

// The engine against a real Tuwunel (RE10 on the server fakehs stands in for, SPEC M44's
// harness): `go test -tags tuwunel` with FAKEHS_TUWUNEL_BIN naming the binary. A new team's
// rooms are created, a second run with nothing changed writes nothing, and proto's trust checks
// accept the rooms as the server holds them.

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/tuwunelhs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const tuwunelTestLimit = 2 * time.Minute

// realAdmin registers the team's admin account through the registration key, makes every
// account the roster names, and gives the admin client the engine uses.
func realAdmin(ctx context.Context, t *testing.T, hs tuwunelhs.Server) *matrix.Admin {
	t.Helper()
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
	if sess.UserID != admin {
		t.Fatalf("registered %s, want %s", sess.UserID, admin)
	}
	a, err := anon.WithToken(sess.AccessToken.Reveal())
	if err != nil {
		t.Fatalf("WithToken: %v", err)
	}
	for _, id := range []string{alice, bob, coord, worker} {
		if _, err := a.CreateAccount(ctx, id, tuwunelhs.RandomKey(t), ""); err != nil {
			t.Fatalf("CreateAccount %s: %v", id, err)
		}
	}
	return a
}

func TestTuwunelConverge(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), tuwunelTestLimit)
	defer cancel()
	hs := tuwunelhs.Start(ctx, t, server)
	w := &world{admin: realAdmin(ctx, t, hs), team: Team{Server: server, Team: team, Roster: roster()}}
	// The server answer fakehs's AdminRoom quirk records: the registered admin is in one room,
	// the server's own, which it did not create.
	if joined, err := w.admin.JoinedRooms(ctx); err != nil || len(joined) != 1 || len(w.adminCreated(t)) != 0 {
		t.Fatalf("a registered admin is in %v (%v), want the server's admin room alone", joined, err)
	}

	rec := &recorder{}
	first, err := Converge(ctx, w.admin, rec, w.team, proto.RoomIDs{})
	if err != nil {
		t.Fatalf("first Converge: %v", err)
	}
	if first.RosterRev != 1 || rec.ids != first.Rooms {
		t.Fatalf("first run: roster rev %d (want 1), recorded %+v, rooms %+v", first.RosterRev, rec.ids, first.Rooms)
	}
	if want := newTeamChanges(first.Rooms); !slices.Equal(first.Changed, want) {
		t.Fatalf("first run changed:\n%s\nwant:\n%s", strings.Join(first.Changed, "\n"), strings.Join(want, "\n"))
	}
	w.team.Roster.Rev = first.RosterRev

	again, err := Converge(ctx, w.admin, &recorder{}, w.team, first.Rooms)
	if err != nil {
		t.Fatalf("second Converge: %v", err)
	}
	if len(again.Changed) != 0 || again.RosterRev != 0 || again.Rooms != first.Rooms {
		t.Fatalf("second run with nothing changed = %+v", again)
	}

	w.trusted(t, first.Rooms)
}
