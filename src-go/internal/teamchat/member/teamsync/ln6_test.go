package teamsync

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// LN6: Verify re-fetches an event and judges whose it is, never whom it addresses.
func TestVerify(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	hid := w.say(alice, human("not for me"))
	got, err := tm.Verify(t.Context(), hid)
	check(t, err)
	sum := sha256.Sum256([]byte("not for me"))
	if got.Item != records.KindHuman || got.From != user(alice) || got.Room != proto.PurposeMain || got.Text != "not for me" ||
		got.BodySHA256 != hex.EncodeToString(sum[:]) || got.ID != hid || got.Team != w.team || got.SentMS == 0 {
		t.Errorf("verified = %+v", got)
	}
	pid := w.agentText(peer, "to coord", user(coord))
	if got, err := tm.Verify(t.Context(), pid); err != nil || got.Item != records.KindPeer || got.Text != "to coord" {
		t.Errorf("peer = %+v, %v", got, err)
	}
	if _, err := tm.Verify(t.Context(), "$"+"nosucheventanywhere_aaaaaaaaaaaaaaaaaaaaaa"); !isCode(err, exits.CodeNotFound) {
		t.Errorf("a missing event: %v", err)
	}
	emote := w.say(alice, map[string]any{"msgtype": "m.emote", "body": "waves"})
	if _, err := tm.Verify(t.Context(), emote); !isCode(err, exits.CodeUntrusted) {
		t.Errorf("an emote: %v", err)
	}
}

// LN6: History reads main backwards and gives it oldest first, in and out.
func TestHistory(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	w.say(alice, human("one"))
	w.agentText(me, "two", user(coord))
	got, err := tm.History(t.Context(), proto.PurposeMain, 2)
	check(t, err)
	if len(got) != 2 {
		t.Fatalf("history = %+v", got)
	}
	if h, ok := got[0].Item.(records.Human); !ok || got[0].Dir != "in" || h.Text != "one" || h.FromID != user(alice) {
		t.Errorf("first = %+v", got[0])
	}
	if p, ok := got[1].Item.(records.Peer); !ok || got[1].Dir != "out" || p.Text != "two" {
		t.Errorf("second = %+v", got[1])
	}
}

func isCode(err error, code string) bool {
	e, ok := errors.AsType[*exits.Error](err)
	return ok && e.Code == code
}
