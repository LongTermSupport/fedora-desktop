//go:build tuwunel

package loadgen

// The generator against a real Tuwunel (`go test -tags tuwunel` with FAKEHS_TUWUNEL_BIN naming
// the binary): the setup on a fresh server, a unit of every pattern, and the probe's read of
// what they made.

import (
	"context"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/tuwunelhs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/traffic"
)

const tuwunelTestLimit = 3 * time.Minute

func TestTuwunelEveryPattern(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), tuwunelTestLimit)
	defer cancel()
	hs := tuwunelhs.Start(ctx, t, labName)
	wall := clock.System()
	since := wall.WallMS() - traffic.HourMS
	team, err := Setup(ctx, SetupOptions{Base: hs.Base, ServerName: labName, RegKey: hs.RegKey, Agents: 3, Clock: wall})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if _, err := Setup(ctx, SetupOptions{Base: hs.Base, ServerName: labName, RegKey: hs.RegKey, Agents: 3, Clock: wall}); err == nil {
		t.Fatal("a second setup on the same server passed")
	}
	g, err := NewGenerator(ctx, team, wall, Options{})
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	defer g.Close()
	for _, p := range Patterns {
		if err := g.Unit(ctx, p); err != nil {
			t.Fatalf("Unit %s: %v", p, err)
		}
	}
	w := traffic.Window{SinceMS: since, UntilMS: wall.WallMS() + traffic.HourMS}
	got := map[string]int64{}
	for _, r := range probe(t, team, w).Rows {
		if r.Type == proto.MessageType {
			got[r.Room+" "+dash(r.Typed)+" "+dash(r.Rel)] += r.Count
		}
	}
	want := map[string]int64{
		"main teamchat.ping -": 1, "main teamchat.ping thread": 1, "main teamchat.notice -": 1, "main - -": 1, "main teamchat.text -": 1,
		"reports teamchat.report -": 1, "reports teamchat.receipt -": 1,
		"questions teamchat.question -": 1, "questions - thread": 1, "questions teamchat.thread thread": 2, "questions - edit": 1,
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("%s: %d, want %d (all: %v)", k, got[k], n, got)
		}
	}
	presence, known, err := member(t, team, team.Human.Token).Presence(ctx, team.Agents[0].UserID)
	if err != nil || !known || presence != presenceOnline {
		t.Errorf("the flapped agent's presence %q (known %v, %v), want online", presence, known, err)
	}
}
