//go:build tuwunel

package apply

// Apply against a real Tuwunel (AP12's tagged half, through `matrix/tuwunelhs`): `go test -tags
// tuwunel` with FAKEHS_TUWUNEL_BIN naming the binary. A declaration is approved and applied to a
// new team, a second run changes nothing, the members join the file and the wanted coordinator
// is promoted, proto's checks accept the roster event as the server holds it, and a last run
// changes nothing.

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/tuwunelhs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const tuwunelTestLimit = 2 * time.Minute

// realAdmin registers the server's admin account through the registration key and makes the
// two members' accounts; the humans' accounts are apply's to make.
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
	a, err := anon.WithToken(sess.AccessToken.Reveal())
	if err != nil {
		t.Fatalf("WithToken: %v", err)
	}
	for _, h := range []string{coord, other} {
		if _, err := a.CreateAccount(ctx, user(h), tuwunelhs.RandomKey(t), ""); err != nil {
			t.Fatalf("CreateAccount %s: %v", h, err)
		}
	}
	return a
}

// unchanged runs apply again and fails on any change, in its lines or in the roster file.
func (w *world) unchanged(t *testing.T, in declare.Input, label string) {
	t.Helper()
	seq := w.file(t).roster.Seq
	if again := w.run(t, in); len(again.Changed) != 0 || w.file(t).roster.Seq != seq {
		t.Fatalf("%s: changed %v, roster seq %d -> %d", label, again.Changed, seq, w.file(t).roster.Seq)
	}
}

func TestTuwunelApply(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), tuwunelTestLimit)
	defer cancel()
	hs := tuwunelhs.Start(ctx, t, server)
	w := &world{admin: realAdmin(ctx, t, hs), approved: approvals{}, dir: t.TempDir()}
	in := input(t, teamDoc())
	w.approve(t, in)

	first := w.run(t, in)
	if !slices.Equal(first.Changed[:2], []string{"human alice created", "human bob created"}) ||
		!slices.Contains(first.Changed, "room main state teamchat.roster") {
		t.Fatalf("first run changed %v", first.Changed)
	}
	s := w.file(t)
	if s.rooms != first.Rooms || s.rooms.Questions == "" || s.roster.Rev != 1 || s.roster.CoordinatorWanted != coord {
		t.Fatalf("files after the first run %+v %+v", s.roster, s.rooms)
	}
	w.plainAccounts(t, "alice", "bob")
	w.unchanged(t, in, "the second run")

	w.addMember(t, coord, active())
	w.addMember(t, other, active())
	promoted := w.run(t, in)
	if !slices.Equal(promoted.Roles, []RoleChange{{coord, RolePromoted}}) {
		t.Fatalf("promotion: changed %v, roles %v", promoted.Changed, promoted.Roles)
	}
	s = w.file(t)
	r := w.roster(t, s.rooms, user(other))
	if r.Members[user(coord)].Role != proto.RoleCoordinator || r.Members[user(other)].Role != proto.RoleWorker ||
		r.Rev != s.roster.Rev || !slices.Equal(r.Humans, []string{user("alice"), user("bob")}) {
		t.Fatalf("roster event as the server holds it %+v", r)
	}
	w.unchanged(t, in, "the run after the promotion")
}
