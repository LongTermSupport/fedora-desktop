//go:build tuwunel

package wiring

import (
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/tuwunelhs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The member end to end over a real Tuwunel (SPEC M44): the same processes as over fakehs.

// tuwunelServer is a Tuwunel FAKEHS_TUWUNEL_BIN names, its admin registered and the world's
// accounts made.
func tuwunelServer(t *testing.T) homeserver {
	t.Helper()
	ctx := t.Context()
	hs := tuwunelhs.Start(ctx, t, server)
	anon, err := matrix.NewAdmin(matrix.Config{BaseURL: hs.Base, Clock: clock.System()})
	check(t, err)
	t.Cleanup(anon.Close)
	sess, err := anon.Register(ctx, hs.RegKey, matrix.Registration{Username: proto.AdminLocalpart, Password: tuwunelhs.RandomKey(t), Admin: true})
	check(t, err)
	adm, err := anon.WithToken(sess.AccessToken.Reveal())
	check(t, err)
	for _, local := range accounts {
		if _, err := adm.CreateAccount(ctx, user(local), tuwunelhs.RandomKey(t), ""); err != nil {
			t.Fatalf("CreateAccount %s: %v", local, err)
		}
	}
	mint := func(id string) (string, error) {
		tok, err := adm.MintToken(ctx, id)
		return tok.Reveal(), err
	}
	return homeserver{base: hs.Base, admin: adm, mint: mint}
}

func TestTuwunelE2EItems(t *testing.T) {
	w := newWorld(t, tuwunelServer(t))
	s := w.seated()
	human, fromPeer := w.say("hello"), w.peerText("hi from peer")
	s.recvUntil(human, fromPeer)
	if recs := s.records("recv", "--team", teamName); len(recs) != 0 {
		t.Fatalf("a second recv: %v", recs)
	}
}

func TestTuwunelE2EHotAddRemove(t *testing.T) { hotAddRemove(t, tuwunelServer(t)) }

func TestTuwunelE2EKeeperTerm(t *testing.T) {
	w := newWorld(t, tuwunelServer(t))
	s := w.seated()
	kill(t, s.keeper.Pid(), syscall.SIGTERM)
	s.wait()
	w.awaitNotice("left other")
}
