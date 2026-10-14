//go:build !ccyteamstest

package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/config"
)

// A play runs under Ansible with the user's sudo, so a path the environment supplies must not
// be run unless it is the checkout's own play or the environment is deliberately trusted.
func TestAPlayOutsideTheCheckoutIsRefusedAndNotRun(t *testing.T) {
	for _, play := range []string{
		"/tmp/evil-play", "/rootkit/play.yml", "relative/play.yml", "/root/../tmp/evil", "",
	} {
		r := newOpsRig(t)
		r.ops.Cfg.Play = play
		r.ops.Cfg.ElementPlay = play
		if err := r.ops.runPlay(context.Background(), []string{"a"}); err == nil {
			t.Errorf("%q: runPlay accepted a play outside the checkout", play)
		}
		if err := r.ops.RunElementPlay(context.Background(), []string{"a"}); err == nil {
			t.Errorf("%q: runElementPlay accepted a play outside the checkout", play)
		}
		if len(r.fake.Calls()) != 0 {
			t.Errorf("%q: ran %v", play, r.fake.Calls())
		}
	}
}

func TestARefusedPlayNamesItselfAndHowToTrustIt(t *testing.T) {
	r := newOpsRig(t)
	r.ops.Cfg.Play = "/tmp/evil-play"
	if err := r.ops.runPlay(context.Background(), []string{"a"}); err == nil || !strings.Contains(err.Error(), "/tmp/evil-play") {
		t.Fatalf("got %v", err)
	}
}

func TestThePlayInsideTheCheckoutRuns(t *testing.T) {
	r := newOpsRig(t)
	r.on(opsPlay, execx.Result{})
	if err := r.ops.runPlay(context.Background(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
}

// The untagged (production) build has no switch, in the environment or elsewhere, that lets a
// play run from outside the checkout.
func TestTheUntaggedBuildDoesNotTrustPlays(t *testing.T) {
	if trustPlays {
		t.Fatal("trustPlays is true without the ccyteamstest tag")
	}
	r := newOpsRig(t)
	r.ops.Cfg = config.Load("/root", func(k string) string {
		return map[string]string{
			"HOME": "/h", "CCY_TEAMS_PLAY": "/tmp/stub-play", "CCY_TEAMS_TRUST_PLAYS": "1",
		}[k]
	})
	if err := r.ops.runPlay(context.Background(), []string{"a"}); err == nil || len(r.fake.Calls()) != 0 {
		t.Fatalf("got %v, ran %v", err, r.fake.Calls())
	}
}
